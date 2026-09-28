package kshadow

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/spellbench/v1agent"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// Shadow is one staged gorge engine for one kernel decision.
type Shadow struct {
	E  *rules.Engine
	Me state.PlayerID
	// ArenaToObj maps every staged kernel object (kernel arena id) to its
	// gorge object; ObjToArena is the reverse.
	ArenaToObj map[uint32]state.ObjID
	ObjToArena map[state.ObjID]uint32
	// Lossy lists what the staging could not reproduce (one entry per
	// gap, a stable reason string); Fatal is set when the engine is not
	// positioned at a decision for Me (the shadow is then unusable).
	Lossy []string
	Fatal string
	// Hidden are the gorge objects dealt into hidden zones: the
	// opponent's hand and both libraries (redeal pools).
	OppHand, OppLib, MyLib []state.ObjID
	// StagedStep is the step the observation was staged at.
	StagedStep state.Step
}

// Options tune one Build.
type Options struct {
	// Seed picks the deal of hidden cards and the engine's future chance.
	Seed uint64
	// NoPumps skips staging unexplained P/T and keyword differences as
	// until-end-of-turn effects (the fidelity ablation).
	NoPumps bool
	// NoAdvance leaves the engine unadvanced (tests of the staging itself).
	NoAdvance bool
	// Priority marks a kernel priority decision (Classify == ClassPriority):
	// the kernel's pre-declaration combat windows are then staged as the
	// window gorge gives them (begin combat; after the attack declaration).
	Priority bool
}

// kernelStep maps the kernel's phase names onto gorge steps.
var kernelStep = map[string]state.Step{
	"untap": state.StepUntap, "upkeep": state.StepUpkeep, "draw": state.StepDraw,
	"main1": state.StepMain1, "begin_combat": state.StepBeginCombat,
	"declare_attackers": state.StepDeclareAttackers, "declare_blockers": state.StepDeclareBlockers,
	// Priority in the combat damage step comes after the damage; gorge's
	// step would deal it again, so it is staged as end of combat.
	"combat_damage": state.StepEndCombat, "first_strike_damage": state.StepEndCombat,
	"end_combat": state.StepEndCombat, "main2": state.StepMain2, "end": state.StepEnd,
	"cleanup": state.StepCleanup,
}

func seatID(seat string) state.PlayerID {
	if seat == "p1" {
		return 1
	}
	return 0
}

type builder struct {
	s    *Setup
	sh   *Shadow
	e    *rules.Engine
	g    *state.Game
	obs  *v1agent.KObservation
	p    *v1agent.KProjection
	rng  *rand.Rand
	free [2]map[string][]state.ObjID // folded name -> unassigned deck objects
	// deckObj[p][i] is deck card i's gorge object.
	deckObj [2][]state.ObjID
}

func (b *builder) ev(e events.Event) events.Event { return events.Emit(b.g, b.e.L, e) }

func (b *builder) lossy(format string, args ...any) {
	b.sh.Lossy = append(b.sh.Lossy, fmt.Sprintf(format, args...))
}

// Build stages obs (the acting seat's ObservationV5) into a fresh engine.
func (s *Setup) Build(obs *v1agent.KObservation, o Options) *Shadow {
	me := seatID(obs.ActingPlayer)
	sh := &Shadow{Me: me, ArenaToObj: map[uint32]state.ObjID{}, ObjToArena: map[state.ObjID]uint32{}}
	cfg := rules.Config{
		Seed:   o.Seed,
		Names:  []string{"p0", "p1"},
		Decks:  [][]*cards.Card{s.Decks[0], s.Decks[1]},
		Tokens: s.Reg.Tokens,
	}
	e := rules.New(cfg)
	b := &builder{s: s, sh: sh, e: e, g: e.G, obs: obs, p: &obs.Projection,
		rng: rand.New(rand.NewPCG(o.Seed, o.Seed^0x6b736861646f77))}
	sh.E = e
	if e.G.Over {
		sh.Fatal = "genesis ended the game"
		return sh
	}
	b.indexDeck()
	b.stageZones()
	b.stageTurn(o.Priority)
	b.stageStack()
	b.stageCombat()
	b.stageDesignations()
	if !o.NoPumps {
		b.stagePumps()
	}
	if o.NoAdvance {
		return sh
	}
	b.advance()
	return sh
}

