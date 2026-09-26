// morph_turnup_cost_test.go — the CR 708.6 morph-family turn-face-up cost's
// NON-MANA and ANNOUNCED-X components. Each test proves, end to end: the
// printed turn-up cost is parsed from the card's keyword parameter, the offer
// is withheld when a component has no eligible candidate (and when the mana
// for the announced X is short), choosing the action poses the real cost
// choices (reveal / sacrifice / discard / return / X), paying them moves
// exactly the chosen objects and mana once before the TurnFaceUp event, no
// stack object is used, and the whole game replays byte-identically.
//
// The decks are compiled corpus cards only (no Forge script text is
// committed here); the helpers come from morph_turnup_test.go,
// morph_test.go, manifest_test.go and cast_test.go.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// putCorpusPermanent finds the named card in seat 0's hand/library and moves
// it to seat 0's battlefield with a logged MoveZone. It is the helper the
// cost-candidate tests use to seat sacrifice/return fodder; the card must
// still be a real corpus card (never a hand-authored script).
func putCorpusPermanent(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	return searchMoveByName(t, e, name, state.ZBattlefield)
}

// morphAndPool casts the named morph carrier face down and then funds the
// given extra pool, leaving seat 0 with priority and the card face down.
func morphAndPool(t *testing.T, e *Engine, name, mode, symbols string, poolAfter int, extra string) state.ObjID {
	t.Helper()
	id := morphDownCast(t, e, name, mode, symbols, poolAfter)
	if extra != "" {
		addMana(t, e, 0, extra)
	}
	if o := e.G.Obj(id); !o.FaceDown {
		t.Fatalf("precondition: %s is not face down", name)
	}
	return id
}

// turnFaceUpOptionPresent reports whether the pending seat-0 priority
// decision offers turn_face_up for id.
func turnFaceUpOptionPresent(t *testing.T, e *Engine, id state.ObjID) bool {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "turn_face_up" && o.Obj == id {
			return true
		}
	}
	return false
}

// TestMorphTurnFaceUpPayableGateCoversNonManaAndX asserts the offer is a real
// payability gate: it is withheld when a non-mana component has no candidate
// and when the announced X cannot be paid at all, and offered otherwise. No
// TurnFaceUp event is emitted for a withheld action.
func TestMorphTurnFaceUpPayableGateCoversNonManaAndX(t *testing.T) {
	reg := searchTestRegistry(t)
	// Skirk Volcanist's Morph turn-up cost is Sac<2/Mountain>.
	e, _ := manifestEngine(t, reg, "Skirk Volcanist", "Mountain", "Mountain", "Mountain")
	id := morphDownCast(t, e, "Skirk Volcanist", "morphed", "CCCCR", 2)
	// Precondition: the printed cost really is a two-Mountain sacrifice, and
	// the board has no Mountains yet.
	mf, ok := morphFaceUpCost(e.G.Obj(id))
	if !ok || len(mf.cost.Sac) != 1 || mf.cost.Sac[0].N != 2 {
		t.Fatalf("precondition: parsed turn-up cost = %+v, want Sac<2/...>", mf.cost)
	}
	if len(e.G.Zone(state.ZBattlefield, 0)) != 1 {
		t.Fatalf("precondition: seat 0 should hold only the face-down permanent, holds %d", len(e.G.Zone(state.ZBattlefield, 0)))
	}
	if turnFaceUpOptionPresent(t, e, id) {
		t.Fatalf("turn_face_up offered with no Mountain to sacrifice (the cost is Sac<2/Mountain>)")
	}
	// One Mountain is still short of two: the offer stays withheld.
	putCorpusPermanent(t, e, "Mountain")
	if turnFaceUpOptionPresent(t, e, id) {
		t.Fatalf("turn_face_up offered with only one Mountain (the cost is Sac<2/Mountain>)")
	}
	// Two Mountains satisfy the cost: the offer appears.
	m1 := putCorpusPermanent(t, e, "Mountain")
	m2 := putCorpusPermanent(t, e, "Mountain")
	if m1 == m2 {
		t.Fatalf("precondition: two distinct Mountains required")
	}
	if !turnFaceUpOptionPresent(t, e, id) {
		t.Fatalf("turn_face_up withheld with two Mountains available")
	}
	// The X carrier (Bane of the Living, Morph:X B B) is withheld when the
	// pool cannot even pay {B}{B} at X=0.
	ex, excfg := manifestEngine(t, reg, "Bane of the Living")
	bid := morphDownCast(t, ex, "Bane of the Living", "morphed", "CCBB", 1)
	bmf, ok := morphFaceUpCost(ex.G.Obj(bid))
	if !ok || bmf.cost.X != 1 || bmf.cost.Colored[state.ManaIndex('B')] != 2 {
		t.Fatalf("precondition: Bane turn-up cost = %+v, want X B B", bmf.cost)
	}
	if turnFaceUpOptionPresent(t, ex, bid) {
		t.Fatalf("turn_face_up (X B B) offered with a single leftover {B}")
	}
	addMana(t, ex, 0, "B")
	if !turnFaceUpOptionPresent(t, ex, bid) {
		t.Fatalf("turn_face_up (X B B) withheld with {B}{B} available (X=0)")
	}
	replayCheck(t, ex, excfg)
}

