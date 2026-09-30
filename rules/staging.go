package rules

import (
	"fmt"
	"sort"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Staging (NewStaged) builds a hypothetical engine at an arbitrary point of a
// two-player game from a rules-level description (Stage), for research
// harnesses that start from a reconstructed position (internal/searchbench).
// It is deliberately spec-agnostic: no JSON, no seat letters, no aliases.
//
// Contract:
//
//   - Every placement is an event folded by events.Apply and appended to the
//     log. The object arena itself is created the way genesis creates it
//     (one AddObject per owned card, in the owner's library), which is the
//     Config-derived part of any engine; tokens are minted by TokenCreate.
//   - Staging events are emitted RAW (events.Emit), never through
//     Engine.emit: no triggered ability is queued, no replacement effect or
//     ETB effect runs (an "as it enters" choice is simply not made), no
//     entry counter is placed by the engine's entry path, and no rules-side
//     "this turn" ledger records them. A planeswalker still gets its printed
//     loyalty because the stager places the entry counters itself (Stage
//     counters SET a kind; unlisted kinds keep what the card enters with).
//   - A permanent is summoning sick exactly when Stage says so, and staging
//     leaves no "entered this turn" history: every zone entry happens before
//     the last TurnChange, which clears Object.EnteredThisTurn and
//     Game.Entered. Sickness is settled by who controls each permanent at
//     the two last TurnChanges (turn-1's player, then the active player's
//     turn), because a TurnChange clears the sickness of its own player's
//     permanents only and a ControlChange to another seat sets it without a
//     zone entry. So a sick permanent of the active player is parked under
//     the other seat across the last TurnChange and handed back after it,
//     and a sick permanent of the other seat is parked under the active
//     player across turn-1's TurnChange. Parking moves a permanent to the
//     end of its controller's battlefield list: the one ordering artefact,
//     and the log shows the two control changes.
//   - The log carries a TurnChange for every turn 1..Turn, so turn-count
//     heads read a history of turns (with nothing done in them).
//   - Library order is exactly Stage's; later shuffles draw from a fresh
//     chance-recording generator seeded by Config.Seed, so the engine is a
//     hypothetical one (SubmitHypothetical, ChanceTranscript). Like
//     CloneHypothetical's copies it is not replayable from Config alone.
//   - Same Config and Stage give the same event stream and chain head.
//
// Stage has no stack, no declared blockers and no face-down permanents (the
// search benchmark's items need none). NewStaged refuses more or fewer than
// two players, Config decks, mulligans or a non-constructed format, the
// untap and combat-damage steps, a cleanup without StageBeginStep, a turn
// that is not the active seat's, and any Stage field that names nothing.

// StageEnter is how the staged step is entered.
type StageEnter uint8

const (
	// StagePriorityFresh resumes inside the step's priority window, skipping
	// its turn-based actions; the active player gets priority.
	StagePriorityFresh StageEnter = iota
	// StageBeginStep runs the step from its start through the engine's own
	// step entry (setStep + finishEnteredStep): its beginning-of-step
	// triggers and turn-based actions happen. That is the game running,
	// not staging.
	StageBeginStep
	// StagePriorityHeld resumes mid priority round: Stage.PriorityPlayer
	// holds priority and Stage.Passed players have already passed.
	StagePriorityHeld
)

// StagedCounter sets one counter kind on a staged permanent.
type StagedCounter struct {
	Kind string
	N    int32
}

// StagedPermanent is one battlefield permanent.
type StagedPermanent struct {
	// Card is the printed card; nil for a token.
	Card *cards.Card
	// Token is the Config.Tokens key of a token (Card nil).
	Token string
	// Owner and Controller. A token is owned by its controller.
	Owner, Controller state.PlayerID
	Tapped, Sick      bool
	Damage            int32
	// Counters SET each listed kind (sorted by Kind before placement).
	Counters []StagedCounter
	// AttachTo is 1 + the index into Stage.Permanents of the host, 0 none.
	AttachTo int
}

// StagedPlayer is one seat's non-battlefield zones and counters.
type StagedPlayer struct {
	Life int32
	// Library, top card first.
	Library []*cards.Card
	// Hand in order; Graveyard bottom to top; Exile in order.
	Hand, Graveyard, Exile []*cards.Card
	LandsPlayed            int
	// ManaPool is floating mana as W/U/B/R/G/C letters.
	ManaPool string
}

// StagedAttack declares one attacker (an index into Stage.Permanents)
// against a player.
type StagedAttack struct {
	Attacker int
	Defender state.PlayerID
}

// Stage is a two-player position.
type Stage struct {
	Turn             int32
	Active, Starting state.PlayerID
	Step             state.Step
	Enter            StageEnter
	// PriorityPlayer and Passed are read for StagePriorityHeld only.
	PriorityPlayer state.PlayerID
	Passed         int
	Players        []StagedPlayer
	Permanents     []StagedPermanent
	// Attackers, for a step at or after declare attackers entered with
	// priority (the declaration has been made).
	Attackers []StagedAttack
}

// StagedObjects names the objects staging created, in Stage order.
type StagedObjects struct {
	Permanents                      []state.ObjID
	Library, Hand, Graveyard, Exile [][]state.ObjID
}

// NewStaged builds the staged engine. cfg supplies Names (exactly two),
// Seed, Tokens and the other engine options; its Decks must be empty (each
// seat's deck manifest is built from the cards Stage places for it). The
// returned engine has no pending decision: Advance it.
func NewStaged(cfg Config, st Stage) (*Engine, StagedObjects, error) {
	var ids StagedObjects
	if err := validateStage(cfg, st); err != nil {
		return nil, ids, err
	}
	// Each seat's owned cards, in the order the arena is built: library
	// (top first), hand, graveyard, exile, then its owned battlefield cards
	// in Stage order.
	decks := make([][]*cards.Card, 2)
	for p := range st.Players {
		sp := st.Players[p]
		for _, zone := range [][]*cards.Card{sp.Library, sp.Hand, sp.Graveyard, sp.Exile} {
			decks[p] = append(decks[p], zone...)
		}
	}
	for _, pm := range st.Permanents {
		if pm.Card != nil {
			decks[pm.Owner] = append(decks[pm.Owner], pm.Card)
		}
	}
	cfg.Decks = decks
	random := newRNG(cfg.Seed)
	random.chance = &chanceState{}
	e := newEngineShell(cfg, random)
	raw := func(ev events.Event) events.Event { return events.Emit(e.G, e.L, ev) }
	raw(events.Event{Kind: events.GameStart, Amount: 2})

	// The arena: genesis's own shape (AddObject into the owner's library).
	perm := make([]state.ObjID, len(st.Permanents))
	ids.Library = make([][]state.ObjID, 2)
	ids.Hand = make([][]state.ObjID, 2)
	ids.Graveyard = make([][]state.ObjID, 2)
	ids.Exile = make([][]state.ObjID, 2)
	for p := range st.Players {
		sp := st.Players[p]
		pid := state.PlayerID(p)
		add := func(cs []*cards.Card) []state.ObjID {
			out := make([]state.ObjID, 0, len(cs))
			for _, c := range cs {
				out = append(out, e.G.AddObject(c, pid).ID)
			}
			return out
		}
		ids.Library[p] = add(sp.Library)
		ids.Hand[p] = add(sp.Hand)
		ids.Graveyard[p] = add(sp.Graveyard)
		ids.Exile[p] = add(sp.Exile)
		for i, pm := range st.Permanents {
			if pm.Card != nil && pm.Owner == pid {
				perm[i] = e.G.AddObject(pm.Card, pid).ID
			}
		}
		var lib []state.ObjID
		lib = append(lib, ids.Library[p]...)
		lib = append(lib, ids.Hand[p]...)
		lib = append(lib, ids.Graveyard[p]...)
		lib = append(lib, ids.Exile[p]...)
		for i, pm := range st.Permanents {
			if pm.Card != nil && pm.Owner == pid {
				lib = append(lib, perm[i])
			}
		}
		e.G.SetZone(state.ZLibrary, pid, lib)
	}
	raw(events.Event{Kind: events.StartingPlayerChange, Player: st.Starting})

	// Hidden and public non-battlefield zones.
	for p := range st.Players {
		for _, z := range []struct {
			ids []state.ObjID
			to  state.Zone
		}{{ids.Hand[p], state.ZHand}, {ids.Graveyard[p], state.ZGraveyard}, {ids.Exile[p], state.ZExile}} {
			for _, id := range z.ids {
				raw(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: z.to})
			}
		}
	}

	// The battlefield. Sickness is settled by who controls each permanent
	// at the two final TurnChanges (see the type comment): prev is the seat
	// whose turn Turn-1 was.
	active := st.Active
	prev := 1 - active
	parkedActive := []int{} // active's sick permanents, handed back after TurnChange(Turn)
	parkedPrev := []int{}   // prev's sick permanents, handed back after TurnChange(Turn-1)
	for i, pm := range st.Permanents {
		if pm.Card == nil {
			id := e.G.NextID
			raw(events.Event{Kind: events.TokenCreate, Player: pm.Controller, Text: pm.Token})
			if e.G.Obj(id) == nil {
				return nil, ids, fmt.Errorf("stage: permanent %d: token %q did not mint", i, pm.Token)
			}
			perm[i] = id
		} else {
			raw(events.Event{Kind: events.MoveZone, Obj: perm[i], From: state.ZLibrary, To: state.ZBattlefield})
		}
		// The controller during the final TurnChanges.
		hold := pm.Controller
		switch {
		case pm.Sick && pm.Controller == active:
			hold = prev
			parkedActive = append(parkedActive, i)
		case pm.Sick && pm.Controller == prev && st.Turn >= 2:
			hold = active
			parkedPrev = append(parkedPrev, i)
		}
		if hold != e.G.Obj(perm[i]).Controller {
			raw(events.Event{Kind: events.ControlChange, Obj: perm[i], Player: hold})
		}
	}
	for i, pm := range st.Permanents {
		id := perm[i]
		o := e.G.Obj(id)
		want := map[string]int32{} // lookup only; placement order is sorted below
		for _, g := range events.EntryCounterGrants(o, false) {
			want[g.Kind] += g.Amount
		}
		for _, c := range pm.Counters {
			want[c.Kind] = c.N
		}
		kinds := make([]string, 0, len(want))
		for k := range want {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		for _, k := range kinds {
			if d := want[k] - o.Counter(k); d != 0 {
				raw(events.Event{Kind: events.CounterChange, Obj: id, Counter: k, Amount: d})
			}
		}
		if pm.Damage > 0 {
			raw(events.Event{Kind: events.Damage, Obj: id, Amount: pm.Damage})
		}
		if pm.Tapped && !stagedAttacker(st, i) {
			raw(events.Event{Kind: events.Tap, Obj: id})
		}
	}
	for i, pm := range st.Permanents {
		if pm.AttachTo > 0 {
			raw(events.Event{Kind: events.Attach, Obj: perm[i], IDs: []state.ObjID{perm[pm.AttachTo-1]}})
		}
	}
	for p := range st.Players {
		if d := st.Players[p].Life - e.G.Players[p].Life; d != 0 {
			raw(events.Event{Kind: events.LifeChange, Player: state.PlayerID(p), Amount: d})
		}
	}
	// Library order: the zone slice is top first (a draw takes index 0).
	for p := range st.Players {
		raw(events.Event{Kind: events.LibraryOrder, Player: state.PlayerID(p), IDs: ids.Library[p], Secret: true})
	}

	// Turn history, then the two turn boundaries that settle sickness.
	for t := int32(1); t <= st.Turn; t++ {
		owner := st.Starting
		if t%2 == 0 {
			owner = 1 - st.Starting
		}
		raw(events.Event{Kind: events.TurnChange, Player: owner, Amount: t})
		if t == st.Turn-1 {
			for _, i := range parkedPrev {
				raw(events.Event{Kind: events.ControlChange, Obj: perm[i], Player: prev})
			}
		}
	}
	for _, i := range parkedActive {
		raw(events.Event{Kind: events.ControlChange, Obj: perm[i], Player: active})
	}
	for p := range st.Players {
		for n := 0; n < st.Players[p].LandsPlayed; n++ {
			raw(events.Event{Kind: events.LandPlayed, Player: state.PlayerID(p)})
		}
	}

	// The step. A step after the attacker declaration carries its
	// declaration in the declare-attackers step (the engine reads "was a
	// declaration made this step" off the log back to the last StepChange);
	// entering declare attackers or declare blockers with priority means the
	// declaration was made, so an empty one is logged as the marker the
	// engine's own no-attack path logs.
	if e.setNameInPool {
		e.refreshRenames()
	}
	if e.layer4InPool {
		e.refreshDerivedTypes()
	}
	if st.Step > state.StepBeginCombat && st.Step <= state.StepEndCombat {
		raw(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	}
	declared := st.Step > state.StepDeclareAttackers ||
		(st.Step == state.StepDeclareAttackers && st.Enter != StageBeginStep)
	if declared && st.Step <= state.StepEndCombat {
		raw(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
		var byDef [2][]state.ObjID
		for _, a := range st.Attackers {
			byDef[a.Defender] = append(byDef[a.Defender], perm[a.Attacker])
		}
		declaredAny := false
		for p, atk := range byDef {
			if len(atk) > 0 {
				declaredAny = true
				raw(events.Event{Kind: events.DeclareAttackers, Player: state.PlayerID(p), IDs: atk})
			}
		}
		if !declaredAny {
			raw(events.Event{Kind: events.DeclareAttackers, Player: 1 - st.Active})
		}
		for _, a := range st.Attackers {
			if !e.hasKeywordH(perm[a.Attacker], kwhVigilance) {
				raw(events.Event{Kind: events.Tap, Obj: perm[a.Attacker]})
			}
		}
	}
	ids.Permanents = perm
	if st.Enter == StageBeginStep {
		if st.Step != state.StepDeclareAttackers || !declared {
			prior := e.pending
			e.setStep(st.Step)
			if e.pending == nil || e.pending == prior {
				e.finishEnteredStep()
			}
		}
		return e, ids, nil
	}
	if st.Step != state.StepDeclareAttackers || !declared {
		raw(events.Event{Kind: events.StepChange, Step: st.Step})
	}
	if st.Step == state.StepDeclareBlockers {
		raw(events.Event{Kind: events.DeclareBlockers, Player: 1 - st.Active})
	}
	for p := range st.Players {
		for _, r := range st.Players[p].ManaPool {
			raw(events.Event{Kind: events.ManaAdd, Player: state.PlayerID(p), Counter: string(r), Amount: 1})
		}
	}
	holder, passes := st.Active, int32(0)
	if st.Enter == StagePriorityHeld {
		holder, passes = st.PriorityPlayer, int32(st.Passed)
	}
	raw(events.Event{Kind: events.Priority, Player: holder, Amount: passes})
	return e, ids, nil
}

func stagedAttacker(st Stage, i int) bool {
	for _, a := range st.Attackers {
		if a.Attacker == i {
			return true
		}
	}
	return false
}

func validateStage(cfg Config, st Stage) error {
	switch {
	case len(cfg.Names) != 2 || len(st.Players) != 2:
		return fmt.Errorf("stage: exactly two players (names %d, players %d)", len(cfg.Names), len(st.Players))
	case len(cfg.Decks) > 0 || len(cfg.Sideboards) > 0 || len(cfg.PlanarDecks) > 0 || len(cfg.Commanders) > 0:
		return fmt.Errorf("stage: Config decks are built from the Stage; pass none")
	case cfg.Mulligans > 0:
		return fmt.Errorf("stage: a staged game has no pregame")
	case cfg.Format != FormatConstructed:
		return fmt.Errorf("stage: only the constructed format is supported")
	case st.Turn < 1:
		return fmt.Errorf("stage: turn %d", st.Turn)
	case st.Active > 1 || st.Starting > 1:
		return fmt.Errorf("stage: active %d / starting %d", st.Active, st.Starting)
	case (st.Turn%2 == 1) != (st.Active == st.Starting):
		return fmt.Errorf("stage: turn %d does not belong to seat %d (starting seat %d)", st.Turn, st.Active, st.Starting)
	case !st.Step.Valid() || st.Step == state.StepUntap:
		return fmt.Errorf("stage: step %v cannot be entered (use the previous turn's end step)", st.Step)
	case st.Step == state.StepCombatDamage:
		return fmt.Errorf("stage: the combat damage step cannot be entered")
	case st.Step == state.StepCleanup && st.Enter != StageBeginStep:
		return fmt.Errorf("stage: cleanup has no priority window")
	case st.Enter > StagePriorityHeld:
		return fmt.Errorf("stage: enter mode %d", st.Enter)
	case st.Enter == StagePriorityHeld && (st.PriorityPlayer > 1 || st.Passed < 0 || st.Passed > 1):
		return fmt.Errorf("stage: priority held by %d with %d passes", st.PriorityPlayer, st.Passed)
	}
	for p, sp := range st.Players {
		if sp.LandsPlayed < 0 {
			return fmt.Errorf("stage: player %d lands played %d", p, sp.LandsPlayed)
		}
		for _, r := range sp.ManaPool {
			switch r {
			case 'W', 'U', 'B', 'R', 'G', 'C':
			default:
				return fmt.Errorf("stage: player %d mana pool %q", p, sp.ManaPool)
			}
		}
		for _, zone := range [][]*cards.Card{sp.Library, sp.Hand, sp.Graveyard, sp.Exile} {
			for _, c := range zone {
				if c == nil || len(c.Faces) == 0 {
					return fmt.Errorf("stage: player %d has a nil card", p)
				}
			}
		}
	}
	for i, pm := range st.Permanents {
		switch {
		case pm.Owner > 1 || pm.Controller > 1:
			return fmt.Errorf("stage: permanent %d owner %d controller %d", i, pm.Owner, pm.Controller)
		case pm.Card == nil && pm.Token == "":
			return fmt.Errorf("stage: permanent %d names no card or token", i)
		case pm.Card != nil && len(pm.Card.Faces) == 0:
			return fmt.Errorf("stage: permanent %d card has no face", i)
		case pm.Card == nil && cfg.Tokens[pm.Token] == nil:
			return fmt.Errorf("stage: permanent %d token %q is not in Config.Tokens", i, pm.Token)
		case pm.Card == nil && pm.Owner != pm.Controller:
			return fmt.Errorf("stage: permanent %d: a token is owned by its controller", i)
		case pm.Damage < 0:
			return fmt.Errorf("stage: permanent %d damage %d", i, pm.Damage)
		case pm.AttachTo < 0 || pm.AttachTo > len(st.Permanents) || pm.AttachTo == i+1:
			return fmt.Errorf("stage: permanent %d attaches to %d", i, pm.AttachTo)
		case st.Turn == 1 && pm.Controller != st.Active && !pm.Sick:
			return fmt.Errorf("stage: permanent %d: on turn 1 the other seat's permanents are all sick", i)
		}
		for _, c := range pm.Counters {
			if c.N < 0 || c.Kind == "" {
				return fmt.Errorf("stage: permanent %d counter %q=%d", i, c.Kind, c.N)
			}
		}
	}
	if len(st.Attackers) > 0 {
		if st.Step < state.StepDeclareAttackers || st.Step > state.StepEndCombat ||
			(st.Step == state.StepDeclareAttackers && st.Enter == StageBeginStep) {
			return fmt.Errorf("stage: attackers need a step after their declaration")
		}
		seen := map[int]bool{} // lookup only
		for _, a := range st.Attackers {
			switch {
			case a.Attacker < 0 || a.Attacker >= len(st.Permanents):
				return fmt.Errorf("stage: attacker index %d", a.Attacker)
			case st.Permanents[a.Attacker].Controller != st.Active:
				return fmt.Errorf("stage: attacker %d is not the active player's", a.Attacker)
			case a.Defender == st.Active || a.Defender > 1:
				return fmt.Errorf("stage: attacker %d defends %d", a.Attacker, a.Defender)
			case seen[a.Attacker]:
				return fmt.Errorf("stage: attacker %d declared twice", a.Attacker)
			}
			seen[a.Attacker] = true
		}
	}
	return nil
}
