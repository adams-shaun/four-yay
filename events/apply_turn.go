// Event folds for turn and phase structure: extra turns and phases, step and turn changes, turn skips.
//
// Split out of events/apply.go: code moved verbatim, no behaviour
// change. Each fold function is the body of the matching case in
// Apply (g, e) switch; see apply.go for the dispatch.
package events

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// foldEndTurn folds Kind EndTurn into state.
func foldEndTurn(g *state.Game, e Event) {
	// CR 723.1a/c: all spells and abilities on the stack cease to exist,
	// and every creature/planeswalker is removed from combat. IDs is a
	// snapshot of the stack taken by the effect before this fold.
	for _, id := range e.IDs {
		if o := g.Obj(id); o != nil && o.Zone == state.ZStack {
			Move(g, id, state.ZStack, state.ZExile)
		}
	}
	// CR 723.1c's removal from combat is exactly CR 511.3's whole-combat
	// reset, so it shares EndCombatReset's own helper rather than
	// restating the field clears.
	resetCombat(g, 0)
}

// foldSkipTurn folds Kind SkipTurn into state.
func foldSkipTurn(g *state.Game, e Event) {
	if validPlayer(g, e.Player) && e.Amount != 0 {
		if g.SkipTurns == nil {
			g.SkipTurns = map[state.PlayerID]int{}
		}
		g.SkipTurns[e.Player] = max(0, g.SkipTurns[e.Player]+int(e.Amount))
	}
}

// foldStepChange folds Kind StepChange into state.
func foldStepChange(g *state.Game, e Event) {
	// "Until end of combat, you don't lose this mana as steps and phases
	// end" (CR 702.189a Firebending) expires as the combat phase ends. The
	// step being LEFT is g.Step, read before the assignment below; the
	// demotion subtracts each seat's CombatMana from its PersistentMana so
	// the very same boundary's ManaClear (rules.setStep calls
	// finishStepBoundary after emitting this event) empties the demoted
	// units with the ordinary share. Doing it here, rather than in the
	// later EndCombatReset, is what keeps the units in the pool through the
	// end-of-combat STEP (a player may still spend them there) while
	// emptying them when it ends. A game with no combat-persistent mana
	// (every game before this keyword, and every legacy deck) has all-zero
	// tallies, so this fold is inert and its event stream is unchanged.
	if g.Step == state.StepEndCombat && e.Step != state.StepEndCombat {
		for i := range g.Players {
			p := &g.Players[i]
			for j := range p.CombatMana {
				if p.CombatMana[j] == 0 {
					continue
				}
				d := p.CombatMana[j]
				if p.PersistentMana[j] < d {
					d = p.PersistentMana[j]
				}
				p.PersistentMana[j] -= d
				p.CombatMana[j] = 0
			}
		}
	}
	g.Step = e.Step
	// The per-turn combat-phase count (CR 500.6: a turn has exactly one
	// combat phase -- except the additional ones an api:AddPhase grant
	// splices in): one increment per BeginCombat ENTRY, so an extra combat
	// counts a second time and ConditionFirstCombat$ (Raiyuu's "if it's the
	// first combat phase of the turn" gate, effects/conditions.go) reads
	// the real ordinal. TurnChange resets it below.
	if e.Step.Valid() && e.Step == state.StepBeginCombat {
		g.CombatsThisTurn++
		// ChoiceRestriction$ YourLastCombat: preserve only picks from
		// each controlled permanent's immediately preceding combat, then
		// rotate its combat identity. The log is the forbidden set during
		// the combat just begun; an intervening pickless combat therefore
		// releases an older pick on the following combat.
		for i := range g.Objs {
			o := &g.Objs[i]
			if o.Zone != state.ZBattlefield || o.Controller != g.Active {
				continue
			}
			kept := o.ModeChoices[:0]
			for _, pick := range o.ModeChoices {
				if pick.Scope != state.ModeScopeYourLastCombat ||
					(pick.Turn == o.CurCombatTurn && pick.Combat == o.CurCombatCombat) {
					kept = append(kept, pick)
				}
			}
			o.ModeChoices = kept
			o.CurCombatTurn, o.CurCombatCombat = g.Turn, g.CombatsThisTurn
		}
	}
	// kw:Echo's provenance (CR 702.35a): the Draw step's beginning means
	// this turn's upkeep just ended, so the turn's upkeep is now the
	// controller's "most recent upkeep". Recording here (not at the
	// Upkeep StepChange) is what makes the gate's comparison read the
	// PREVIOUS upkeep at the next upkeep's beginning: the echo trigger
	// fires on the same StepChange that would otherwise have just
	// overwritten the record, and an acquisition made during that upkeep
	// itself still counts via its AcqStep >= StepUpkeep half. Zero stays
	// zero until a seat's first draw step (their first upkeep then reads
	// as absent — gate vacuously true).
	if e.Step == state.StepDraw && validPlayer(g, g.Active) {
		g.Players[g.Active].LastUpkeepTurn = g.Turn
	}
}