// TestMorphTurnFaceUpPaysNonManaCosts covers the four printed non-mana
// carriers: Reveal, Sac, Discard and Return. Each pays exactly the chosen
// object(s) once, before the TurnFaceUp event, with no stack use.
func TestMorphTurnFaceUpPaysNonManaCosts(t *testing.T) {
	t.Run("Reveal", func(t *testing.T) {
		reg := searchTestRegistry(t)
		e, cfg := manifestEngine(t, reg, "Watcher of the Roost", "White Knight")
		id := morphAndPool(t, e, "Watcher of the Roost", "morphed", "CCCW", 1, "")
		mf, ok := morphFaceUpCost(e.G.Obj(id))
		if !ok || len(mf.cost.Reveal) != 1 {
			t.Fatalf("precondition: parsed turn-up cost = %+v, want Reveal<1/...>", mf.cost)
		}
		white := searchMoveByName(t, e, "White Knight", state.ZHand)
		if o := e.G.Obj(white); o == nil || o.Zone != state.ZHand {
			t.Fatalf("precondition: White Knight not in hand")
		}
		mark := len(e.L.Events)
		idx := turnFaceUpIndex(t, e, id)
		submitChoices(t, e, idx)
		// The reveal cost is paid with the one matching hand card without an
		// ask (a forced singleton), so the action settles in one intent.
		if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
			t.Fatalf("unexpected cost ask for a singleton reveal: %+v", d)
		}
		assertTurnUpEventOnce(t, e, id, mark)
		if e.G.Obj(id).FaceDown {
			t.Fatalf("Watcher of the Roost still face down")
		}
		// The reveal was announced publicly, naming exactly the chosen card.
		note := 0
		for _, ev := range e.L.Events[mark:] {
			if ev.Kind == events.Note && len(ev.IDs) == 1 && ev.IDs[0] == white {
				note++
			}
		}
		if note != 1 {
			t.Fatalf("reveal-cost Notes naming the White Knight = %d, want 1", note)
		}
		// The revealed card stays in hand (a reveal is not a move).
		if o := e.G.Obj(white); o.Zone != state.ZHand {
			t.Fatalf("revealed card moved to %s, want to remain in hand", o.Zone)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("Sacrifice", func(t *testing.T) {
		reg := searchTestRegistry(t)
		e, cfg := manifestEngine(t, reg, "Skirk Volcanist", "Mountain", "Mountain", "Mountain")
		id := morphAndPool(t, e, "Skirk Volcanist", "morphed", "CCCCR", 2, "")
		m1 := putCorpusPermanent(t, e, "Mountain")
		m2 := putCorpusPermanent(t, e, "Mountain")
		// Precondition: both fodder Mountains really are on the battlefield.
		for _, mid := range []state.ObjID{m1, m2} {
			if o := e.G.Obj(mid); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: Mountain %d not on the battlefield", mid)
			}
		}
		mark := len(e.L.Events)
		idx := turnFaceUpIndex(t, e, id)
		submitChoices(t, e, idx)
		// Two candidates for a two-sacrifice cost: no ask is needed (the
		// singleton-shortcut generalises to "all candidates chosen").
		if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
			// If an ask was posed, answer it with every offered sacrifice.
			if d.Options[0].Kind != "sacrifice" {
				t.Fatalf("sacrifice ask kinds = %v", d.Options)
			}
			submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
		}
		assertTurnUpEventOnce(t, e, id, mark)
		if e.G.Obj(id).FaceDown {
			t.Fatalf("Skirk Volcanist still face down")
		}
		for _, mid := range []state.ObjID{m1, m2} {
			if o := e.G.Obj(mid); o == nil || o.Zone != state.ZGraveyard {
				t.Fatalf("Mountain %d zone = %v, want graveyard after the sacrifice cost", mid, o.Zone)
			}
		}
		// No mana was charged: the turn-up cost is only the two Mountains.
		replayCheck(t, e, cfg)
	})

	t.Run("Discard", func(t *testing.T) {
		reg := searchTestRegistry(t)
		e, cfg := manifestEngine(t, reg, "Gathan Raiders", "White Knight")
		id := morphAndPool(t, e, "Gathan Raiders", "morphed", "CCCCCRR", 4, "")
		card := searchMoveByName(t, e, "White Knight", state.ZHand)
		mark := len(e.L.Events)
		idx := turnFaceUpIndex(t, e, id)
		submitChoices(t, e, idx)
		if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
			if d.Options[0].Kind != "discard" {
				t.Fatalf("discard ask kinds = %v", d.Options)
			}
			// Pick exactly the White Knight, so the zone assertion below is
			// about the card the ask actually offered, not option order.
			pick := -1
			for _, o := range d.Options {
				if o.Obj == card {
					pick = o.Index
				}
			}
			if pick < 0 {
				t.Fatalf("the White Knight was not offered as a discard candidate: %+v", d.Options)
			}
			submitChoices(t, e, pick)
		}
		assertTurnUpEventOnce(t, e, id, mark)
		if e.G.Obj(id).FaceDown {
			t.Fatalf("Gathan Raiders still face down")
		}
		if o := e.G.Obj(card); o.Zone != state.ZGraveyard {
			t.Fatalf("discarded card zone = %v, want graveyard", o.Zone)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("Return", func(t *testing.T) {
		reg := searchTestRegistry(t)
		e, cfg := manifestEngine(t, reg, "Raven Guild Initiate", "Storm Crow")
		id := morphAndPool(t, e, "Raven Guild Initiate", "morphed", "CCCU", 1, "")
		bird := putCorpusPermanent(t, e, "Storm Crow")
		if o := e.G.Obj(bird); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Storm Crow not on the battlefield")
		}
		mark := len(e.L.Events)
		idx := turnFaceUpIndex(t, e, id)
		submitChoices(t, e, idx)
		if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
			if d.Options[0].Kind != "returncost" {
				t.Fatalf("return ask kinds = %v", d.Options)
			}
			submitChoices(t, e, d.Options[0].Index)
		}
		assertTurnUpEventOnce(t, e, id, mark)
		if e.G.Obj(id).FaceDown {
			t.Fatalf("Raven Guild Initiate still face down")
		}
		if o := e.G.Obj(bird); o.Zone != state.ZHand {
			t.Fatalf("returned Bird zone = %v, want hand", o.Zone)
		}
		replayCheck(t, e, cfg)
	})
}