// indexDeck maps deck positions to the objects genesis created (AddObject
// runs in deck order, seat by seat) and returns every card to its library.
func (b *builder) indexDeck() {
	for p := 0; p < 2; p++ {
		b.free[p] = map[string][]state.ObjID{}
	}
	for i := range b.g.Objs {
		o := &b.g.Objs[i]
		if o.IsToken || o.Card == nil || int(o.Owner) > 1 {
			continue
		}
		b.deckObj[o.Owner] = append(b.deckObj[o.Owner], o.ID)
	}
	for p := 0; p < 2; p++ {
		for _, id := range b.deckObj[p] {
			o := b.g.Obj(id)
			seen := map[string]bool{}
			for _, f := range o.Card.Faces {
				if f == nil {
					continue
				}
				k := fold(f.Name)
				if !seen[k] {
					seen[k] = true
					b.free[p][k] = append(b.free[p][k], id)
				}
			}
		}
		for _, id := range append([]state.ObjID(nil), b.g.Zone(state.ZHand, state.PlayerID(p))...) {
			b.ev(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
		}
	}
}

// take assigns an unassigned deck object of owner named name.
func (b *builder) take(owner state.PlayerID, name string, arena uint32) (state.ObjID, bool) {
	if id, ok := b.sh.ArenaToObj[arena]; ok {
		return id, true
	}
	k := fold(name)
	list := b.free[owner][k]
	if len(list) == 0 {
		return 0, false
	}
	id := list[0]
	// Remove id from every name list (a multi-face card is listed under
	// each face).
	for n, l := range b.free[owner] {
		for j, x := range l {
			if x == id {
				b.free[owner][n] = append(l[:j:j], l[j+1:]...)
				break
			}
		}
	}
	b.sh.ArenaToObj[arena] = id
	b.sh.ObjToArena[id] = arena
	return id, true
}

func (b *builder) move(id state.ObjID, to state.Zone) {
	o := b.g.Obj(id)
	if o == nil || o.Zone == to {
		return
	}
	b.ev(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: to})
}

type permEntry struct {
	c     *v1agent.KCard
	id    state.ObjID
	fresh bool // entered this turn: staged after the turn boundary
}

func (b *builder) stageZones() {
	p := b.p
	var perms []permEntry
	// Battlefield (non-token) cards.
	for side := 0; side < 2; side++ {
		for i := range p.Battlefield[side] {
			c := &p.Battlefield[side][i]
			if c.IsToken {
				perms = append(perms, permEntry{c: c})
				continue
			}
			owner := seatID(c.Stable.Owner)
			id, ok := b.take(owner, c.Name, c.Stable.ArenaID)
			if !ok {
				b.lossy("unmatched battlefield card %s", c.Name)
				continue
			}
			perms = append(perms, permEntry{c: c, id: id})
		}
	}
	// Graveyards and exile.
	for side := 0; side < 2; side++ {
		for i := range p.Graveyards[side] {
			c := &p.Graveyards[side][i]
			if c.IsToken {
				continue
			}
			if id, ok := b.take(seatID(c.Stable.Owner), c.Name, c.Stable.ArenaID); ok {
				b.move(id, state.ZGraveyard)
			} else {
				b.lossy("unmatched graveyard card %s", c.Name)
			}
		}
	}
	for i := range p.Exile {
		c := &p.Exile[i]
		if c.IsToken {
			continue
		}
		if id, ok := b.take(seatID(c.Stable.Owner), c.Name, c.Stable.ArenaID); ok {
			b.move(id, state.ZExile)
		} else {
			b.lossy("unmatched exile card %s", c.Name)
		}
	}
	// Our hand.
	for _, h := range b.obs.OwnHand {
		if id, ok := b.take(b.sh.Me, h.Name, h.Stable.ArenaID); ok {
			b.move(id, state.ZHand)
		} else {
			b.lossy("unmatched hand card %s", h.Name)
		}
	}
	// Spells on the stack (placed later, in stack order; claimed now so
	// the hidden deal cannot use them).
	for i := range p.Stack {
		it := &p.Stack[i]
		if it.Kind != "spell" || it.IsCopy {
			continue
		}
		name := ""
		if k := v1agent.KernelCardByID(it.Source.CardDBID); k != nil {
			name = k.Name
		}
		if _, ok := b.take(seatID(it.Source.Owner), name, it.Source.ArenaID); !ok {
			b.lossy("unmatched stack spell %s", name)
		}
	}
	// Old permanents enter before the turn boundary (not summoning sick),
	// this turn's after it.
	// The kernel's summoning_sick flag decides (CR 302.6), for both seats:
	// its turn field counts rounds (both seats play "turn N"), so
	// entered_battlefield_turn == turn does not mean "entered this turn".
	for i := range perms {
		perms[i].fresh = perms[i].c.SummoningSick
	}
	b.enterPerms(perms, false)
	b.hiddenDeal()
	// The turn boundary: TurnChange clears summoning sickness for that
	// player's permanents and every per-turn fact.
	active := seatID(p.ActivePlayer)
	turn := int32(p.Turn)
	if turn < 1 {
		turn = 1
	}
	if turn > 1 {
		b.ev(events.Event{Kind: events.TurnChange, Player: 1 - active, Amount: turn - 1})
	}
	b.ev(events.Event{Kind: events.TurnChange, Player: active, Amount: turn})
	b.enterPerms(perms, true)
	b.permState(perms)
	b.stageExilePlay()
}

