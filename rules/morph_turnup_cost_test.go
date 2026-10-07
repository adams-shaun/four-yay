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
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
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
	t.Parallel()
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
	t.Parallel()
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

	// PayLife is a non-mana cost too. The ORIGINAL mana/life-only turn-up
	// implementation already charged Cost.Life through payMana, so this subtest
	// is a REGRESSION GUARD for the pre-existing life charge rather than proof
	// of this ticket's new hunk; it lives inside the test that IS proven to
	// fail without the non-mana payment so the coverage is attached to a test
	// that can fail (the standalone life test could not).
	t.Run("PayLife", func(t *testing.T) {
		reg := searchTestRegistry(t)
		e, cfg := manifestEngine(t, reg, "Zombie Cutthroat")
		id := morphAndPool(t, e, "Zombie Cutthroat", "morphed", "CCCCBB", 3, "")
		mf, ok := morphFaceUpCost(e.G.Obj(id))
		if !ok || mf.cost.Life != 5 {
			t.Fatalf("precondition: parsed turn-up cost = %+v, want PayLife<5>", mf.cost)
		}
		before := e.G.Players[0].Life
		mark := len(e.L.Events)
		submitChoices(t, e, turnFaceUpIndex(t, e, id))
		assertTurnUpEventOnce(t, e, id, mark)
		if e.G.Obj(id).FaceDown {
			t.Fatalf("Zombie Cutthroat still face down")
		}
		if got, want := e.G.Players[0].Life, before-5; got != want {
			t.Fatalf("life after turn-up = %d, want %d (PayLife<5>)", got, want)
		}
		replayCheck(t, e, cfg)
	})
}

// TestMorphTurnFaceUpTriggerUsesAnnouncedX proves CR 107.3m binds the
// turn-up announcement to the carrier's TurnFaceUp trigger, not only to the
// mana payment. Bane's printed trigger reads Count$xPaid; with X=2 its -2/-2
// kills the real 2/2 Grizzly Bears, whereas an unbound X leaves it alive.
func TestMorphTurnFaceUpTriggerUsesAnnouncedX(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Bane of the Living", "Grizzly Bears")
	id := morphAndPool(t, e, "Bane of the Living", "morphed", "CCBB", 1, "BBCC")
	bear := putCorpusPermanent(t, e, "Grizzly Bears")
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || e.Derived(bear).Power != 2 || e.Derived(bear).Toughness != 2 {
		t.Fatalf("precondition: Grizzly Bears=%+v derived=%+v, want battlefield 2/2", o, e.Derived(bear))
	}
	mark := len(e.L.Events)
	submitChoices(t, e, turnFaceUpIndex(t, e, id))
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("X ask = %+v, want Bane's bounded X announcement", d)
	}
	pick := -1
	for _, opt := range d.Options {
		if opt.Amount == 2 {
			pick = opt.Index
		}
	}
	if pick < 0 {
		t.Fatalf("X=2 not offered: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	foundX := false
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.TurnFaceUp && ev.Obj == id {
			foundX = ev.Amount == 2
		}
	}
	if !foundX {
		t.Fatalf("TurnFaceUp event did not carry announced X=2: %+v", e.L.Events[mark:])
	}
	answerQuiet(t, e, 60)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Bane's -X/-X trigger did not use announced X=2; Grizzly Bears=%+v", o)
	}
	replayCheck(t, e, cfg)
}