// TestMorphTurnFaceUpPaysLife asserts a PayLife<N> turn-up cost charges the
// payer's life exactly once before the TurnFaceUp event.
func TestMorphTurnFaceUpPaysLife(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Zombie Cutthroat")
	id := morphAndPool(t, e, "Zombie Cutthroat", "morphed", "CCCCBB", 3, "")
	mf, ok := morphFaceUpCost(e.G.Obj(id))
	if !ok || mf.cost.Life != 5 {
		t.Fatalf("precondition: parsed turn-up cost = %+v, want PayLife<5>", mf.cost)
	}
	before := e.G.Players[0].Life
	mark := len(e.L.Events)
	idx := turnFaceUpIndex(t, e, id)
	submitChoices(t, e, idx)
	assertTurnUpEventOnce(t, e, id, mark)
	if e.G.Obj(id).FaceDown {
		t.Fatalf("Zombie Cutthroat still face down")
	}
	if got, want := e.G.Players[0].Life, before-5; got != want {
		t.Fatalf("life after turn-up = %d, want %d (PayLife<5>)", got, want)
	}
	replayCheck(t, e, cfg)
}

// TestMorphTurnFaceUpAnnouncesAndPaysX asserts an {X} turn-up cost poses an
// explicit bounded X choice: X=0 is offered when the base mana is payable and
// the announced value is charged exactly. Bane of the Living (Morph:X B B)
// and Aurelia's Vindicator (Disguise:X 3 W) are the two printed X shapes.
func TestMorphTurnFaceUpAnnouncesAndPaysX(t *testing.T) {
	t.Run("MorphXB B", func(t *testing.T) {
		reg := searchTestRegistry(t)
		e, cfg := manifestEngine(t, reg, "Bane of the Living")
		id := morphAndPool(t, e, "Bane of the Living", "morphed", "CCBB", 1, "BBCC")
		// Bane's Morph parameter is X B B; Disguise's rider is absent.
		if o := e.G.Obj(id); o.CastFlags&state.FlagMorphed == 0 {
			t.Fatalf("precondition: Bane not morphed")
		}
		before := e.G.Players[0].Pool.Total()
		mark := len(e.L.Events)
		idx := turnFaceUpIndex(t, e, id)
		submitChoices(t, e, idx)
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
			t.Fatalf("X ask = %+v, want a KChoose of \"x\" options", d)
		}
		// Precondition: the announced range starts at 0 (the base {B}{B}).
		if d.Options[0].Amount != 0 {
			t.Fatalf("lowest offered X = %d, want 0 (base cost {B}{B} payable)", d.Options[0].Amount)
		}
		// Choose X=2 (need one more {B}{B} + {2} generic from the pool).
		pick := -1
		for _, o := range d.Options {
			if o.Amount == 2 {
				pick = o.Index
			}
		}
		if pick < 0 {
			t.Fatalf("X=2 not offered: %+v", d.Options)
		}
		submitChoices(t, e, pick)
		assertTurnUpEventOnce(t, e, id, mark)
		if e.G.Obj(id).FaceDown {
			t.Fatalf("Bane of the Living still face down")
		}
		// {2}{B}{B} == 4 mana spent.
		if got, want := e.G.Players[0].Pool.Total(), before-4; got != want {
			t.Fatalf("pool after X=2 turn-up = %d, want %d", got, want)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("DisguiseX3W", func(t *testing.T) {
		reg := searchTestRegistry(t)
		e, cfg := manifestEngine(t, reg, "Aurelia's Vindicator")
		id := morphAndPool(t, e, "Aurelia's Vindicator", "disguised", "CCCCWW", 3, "CCW")
		if o := e.G.Obj(id); o.CastFlags&state.FlagDisguised == 0 {
			t.Fatalf("precondition: Aurelia's Vindicator not disguised")
		}
		mf, ok := morphFaceUpCost(e.G.Obj(id))
		if !ok || mf.cost.X != 1 || mf.cost.Generic != 3 {
			t.Fatalf("precondition: parsed turn-up cost = %+v, want X 3 W", mf.cost)
		}
		before := e.G.Players[0].Pool.Total()
		mark := len(e.L.Events)
		idx := turnFaceUpIndex(t, e, id)
		submitChoices(t, e, idx)
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
			t.Fatalf("X ask = %+v, want a KChoose of \"x\" options", d)
		}
		// X=0 costs {3}{W}; the pool has exactly {C}{C}{W} left, so pick X=0.
		if d.Options[0].Amount != 0 {
			t.Fatalf("lowest offered X = %d, want 0", d.Options[0].Amount)
		}
		submitChoices(t, e, d.Options[0].Index)
		assertTurnUpEventOnce(t, e, id, mark)
		if e.G.Obj(id).FaceDown {
			t.Fatalf("Aurelia's Vindicator still face down")
		}
		if got, want := e.G.Players[0].Pool.Total(), before-4; got != want {
			t.Fatalf("pool after X=0 turn-up = %d, want %d ({3}{W})", got, want)
		}
		replayCheck(t, e, cfg)
	})
}

// TestMorphTurnFaceUpSacExcludesTheSource proves the shared candidate helper
// the turn-up flow reads (sacrificeCostCandidates, called with the face-down
// permanent as source) never returns the source itself for a creature-other
// sacrifice cost. The turn-up flow passes tp.card as the source to that exact
// helper, so a printed "Sacrifice another creature" cost can never be paid
// with the permanent it is turning face up.
func TestMorphTurnFaceUpSacExcludesTheSource(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _ := manifestEngine(t, reg, "Kin-Tree Warden", "Grizzly Bears")
	id := morphDownCast(t, e, "Kin-Tree Warden", "morphed", "CCCG", 1)
	if o := e.G.Obj(id); !o.FaceDown || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Kin-Tree Warden not a face-down battlefield permanent")
	}
	// PRECONDITION: the face-down source is a creature, so a bare Creature
	// spec DOES match it -- otherwise the Other exclusion is vacuous.
	if !e.matchesSpecFrom("Creature", id, 0, id) {
		t.Fatalf("precondition: the face-down Kin-Tree Warden does not match Creature")
	}
	bare := e.sacrificeCostCandidates(0, id, CostPart{N: 1, Spec: "Creature"}, false)
	if len(bare) != 1 || bare[0] != id {
		t.Fatalf("Creature spec candidates = %v, want exactly the source %d", bare, id)
	}
	other := e.sacrificeCostCandidates(0, id, CostPart{N: 1, Spec: "Creature.Other"}, false)
	for _, oid := range other {
		if oid == id {
			t.Fatalf("Creature.Other candidates include the face-down source itself: %v", other)
		}
	}
	if len(other) != 0 {
		t.Fatalf("Creature.Other candidates = %v, want none (no other creature)", other)
	}
	// The whole turn-up gate agrees: with only the source available for a
	// Creature.Other sacrifice, the action is not offered.
	if e.morphTurnUpPayable(0, id, Cost{Generic: 0, Sac: []CostPart{{N: 1, Spec: "Creature.Other"}}}) {
		t.Fatalf("morphTurnUpPayable accepted a Creature.Other sacrifice payable only by the source")
	}
}