// stageAbilityUses records this turn's activations of c's abilities as
// ManaActivate markers (Apply is inert for them) after the turn boundary:
// gorge counts per-turn uses (activation limits, "activate only once each
// turn") from AbilityPush/ManaActivate events since the last TurnChange.
// The kernel indexes mana and other activated abilities separately; gorge
// indexes the face's whole ability list.
func (b *builder) stageAbilityUses(id state.ObjID, c *v1agent.KCard) {
	if len(c.AbilityUses) == 0 {
		return
	}
	o := b.g.Obj(id)
	if o == nil || o.Face() == nil {
		return
	}
	var mana, other []int
	for i, sa := range o.Face().Abilities {
		if sa == nil || sa.Kind != "AB" {
			continue
		}
		if sa.API == "Mana" {
			mana = append(mana, i)
		} else {
			other = append(other, i)
		}
	}
	for _, u := range c.AbilityUses {
		list := other
		if u.Kind == "mana" {
			list = mana
		}
		if u.Index < 0 || u.Index >= len(list) {
			b.lossy("ability use of %s not mapped", c.Name)
			continue
		}
		for k := 0; k < u.Uses; k++ {
			b.ev(events.Event{Kind: events.ManaActivate, Obj: id, Player: o.Controller, Amount: int32(list[u.Index])})
		}
	}
}

// stageExilePlay grants each kernel exile play permission as gorge's
// may-play effect over that one exiled card (the Effect-delivered
// "Card.IsRemembered" grant impulse draws register).
func (b *builder) stageExilePlay() {
	for _, ep := range b.p.ExilePlay {
		id, ok := b.sh.ArenaToObj[ep.Object.ArenaID]
		if !ok {
			b.lossy("exile play permission for an unstaged card")
			continue
		}
		ce := state.ContinuousEffect{Source: id, Controller: seatID(ep.Holder), Affects: "Card.IsRemembered",
			AffectedZone: "Exile", MayPlay: true, FromEffect: true, Remembered: []state.ObjID{id}}
		if ep.Expiry.Kind == "end_of_turn" || ep.Expiry.Kind == "until_end_of_turn" {
			ce.UntilEOT = true
		} else {
			ce.Permanent = true
		}
		b.e.AddContinuous(ce)
	}
}

func (b *builder) enterPerms(perms []permEntry, fresh bool) {
	for i := range perms {
		pe := &perms[i]
		if pe.fresh != fresh {
			continue
		}
		c := pe.c
		ctrl := seatID(c.Stable.Controller)
		if c.IsToken {
			before := len(b.g.Objs)
			if stem, ok := b.s.TokenStem(c.Name); ok {
				b.ev(events.Event{Kind: events.TokenCreate, Player: ctrl, Text: stem})
			} else if src, ok := b.cardNamed(tokenCardName(c.Name)); ok {
				// A token copy of a card (embalm, encore): mint it from
				// any deck object of that name.
				b.ev(events.Event{Kind: events.CardToken, Player: ctrl, Obj: src})
				b.lossy("card-copy token %s", c.Name)
			} else {
				b.lossy("unknown token %s", c.Name)
				continue
			}
			if len(b.g.Objs) == before {
				b.lossy("token %s not created", c.Name)
				continue
			}
			pe.id = b.g.Objs[len(b.g.Objs)-1].ID
			b.sh.ArenaToObj[c.Stable.ArenaID] = pe.id
			b.sh.ObjToArena[pe.id] = c.Stable.ArenaID
		} else {
			if pe.id == 0 {
				continue
			}
			b.move(pe.id, state.ZBattlefield)
			if o := b.g.Obj(pe.id); o != nil && c.FaceIndex > 0 && int(o.FaceIdx) != c.FaceIndex {
				b.ev(events.Event{Kind: events.FlipFace, Obj: pe.id, Amount: int32(c.FaceIndex)})
			}
			if o := b.g.Obj(pe.id); o != nil && o.Controller != ctrl {
				b.ev(events.Event{Kind: events.ControlChange, Obj: pe.id, Player: ctrl})
			}
		}
	}
}

