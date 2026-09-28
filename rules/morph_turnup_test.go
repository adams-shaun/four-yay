// morph_turnup_test.go — the CR 708.6 / CR 116.2b morph-family turn-face-up
// SPECIAL ACTION. Each corpus carrier proves, end to end: a face-down
// permanent its controller cast with Morph, Megamorph or Disguise offers a
// "turn_face_up" priority option BESIDE the other priority actions, the
// option is offered only to its controller, choosing it pays the keyword's
// own printed turn-up cost (never the {3} face-down cast), the action uses
// no stack (no PutOnStack, the stack stays empty), the event-derived
// FaceDown state flips so the printed characteristics are live again,
// Megamorph's rider puts a +1/+1 counter, Disguise's face-down ward {2} is
// enforced while face down and is gone once face up, and the whole game
// replays byte-identically from the log.
//
// A manifest or cloak carrier (no morph family flag) is deliberately never
// offered this action, so the existing manifest/cloak turn-up rules are
// untouched: the family flag is the gate, and morphFaceUpCost reports false
// for a face-down permanent the morph family did not put down.
//
// The decks are compiled corpus cards only (no Forge script text is
// committed here). The helpers come from morph_test.go, cast_test.go,
// manifest_test.go and search_library_test.go. No repo deck carries a morph
// carrier, so these tests do not move the golden heads or the botbench pin.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// turnFaceUpIndex returns the seat-0 pending priority decision's turn_face_up
// option index for id, failing the test when the option is absent. It also
// asserts the pending decision belongs to seat 0 (the controller), so a test
// that forgot to pass priority cannot silently read the opponent's list.
func turnFaceUpIndex(t *testing.T, e *Engine, id state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	if d.Player != 0 {
		t.Fatalf("priority decision is seat %d's, want seat 0's", d.Player)
	}
	for _, o := range d.Options {
		if o.Kind == "turn_face_up" && o.Obj == id {
			return o.Index
		}
	}
	t.Fatalf("no turn_face_up option for %d: %+v", id, d.Options)
	return -1
}

// assertNoTurnFaceUpForSeat checks seat 1 is never offered the turn_face_up
// action for a permanent seat 0 controls.
func assertNoTurnFaceUpForSeat(t *testing.T, e *Engine) {
	t.Helper()
	for _, o := range e.legalActions(1) {
		if o.Kind == "turn_face_up" {
			t.Fatalf("seat 1 was offered turn_face_up for a permanent it does not control: %+v", o)
		}
	}
}

// assertTurnUpEventOnce checks the log records exactly one TurnFaceUp for id,
// emitted by the picked action (after mark), and no PutOnStack for it after
// mark (the no-stack-use proof).
func assertTurnUpEventOnce(t *testing.T, e *Engine, id state.ObjID, mark int) {
	t.Helper()
	turnUps, pushes := 0, 0
	for _, ev := range e.L.Events[mark:] {
		switch {
		case ev.Kind == events.TurnFaceUp && ev.Obj == id:
			turnUps++
		case ev.Kind == events.PutOnStack && ev.Obj == id:
			pushes++
		}
	}
	if turnUps != 1 {
		t.Fatalf("TurnFaceUp events for %d after the action = %d, want 1", id, turnUps)
	}
	if pushes != 0 {
		t.Fatalf("the turn-face-up action put %d object(s) on the stack, want 0 (a special action, CR 116.2b)", pushes)
	}
}