// foldTurnChange folds Kind TurnChange into state.
func foldTurnChange(g *state.Game, e Event) {
	if validPlayer(g, e.Player) {
		g.Turn = e.Amount
		g.Active = e.Player
		g.Players[e.Player].LandsPlayed = 0
		// g.Zone(ZBattlefield, e.Player) can only ever hold IDs that Move
		// already confirmed are real objects, so this nil check is
		// currently unreachable in practice -- but it is one line, it
		// matches every other zone-walk in this switch (DeclareAttackers,
		// DeclareBlockers) that guards g.Obj before dereferencing, and it
		// stops that invariant from becoming a silent, easy-to-reopen
		// panic if a future Kind ever populates a zone list some other
		// way. Found in the same audit as Ruling T20-e.
		for _, id := range g.Zone(state.ZBattlefield, e.Player) {
			if o := g.Obj(id); o != nil {
				o.SummonSick = false
			}
		}
		// TurnChange is the existing per-turn reset boundary. Zone-entry
		// provenance and damage history are object facts rather than facts
		// of the incoming active player, so reset every arena object here.
		for i := range g.Objs {
			g.Objs[i].EnteredThisTurn = false
			g.Objs[i].WasDealtDamageThisTurn = false
			g.Objs[i].DamageReceivedThisTurn = 0
			g.Objs[i].DamageTakenThisTurnBy = nil
			g.Objs[i].ActivatedThisTurn = 0
			g.Objs[i].AttacksThisTurn = 0
			// CR 702.100a: exerted is a per-turn fact. ExertSkipUntap is
			// deliberately NOT reset here -- its window spans the turn
			// boundary and is consumed at the next untap step instead.
			g.Objs[i].ExertedThisTurn = false
			// An untap election belongs to one controller's untap
			// step; the next turn gets a fresh election.
			g.Objs[i].UntapChoice = ""
			// CR 702.160: enlist is a per-combat fact; the stamp is cleared at
			// the turn boundary (a same-turn second combat compares its own
			// CombatsThisTurn against the stamp, so it needs no separate
			// reset).
			g.Objs[i].EnlistedTurn = 0
			g.Objs[i].EnlistedCombat = 0
			// CR 702.122: crew is a per-turn fact. The Vehicles crewed this
			// turn are dropped at the turn boundary so a creature does not
			// read as a crewer in a later turn (which a fresh CrewedTurn
			// stamp already ensures, but the slice must not leak the stale
			// ids into a much later same-numbered turn).
			g.Objs[i].CrewedVehicles = nil
			g.Objs[i].CrewedTurn = 0
			// CR 702.171b: Saddled is a per-turn designation.
			g.Objs[i].SaddledTurn = 0
			// Only default-duration goads expire at the goader's next turn.
			g.Objs[i].Goads = expireTurnGoads(g.Objs[i].Goads, e.Player)
			// ChoiceRestriction$ ThisTurn is the only per-turn scope. Keep
			// ThisGame for the battlefield stint and YourLastCombat across
			// turns; its combat-start rotation expires it at the right boundary.
			keptModes := g.Objs[i].ModeChoices[:0]
			for _, pick := range g.Objs[i].ModeChoices {
				if pick.Scope != state.ModeScopeThisTurn {
					keptModes = append(keptModes, pick)
				}
			}
			g.Objs[i].ModeChoices = keptModes
		}
		// The per-add entry list is per-turn state too.
		g.Entered = nil
		// Extra phases never survive into the next turn: whatever is still
		// queued (or mid-extra-phase) at the turn boundary is dropped here,
		// so a grant whose splice point this turn has already passed is
		// silently spent at the boundary rather than firing next turn. The
		// per-turn combat-phase count resets with them.
		g.ExtraPhases = nil
		g.CombatsThisTurn = 0
		// The per-ability resolution tally is a per-turn fact (CR 608.2m
		// counts resolutions in the turn), so it is dropped at the turn
		// boundary exactly as CombatsThisTurn is. Clearing (rather than
		// zeroing entries) keeps the map empty for the overwhelmingly
		// common game that never resolves a Count$ResolvedThisTurn carrier.
		g.ResolvedThisTurn = nil
		// Snapshot the monarch designation as the NEW turn begins, for the
		// trig:BecomeMonarch BeginTurn$ intervening-if ("if you were the
		// monarch as the turn began"). Folding it here, from state
		// MonarchChange already established, keeps the read replay-exact
		// without a new event or event field; a game with no monarch ever
		// set carries the false presence bit and the condition fails
		// closed.
		g.TurnStartMonarch, g.HasTurnStartMonarch = g.Monarch, g.HasMonarch
	}
	// PersistentMana$ True mana expires at the end of the turn it was
	// produced in (the carriers' "until end of turn" bound), whichever
	// seat holds it — so every seat's tally drops here and the units
	// become ordinary pool mana again, emptied by the next boundary's
	// ManaClear exactly like mana that was never persistent. The demotion
	// takes the persistent RESTRICTION batches with it: ManaClear keeps a
	// persistent batch unconditionally, so a batch left here after the
	// tally's zero would outlive its units as a phantom whose Amount
	// manaAvailableFor subtracts from every non-matching payment — hiding
	// the seat's real mana behind units that no longer exist. The printed
	// restriction (Klauth's "Spend this mana only to cast spells") is not
	// time-bounded — only the don't-lose clause is — so the batch is
	// DEMOTED to ordinary, not dropped: its units stay restricted until
	// the next boundary's ManaClear empties them with the batch.
	for i := range g.Players {
		g.Players[i].PersistentMana = state.Mana{}
		// The combat-persistent subset can never outlive the turn either:
		// it expires at end of combat, but a TurnChange that somehow
		// arrived first (an extra turn boundary) must not leave the tally
		// naming units the pool no longer holds.
		g.Players[i].CombatMana = state.Mana{}
		for j := range g.Players[i].RestrictedMana {
			g.Players[i].RestrictedMana[j].Persistent = false
		}
	}
}