// permState stages tapped state, damage, counters and attachments.
func (b *builder) permState(perms []permEntry) {
	for i := range perms {
		pe := &perms[i]
		if pe.id == 0 {
			continue
		}
		c := pe.c
		o := b.g.Obj(pe.id)
		if o == nil {
			continue
		}
		if c.Tapped != o.Tapped {
			if c.Tapped {
				b.ev(events.Event{Kind: events.Tap, Obj: pe.id})
			} else {
				b.ev(events.Event{Kind: events.Untap, Obj: pe.id})
			}
		}
		if c.Damage > 0 && o.Damage == 0 {
			b.ev(events.Event{Kind: events.Damage, Obj: pe.id, Amount: int32(c.Damage)})
		}
		for _, kc := range []struct {
			kind string
			n    int
		}{{"P1P1", c.Counters.P1P1}, {"M1M1", c.Counters.M1M1}, {"M0M1", c.Counters.M0M1}, {"STUN", c.Counters.Stun}, {"LORE", c.Counters.Lore}} {
			if have := o.Counter(kc.kind); int(have) != kc.n {
				b.ev(events.Event{Kind: events.CounterChange, Obj: pe.id, Counter: kc.kind, Amount: int32(kc.n) - have})
			}
		}
		if c.SkipNextUntap {
			b.lossy("skip_next_untap not staged")
		}
		b.stageAbilityUses(pe.id, c)
	}
	// Attachments: attachments lists what is attached to the card.
	for i := range perms {
		pe := &perms[i]
		for _, a := range pe.c.Attachments {
			aid, ok := b.sh.ArenaToObj[a]
			if !ok || pe.id == 0 {
				b.lossy("unmatched attachment")
				continue
			}
			b.ev(events.Event{Kind: events.Attach, Obj: aid, IDs: []state.ObjID{pe.id}})
		}
	}
}

// hiddenDeal fills the hidden zones: our library from our unseen cards (a
// random order), the opponent's hand and library from its unseen cards.
func (b *builder) hiddenDeal() {
	p := b.p
	for pl := 0; pl < 2; pl++ {
		owner := state.PlayerID(pl)
		var unseen []state.ObjID
		for _, id := range b.deckObj[pl] {
			if _, ok := b.sh.ObjToArena[id]; !ok {
				unseen = append(unseen, id)
			}
		}
		// Deterministic shuffle of the unseen pool.
		b.rng.Shuffle(len(unseen), func(i, j int) { unseen[i], unseen[j] = unseen[j], unseen[i] })
		handN := 0
		if owner != b.sh.Me {
			handN = p.HandCounts[pl]
		}
		if handN > len(unseen) {
			b.lossy("opponent hand %d exceeds unseen pool %d", handN, len(unseen))
			handN = len(unseen)
		}
		hand, lib := unseen[:handN], unseen[handN:]
		for _, id := range hand {
			b.move(id, state.ZHand)
		}
		for _, id := range lib {
			b.move(id, state.ZLibrary)
		}
		want := p.LibraryCounts[pl]
		if len(lib) != want {
			b.lossy("library size %d vs kernel %d (seat %d)", len(lib), want, pl)
		}
		// Trim or keep: a library longer than the kernel's loses its
		// excess to exile (identity-less cards), so draws line up.
		for len(lib) > want && want >= 0 {
			id := lib[len(lib)-1]
			lib = lib[:len(lib)-1]
			b.move(id, state.ZExile)
		}
		order := append([]state.ObjID(nil), lib...)
		b.ev(events.Event{Kind: events.Shuffle, Player: owner, IDs: order, Secret: true})
		if owner == b.sh.Me {
			b.sh.MyLib = order
		} else {
			b.sh.OppHand = append([]state.ObjID(nil), hand...)
			b.sh.OppLib = order
		}
	}
}

func (b *builder) stageTurn(priority bool) {
	p := b.p
	for pl := 0; pl < 2; pl++ {
		if d := int32(p.Life[pl]) - b.g.Players[pl].Life; d != 0 {
			b.ev(events.Event{Kind: events.LifeChange, Player: state.PlayerID(pl), Amount: d})
		}
		for i, sym := range []string{"W", "U", "B", "R", "G", "C"} {
			if n := p.ManaPools[pl][i]; n > 0 {
				b.ev(events.Event{Kind: events.ManaAdd, Player: state.PlayerID(pl), Counter: sym, Amount: int32(n)})
			}
		}
		for k := 0; k < p.Status[pl].LandsPlayed; k++ {
			b.ev(events.Event{Kind: events.LandPlayed, Player: state.PlayerID(pl)})
		}
	}
	st, ok := kernelStep[p.Phase]
	if !ok {
		b.sh.Fatal = "unknown phase " + p.Phase
		return
	}
	if priority {
		switch {
		case st == state.StepDeclareAttackers && !p.Combat.AttackersDeclared:
			st = state.StepBeginCombat
		case st == state.StepDeclareBlockers && !p.Combat.BlockersDeclared:
			st = state.StepDeclareAttackers
		}
	}
	b.sh.StagedStep = st
	b.ev(events.Event{Kind: events.StepChange, Step: st})
}