func TestMorphTurnFaceUpIsASpecialActionPaysItsKeywordCost(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Kin-Tree Warden")
	id := morphDownCast(t, e, "Kin-Tree Warden", "morphed", "CCCG", 1)
	// Precondition: a face-down 2/2 whose printed 1/1 is hidden, and the pool
	// holds exactly the leftover {G} that happens to fund this card's morph
	// turn-up cost.
	if o := e.G.Obj(id); !o.FaceDown || e.Derived(id).Power != 2 || e.Derived(id).Toughness != 2 {
		t.Fatalf("precondition: Kin-Tree Warden faceDown=%v derived=%d/%d, want face-down 2/2",
			e.G.Obj(id).FaceDown, e.Derived(id).Power, e.Derived(id).Toughness)
	}

	assertNoTurnFaceUpForSeat(t, e)
	idx := turnFaceUpIndex(t, e, id)
	mark := len(e.L.Events)
	before := e.G.Players[0].Pool.Total()
	submitChoices(t, e, idx)

	// No stack use: the action resolved immediately.
	if len(e.G.Stack) != 0 {
		t.Fatalf("turn-face-up left %d object(s) on the stack, want 0", len(e.G.Stack))
	}
	assertTurnUpEventOnce(t, e, id, mark)
	o := e.G.Obj(id)
	if o.FaceDown {
		t.Fatalf("Kin-Tree Warden FaceDown=true after the turn-up, want false")
	}
	// The printed 1/1 is live again (CR 708.8), and the turn-up cost was the
	// keyword's printed {G}, leaving the pool at exactly before-1.
	if der := e.Derived(id); der.Power != 1 || der.Toughness != 1 {
		t.Fatalf("after turn-up Kin-Tree Warden P/T = %d/%d, want its printed 1/1", der.Power, der.Toughness)
	}
	if got, want := e.G.Players[0].Pool.Total(), before-1; got != want {
		t.Fatalf("pool after morph turn-up = %d, want %d (the printed {G} morph cost)", got, want)
	}
	// Once face up, the action is no longer offered for this permanent.
	if len(e.Pending().Options) > 0 {
		for _, opt := range e.Pending().Options {
			if opt.Kind == "turn_face_up" && opt.Obj == id {
				t.Fatalf("turn_face_up still offered for the now face-up permanent: %+v", opt)
			}
		}
	}
	replayCheck(t, e, cfg)
}

// TestMorphTurnFaceUpIsOfferedWithASpellOnTheStack proves the action is not
// sorcery-gated (CR 708.6: turn it face up "any time you have priority"):
// while seat 1's bolt sits unresolved on the stack and seat 0 holds the
// response priority, the face-down permanent's turn_face_up option is still
// offered. This is the "regardless of timing" half of the contract.
func TestMorphTurnFaceUpIsOfferedWithASpellOnTheStack(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := manifestEngine(t, reg, "Kin-Tree Warden")
	id := morphDownCast(t, e, "Kin-Tree Warden", "morphed", "CCCG", 1)
	if o := e.G.Obj(id); !o.FaceDown {
		t.Fatalf("precondition: Kin-Tree Warden is not face down")
	}
	// Seat 1 casts a bolt at seat 0 (the passPriorityTimes inside seatBolt
	// hands seat 1 priority).
	bolt := seatBolt(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 1 {
		t.Fatalf("expected seat 1 priority, got %+v", d)
	}
	opt := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bolt {
			opt = o.Index
		}
	}
	if opt < 0 {
		t.Fatalf("bolt not offered to seat 1: %+v", d.Options)
	}
	submitChoices(t, e, opt)
	dt := e.Pending()
	if dt == nil || dt.Kind != decision.KTarget {
		t.Fatalf("bolt target ask: %+v", dt)
	}
	idx := -1
	for _, o := range dt.Options {
		if o.Kind == "player" && o.Player == 0 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no seat-0 player target option: %+v", dt.Options)
	}
	submitChoices(t, e, idx)
	// Pass back to seat 0 so it holds the response priority. The bolt must
	// still be on the stack (a non-empty stack is the non-sorcery window).
	for i := 0; i < 3; i++ {
		r := e.Pending()
		if r == nil || r.Kind != decision.KPriority || r.Player == 0 {
			break
		}
		passPriorityTimes(t, e, 1)
	}
	if len(e.G.Stack) == 0 {
		t.Fatalf("precondition: the bolt is not on the stack")
	}
	resp := e.Pending()
	if resp == nil || resp.Kind != decision.KPriority || resp.Player != 0 {
		t.Fatalf("expected seat 0's response priority, got %+v", resp)
	}
	found := false
	for _, o := range resp.Options {
		if o.Kind == "turn_face_up" && o.Obj == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("turn_face_up not offered with a spell on the stack: %+v", resp.Options)
	}
}

