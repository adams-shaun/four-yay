package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// scrySrc is a sorcery with a plain Scry 3: look at the top three, put any
// number on the bottom, the rest back on top in the player's order. Nothing
// else, so the only observable effect under test is the reorder.
const scrySrc = "Name:ScryMe\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ Scry | Defined$ You | ScryNum$ 3\nOracle:x\n"

// surveilSrc is a sorcery with a plain Surveil 2: look at the top two, put
// any number into the graveyard, the rest back on top in the player's order.
const surveilSrc = "Name:SurveilMe\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ Surveil | Defined$ You | Amount$ 2\nOracle:x\n"

// scryFixture builds an engine whose seat 0 can cast scrySrc, with the mana
// to do so, and returns (engine, config, fixture-id).
func scryFixture(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := newFixtureDeck(t, seed, scrySrc)
	addMana(t, e, 0, "U")
	return e, cfg, id
}

// surveilFixture builds an engine whose seat 0 can cast surveilSrc, with the
// mana to do so, and returns (engine, config, fixture-id).
func surveilFixture(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := newFixtureDeck(t, seed, surveilSrc)
	addMana(t, e, 0, "U")
	return e, cfg, id
}

// scryDecision casts scrySrc and returns the pending KArrange decision.
func scryDecision(t *testing.T, e *Engine, id state.ObjID) *decision.Decision {
	t.Helper()
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KArrange {
		t.Fatalf("expected a pending KArrange decision, got %+v", d)
	}
	return d
}

// surveilDecision casts surveilSrc and returns the pending KArrange decision.
func surveilDecision(t *testing.T, e *Engine, id state.ObjID) *decision.Decision {
	t.Helper()
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KArrange {
		t.Fatalf("expected a pending KArrange decision, got %+v", d)
	}
	return d
}

// TestScryAnswerKeepsChosenOnTopAndUnchosenAtBottom is the leaf 2, the one
// that catches pile B being dropped beneath pile A instead of at the bottom:
// with 3 offered and the answer [2,0], the unchosen card (option 1) must sit
// at the very BOTTOM of the library, beneath the untouched remainder. The
// library must have a remainder of at least 2 cards or the two placements
// are indistinguishable.
func TestScryAnswerKeepsChosenOnTopAndUnchosenAtBottom(t *testing.T) {
	t.Parallel()
	e, cfg, id := scryFixture(t, 202)
	d := scryDecision(t, e, id)
	top := []state.ObjID{d.Options[0].Obj, d.Options[1].Obj, d.Options[2].Obj}
	libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	remainder := libBefore[3:]
	if len(remainder) < 2 {
		t.Fatalf("remainder after the top 3 is only %d card(s), need >= 2 for the two placements to be distinguishable", len(remainder))
	}

	submitChoices(t, e, 2, 0)

	libAfter := e.G.Zone(state.ZLibrary, 0)
	want := make([]state.ObjID, 0, len(libBefore))
	want = append(want, top[2], top[0])
	want = append(want, remainder...)
	want = append(want, top[1])
	if !sameObjIDs(want, libAfter) {
		t.Fatalf("library after [2,0] = %v, want %v (option 1 must be at the very bottom, beneath the remainder)", libAfter, want)
	}
	if n := countLibraryOrder(e); n != 1 {
		t.Fatalf("LibraryOrder events = %d, want exactly 1", n)
	}
	replayCheck(t, e, cfg)
}

// TestScryAnswerEmptySendsAllToBottom is the leaf 3: answering [] puts every
// offered card on the bottom, in the order they were offered.
func TestScryAnswerEmptySendsAllToBottom(t *testing.T) {
	t.Parallel()
	e, _, id := scryFixture(t, 203)
	d := scryDecision(t, e, id)
	top := []state.ObjID{d.Options[0].Obj, d.Options[1].Obj, d.Options[2].Obj}
	libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	remainder := libBefore[3:]

	submitChoices(t, e)

	libAfter := e.G.Zone(state.ZLibrary, 0)
	want := make([]state.ObjID, 0, len(libBefore))
	want = append(want, remainder...)
	want = append(want, top...)
	if !sameObjIDs(want, libAfter) {
		t.Fatalf("library after [] = %v, want %v (all N to the bottom, in offered order)", libAfter, want)
	}
}