// stageStack places the stack bottom to top. Kernel stack_index 0 is the
// bottom (INFERRED from the resolution order; verified by the fidelity
// check's stack comparison).
func (b *builder) stageStack() {
	items := append([]v1agent.KStackItem(nil), b.p.Stack...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Index < items[j].Index })
	for i := range items {
		it := &items[i]
		ctrl := seatID(it.Controller)
		switch {
		case it.Kind == "spell" && !it.IsCopy:
			id, ok := b.sh.ArenaToObj[it.Source.ArenaID]
			if !ok {
				b.sh.Fatal = "stack spell not staged"
				return
			}
			o := b.g.Obj(id)
			b.ev(events.Event{Kind: events.PutOnStack, Obj: id, From: o.Zone, To: state.ZStack, Player: ctrl})
			if o.Controller != ctrl {
				b.ev(events.Event{Kind: events.ControlChange, Obj: id, Player: ctrl})
			}
			if it.XValue > 0 || it.Kicked {
				flags := ""
				if it.Kicked {
					flags = "kicked"
				}
				b.ev(events.Event{Kind: events.CastInfo, Obj: id, Amount: int32(it.XValue), Counter: flags})
			}
			b.stageTargets(id, it)
		case it.Kind == "triggered_ability" || it.Kind == "activated_ability":
			src, ok := b.sh.ArenaToObj[it.Source.ArenaID]
			if !ok {
				// The source has left (a token sacrificed for the cost, a
				// card that died or shuffled away, a ninjutsu card in hand):
				// stage it from its last known name.
				if src, ok = b.lkiSource(it); !ok {
					b.sh.Fatal = "stack ability source not staged"
					return
				}
			}
			o := b.g.Obj(src)
			f := o.Face()
			if f == nil {
				b.sh.Fatal = "stack ability source has no face"
				return
			}
			before := len(b.g.Objs)
			if it.Kind == "triggered_ability" {
				if n, names := cards.SagaChapters(f); n > 0 && len(names) > 0 {
					lore := int(o.Counter("LORE"))
					if lore < 1 {
						lore = 1
					}
					if lore > len(names) {
						lore = len(names)
					}
					b.ev(events.Event{Kind: events.DelayedPush, Player: ctrl, Obj: src, Amount: -1, Counter: names[lore-1]})
				} else {
					idx, sure := b.pickTrigger(f, o, it.Source.Zone)
					granted := false
					if idx < 0 || !firesFrom(f.Triggers[idx], it.Source.Zone) {
						// No printed trigger fits: a trigger granted by a
						// static (an Equipment's AddTrigger$).
						if grantor, exec, ok := b.grantedTrigger(src); ok {
							b.ev(events.Event{Kind: events.GrantTriggerPush, Player: ctrl, Obj: src,
								Amount: int32(grantor), Counter: exec, Text: "granted trigger"})
							granted = true
						}
					}
					switch {
					case granted:
					case idx < 0:
						b.sh.Fatal = fmt.Sprintf("triggered ability of %s: %d triggers", f.Name, len(f.Triggers))
						return
					default:
						if !sure {
							b.lossy("ambiguous trigger of %s", f.Name)
						}
						b.ev(events.Event{Kind: events.TriggerPush, Player: ctrl, Obj: src, Amount: int32(idx)})
					}
				}
			} else {
				idx := -1
				n := 0
				for j, sa := range f.Abilities {
					if sa != nil && sa.Kind == "AB" && sa.API != "Mana" {
						idx = j
						n++
					}
				}
				if n != 1 {
					b.sh.Fatal = fmt.Sprintf("activated ability of %s: %d candidates", f.Name, n)
					return
				}
				b.ev(events.Event{Kind: events.AbilityPush, Player: ctrl, Obj: src, Amount: int32(idx)})
			}
			if len(b.g.Objs) == before {
				b.sh.Fatal = "stack ability not created"
				return
			}
			b.stageTargets(b.g.Objs[len(b.g.Objs)-1].ID, it)
		case it.Kind == "madness_offer":
			// CR 702.35a: the madness trigger of a card discarded into
			// exile; gorge mints it as a keyword trigger over the card.
			name := ""
			if k := v1agent.KernelCardByID(it.Source.CardDBID); k != nil {
				name = k.Name
			}
			id, ok := b.take(seatID(it.Source.Owner), name, it.Source.ArenaID)
			if !ok {
				b.sh.Fatal = "madness card not staged"
				return
			}
			b.move(id, state.ZExile)
			before := len(b.g.Objs)
			b.ev(events.Event{Kind: events.KeywordTriggerPush, Player: ctrl, Obj: id, Counter: "__kwMadnessCast", Text: "madness cast"})
			if len(b.g.Objs) == before {
				b.sh.Fatal = "madness trigger not created"
				return
			}
		default:
			b.sh.Fatal = "unstageable stack item " + it.Kind
			return
		}
	}
}

