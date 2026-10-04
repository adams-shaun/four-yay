package rules

// Restored from effects/dig_ask_test.go (W3 legacy removal): Dig's
// look-and-take ask, its Optional$/mandatory bounds, the bottom choice, the
// ordered-bottom arrange and the multi-library walk, answered through the
// resolution kernel on a real engine.

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const (
	kr0DigBear = "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"
	kr0DigIsle = "Name:Isle\nTypes:Basic Land Island\nOracle:x\n"
	kr0DigSA   = "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 1 | Optional$ True | ChangeValid$ Land | DestinationZone$ Hand"
)

// kr0DigBoard sets seat 0's library to [bear, land, land, bear] with an
// empty hand and returns the library ids and a Ctx maker.
func kr0DigBoard(t *testing.T) (*Engine, []state.ObjID, func() *effects.Ctx) {
	t.Helper()
	e := kr0Engine(t, 2)
	src := kr0Src(t, e, 0, "Name:Digger\nTypes:Sorcery\nOracle:x\n", state.ZBattlefield)
	ids := kr0SetLibrary(t, e, 0, kr0DigBear, kr0DigIsle, kr0DigIsle, kr0DigBear)
	kr0SetHand(t, e, 0)
	return e, ids, func() *effects.Ctx { return &effects.Ctx{Source: src, Controller: 0} }
}