// foldExtraTurn folds Kind ExtraTurn into state.
func foldExtraTurn(g *state.Game, e Event) {
	// One grant or consumption of an extra turn (CR 500.7). The count and
	// the ordered pending queue are game state folded here so a log-only
	// reconstruction holds the same pending extras the live game did; the
	// turn structure's own consumption is the -1 form, emitted by rules'
	// advanceStep at the exact boundary it repeats the seat instead of
	// moving on. The queue is the ORDER the rule takes them in: grants
	// append in creation order, the -1 consumption removes the seat's LAST
	// entry (most recently created first, CR 500.7), so the total counts and
	// the queue agree by construction.
	if validPlayer(g, e.Player) && e.Amount != 0 {
		if g.ExtraTurns == nil {
			g.ExtraTurns = map[state.PlayerID]int{}
		}
		amount := min(e.Amount, maxQueuedGrants)
		g.ExtraTurns[e.Player] += int(amount)
		if g.ExtraTurns[e.Player] < 0 {
			g.ExtraTurns[e.Player] = 0
		}
		if e.Amount > 0 {
			// Amount is a number of distinct grants, not merely the
			// aggregate counter. Keep one queue entry per granted turn so
			// NumTurns$ 2 (Time Stretch) is consumed twice. Text is an
			// already encoded Event field; this canonical marker carries the
			// Forge SkipUntap$ rider without changing Event's hash-chain
			// schema.
			skipUntap := e.Text == ExtraTurnSkipUntapText
			for n := int32(0); n < amount; n++ {
				g.ExtraTurnQueue = append(g.ExtraTurnQueue, state.ExtraTurn{Player: e.Player, SkipUntap: skipUntap})
			}
		} else {
			for i := len(g.ExtraTurnQueue) - 1; i >= 0; i-- {
				if g.ExtraTurnQueue[i].Player == e.Player {
					g.ExtraTurnQueue = append(g.ExtraTurnQueue[:i], g.ExtraTurnQueue[i+1:]...)
					break
				}
			}
		}
	}
	// Forge's ExtraTurnDelayedTrigger$ (Final Fortune: "At the beginning
	// of that turn's end step, you lose the game") registers the delayed
	// trigger HERE, at CONSUMPTION time, with the consumed turn's number
	// as its MinTurn -- so the ordinary Mode$ Phase delayed firing skips
	// the granting turn's own end step and fires exactly once, in the
	// granted turn. Registration must ride the consumption, not the grant:
	// with several extra turns pending (CR 500.7 takes them most recently
	// created first) the turn a grant PRODUCES is not known at grant time
	// -- it is exactly the turn about to begin when the -1 fires. Only the
	// -1 form registers; the +grant carries no Counter at consumption. A
	// consumption with no source object, no Execute$ name, or a source
	// whose face lacks the SVar degrades to no registration rather than
	// panicking (the same totality stance DelayedRegister applies).
	if e.Amount < 0 && e.Counter != "" && e.Obj != 0 && g.Obj(e.Obj) != nil {
		f := g.Obj(e.Obj).Face()
		if f != nil && cards.ResolveSVar(f.SVars, e.Counter) != nil {
			// The registered trigger's phase: IDs[0] carries the state.Step
			// the granting DelayedTrigger SA's Phase$ named (effAddTurn
			// parsed it through the shared parser and the consumption
			// forwarded it). An old log's consumption — or a forwarder
			// that degraded — carries no IDs, and the end step Final
			// Fortune's body names is the fallback every earlier
			// registration used. An out-of-range ordinal degrades too.
			phase := state.StepEnd
			if len(e.IDs) > 0 {
				if p := state.Step(e.IDs[0]); p.Valid() {
					phase = p
				}
			}
			g.Delayed = append(g.Delayed, state.DelayedTrigger{
				ID:         g.DelayedNext,
				Phase:      phase,
				Source:     e.Obj,
				Controller: e.Player,
				Execute:    e.Counter,
				MinTurn:    g.Turn + 1,
			})
			g.DelayedNext++
		}
	}
}