func (b *builder) stageTargets(id state.ObjID, it *v1agent.KStackItem) {
	for _, t := range it.Targets {
		amount := int32(2)
		if t.Kind == "player" {
			amount = 3
			b.ev(events.Event{Kind: events.TargetsChosen, Obj: id, Player: seatID(t.Player), Amount: amount})
		} else if t.Object != nil {
			tid, ok := b.sh.ArenaToObj[t.Object.ArenaID]
			if !ok {
				b.lossy("stack target not staged")
				continue
			}
			b.ev(events.Event{Kind: events.TargetsChosen, Obj: id, IDs: []state.ObjID{tid}, Amount: amount})
		}
	}
}

func (b *builder) stageCombat() {
	c := &b.p.Combat
	if !c.AttackersDeclared {
		return
	}
	st := b.g.Step
	if st < state.StepDeclareAttackers || st > state.StepCombatDamage {
		return
	}
	active := seatID(b.p.ActivePlayer)
	var ids []state.ObjID
	for _, a := range c.Attackers {
		if id, ok := b.sh.ArenaToObj[a.ArenaID]; ok {
			ids = append(ids, id)
		} else {
			b.lossy("attacker not staged")
		}
	}
	// The declaration belongs to the declare-attackers step: a later step
	// replays the boundary so the engine's per-step derivation holds.
	if st != state.StepDeclareAttackers {
		b.ev(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	}
	b.ev(events.Event{Kind: events.DeclareAttackers, Player: 1 - active, IDs: ids})
	if st == state.StepDeclareAttackers {
		return
	}
	b.ev(events.Event{Kind: events.StepChange, Step: state.StepDeclareBlockers})
	if c.BlockersDeclared {
		var pairs [][2]state.ObjID
		blocks := c.Blocks()
		for _, a := range c.Attackers {
			aid, ok := b.sh.ArenaToObj[a.ArenaID]
			if !ok {
				continue
			}
			for _, bl := range blocks[a.ArenaID] {
				if bid, ok := b.sh.ArenaToObj[bl.ArenaID]; ok {
					pairs = append(pairs, [2]state.ObjID{aid, bid})
				}
			}
		}
		b.ev(events.Event{Kind: events.DeclareBlockers, Player: 1 - active, Pairs: pairs})
	}
	if st != state.StepDeclareBlockers {
		b.ev(events.Event{Kind: events.StepChange, Step: st})
	}
}

func (b *builder) stageDesignations() {
	if b.p.Initiative != nil {
		b.ev(events.Event{Kind: events.InitiativeChange, Player: seatID(*b.p.Initiative)})
	}
}

// stagePumps registers, for every battlefield creature whose derived P/T
// or evasion keywords differ from the kernel's effective values, an
// until-end-of-turn continuous effect closing the difference (spec D§4.2
// step 3: unexplained deltas are pump effects).
func (b *builder) stagePumps() {
	for side := 0; side < 2; side++ {
		for i := range b.p.Battlefield[side] {
			c := &b.p.Battlefield[side][i]
			id, ok := b.sh.ArenaToObj[c.Stable.ArenaID]
			if !ok || !c.IsCreature() || c.Characteristics.Power == nil {
				continue
			}
			if !b.e.IsCreature(id) {
				continue
			}
			dp := int32(c.Power()) - b.e.Power(id)
			dt := int32(c.Toughness()) - b.e.Toughness(id)
			var kws []string
			for _, kw := range keywordTable {
				if kw.get(&c.Characteristics.Keywords) && !b.e.HasKeyword(id, kw.gorge) {
					kws = append(kws, kw.gorge)
				}
			}
			ctrl := seatID(c.Stable.Controller)
			if dp != 0 || dt != 0 {
				b.e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Controller: ctrl,
					Layer: state.LPT, Sub: state.SubModify, AddPower: dp, AddToughness: dt, UntilEOT: true})
				b.lossy("pump staged")
			}
			if len(kws) > 0 {
				b.e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Controller: ctrl,
					Layer: state.LAbilities, AddKeywords: kws, UntilEOT: true})
				b.lossy("keyword grant staged")
			}
		}
	}
}