// TestSurveilAnswerKeepsChosenOnTopAndMovesOtherToGraveyard is the leaf 4:
// Surveil 2 with the answer [0] leaves card0 on top and moves card1 to the
// graveyard, and the emitted kinds are exactly LibraryOrder then MoveZone in
// that order (the contract a replay depends on).
func TestSurveilAnswerKeepsChosenOnTopAndMovesOtherToGraveyard(t *testing.T) {
	t.Parallel()
	e, cfg, id := surveilFixture(t, 204)
	d := surveilDecision(t, e, id)
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("Min/Max = %d/%d, want 0/2", d.Min, d.Max)
	}
	if d.Options[0].Kind != "graveyard" || d.Options[1].Kind != "graveyard" {
		t.Fatalf("option Kind = %q/%q, want \"graveyard\"/\"graveyard\"", d.Options[0].Kind, d.Options[1].Kind)
	}
	top := []state.ObjID{d.Options[0].Obj, d.Options[1].Obj}
	libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	start := len(e.L.Events)

	submitChoices(t, e, 0)

	// The card that stayed on top is option 0; option 1 moved to the graveyard.
	libAfter := e.G.Zone(state.ZLibrary, 0)
	want := make([]state.ObjID, 0, len(libBefore))
	want = append(want, top[0])
	want = append(want, libBefore[2:]...)
	if !sameObjIDs(want, libAfter) {
		t.Fatalf("library after [0] = %v, want %v (card0 on top, remainder beneath)", libAfter, want)
	}
	// The pile-B card (option 1) is in the graveyard, and NOT in the library.
	if !zoneContains(e.G.Zone(state.ZGraveyard, 0), top[1]) {
		t.Fatalf("option 1 (%v) not in seat 0's graveyard after surveil [0]", top[1])
	}
	// Emitted kinds in sequence, from the point the answer was submitted:
	// the DecisionMade marker, then LibraryOrder, then MoveZone (pile B).
	seq := eventsIn(e.L.Events[start:])
	if len(seq) < 3 || seq[0] != events.DecisionMade || seq[1] != events.LibraryOrder || seq[2] != events.MoveZone {
		t.Fatalf("event-kind sequence after surveil [0] starts %v, want [decision_made library_order move_zone ...]", seq)
	}
	// The MoveZone must be the pile-B card, library -> graveyard.
	mv := e.L.Events[start+2]
	if mv.Obj != top[1] || mv.From != state.ZLibrary || mv.To != state.ZGraveyard {
		t.Fatalf("first MoveZone after the reorder = %+v, want a move of %v from library to graveyard", mv, top[1])
	}
	replayCheck(t, e, cfg)
}

// TestSurveilAnswerEmptySendsEveryOfferedCardToGraveyard is the leaf 5:
// answering [] moves every offered card to the graveyard, in offered order
// (assert the graveyard's own order, not just membership).
func TestSurveilAnswerEmptySendsEveryOfferedCardToGraveyard(t *testing.T) {
	t.Parallel()
	e, _, id := surveilFixture(t, 205)
	d := surveilDecision(t, e, id)
	top := []state.ObjID{d.Options[0].Obj, d.Options[1].Obj}
	libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)

	submitChoices(t, e)

	libAfter := e.G.Zone(state.ZLibrary, 0)
	wantLib := append([]state.ObjID(nil), libBefore[2:]...)
	if !sameObjIDs(wantLib, libAfter) {
		t.Fatalf("library after [] = %v, want remainder %v (nothing kept on top)", libAfter, wantLib)
	}
	gy := e.G.Zone(state.ZGraveyard, 0)
	// Both offered cards reached the graveyard, in the order they were
	// offered (the surveil spell itself also lands there afterwards, so only
	// the front of the graveyard is the pile).
	if len(gy) < 2 || gy[0] != top[0] || gy[1] != top[1] {
		t.Fatalf("graveyard = %v, want it to start with the offered cards in order %v", gy, top)
	}
}