// TestMorphTurnFaceUpAnnouncesAndPaysX asserts an {X} turn-up cost poses an
// explicit bounded X choice: X=0 is offered when the base mana is payable and
// the announced value is charged exactly. Bane of the Living (Morph:X B B)
// and Aurelia's Vindicator (Disguise:X 3 W) are the two printed X shapes.
func TestMorphTurnFaceUpAnnouncesAndPaysX(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	bare := pay.SacrificeCostCandidates(asPayer(e), 0, id, CostPart{N: 1, Spec: "Creature"}, false)
	if len(bare) != 1 || bare[0] != id {
		t.Fatalf("Creature spec candidates = %v, want exactly the source %d", bare, id)
	}
	other := pay.SacrificeCostCandidates(asPayer(e), 0, id, CostPart{N: 1, Spec: "Creature.Other"}, false)
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
	if e.morphTurnUpPayable(0, id, Cost{Generic: 0, Sac: []CostPart{{N: 1, Spec: "Creature.Other"}}}, costMods{}) {
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
	t.Parallel()
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
// waived, END TO END through morphFaceUpCost and the offer:
//
//   - a genuinely unmodelled cost token (a synthetic `Y` morph parameter)
//     leaves Cost.Unknown non-empty, so the face-down permanent offers NO
//     turn_face_up action at all;
//   - a variable-count non-mana part (Sac<X/Spec>) carries an announced
//     count the turn-up flow cannot pose, so morphFaceUpCost refuses the
//     whole action instead of paying zero objects.
//
// The Disguise cost-reduction rider (`R:X:...`, Fugitive Codebreaker's printed
// parameter) used to be the Part A fixture, but that was the defect: the whole
// KeywordParam remainder was handed to ParseCost. cards.Face.KeywordCostParam
// now reads the cost as the first colon-field (Forge's KeywordWithCost rule),
// so the rider's SVar name and reminder text no longer reach ParseCost and the
// Disguise turn-up is offered for the full printed cost -- see
// TestFugitiveCodebreakerDisguiseTurnFaceUpOffersAndChargesFullCost.
//
// Both assertions go through morphFaceUpCost, so removing either guard from
// that function turns this test red (a direct ParseCost or
// morphTurnUpCountAnnounced assertion would not).
func TestMorphTurnUpCostFailsClosedOnUnmodelledShapes(t *testing.T) {
	t.Parallel()

	// Part A: a synthetic morph carrier whose turn-up cost is an unrecognised
	// token. ParseCost reports the token through Cost.Unknown (and charges one
	// phantom generic), so a face-down permanent must offer no turn_face_up
	// action. The fixture is inline rather than a corpus card precisely because
	// a real carrier's shaped keyword parameter is now read correctly.
	raw := "Y"
	if c := ParseCost(raw); len(c.Unknown) == 0 {
		t.Fatalf("precondition: ParseCost(%q) reported no Unknown", raw)
	}
	reg := searchTestRegistry(t)
	e, _ := manifestEngine(t, reg)
	id := onBoard(t, e, 0, "Name:Unmodelled-cost morph\nTypes:Creature\nK:Morph:Y\nOracle:x\n")
	e.emit(events.Event{Kind: events.TurnFaceDown, Obj: id})
	e.emit(events.Event{Kind: events.CastInfo, Obj: id, Counter: events.FlagsString(state.FlagMorphed)})
	// PRECONDITION: the permanent really is a face-down morph carrier, so the
	// withheld offer below is about the unmodelled cost and not about a missing
	// family flag.
	if o := e.G.Obj(id); !o.FaceDown || o.CastFlags&state.FlagMorphed == 0 {
		t.Fatalf("precondition: fixture faceDown=%v flags=%d, want a face-down morph carrier", o.FaceDown, o.CastFlags)
	}
	if _, ok := morphFaceUpCost(e.G.Obj(id)); ok {
		t.Fatalf("morphFaceUpCost accepted the unmodelled cost %q; it must fail closed", raw)
	}
	if turnFaceUpOptionPresent(t, e, id) {
		t.Fatalf("turn_face_up offered for a cost carrying an unmodelled token: the token was silently waived")
	}

	// Part B: a Sac<X/Spec> part is the variable form. morphFaceUpCost reads
	// the face's keyword parameter, so an inline face-down morph carrier with
	// that exact parameter is the fixture. PRECONDITION: the same fixture with
	// a FIXED count IS accepted, so the refusal below is specific to the
	// announced form and not to any Sac cost at all.
	fixed := onBoard(t, e, 0, "Name:Fixed-count morph\nTypes:Creature\nK:Morph:Sac<1/Creature>\nOracle:x\n")
	e.emit(events.Event{Kind: events.TurnFaceDown, Obj: fixed})
	e.emit(events.Event{Kind: events.CastInfo, Obj: fixed, Counter: events.FlagsString(state.FlagMorphed)})
	if f, ok := morphFaceUpCost(e.G.Obj(fixed)); !ok || len(f.cost.Sac) != 1 || f.cost.Sac[0].Announced {
		t.Fatalf("precondition: fixed-count fixture not accepted as a fixed Sac cost: %+v ok=%v", f.cost, ok)
	}
	variable := onBoard(t, e, 0, "Name:Variable-count morph\nTypes:Creature\nK:Morph:Sac<X/Creature>\nOracle:x\n")
	e.emit(events.Event{Kind: events.TurnFaceDown, Obj: variable})
	e.emit(events.Event{Kind: events.CastInfo, Obj: variable, Counter: events.FlagsString(state.FlagMorphed)})
	if _, ok := morphFaceUpCost(e.G.Obj(variable)); ok {
		t.Fatalf("morphFaceUpCost accepted an announced-count Sac<X/Creature> turn-up cost; the flow would pay zero objects")
	}
}

// TestMorphTurnUpCostParsesMultiXShape drives the two-X printed form END TO
// END: Warbreak Trumpeter's Morph parameter is `X X R`, and CR 601.2b makes
// every {X} symbol the SAME announced value, so X=2 costs {2}{2}{R} -- four
// generic plus one red. The turn-up must pose the explicit X ask, offer X=2,
// charge exactly that, and flip the permanent. A payment that folded a single
// generic per cost (or priced X as zero) fails the pool assertion; removing
// the X ask from the turn-up flow fails the ask assertion.
func TestMorphTurnUpCostParsesMultiXShape(t *testing.T) {
	t.Parallel()
	// The parse half is the precondition the end-to-end half depends on: the
	// parameter really parses to X==2 plus {R}, and WithX folds two generics.
	c := ParseCost("X X R")
	if c.X != 2 {
		t.Fatalf("precondition: ParseCost(\"X X R\").X = %d, want 2", c.X)
	}
	if c.Colored[state.ManaIndex('R')] != 1 {
		t.Fatalf("precondition: ParseCost(\"X X R\") red pips = %d, want 1", c.Colored[state.ManaIndex('R')])
	}
	single := ParseCost("X R")
	if single.X != 1 {
		t.Fatalf("precondition: ParseCost(\"X R\").X = %d, want 1", single.X)
	}
	if got, want := c.WithX(2).Generic, single.WithX(2).Generic+2; got != want {
		t.Fatalf("precondition: WithX(2) generic for \"X X R\" = %d, want %d (two X symbols)", got, want)
	}

	reg := searchTestRegistry(t)
	// Warbreak Trumpeter prints {R}; the face-down cast is {3}, so fund {4}
	// (three for the cast, one leftover {R}) and add {C}{C}{C}{R} for the
	// turn-up. `morphDownCast` asserts exactly the {3} is spent.
	e, cfg := manifestEngine(t, reg, "Warbreak Trumpeter")
	id := morphAndPool(t, e, "Warbreak Trumpeter", "morphed", "CCCR", 1, "CCCCR")
	mf, ok := morphFaceUpCost(e.G.Obj(id))
	if !ok || mf.cost.X != 2 {
		t.Fatalf("precondition: Warbreak Trumpeter turn-up cost = %+v, want X==2 (X X R)", mf.cost)
	}
	before := e.G.Players[0].Pool.Total()
	mark := len(e.L.Events)
	submitChoices(t, e, turnFaceUpIndex(t, e, id))
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("X ask = %+v, want a KChoose of \"x\" options (X X R must announce a value)", d)
	}
	// PRECONDITION: X=2 is actually offered and is not the lowest offer, so
	// the pool assertion below is about the announced value and not about X=0.
	if d.Options[0].Amount != 0 {
		t.Fatalf("lowest offered X = %d, want 0 (base {R} payable)", d.Options[0].Amount)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Amount == 2 {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("X=2 not offered for X X R: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	assertTurnUpEventOnce(t, e, id, mark)
	if e.G.Obj(id).FaceDown {
		t.Fatalf("Warbreak Trumpeter still face down")
	}
	// X X R at X=2 is {2}{2}{R}: four generic plus the red pip.
	if got, want := e.G.Players[0].Pool.Total(), before-5; got != want {
		t.Fatalf("pool after X=2 turn-up (X X R) = %d, want %d ({2}{2}{R})", got, want)
	}
	replayCheck(t, e, cfg)
}

// TestMorphTurnUpCostInvalidatedChoiceAbortsThePayment proves the settlement
// re-derives every saved cost object against the live board before anything
// moves (turnUpChoicesValid). Skirk Volcanist's turn-up cost is Sac<2/Mountain>
// and the ask carries three eligible Mountains (a real choice, not the forced
// singleton shortcut); while that ask is pending, one offered Mountain dies.
// The stale answer still names it, so the whole payment must abort with
// NOTHING settled: the dead Mountain is not "sacrificed" a second time out of
// its new zone, the surviving offered Mountain stays on the battlefield, the
// face-down permanent never flips, and only the abort Note is recorded. A
// settlement that trusted the saved IDs would half-pay the cost and still
// emit the TurnFaceUp, which is exactly what this test pins shut.
func TestMorphTurnUpCostInvalidatedChoiceAbortsThePayment(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Skirk Volcanist", "Mountain", "Mountain", "Mountain", "Mountain")
	id := morphDownCast(t, e, "Skirk Volcanist", "morphed", "CCCCR", 2)
	mf, ok := morphFaceUpCost(e.G.Obj(id))
	if !ok || len(mf.cost.Sac) != 1 || mf.cost.Sac[0].N != 2 {
		t.Fatalf("precondition: parsed turn-up cost = %+v, want Sac<2/Mountain>", mf.cost)
	}
	// Three Mountains so the sacrifice ask is a genuine choice; two would be
	// auto-recorded without an ask and there would be no stale-answer window.
	m1 := putCorpusPermanent(t, e, "Mountain")
	m2 := putCorpusPermanent(t, e, "Mountain")
	m3 := putCorpusPermanent(t, e, "Mountain")
	if m1 == m2 || m2 == m3 || m1 == m3 {
		t.Fatalf("precondition: three distinct Mountains required")
	}
	if got := len(e.G.Zone(state.ZBattlefield, 0)); got != 4 {
		t.Fatalf("precondition: seat 0 battlefield holds %d permanents, want 4 (source + 3 Mountains)", got)
	}
	submitChoices(t, e, turnFaceUpIndex(t, e, id))
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 2 || d.Max != 2 {
		t.Fatalf("precondition: sacrifice ask = %+v, want an exact-2 KChoose", d)
	}
	idx := map[state.ObjID]int{}
	for _, o := range d.Options {
		if o.Kind == "sacrifice" {
			idx[o.Obj] = o.Index
		}
	}
	for _, m := range []state.ObjID{m1, m2, m3} {
		if _, ok := idx[m]; !ok {
			t.Fatalf("precondition: Mountain %d not offered by the sacrifice ask: %+v", m, d.Options)
		}
	}
	// The invalidation window: while the ask is pending, m2 leaves the
	// battlefield. The engine never drives this itself (a special action
	// cannot be responded to) — a replacement or a death trigger on another
	// part's payment event can — so the test drives the same board change
	// through a logged event at the exact point the flow is mid-payment.
	mark := len(e.L.Events)
	e.emit(events.Sacrifice(m2))
	if o := e.G.Obj(m2); o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: invalidated Mountain zone = %v, want graveyard", o.Zone)
	}
	// The stale answer still names the dead Mountain. The payment must abort
	// as a whole: no sacrifice of m3, no TurnFaceUp, no mana moved. The
	// assertion window starts AFTER the invalidation event, so only what the
	// flow itself did is scanned.
	mark = len(e.L.Events)
	submitChoices(t, e, idx[m2], idx[m3])
	if o := e.G.Obj(id); !o.FaceDown {
		t.Fatalf("the face-down permanent was turned up by a cost it no longer owes in full")
	}
	if got := e.G.Obj(m3).Zone; got != state.ZBattlefield {
		t.Fatalf("surviving offered Mountain settled anyway: zone = %v, want battlefield", got)
	}
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.TurnFaceUp {
			t.Fatalf("TurnFaceUp emitted after the cost object was invalidated: %+v", ev)
		}
		if ev.Kind == events.MoveZone && (ev.Obj == m2 || ev.Obj == m3) {
			t.Fatalf("a zone change settled after the cost object was invalidated: %+v", ev)
		}
	}
	aborted := false
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "no longer payable") {
			aborted = true
		}
	}
	if !aborted {
		t.Fatalf("no abort Note after the invalidated answer; the flow neither paid nor explained: %+v", e.L.Events[mark:])
	}
	// The flow is over and priority is back with seat 0.
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the abort, pending = %+v, want seat 0's priority back", d)
	}
	replayCheck(t, e, cfg)
}