// TestMorphTurnFaceUpBotAnswerValidates drives every turn-up cost ask through
// the SAME bot policy the hosted seat uses (botpolicy.Decide via newTestBot /
// BoardFromGame) and submits the bot's own answer through Engine.Submit, which
// runs Decision.Validate. It pins the class the brief worries about: a new
// decision constraint that the bot's answer violates would let the
// deterministic bot re-submit the same rejected intent forever (a livelock).
// Each subtest asserts a real ask was reached (a non-vacuous one) and that the
// bot's answer was accepted and actually paid the cost.
func TestMorphTurnFaceUpBotAnswerValidates(t *testing.T) {
	t.Run("X", func(t *testing.T) {
		reg := searchTestRegistry(t)
		e, cfg := manifestEngine(t, reg, "Bane of the Living")
		id := morphAndPool(t, e, "Bane of the Living", "morphed", "CCBB", 1, "BBCC")
		bot := newTestBot(7)
		before := e.G.Players[0].Pool.Total()
		mark := len(e.L.Events)
		submitChoices(t, e, turnFaceUpIndex(t, e, id))
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || len(d.Options) < 2 || d.Options[0].Kind != "x" {
			t.Fatalf("X ask = %+v, want a KChoose with at least two \"x\" options", d)
		}
		// PRECONDITION: the offer is a real ascending range, so the bot's
		// highest-X pick is a non-first, non-vacuous answer.
		if d.Options[len(d.Options)-1].Amount <= d.Options[0].Amount {
			t.Fatalf("X options do not ascend: %+v", d.Options)
		}
		in := bot.answer(e, d)
		if err := e.Submit(in); err != nil {
			t.Fatalf("the bot's own X answer %v was rejected by Decision.Validate: %v", in.Choices, err)
		}
		assertTurnUpEventOnce(t, e, id, mark)
		if e.G.Obj(id).FaceDown {
			t.Fatalf("Bane of the Living still face down after the bot's X answer")
		}
		// The bot's announced X was PRICED: {X}{B}{B} costs x generic plus
		// {B}{B}. Correcting for the pool the action leaves is what proves the
		// bogus X=0 pricing does not hide here.
		chosenX := d.Options[in.Choices[0]].Amount
		if chosenX <= 0 {
			t.Fatalf("the bot's X pick = %d, want a positive announcement (a non-vacuous answer)", chosenX)
		}
		if got, want := e.G.Players[0].Pool.Total(), before-(int32(chosenX)+2); got != want {
			t.Fatalf("pool after the bot's X=%d turn-up = %d, want %d", chosenX, got, want)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("Sacrifice", func(t *testing.T) {
		reg := searchTestRegistry(t)
		e, cfg := manifestEngine(t, reg, "Skirk Volcanist", "Mountain", "Mountain", "Mountain", "Mountain")
		id := morphAndPool(t, e, "Skirk Volcanist", "morphed", "CCCCR", 2, "")
		bot := newTestBot(11)
		// Seat four Mountains so the Sac<2/Mountain> ask offers MORE than the
		// two it needs -- the ask is a real Min==Max==2 choice, not the
		// singleton no-ask shortcut.
		for i := 0; i < 4; i++ {
			putCorpusPermanent(t, e, "Mountain")
		}
		mark := len(e.L.Events)
		submitChoices(t, e, turnFaceUpIndex(t, e, id))
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "sacrifice" {
			t.Fatalf("sacrifice ask = %+v, want a KChoose of \"sacrifice\" options", d)
		}
		// PRECONDITION: the ask really is a choice between more candidates
		// than it needs, so the bot's pick exercises the Min/Max constraint.
		if len(d.Options) <= int(d.Min) {
			t.Fatalf("sacrifice ask options = %d, want more than Min %d (a real choice)", len(d.Options), d.Min)
		}
		in := bot.answer(e, d)
		if err := e.Submit(in); err != nil {
			t.Fatalf("the bot's own sacrifice answer %v was rejected by Decision.Validate: %v", in.Choices, err)
		}
		assertTurnUpEventOnce(t, e, id, mark)
		if e.G.Obj(id).FaceDown {
			t.Fatalf("Skirk Volcanist still face down after the bot's sacrifice answer")
		}
		// Exactly two Mountains were sacrificed -- the bot's answer paid the
		// printed Sac<2/Mountain> cost, not fewer.
		buried := 0
		for _, ev := range e.L.Events[mark:] {
			if events.IsSacrifice(ev) && e.G.Obj(ev.Obj).Zone == state.ZGraveyard {
				buried++
			}
		}
		if buried != 2 {
			t.Fatalf("sacrifices after the bot's answer = %d, want 2 (Sac<2/Mountain>)", buried)
		}
		replayCheck(t, e, cfg)
	})
}

// TestMorphTurnUpCostFailsClosedOnUnmodelledShapes pins the two ways a
// morph-family turn-up keyword parameter is refused rather than silently
// waived: an unmodelled cost token (the Disguise cost-reduction rider
// `R:X:...`, Fugitive Codebreaker's `Disguise:5 R:X:...`) leaves Cost.Unknown
// non-empty, and a variable-count non-mana part (Sac<X/Spec>) carries an
// announced count the turn-up flow cannot pose, so morphFaceUpCost refuses
// the whole action instead of paying zero objects.
func TestMorphTurnUpCostFailsClosedOnUnmodelledShapes(t *testing.T) {
	// Fugitive Codebreaker's exact printed Disguise parameter (the one corpus
	// carrier of a cost-reduction rider).
	raw := "5 R:X:This cost is reduced by {1} for each instant and sorcery card in your graveyard."
	c := ParseCost(raw)
	if len(c.Unknown) == 0 {
		t.Fatalf("ParseCost(%q) reported no Unknown; morphFaceUpCost would silently waive the reduction rider", raw)
	}

	// A Sac<X/Spec> part is Announced; the guard refuses it.
	variable := Cost{Sac: []CostPart{{Spec: "Creature", Announced: true}}}
	if !morphTurnUpCountAnnounced(variable) {
		t.Fatalf("morphTurnUpCountAnnounced(%+v) = false, want true for an announced Sac count", variable.Sac)
	}
	// PRECONDITION: the same spec WITHOUT Announced is NOT refused, so the
	// guard is specific to the variable form rather than to Sac parts at all.
	fixed := Cost{Sac: []CostPart{{N: 1, Spec: "Creature"}}}
	if morphTurnUpCountAnnounced(fixed) {
		t.Fatalf("morphTurnUpCountAnnounced(%+v) = true, want false for a fixed-count Sac", fixed.Sac)
	}
}

// TestMorphTurnUpCostParsesMultiXShape pins the two-X printed form. The
// corpus prints `X X R` (Warbreak Trumpeter) as well as `X B B` and `X 3 W`,
// and CR 601.2b makes every {X} symbol the SAME announced value, so the
// payment is 2*X generic plus {R}. WithX folds exactly one generic per X
// symbol; a parser that collapsed the repeated symbol would undercharge.
func TestMorphTurnUpCostParsesMultiXShape(t *testing.T) {
	c := ParseCost("X X R")
	if c.X != 2 {
		t.Fatalf("ParseCost(\"X X R\").X = %d, want 2", c.X)
	}
	if c.Colored[state.ManaIndex('R')] != 1 {
		t.Fatalf("ParseCost(\"X X R\") red pips = %d, want 1", c.Colored[state.ManaIndex('R')])
	}
	// PRECONDITION: the multi-X cost differs from the single-X one, so the
	// X-count assertion above is not vacuous.
	single := ParseCost("X R")
	if single.X != 1 {
		t.Fatalf("ParseCost(\"X R\").X = %d, want 1", single.X)
	}
	if got, want := c.WithX(2).Generic, single.WithX(2).Generic+2; got != want {
		t.Fatalf("WithX(2) generic for \"X X R\" = %d, want %d (two X symbols)", got, want)
	}
}