func TestMegamorphTurnFaceUpAddsAPlusOnePlusOneCounter(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Kolaghan Stormsinger")
	id := morphDownCast(t, e, "Kolaghan Stormsinger", "megamorphed", "CCCR", 1)
	if o := e.G.Obj(id); !o.FaceDown || o.CastFlags&state.FlagMegamorphed == 0 {
		t.Fatalf("precondition: faceDown=%v flags=%v, want a face-down megamorphed permanent",
			e.G.Obj(id).FaceDown, e.G.Obj(id).CastFlags)
	}

	assertNoTurnFaceUpForSeat(t, e)
	idx := turnFaceUpIndex(t, e, id)
	mark := len(e.L.Events)
	submitChoices(t, e, idx)

	// No stack use by the ACTION: the permanent itself never went on the
	// stack -- the TurnFaceUp event made it face up in place. Kolaghan
	// Stormsinger's own printed "when turned face up" trigger legitimately
	// uses the stack (a real TurnFaceUp match), so it is answered and drained
	// before the board assertions below.
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("after megamorph turn-up the permanent is in %s, want the battlefield (a special action never stacks it)", o.Zone)
	}
	assertTurnUpEventOnce(t, e, id, mark)
	if dr := e.Pending(); dr != nil && dr.Kind == decision.KTarget {
		submitChoices(t, e, dr.Options[0].Index)
	}
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(id)
	if o.FaceDown {
		t.Fatalf("Kolaghan Stormsinger FaceDown=true after the turn-up, want false")
	} // The megamorph rider: exactly one +1/+1 counter, recorded by a
	// CounterChange the replay folds into Object.Counters.
	n := int32(0)
	for _, c := range o.Counters {
		if c.Kind == "P1P1" {
			n += c.N
		}
	}
	if n != 1 {
		t.Fatalf("P1P1 counters after megamorph turn-up = %d, want 1 (counters=%+v)", n, o.Counters)
	}
	counterEvents := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.CounterChange && ev.Obj == id && ev.Counter == "P1P1" && ev.Amount == 1 {
			counterEvents++
		}
	}
	if counterEvents != 1 {
		t.Fatalf("CounterChange P1P1 events after the turn-up = %d, want 1", counterEvents)
	}
	// Printed 1/1 plus the +1/+1 counter: 2/2 on the battlefield.
	if der := e.Derived(id); der.Power != 2 || der.Toughness != 2 {
		t.Fatalf("after megamorph turn-up P/T = %d/%d, want 2/2 (printed 1/1 + one +1/+1 counter)", der.Power, der.Toughness)
	}
	replayCheck(t, e, cfg)
}

// seatBolt puts a Lightning-Bolt-shaped fixture into seat 1's hand and funds
// it, then passes seat 0's priority so seat 1 holds it. The bolt's text is
// written inline (never committed Forge script).
func seatBolt(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	bolt := e.G.AddObject(card(t, "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"), 1)
	e.G.SetZone(state.ZHand, 1, append(e.G.Zone(state.ZHand, 1), bolt.ID))
	addMana(t, e, 1, "R")
	passPriorityTimes(t, e, 1)
	return bolt.ID
}

// boltAtTarget has seat 1 cast the bolt at target and drains to the ward ask.
func boltAtTarget(t *testing.T, e *Engine, bolt, target state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 1 {
		t.Fatalf("expected seat 1 priority, got %+v", d)
	}
	opt := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bolt {
			opt = o.Index
		}
	}
	if opt < 0 {
		t.Fatalf("bolt not offered to seat 1: %+v", d.Options)
	}
	submitChoices(t, e, opt)
	dt := e.Pending()
	if dt == nil || dt.Kind != decision.KTarget {
		t.Fatalf("bolt target ask: %+v", dt)
	}
	idx := -1
	for _, o := range dt.Options {
		if o.Obj == target {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("bolt target options missing %d: %+v", target, dt.Options)
	}
	submitChoices(t, e, idx)
	passPriorityTimes(t, e, 2)
}

