package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The Dig default-remainder leaves, pinned on the REAL corpus card the deck
// audit named (Ancient Stirrings, 4 copies in the tron deck): the untaken
// window cards go to the library's BOTTOM in an order the player picks, the
// ordered-bottom KArrange (Min == Max == the remainder, Option.Kind
// "dig_bottom") poses after the take, and the bot's own deterministic answer
// validates (the no-livelock contract). The synthetic-shape halves of the
// same behaviour live in dig_ask_test.go / dig_variants_test.go; this file
// proves the corpus card end to end.

// stirringsEngine builds a two-seat engine whose seat 0 holds the real
// corpus Ancient Stirrings in hand and a library the test then pins: the
// caller reorders it through digReorder (the event path) so the top five are
// whatever the leaves need.
func stirringsEngine(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	stir, ok := reg.Lookup("Ancient Stirrings")
	if !ok {
		t.Fatal("Ancient Stirrings missing from the corpus")
	}
	saw, ok := reg.Lookup("Bone Saw")
	if !ok {
		t.Fatal("Bone Saw missing from the corpus")
	}
	forest, ok := reg.Lookup("Forest")
	if !ok {
		t.Fatal("Forest missing from the corpus")
	}
	sa := stir.Faces[0].SpellAbility()
	if sa == nil || sa.API != "Dig" || sa.Params["DigNum"] != "5" || sa.Params["ChangeValid"] != "Card.Colorless" {
		t.Fatalf("Ancient Stirrings corpus shape drifted: %+v", sa)
	}
	if !strings.Contains(stir.Faces[0].Oracle, "put the rest on the bottom of your library in any order") {
		t.Fatal("Ancient Stirrings Oracle no longer states the bottom-in-any-order remainder")
	}
	filler := func(n int) []*cards.Card {
		out := make([]*cards.Card, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, forest)
		}
		return out
	}
	cfg := Config{Seed: seed, Tokens: reg.Tokens,
		Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{stir, stir, stir, stir, saw, saw}, filler(34)...),
			filler(40),
		}}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	id := moveByName(t, e, 0, "Ancient Stirrings", state.ZHand)
	return e, cfg, id
}

// assertBottomOrder checks the two structural halves of a bottomed window:
// the cards that were BELOW the window now lead the library in their
// original order, and the given order closes it at the bottom.
func assertBottomOrder(t *testing.T, libAfter, belowWindow, bottom []state.ObjID) {
	t.Helper()
	if len(libAfter) != len(belowWindow)+len(bottom) {
		t.Fatalf("library size %d, want %d", len(libAfter), len(belowWindow)+len(bottom))
	}
	for i, oid := range belowWindow {
		if libAfter[i] != oid {
			t.Fatalf("library[%d] = %v, want %v (the below-window cards surfaced unchanged)", i, libAfter[i], oid)
		}
	}
	base := len(belowWindow)
	for i, oid := range bottom {
		if libAfter[base+i] != oid {
			t.Fatalf("library bottom[%d] = %v, want %v (the answered bottom order)", i, libAfter[base+i], oid)
		}
	}
}

// TestAncientStirringsBottomsTheRemainderInTheChosenOrder is the audit's
// core leaf through the real engine and the real corpus card: the optional
// take asks (the window holds more eligible cards than ChangeNum$ 1), the
// answered card goes to the hand, the ordered-bottom ask poses over the four
// untaken cards, the answer's order IS the bottom order, and the whole game
// replays.
func TestAncientStirringsBottomsTheRemainderInTheChosenOrder(t *testing.T) {
	t.Parallel()
	e, cfg, id := stirringsEngine(t, 61)
	libBefore := digReorder(t, e, "Bone Saw", "Bone Saw")
	belowWindow := append([]state.ObjID(nil), libBefore[5:]...)
	if len(belowWindow) == 0 {
		t.Fatal("fixture library has no cards below the window")
	}
	d := digCast(t, e, id, "G", true)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) < 2 || d.Options[0].Kind != "dig" {
		t.Fatalf("expected the take KChoose over a strict-superset window, got %+v", d)
	}
	if d.Min != 0 || d.Max != 1 {
		t.Fatalf("Min/Max = %d/%d, want 0/1 (the optional take of one)", d.Min, d.Max)
	}
	if d.Options[0].Obj == d.Options[1].Obj {
		t.Fatal("the two take options are the same card: the choice could not differ")
	}
	picked := d.Options[1].Obj // the SECOND colorless card: the answer, not a default, is honoured
	submitChoices(t, e, 1)

	arr := e.Pending()
	if arr == nil || arr.Kind != decision.KArrange {
		t.Fatalf("no ordered-bottom ask posed after the take: %+v", arr)
	}
	if arr.ResumeKind != "dig_arrange" || arr.Min != 4 || arr.Max != 4 || len(arr.Options) != 4 || arr.Options[0].Kind != "dig_bottom" {
		t.Fatalf("arrange = %+v, want a ResumeKind dig_arrange Min==Max==4 dig_bottom ask", arr)
	}
	if !strings.Contains(arr.Prompt, "bottom of your library in any order") {
		t.Fatalf("arrange prompt = %q", arr.Prompt)
	}
	// The offered options are exactly the four untaken window cards, in
	// their existing relative order.
	untaken := make([]state.ObjID, 0, 4)
	for _, oid := range libBefore[:5] {
		if oid != picked {
			untaken = append(untaken, oid)
		}
	}
	for i, o := range arr.Options {
		if o.Obj != untaken[i] {
			t.Fatalf("arrange option %d = %v, want %v (the untaken window cards in window order)", i, o.Obj, untaken[i])
		}
	}
	// Answer with a NON-offered order -- the point of the ask -- and expect
	// exactly that order at the bottom.
	submitChoices(t, e, 2, 0, 3, 1)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(picked); o == nil || o.Zone != state.ZHand {
		t.Fatalf("taken card zone = %v, want Hand", o.Zone)
	}
	wantBottom := []state.ObjID{untaken[2], untaken[0], untaken[3], untaken[1]}
	assertBottomOrder(t, e.G.Zone(state.ZLibrary, 0), belowWindow, wantBottom)
	replayCheck(t, e, cfg)
}