// TestDigAsksWhenTheWindowHoldsMoreEligibleCardsThanChangeNumKernel: a
// window with more eligible cards than ChangeNum poses the owner a Min-0
// (Optional$) Max-ChangeNum KChoose over the ELIGIBLE cards only, after a
// Secret look Note carrying the window and before anything moves; the
// answered (second) land goes to hand by a Secret move and the rest stay on
// top in order.
func TestDigAsksWhenTheWindowHoldsMoreEligibleCardsThanChangeNumKernel(t *testing.T) {
	t.Parallel()
	e, ids, mk := kr0DigBoard(t)
	start := len(e.L.Events)
	d := kr0Run(t, e, kr0SA(t, kr0DigSA), mk, nil)
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 || d.Min != 0 || d.Max != 1 || d.ResumeKind != "dig" {
		t.Fatalf("decision = %+v, want a Min 0 / Max 1 dig KChoose for seat 0", d)
	}
	if !strings.Contains(d.Prompt, "Look at the top 3") {
		t.Fatalf("prompt = %q, want it to name the look", d.Prompt)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "dig" || d.Options[0].Obj != ids[1] || d.Options[1].Obj != ids[2] {
		t.Fatalf("options = %+v, want the two lands in library order, Kind \"dig\"", d.Options)
	}
	var look *events.Event
	for _, ev := range kr0Since(e, start) {
		if ev.Kind == events.Note && ev.Text == "looks at the top of the library" {
			ev := ev
			look = &ev
			break
		}
	}
	if look == nil || !look.Secret || look.Player != 0 || !slices.Equal(look.IDs, ids[:3]) {
		t.Fatalf("look Note = %+v, want a Secret Note to seat 0 carrying %v", look, ids[:3])
	}
	if len(e.G.Zone(state.ZHand, 0)) != 0 {
		t.Fatal("cards moved before the answer")
	}
	kr0Answer(t, e, kr0Opt(t, d, ids[2]))
	if hand := e.G.Zone(state.ZHand, 0); !slices.Equal(hand, []state.ObjID{ids[2]}) {
		t.Fatalf("hand = %v, want [%d] (the SECOND land)", hand, ids[2])
	}
	if lib := e.G.Zone(state.ZLibrary, 0); !slices.Equal(lib, []state.ObjID{ids[0], ids[1], ids[3]}) {
		t.Fatalf("library = %v, want [%d %d %d]", lib, ids[0], ids[1], ids[3])
	}
	found := false
	for _, ev := range kr0Since(e, start) {
		found = found || ev.Kind == events.MoveZone && ev.Obj == ids[2] && ev.Secret && ev.Player == 0
	}
	if !found {
		t.Fatal("the answered take's MoveZone is not a Secret move naming the library's owner")
	}
}

// TestDigBottomChoiceExplainsBothOutcomesKernel: the one-card "may put it
// on the bottom" shape names both outcomes in its prompt and options, and
// selecting the second land bottoms exactly it.
func TestDigBottomChoiceExplainsBothOutcomesKernel(t *testing.T) {
	t.Parallel()
	e, ids, mk := kr0DigBoard(t)
	d := kr0Run(t, e, kr0SA(t, "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 1 | Optional$ True | ChangeValid$ Land | DestinationZone$ Library | LibraryPosition$ -1 | SkipReorder$ True"), mk, nil)
	if d == nil {
		t.Fatal("no decision was posed")
	}
	if got, want := d.Prompt, "Look at the top 3 card(s) of your library: Select up to 1 matching card(s) to put on the bottom of your library. Leave unselected card(s) on top."; got != want {
		t.Fatalf("prompt = %q, want %q", got, want)
	}
	if len(d.Options) != 2 || d.Options[0].Label != "Put Isle on bottom" || d.Options[1].Label != "Put Isle on bottom" {
		t.Fatalf("options = %+v, want explicit bottom actions", d.Options)
	}
	kr0Answer(t, e, kr0Opt(t, d, ids[2]))
	if got, want := e.G.Zone(state.ZLibrary, 0), []state.ObjID{ids[0], ids[1], ids[3], ids[2]}; !slices.Equal(got, want) {
		t.Fatalf("library = %v, want %v", got, want)
	}
}

// TestDigOptionalDeclineMovesNothingKernel: an empty answer to the Optional$
// take moves nothing.
func TestDigOptionalDeclineMovesNothingKernel(t *testing.T) {
	t.Parallel()
	e, ids, mk := kr0DigBoard(t)
	if d := kr0Run(t, e, kr0SA(t, kr0DigSA), mk, nil); d == nil {
		t.Fatal("no decision was posed")
	}
	kr0Answer(t, e)
	if lib := e.G.Zone(state.ZLibrary, 0); len(lib) != 4 || lib[0] != ids[0] || lib[3] != ids[3] {
		t.Fatalf("library = %v, want the decline to move nothing", lib)
	}
	if len(e.G.Zone(state.ZHand, 0)) != 0 {
		t.Fatal("a declined Optional take moved a card into hand")
	}
}

// TestDigMandatoryAskMinsAtChangeNumKernel: without Optional$ the take's Min
// is ChangeNum.
func TestDigMandatoryAskMinsAtChangeNumKernel(t *testing.T) {
	t.Parallel()
	e, _, mk := kr0DigBoard(t)
	d := kr0Run(t, e, kr0SA(t, "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 1 | ChangeValid$ Land | DestinationZone$ Hand"), mk, nil)
	if d == nil || d.Min != 1 || d.Max != 1 {
		t.Fatalf("decision = %+v, want Min/Max 1/1 for a mandatory take of 1", d)
	}
}

// TestDigOptionalAsksEvenWhenTheEligibleSetFitsTheCapKernel: an Optional$
// take still asks (0..2) when the eligible set fits the cap, and the decline
// leaves the window unchanged.
func TestDigOptionalAsksEvenWhenTheEligibleSetFitsTheCapKernel(t *testing.T) {
	t.Parallel()
	e, ids, mk := kr0DigBoard(t)
	d := kr0Run(t, e, kr0SA(t, "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 2 | Optional$ True | ChangeValid$ Land | DestinationZone$ Hand | SkipReorder$ True"), mk, nil)
	if d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != 2 {
		t.Fatalf("decision = %+v, want an optional 0..2 take ask", d)
	}
	kr0Answer(t, e)
	if hand := e.G.Zone(state.ZHand, 0); len(hand) != 0 {
		t.Fatalf("hand = %v, want empty after the optional decline", hand)
	}
	if lib := e.G.Zone(state.ZLibrary, 0); !slices.Equal(lib, ids) {
		t.Fatalf("library = %v, want the unchanged %v", lib, ids)
	}
}

// TestDigReentryIgnoresIdsOutsideTheWindowKernel: the take only ever offers
// the in-window eligible cards (never the bear beneath the window), and the
// in-window answer is the only card that moves.
func TestDigReentryIgnoresIdsOutsideTheWindowKernel(t *testing.T) {
	t.Parallel()
	e, ids, mk := kr0DigBoard(t)
	d := kr0Run(t, e, kr0SA(t, kr0DigSA), mk, nil)
	if d == nil {
		t.Fatal("no decision was posed")
	}
	for _, o := range d.Options {
		if o.Obj == ids[3] || o.Obj == ids[0] {
			t.Fatalf("option %+v names a card outside the eligible window", o)
		}
	}
	kr0Answer(t, e, kr0Opt(t, d, ids[1]))
	if hand := e.G.Zone(state.ZHand, 0); !slices.Equal(hand, []state.ObjID{ids[1]}) {
		t.Fatalf("hand = %v, want [%d] (only the in-window pick moved)", hand, ids[1])
	}
}

// TestDigZeroChangeNumSkipsTheTakeAskKernel: ChangeNum$ 0 never poses the
// take; the default bottom remainder poses the ordered-bottom KArrange over
// the whole window, and the answered order bottoms it beneath the untouched
// card.
func TestDigZeroChangeNumSkipsTheTakeAskKernel(t *testing.T) {
	t.Parallel()
	e, ids, mk := kr0DigBoard(t)
	d := kr0Run(t, e, kr0SA(t, "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 0 | Optional$ True | ChangeValid$ Land | DestinationZone$ Hand"), mk, nil)
	if d == nil || d.Kind != decision.KArrange {
		t.Fatalf("decision = %+v, want the ordered-bottom KArrange", d)
	}
	if d.Min != 3 || d.Max != 3 || len(d.Options) != 3 || d.Options[0].Kind != "dig_bottom" {
		t.Fatalf("arrange = Min %d Max %d options %+v, want 3/3 dig_bottom over the window", d.Min, d.Max, d.Options)
	}
	if len(e.G.Zone(state.ZHand, 0)) != 0 {
		t.Fatal("ChangeNum$ 0 took a card")
	}
	order := []state.ObjID{d.Options[2].Obj, d.Options[0].Obj, d.Options[1].Obj}
	kr0Answer(t, e, 2, 0, 1)
	want := append([]state.ObjID{ids[3]}, order...)
	if lib := e.G.Zone(state.ZLibrary, 0); !slices.Equal(lib, want) {
		t.Fatalf("library = %v, want %v (the window bottomed in the answered order)", lib, want)
	}
}

// kr0DigLibraries gives seats 0..n-1 the given libraries and empty hands.
func kr0DigLibraries(t *testing.T, e *Engine, libs ...[]string) [][]state.ObjID {
	t.Helper()
	out := make([][]state.ObjID, len(libs))
	for p, srcs := range libs {
		out[p] = kr0SetLibrary(t, e, state.PlayerID(p), srcs...)
		kr0SetHand(t, e, state.PlayerID(p))
	}
	return out
}

// TestDigMultiPlayerResumeKeepsEveryLibraryKernel: Defined$ Player over
// three libraries -- seat 0's no-choice window completes silently; seat 1's
// take and then its two-card ordered bottom are asked and applied; the walk
// then continues to seat 2 (never abandoned, never re-processing seat 0 or
// 1), which ends with one land taken and its remainder bottomed.
func TestDigMultiPlayerResumeKeepsEveryLibraryKernel(t *testing.T) {
	t.Parallel()
	e := kr0Engine(t, 3)
	src := kr0Src(t, e, 0, "Name:Digger\nTypes:Sorcery\nOracle:x\n", state.ZBattlefield)
	libs := kr0DigLibraries(t, e,
		[]string{kr0DigIsle, kr0DigBear},
		[]string{kr0DigIsle, kr0DigBear, kr0DigIsle},
		[]string{kr0DigIsle, kr0DigBear, kr0DigIsle})
	d := kr0Run(t, e, kr0SA(t, "SP$ Dig | Defined$ Player | DigNum$ 3 | ChangeNum$ 1 | ChangeValid$ Land | DestinationZone$ Hand"),
		func() *effects.Ctx { return &effects.Ctx{Source: src, Controller: 0} }, nil)
	if d == nil || d.Player != 1 || d.Kind != decision.KChoose {
		t.Fatalf("first decision = %+v, want seat 1's take", d)
	}
	if hand := e.G.Zone(state.ZHand, 0); !slices.Equal(hand, []state.ObjID{libs[0][0]}) {
		t.Fatalf("seat 0 hand = %v, want its no-choice take [%d]", hand, libs[0][0])
	}
	if lib := e.G.Zone(state.ZLibrary, 0); !slices.Equal(lib, []state.ObjID{libs[0][1]}) {
		t.Fatalf("seat 0 library = %v, want [%d]", lib, libs[0][1])
	}
	if len(e.G.Zone(state.ZHand, 2)) != 0 {
		t.Fatal("seat 2 was processed before seat 1's ask")
	}
	picked := libs[1][2]
	d = kr0Answer(t, e, kr0Opt(t, d, picked))
	if d == nil || d.Kind != decision.KArrange || d.Player != 1 || d.Min != 2 || d.Max != 2 {
		t.Fatalf("decision = %+v, want seat 1's 2/2 ordered-bottom arrange", d)
	}
	if hand := e.G.Zone(state.ZHand, 0); len(hand) != 1 {
		t.Fatalf("seat 0 was processed twice: hand %v", hand)
	}
	if hand := e.G.Zone(state.ZHand, 1); !slices.Equal(hand, []state.ObjID{picked}) {
		t.Fatalf("seat 1 hand = %v, want [%d]", hand, picked)
	}
	if len(e.G.Zone(state.ZHand, 2)) != 0 {
		t.Fatal("seat 2 was processed before seat 1's arrange")
	}
	order1 := []state.ObjID{d.Options[1].Obj, d.Options[0].Obj}
	d = kr0Answer(t, e, 1, 0)
	if lib := e.G.Zone(state.ZLibrary, 1); !slices.Equal(lib, order1) {
		t.Fatalf("seat 1 library = %v, want the answered bottom order %v", lib, order1)
	}
	if d == nil || d.Player != 2 {
		t.Fatalf("decision = %+v, want seat 2's own ask on the resumed walk", d)
	}
	// Answer every remaining ask of seat 2 with its first legal shape; seat
	// 2 must end with exactly one land taken and its remainder bottomed.
	for i := 0; d != nil && i < 4; i++ {
		if d.Player != 2 {
			t.Fatalf("unexpected decision %+v after seat 2's walk began", d)
		}
		d = kr0Answer(t, e, tapePick(d)...)
	}
	if d != nil {
		t.Fatalf("seat 2's walk never completed: %+v", d)
	}
	if hand := e.G.Zone(state.ZHand, 2); len(hand) != 1 || e.G.Obj(hand[0]).Face().Name != "Isle" {
		t.Fatalf("seat 2 hand = %v, want exactly one land taken", hand)
	}
	if lib := e.G.Zone(state.ZLibrary, 2); len(lib) != 2 {
		t.Fatalf("seat 2 library = %v, want its two-card remainder", lib)
	}
}

// TestDigTakeResumeContinuesToLaterLibraryKernel: after an earlier
// library's take is answered, a LATER library with a real choice poses its
// own take ask (never a silent first-eligible take), and its answer is what
// moves.
func TestDigTakeResumeContinuesToLaterLibraryKernel(t *testing.T) {
	t.Parallel()
	e := kr0Engine(t, 3)
	src := kr0Src(t, e, 0, "Name:Digger\nTypes:Sorcery\nOracle:x\n", state.ZBattlefield)
	libs := kr0DigLibraries(t, e,
		[]string{kr0DigIsle, kr0DigBear},
		[]string{kr0DigIsle, kr0DigIsle},
		[]string{kr0DigIsle, kr0DigIsle})
	d := kr0Run(t, e, kr0SA(t, "SP$ Dig | Defined$ Player | DigNum$ 2 | ChangeNum$ 1 | ChangeValid$ Land | DestinationZone$ Hand"),
		func() *effects.Ctx { return &effects.Ctx{Source: src, Controller: 0} }, nil)
	if d == nil || d.Player != 1 || d.ResumeTarget != 1 {
		t.Fatalf("first decision = %+v, want seat 1's take at target index 1", d)
	}
	if len(e.G.Zone(state.ZHand, 2)) != 0 {
		t.Fatal("seat 2 was processed before seat 1's ask")
	}
	picked := libs[1][1]
	d = kr0Answer(t, e, kr0Opt(t, d, picked))
	if d == nil || d.Player != 2 || d.ResumeTarget != 2 || d.Kind != decision.KChoose {
		t.Fatalf("later library did not get its own take ask: %+v", d)
	}
	if hand := e.G.Zone(state.ZHand, 1); !slices.Equal(hand, []state.ObjID{picked}) {
		t.Fatalf("seat 1 hand = %v, want [%d]", hand, picked)
	}
	if len(e.G.Zone(state.ZHand, 2)) != 0 {
		t.Fatal("seat 2 was completed before its own ask was answered")
	}
	later := libs[2][1]
	if next := kr0Answer(t, e, kr0Opt(t, d, later)); next != nil {
		t.Fatalf("another ask remained after the last library: %+v", next)
	}
	if hand := e.G.Zone(state.ZHand, 2); !slices.Equal(hand, []state.ObjID{later}) {
		t.Fatalf("seat 2 hand = %v, want [%d]", hand, later)
	}
}