func TestDisguiseTurnFaceUpWardTwoIsEnforcedWhileFaceDown(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := manifestEngine(t, reg, "Basilica Stalker")
	id := morphDownCast(t, e, "Basilica Stalker", "disguised", "CCCCCB", 3)
	// Precondition: the derived ward {2} really is on the face-down 2/2 (the
	// cloak status grant), not merely the printed keyword.
	if !hasDerivedKeyword(e.Derived(id).Keywords, "Ward:2") {
		t.Fatalf("precondition: face-down disguise keywords = %v, want Ward:2", e.Derived(id).Keywords)
	}
	bolt := seatBolt(t, e)
	boltAtTarget(t, e, bolt, id)
	dw := e.Pending()
	if dw == nil || dw.Kind != decision.KModes || dw.Player != 1 {
		t.Fatalf("no ward pay-or-decline ask while disguised: %+v", dw)
	}
	// Decline: the ward counters the bolt and the face-down permanent lives.
	submitChoices(t, e, dw.Options[len(dw.Options)-1].Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || !o.FaceDown || o.Damage != 0 {
		t.Fatalf("after the ward declined: zone=%s faceDown=%v damage=%d, want an untouched face-down permanent",
			o.Zone, o.FaceDown, o.Damage)
	}
}

func TestDisguiseTurnFaceUpEndsTheWardTwo(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Basilica Stalker")
	id := morphDownCast(t, e, "Basilica Stalker", "disguised", "CCCCCB", 3)
	if !hasDerivedKeyword(e.Derived(id).Keywords, "Ward:2") {
		t.Fatalf("precondition: face-down disguise keywords = %v, want Ward:2", e.Derived(id).Keywords)
	}
	// Fund the disguise turn-up cost {4}{B} on top of the leftover.
	addMana(t, e, 0, "CCCCB")
	idx := turnFaceUpIndex(t, e, id)
	mark := len(e.L.Events)
	before := e.G.Players[0].Pool.Total()
	submitChoices(t, e, idx)

	if len(e.G.Stack) != 0 {
		t.Fatalf("disguise turn-face-up left %d object(s) on the stack, want 0", len(e.G.Stack))
	}
	assertTurnUpEventOnce(t, e, id, mark)
	o := e.G.Obj(id)
	if o.FaceDown {
		t.Fatalf("Basilica Stalker FaceDown=true after the turn-up, want false")
	}
	// The cloak status ward is gone; the printed 3/4 Flying is live again.
	if hasDerivedKeyword(e.Derived(id).Keywords, "Ward:2") {
		t.Fatalf("ward {2} still derived after the turn-up: %v", e.Derived(id).Keywords)
	}
	if !hasDerivedKeyword(e.Derived(id).Keywords, "Flying") {
		t.Fatalf("printed Flying not live after the turn-up: %v", e.Derived(id).Keywords)
	}
	if der := e.Derived(id); der.Power != 3 || der.Toughness != 4 {
		t.Fatalf("after disguise turn-up P/T = %d/%d, want printed 3/4", der.Power, der.Toughness)
	}
	// The cost was the printed Disguise {4}{B}.
	if got, want := e.G.Players[0].Pool.Total(), before-5; got != want {
		t.Fatalf("pool after disguise turn-up = %d, want %d (the printed {4}{B} disguise cost)", got, want)
	}
	replayCheck(t, e, cfg)
}

// hasDerivedKeyword reports whether kwList holds exactly kw.
func hasDerivedKeyword(kwList []string, kw string) bool {
	for _, k := range kwList {
		if k == kw {
			return true
		}
	}
	return false
}