// keywordTable pairs the kernel's keyword flags with gorge keyword names.
var keywordTable = []struct {
	gorge string
	get   func(k *v1agent.KKeywords) bool
}{
	{"Flying", func(k *v1agent.KKeywords) bool { return k.Flying }},
	{"Reach", func(k *v1agent.KKeywords) bool { return k.Reach }},
	{"Haste", func(k *v1agent.KKeywords) bool { return k.Haste }},
	{"Vigilance", func(k *v1agent.KKeywords) bool { return k.Vigilance }},
	{"Trample", func(k *v1agent.KKeywords) bool { return k.Trample }},
	{"First Strike", func(k *v1agent.KKeywords) bool { return k.FirstStrike }},
	{"Double Strike", func(k *v1agent.KKeywords) bool { return k.DoubleStrike }},
	{"Deathtouch", func(k *v1agent.KKeywords) bool { return k.Deathtouch }},
	{"Menace", func(k *v1agent.KKeywords) bool { return k.Menace }},
	{"Defender", func(k *v1agent.KKeywords) bool { return k.Defender }},
	{"Lifelink", func(k *v1agent.KKeywords) bool { return k.Lifelink }},
	{"Hexproof", func(k *v1agent.KKeywords) bool { return k.Hexproof }},
	{"Indestructible", func(k *v1agent.KKeywords) bool { return k.Indestructible }},
}

// advance positions the engine at its next decision: priority to the
// kernel's priority player (one pass counted when it is not the active
// player: the active player passed to it), then Advance.
func (b *builder) advance() {
	if b.sh.Fatal != "" {
		return
	}
	pp := seatID(b.p.PriorityPlayer)
	passes := int32(0)
	if pp != seatID(b.p.ActivePlayer) {
		passes = 1
	}
	if pr := b.p.EngineContext.PriorityPasses; len(pr) == 2 {
		// The kernel's own pass record: consecutive passes since the last
		// stack change (the priority holder has not passed yet).
		passes = 0
		for i, passed := range pr {
			if passed && state.PlayerID(i) != pp {
				passes++
			}
		}
	}
	b.ev(events.Event{Kind: events.Priority, Player: pp, Amount: passes})
	func() {
		defer func() {
			if r := recover(); r != nil {
				b.sh.Fatal = fmt.Sprintf("advance panicked: %v", r)
			}
		}()
		b.e.Advance()
	}()
	if b.sh.Fatal != "" {
		return
	}
	if b.g.Over {
		b.sh.Fatal = "staged game is over"
		return
	}
	d := b.e.Pending()
	if d == nil {
		b.sh.Fatal = "no pending decision"
		return
	}
	if d.Player != b.sh.Me {
		b.sh.Fatal = fmt.Sprintf("pending decision is the opponent's (%s)", d.Kind)
	}
}

// pickTrigger chooses which of the source face's triggers a kernel
// triggered ability on the stack is: the only one; else the zone-change
// trigger the source's position implies (an enters trigger for a permanent
// that entered this turn, a leaves/dies trigger for a source no longer on
// the battlefield); else the first (sure false).
func (b *builder) pickTrigger(f *cards.Face, o *state.Object, kzone string) (int, bool) {
	switch len(f.Triggers) {
	case 0:
		return -1, false
	case 1:
		return 0, true
	}
	// The kernel names the zone the source was in when the trigger was put
	// on the stack; exactly one trigger able to fire from there is the one.
	var fit []int
	for i, tr := range f.Triggers {
		if firesFrom(tr, kzone) {
			fit = append(fit, i)
		}
	}
	if len(fit) == 1 {
		return fit[0], true
	}
	var enters, leaves []int
	for i, tr := range f.Triggers {
		if tr.Mode != "ChangesZone" && tr.Mode != "ChangesZoneAll" {
			continue
		}
		switch {
		case tr.Params["Destination"] == "Battlefield":
			enters = append(enters, i)
		case tr.Params["Origin"] == "Battlefield":
			leaves = append(leaves, i)
		}
	}
	if o.Zone == state.ZBattlefield && o.EnteredThisTurn && len(enters) == 1 {
		return enters[0], true
	}
	if o.Zone != state.ZBattlefield && len(leaves) == 1 {
		return leaves[0], true
	}
	if o.Zone == state.ZBattlefield && len(enters) == 1 {
		return enters[0], false
	}
	return 0, false
}

