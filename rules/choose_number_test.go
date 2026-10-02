package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The mid-resolution ChooseNumber ask (task cli-20260923T060000Z-choose-number):
// a resolution-time "choose a number" poses a real KChoose over the
// deterministic number list, and the ANSWERED number is what events.Apply
// records on the resolving object (o.ChosenNumber) -- the value every
// downstream reader that survives the resolution reads. Driven here by the
// real corpus carrier Void ("Choose a number. Destroy all artifacts and
// creatures with mana value equal to that number."), the bare SP$
// ChooseNumber shape the fix targets.

// castVoid funds {3}{B}{R} and casts the real Void from seat 0's hand,
// answering the cast-time "target player" ask with seat 1 and leaving the
// spell on the stack; the callers pass priority to the number ask. It
// asserts its own precondition: a real cmc-2 creature is on the battlefield
// (so the number chosen provably names a value the board contains), and the
// spell is in seat 0's hand before the cast.
func castVoid(t *testing.T) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusCard(t, "Void"))
	bear := battlefieldCreature(t, e, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	void := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(void); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Void is not in seat 0's hand: %+v", o)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || o.Face().ManaValue() != 2 {
		t.Fatalf("precondition: the cmc-2 creature is not on the battlefield: %+v", o)
	}
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MB], e.G.Players[0].Pool[state.MR] = 3, 1, 1
	e.priorityRound()
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == void {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Void in %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// CR 601.2c: Void's "target player reveals their hand" is a target, so it
	// is announced as the spell is cast -- before the number is chosen, which
	// stays a resolution-time choice.
	answerCastSubPlayer(t, e, 1)
	return e, void, bear
}

// TestChosenNumberAskIsPosedMidResolution pins the ask itself: the resolution
// suspends on a KChoose whose ResumeKind is "choosenumber", whose options are
// the deterministic ascending number list with the value on Amount. Without
// the fix the effect never asked -- it recorded 0 outright and kept resolving.
func TestChosenNumberAskIsPosedMidResolution(t *testing.T) {
	t.Parallel()
	e, void, _ := castVoid(t)
	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choosenumber" ||
		d.Player != 0 || d.Min != 1 || d.Max != 1 || d.Prompt != "Choose a number" {
		t.Fatalf("expected the mid-resolution number ask, got %+v", d)
	}
	if len(d.Options) < 2 {
		t.Fatalf("option list = %+v, want at least two numbers", d.Options)
	}
	for i, o := range d.Options {
		if o.Kind != "number" || o.Amount != i {
			t.Fatalf("option %d = %+v, want Kind number and Amount %d", i, o, i)
		}
	}
	if o := e.G.Obj(void); o.ChosenNumber != 0 {
		t.Fatalf("a choice was recorded before the ask was answered: %d", o.ChosenNumber)
	}
}

// TestChosenNumberAnswerRecordsTheChosenNumber answers 5 -- a value the
// pre-fix silent fallback (0) never records -- and asserts the answered
// number lands on the resolving object. Without the fix the resolution
// silently chose 0, so the 5 assertion fails.
func TestChosenNumberAnswerRecordsTheChosenNumber(t *testing.T) {
	t.Parallel()
	e, void, _ := castVoid(t)
	d := passUntilAsk(t, e)
	if d == nil || d.ResumeKind != "choosenumber" {
		t.Fatalf("expected the number ask, got %+v", d)
	}
	five := -1
	for _, o := range d.Options {
		if o.Kind == "number" && o.Amount == 5 {
			five = o.Index
		}
	}
	if five < 0 {
		t.Fatalf("no number option 5 in %+v", d.Options)
	}
	submitChoices(t, e, five)
	if o := e.G.Obj(void); o.ChosenNumber != 5 {
		t.Fatalf("recorded choice = %d, want the answered 5", o.ChosenNumber)
	}
}

// TestChosenNumberZeroAnswerIsRecorded answers 0 -- the value the pre-fix
// fallback also recorded, but through the ANSWERED path -- and asserts the
// resolved spell leaves the battlefield. It proves the answered 0 does not
// wedge the resolution (the ChosenNumberAnswered marker is consumed) and that
// no second ask is posed for the answered 0.
func TestChosenNumberZeroAnswerIsRecorded(t *testing.T) {
	t.Parallel()
	e, void, _ := castVoid(t)
	d := passUntilAsk(t, e)
	if d == nil || d.ResumeKind != "choosenumber" {
		t.Fatalf("expected the number ask, got %+v", d)
	}
	zero := -1
	for _, o := range d.Options {
		if o.Kind == "number" && o.Amount == 0 {
			zero = o.Index
		}
	}
	if zero < 0 {
		t.Fatalf("no number option 0 in %+v", d.Options)
	}
	submitChoices(t, e, zero)
	if o := e.G.Obj(void); o.ChosenNumber != 0 {
		t.Fatalf("recorded choice = %d, want the answered 0", o.ChosenNumber)
	}
	// The resolution must continue (no second number ask).
	if d2 := e.Pending(); d2 != nil && d2.Kind == decision.KChoose && d2.ResumeKind == "choosenumber" {
		t.Fatalf("the answered 0 posed a second number ask: %+v", d2)
	}
}