// foldExtraPhase folds Kind ExtraPhase into state.
func foldExtraPhase(g *state.Game, e Event) {
	// One Forge AddPhaseEffect message (DB$ AddPhase). Three forms split
	// on Amount (the ExtraTurn precedent one level up): +1 appends one
	// queue entry per granted phase (NumPhases$ is the emitter's count),
	// -1 marks a grant consumed (the turn structure entered its extra
	// phase), -2 removes a consumed grant (the extra phase completed).
	// A malformed grant -- no IDs, an invalid step ordinal, an invalid
	// player -- is a no-op, the same totality stance as the ExtraTurn
	// case. Totality for consume/complete: an identity that matches no
	// queue entry is a no-op, never a panic.
	switch {
	case e.Amount > 0:
		if !validPlayer(g, e.Player) || len(e.IDs) == 0 {
			break
		}
		entry := state.Step(e.IDs[0])
		if !entry.Valid() {
			break
		}
		riders := DecodeExtraPhaseRiders(e.Text)
		// The extra phase's range: Entry's own by default, overridden by a
		// multi-step ExtraPhase$ grant's RANGEEND rider (a whole named
		// phase; only taken when it does not walk BACKWARD past the entry).
		rangeEnd := state.ExtraPhaseRangeEnd(entry)
		if riders.HasRangeEnd && riders.RangeEnd >= entry {
			rangeEnd = riders.RangeEnd
		}
		ep := state.ExtraPhase{
			Player:    e.Player,
			AfterStep: e.Step,
			Entry:     entry,
			RangeEnd:  rangeEnd,
			Execute:   e.Counter,
			Source:    e.Obj,
		}
		if len(e.IDs) > 1 {
			if fb := state.Step(e.IDs[1]); fb.Valid() {
				ep.HasFollowedBy, ep.FollowedBy = true, fb
			}
		}
		ep.HasDelayedPhase, ep.DelayedPhase = riders.HasDelayedPhase, riders.DelayedPhase
		ep.ValidPlayer = riders.ValidPlayer
		for n := int32(0); n < min(e.Amount, maxQueuedGrants); n++ {
			g.ExtraPhases = append(g.ExtraPhases, ep)
		}
	case e.Amount == -1:
		if i, ok := matchExtraPhase(g, e, false); ok {
			g.ExtraPhases[i].Consumed = true
		}
		// The delayed-trigger rider (Moraug's "At the beginning of that
		// combat, untap all creatures you control") registers HERE, at
		// consume time -- the ExtraTurn Final-Fortune precedent: the
		// registration must ride the consumption, because the extra phase
		// begins exactly now (the very next StepChange is its entry step,
		// which the ordinary delayed-trigger drain fires on). MinTurn is
		// the CURRENT turn -- the extra phase begins in the granting turn,
		// unlike an extra turn's Turn+1. A consume with no source object,
		// no Execute$ name, or a source whose face lacks the SVar degrades
		// to no registration rather than panicking.
		if e.Counter != "" && e.Obj != 0 && g.Obj(e.Obj) != nil {
			f := g.Obj(e.Obj).Face()
			if f != nil && cards.ResolveSVar(f.SVars, e.Counter) != nil {
				// The registered phase: the rider's DELAY= step when the
				// grant forwarded one, else the extra phase's own entry
				// step (IDs[0] -- the only step a consume always carries).
				riders := DecodeExtraPhaseRiders(e.Text)
				phase := state.Step(0)
				okPhase := false
				if riders.HasDelayedPhase {
					phase, okPhase = riders.DelayedPhase, true
				} else if len(e.IDs) > 0 {
					phase, okPhase = state.Step(e.IDs[0]), state.Step(e.IDs[0]).Valid()
				}
				if okPhase {
					dt := state.DelayedTrigger{
						ID:         g.DelayedNext,
						Phase:      phase,
						Source:     e.Obj,
						Controller: e.Player,
						Execute:    e.Counter,
						MinTurn:    g.Turn,
					}
					dt.ValidPlayer = riders.ValidPlayer
					g.Delayed = append(g.Delayed, dt)
					g.DelayedNext++
				}
			}
		}
	case e.Amount == -2:
		if i, ok := matchExtraPhase(g, e, true); ok {
			g.ExtraPhases = append(g.ExtraPhases[:i], g.ExtraPhases[i+1:]...)
		}
	}
}

// foldClockTick folds Kind ClockTick into state.
func foldClockTick(g *state.Game, e Event) {
	g.Clock++
}

// foldStartingPlayerChange folds Kind StartingPlayerChange into state.
func foldStartingPlayerChange(g *state.Game, e Event) {
	if validPlayer(g, e.Player) {
		g.StartingPlayer, g.HasStartingPlayer = e.Player, true
	}
}