// TestAncientStirringsDeclinedTakeStillOrdersTheBottom is the decline leaf
// on the corpus card: declining the optional take leaves all five window
// cards for the ordered-bottom ask, and the answer's order bottoms them.
func TestAncientStirringsDeclinedTakeStillOrdersTheBottom(t *testing.T) {
	t.Parallel()
	e, cfg, id := stirringsEngine(t, 62)
	libBefore := digReorder(t, e, "Bone Saw", "Bone Saw")
	belowWindow := append([]state.ObjID(nil), libBefore[5:]...)

	d := digCast(t, e, id, "G", true)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the take KChoose, got %+v", d)
	}
	submitChoices(t, e) // decline the take; the arrange is what the answer orders
	sub := e.Pending()
	if sub == nil || sub.Kind != decision.KArrange || sub.Min != 5 || sub.Max != 5 || len(sub.Options) != 5 {
		t.Fatalf("arrange = %+v, want Min==Max==5 over the whole declined window", sub)
	}
	handAtAsk := append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...)
	// A non-offered order again, so the assertion cannot pass by accident.
	submitChoices(t, e, 4, 1, 3, 0, 2)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Zone(state.ZHand, 0); len(got) != len(handAtAsk) {
		t.Fatalf("hand size %d, want %d: a declined take moved a card into the hand", len(got), len(handAtAsk))
	}
	wantBottom := []state.ObjID{
		sub.Options[4].Obj, sub.Options[1].Obj, sub.Options[3].Obj, sub.Options[0].Obj, sub.Options[2].Obj,
	}
	assertBottomOrder(t, e.G.Zone(state.ZLibrary, 0), belowWindow, wantBottom)
	replayCheck(t, e, cfg)
}

// TestAncientStirringsBotAnswerValidates is the no-livelock contract: the
// bot's own answer to the ordered-bottom ask runs through Decision.Validate
// clean, so the deterministic bot can never re-submit a rejected answer.
// The board binds (five untaken cards, Min == Max == 5).
func TestAncientStirringsBotAnswerValidates(t *testing.T) {
	t.Parallel()
	e, cfg, id := stirringsEngine(t, 63)
	digReorder(t, e, "Bone Saw", "Bone Saw")

	d := digCast(t, e, id, "G", true)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the take KChoose, got %+v", d)
	}
	submitChoices(t, e) // decline; the arrange is what the bot must answer

	arr := e.Pending()
	if arr == nil || arr.Kind != decision.KArrange || arr.Min != 5 {
		t.Fatalf("arrange = %+v, want the five-card ordered-bottom ask", arr)
	}
	bot := newTestBot(7)
	in := bot.answer(e, arr)
	if err := arr.Validate(in); err != nil {
		t.Fatalf("the bot's own arrange answer does not validate: %v (intent %+v)", err, in)
	}
	// The clamp fallback answers the full permutation in the offered order:
	// the deterministic stand-in the no-host path mirrors.
	if len(in.Choices) != 5 {
		t.Fatalf("bot choices = %v, want the full 5-permutation", in.Choices)
	}
	for i, c := range in.Choices {
		if c != i {
			t.Fatalf("bot choices = %v, want the offered order [0 1 2 3 4]", in.Choices)
		}
	}
	submitChoices(t, e, in.Choices...)
	passUntilStackEmpty(t, e, 20)
	libAfter := e.G.Zone(state.ZLibrary, 0)
	for i, o := range arr.Options {
		if libAfter[len(libAfter)-5+i] != o.Obj {
			t.Fatalf("library bottom[%d] = %v, want %v (the bot's offered-order answer applied)", i, libAfter[len(libAfter)-5+i], o.Obj)
		}
	}
	replayCheck(t, e, cfg)
}