// TestScryReplaysByteIdentically is the leaf 6a: a match with an answered
// Scry replays byte-identically (the same library), caught by replayCheck.
func TestScryReplaysByteIdentically(t *testing.T) {
	t.Parallel()
	e, cfg, id := scryFixture(t, 206)
	d := scryDecision(t, e, id)
	submitChoices(t, e, d.Options[2].Index, d.Options[0].Index)
	replayCheck(t, e, cfg)
}

// TestSurveilReplaysByteIdentically is the leaf 6b: a match with an answered
// Surveil replays byte-identically -- the same library AND the same
// graveyard, which is what the LibraryOrder-then-MoveZone contract buys.
func TestSurveilReplaysByteIdentically(t *testing.T) {
	t.Parallel()
	e, cfg, id := surveilFixture(t, 207)
	d := surveilDecision(t, e, id)
	submitChoices(t, e, d.Options[0].Index)
	replayCheck(t, e, cfg)
}

// TestSurveilEmptyReplaysByteIdentically is the leaf 6c: a Surveil whose
// answer sends every offered card to the graveyard replays to the same
// graveyard order.
func TestSurveilEmptyReplaysByteIdentically(t *testing.T) {
	t.Parallel()
	e, cfg, id := surveilFixture(t, 208)
	_ = surveilDecision(t, e, id)
	submitChoices(t, e)
	replayCheck(t, e, cfg)
}

// TestArrangeUnchangedForRearrangeTopOfLibrary is the leaf 9: the new
// Kind-dispatching handler keeps RearrangeTopOfLibrary's behaviour exactly
// (a full reorder, Min == Max == N, pile B empty), so a chosen permutation
// is the top and the remainder beneath untouched. This mirrors
// TestArrangeAnswerReordersLibrary but re-asserts it after the dispatch
// change, so a routing bug that only bites a non-empty pile B cannot be
// mistaken for "rearrange works".
func TestArrangeUnchangedForRearrangeTopOfLibrary(t *testing.T) {
	t.Parallel()
	e, _, id := arrangeFixture(t, 210)
	// The default ("bottom", pile B empty) route must keep the permutation:
	// run the canonical reorder via the real arrange machinery.
	d := arrangeDecision(t, e, id)
	top := []state.ObjID{d.Options[0].Obj, d.Options[1].Obj, d.Options[2].Obj}
	libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	submitChoices(t, e, 2, 0, 1)
	libAfter := e.G.Zone(state.ZLibrary, 0)
	want := []state.ObjID{top[2], top[0], top[1]}
	for i := 0; i < 3; i++ {
		if libAfter[i] != want[i] {
			t.Fatalf("rearrange top[%d] = %v, want %v", i, libAfter[i], want[i])
		}
	}
	for i := 3; i < len(libBefore); i++ {
		if libAfter[i] != libBefore[i] {
			t.Fatalf("rearrange remainder changed at %d: %v -> %v", i, libBefore[i], libAfter[i])
		}
	}
}

// scrySubSrc is a Scry 3 with a chained Draw sub-ability: after the arrange
// ask is answered and the resolution resumes, the draw must run EXACTLY
// once. It is the shape the through-the-resume leaf uses to prove the
// Ctx.Arrange guard in effLookAndArrange prevents a re-ask -- without the
// guard the resumed effect poses a second KArrange for the same source and
// the draw never runs.
const scrySubSrc = "Name:ScrySub\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ Scry | Defined$ You | ScryNum$ 3 | SubAbility$ DBDraw\n" +
	"SVar:DBDraw:DB$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n"

// surveilSubSrc is a Surveil 2 with a chained Draw sub-ability, the Surveil
// half of the same through-the-resume pin.
const surveilSubSrc = "Name:SurveilSub\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ Surveil | Defined$ You | Amount$ 2 | SubAbility$ DBDraw\n" +
	"SVar:DBDraw:DB$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n"