// tokenCardName strips a kernel card-copy token's suffix ("Sacred Cat
// Embalmed Token" -> "Sacred Cat").
func tokenCardName(n string) string {
	for _, suf := range []string{" Embalmed Token", " Eternalized Token", " Encore Token", " Token"} {
		if len(n) > len(suf) && n[len(n)-len(suf):] == suf {
			return n[:len(n)-len(suf)]
		}
	}
	return n
}

// cardNamed finds any deck object (either seat) with a face named name.
func (b *builder) cardNamed(name string) (state.ObjID, bool) {
	k := fold(name)
	for p := 0; p < 2; p++ {
		for _, id := range b.deckObj[p] {
			o := b.g.Obj(id)
			if o == nil || o.Card == nil {
				continue
			}
			for _, f := range o.Card.Faces {
				if f != nil && fold(f.Name) == k {
					return id, true
				}
			}
		}
	}
	return 0, false
}

// firesFrom reports whether trigger tr can fire while its source is in the
// kernel zone kzone ("Battlefield", "Stack", "Graveyard", ...): an explicit
// TriggerZones$ list decides; otherwise a "when you cast this" trigger
// fires from the stack, a leaves-the-battlefield trigger from wherever the
// source went, and everything else from the battlefield.
func firesFrom(tr cards.Trigger, kzone string) bool {
	if kzone == "" {
		return true
	}
	if tz := tr.Params["TriggerZones"]; tz != "" {
		for _, z := range strings.Split(tz, ",") {
			if strings.EqualFold(strings.TrimSpace(z), kzone) {
				return true
			}
		}
		return false
	}
	castSelf := tr.Mode == "SpellCast" && tr.Params["ValidCard"] == "Card.Self"
	leaves := (tr.Mode == "ChangesZone" || tr.Mode == "ChangesZoneAll") && tr.Params["Origin"] == "Battlefield"
	switch {
	case strings.EqualFold(kzone, "Stack"):
		return castSelf
	case strings.EqualFold(kzone, "Battlefield"):
		return !castSelf
	}
	return leaves
}

// grantedTrigger finds the one trigger a battlefield static grants src
// (AddTrigger$ on a Mode$ Continuous static of src itself or of a permanent
// attached to it): the grantor (0 for a self-grant) and the granted
// trigger's Execute$ body.
func (b *builder) grantedTrigger(src state.ObjID) (state.ObjID, string, bool) {
	var grantor state.ObjID
	exec, n := "", 0
	for _, id := range append(append([]state.ObjID(nil), b.g.Zone(state.ZBattlefield, 0)...), b.g.Zone(state.ZBattlefield, 1)...) {
		o := b.g.Obj(id)
		if o == nil || o.Face() == nil || (id != src && o.AttachedTo != src) {
			continue
		}
		f := o.Face()
		for _, st := range f.Statics {
			names := st.Params["AddTrigger"]
			if names == "" {
				continue
			}
			for _, nm := range strings.Split(names, ",") {
				line := f.SVars[strings.TrimSpace(nm)]
				i := strings.Index(line, "Execute$")
				if i < 0 {
					continue
				}
				rest := strings.TrimSpace(line[i+len("Execute$"):])
				if j := strings.IndexAny(rest, " |"); j >= 0 {
					rest = rest[:j]
				}
				n++
				exec = rest
				grantor = 0
				if id != src {
					grantor = id
				}
			}
		}
	}
	return grantor, exec, n == 1
}

// lkiSource stages a stack ability's departed source from the kernel's
// reference (card DB name, owner, zone): a token is minted and put in the
// graveyard (where it ceases to exist); a card is claimed from its owner's
// unseen cards and put in the zone the kernel names (hand for ninjutsu,
// otherwise the graveyard). The ability then resolves from last known
// information, as in the kernel.
func (b *builder) lkiSource(it *v1agent.KStackItem) (state.ObjID, bool) {
	k := v1agent.KernelCardByID(it.Source.CardDBID)
	if k == nil {
		return 0, false
	}
	owner := seatID(it.Source.Owner)
	var id state.ObjID
	if stem, ok := b.s.TokenStem(k.Name); ok {
		before := len(b.g.Objs)
		b.ev(events.Event{Kind: events.TokenCreate, Player: owner, Text: stem})
		if len(b.g.Objs) == before {
			return 0, false
		}
		id = b.g.Objs[len(b.g.Objs)-1].ID
	} else {
		var ok bool
		if id, ok = b.take(owner, k.Name, it.Source.ArenaID); !ok {
			return 0, false
		}
	}
	to := state.ZGraveyard
	if it.Source.Zone == "Hand" {
		to = state.ZHand
	}
	b.move(id, to)
	b.lossy("ability source staged from its last known name")
	return id, true
}