// onScryResume fixtures seat 0's hand with a Scry 3 that draws one on
// resume, funded with {U}, and returns the engine plus the fixture id.
func scrySubFixture(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := newFixtureDeck(t, seed, scrySubSrc)
	addMana(t, e, 0, "U")
	return e, cfg, id
}

// surveilSubFixture is the Surveil half of scrySubFixture.
func surveilSubFixture(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := newFixtureDeck(t, seed, surveilSubSrc)
	addMana(t, e, 0, "U")
	return e, cfg, id
}

// TestScryResumeRunsSubAbilityOnceAndDoesNotReask pins the Ctx.Arrange
// re-entry guard in effLookAndArrange. A Scry with a chained Draw is cast,
// the first KArrange is answered, and the answer drives the resolution ALL
// THE WAY through the resume. The guard is what stops the resumed effect
// from posing a second KArrange for the same source: without it the effect
// body re-runs (it re-emits its Secret Note, re-builds a KArrange and calls
// h.Ask, which re-suspends before the chained Draw ever runs) -- so the leaf
// asserts BOTH that exactly ONE LibraryOrder was emitted over the whole
// resolution and that the resolution actually finished: the pending decision
// is not another KArrange for the same source, and the chained Draw ran
// exactly once.
func TestScryResumeRunsSubAbilityOnceAndDoesNotReask(t *testing.T) {
	t.Parallel()
	e, cfg, id := scrySubFixture(t, 211)
	d := scryDecision(t, e, id)
	pre := countDraw(e)

	submitChoices(t, e, d.Options[1].Index, d.Options[2].Index, d.Options[0].Index)

	if n := countLibraryOrder(e); n != 1 {
		t.Fatalf("LibraryOrder events over the whole resolution = %d, want exactly 1 (a re-ask would emit a second after its answer)", n)
	}
	if n := countDraw(e); n != pre+1 {
		t.Fatalf("chained Draw ran %d time(s) (pre=%d), want exactly once -- the resumed effect must not have re-asked before it", n-pre, pre)
	}
	if pd := e.Pending(); pd != nil && pd.Kind == decision.KArrange && pd.Source == id {
		t.Fatalf("a second KArrange for the same source (%d) is pending after answering; the Ctx.Arrange guard was removed", id)
	}
	replayCheck(t, e, cfg)
}

// TestSurveilResumeRunsSubAbilityOnceAndDoesNotReask is the Surveil half of
// TestScryResumeRunsSubAbilityOnceAndDoesNotReask.
func TestSurveilResumeRunsSubAbilityOnceAndDoesNotReask(t *testing.T) {
	t.Parallel()
	e, cfg, id := surveilSubFixture(t, 212)
	d := surveilDecision(t, e, id)
	pre := countDraw(e)

	submitChoices(t, e, d.Options[1].Index, d.Options[0].Index)

	if n := countLibraryOrder(e); n != 1 {
		t.Fatalf("LibraryOrder events over the whole resolution = %d, want exactly 1 (a re-ask would emit a second after its answer)", n)
	}
	if n := countDraw(e); n != pre+1 {
		t.Fatalf("chained Draw ran %d time(s) (pre=%d), want exactly once -- the resumed effect must not have re-asked before it", n-pre, pre)
	}
	if pd := e.Pending(); pd != nil && pd.Kind == decision.KArrange && pd.Source == id {
		t.Fatalf("a second KArrange for the same source (%d) is pending after answering; the Ctx.Arrange guard was removed", id)
	}
	replayCheck(t, e, cfg)
}

func sameObjIDs(a, b []state.ObjID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func zoneContains(ids []state.ObjID, id state.ObjID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func eventsIn(evs []events.Event) []events.Kind {
	out := make([]events.Kind, 0, len(evs))
	for _, ev := range evs {
		out = append(out, ev.Kind)
	}
	return out
}
