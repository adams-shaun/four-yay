package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Loamcrafter Faun's "when you do" chain end to end (task
// agent-20260918T195920Z-2fd3b568, the ticket that registered
// api:ImmediateTrigger originally filed under):
//
//	When CARDNAME enters, you may discard one or more land cards. When you do,
//	return up to that many target nonland permanent cards from your graveyard
//	to your hand.
//
// The chain is DB$ Discard | AnyNumber$ True | Optional$ True | Mode$ TgtChoose
// | RememberDiscarded$ True, then DB$ ImmediateTrigger | ConditionDefined$
// Remembered | ConditionCompare$ GE1 | RememberObjects$ Remembered | Execute$
// TrigReturn, whose SVar:X:TriggerRemembered$Amount sizes TargetMax$ X.
//
// These tests run on the REAL compiled corpus card only -- no Forge script
// text is committed here (the licensing rule) -- fetched by name through
// searchCorpusCard. The ticket's residual defect was that TriggerRemembered
// was not a count ref at all, so X read zero, changeZoneChosenTargets' explicit
// zero bound skipped the ask entirely, and the graveyard cards never came
// back. The first test pins the whole flow; the second pins the empty-discard
// no-op the brief calls out (0 discarded -> X = 0 -> no ask, no wedge).

// loamcrafterEngine seats seat 0 a deck headed by Loamcrafter Faun, drives to
// Main 1, puts `lands` Forests into seat 0's hand, `graveyard` Grizzly Bears
// into seat 0's graveyard, and moves Loamcrafter Faun from hand/library onto
// the battlefield -- firing its ChangesZone ETB. It returns the engine parked
// wherever that trigger (or the test's own driving) left it, plus the moved
// ids. The board is deterministic: the moves are explicit logged MoveZone
// events, not draws.
func loamcrafterEngine(t *testing.T, lands, graveyard int) (*Engine, []state.ObjID, []state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	loam := searchCorpusCard(t, reg, "Loamcrafter Faun")
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{loam}
	// Alternating basics and Bears so both a land and a nonland permanent are
	// reliably in the library regardless of the opening hand.
	for len(deck) < 40 {
		deck = append(deck, forest, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = forest
	}
	cfg := seatZeroStart(Config{Seed: 9218, Names: []string{"protagonist", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	hand := moveNamedFromLibrary(t, e, "Forest", state.ZHand, lands,
		map[state.ObjID]bool{})
	grave := moveNamedFromLibrary(t, e, "Grizzly Bears", state.ZGraveyard, graveyard,
		map[state.ObjID]bool{})
	// Preconditions: the board really carries the pieces the assertions need.
	if len(hand) != lands {
		t.Fatalf("precondition: moved %d lands to hand, want %d", len(hand), lands)
	}
	if len(grave) != graveyard {
		t.Fatalf("precondition: moved %d nonland permanents to the graveyard, want %d", len(grave), graveyard)
	}
	for _, id := range hand {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand || o.Face() == nil || !o.Face().IsLand() {
			t.Fatalf("precondition: hand card %d = %+v, want a land in hand", id, o)
		}
	}
	for _, id := range grave {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard || o.Face() == nil || o.Face().IsLand() {
			t.Fatalf("precondition: graveyard card %d = %+v, want a nonland permanent in the graveyard", id, o)
		}
	}
	// The ETB fires here (the MoveZone to the battlefield is what the
	// ChangesZone self-trigger matches).
	searchMoveByName(t, e, "Loamcrafter Faun", state.ZBattlefield)
	onBF := false
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Loamcrafter Faun" {
			onBF = true
		}
	}
	if !onBF {
		t.Fatal("precondition: Loamcrafter Faun is not on the battlefield, so no ETB trigger could fire")
	}
	return e, hand, grave
}

// moveNamedFromLibrary emits explicit logged MoveZone events moving the first
// n library cards named `name` to `to` (skipping ids already claimed by a
// prior call in the same test), then re-asks priority. It never touches .cards
// content -- the card text stays in the gitignored corpus.
func moveNamedFromLibrary(t *testing.T, e *Engine, name string, to state.Zone, n int, used map[state.ObjID]bool) []state.ObjID {
	t.Helper()
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if len(out) == n {
			break
		}
		if used[id] {
			continue
		}
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil || o.Face().Name != name {
			continue
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: to})
		used[id] = true
		out = append(out, id)
	}
	e.pending = nil
	e.priorityRound()
	return out
}

// TestLoamcrafterFaunWhenYouDoReturnsThatMany is the reported card end to end:
// the ETB poses the optional any-number land discard, answering it with two
// lands makes the "when you do" ImmediateTrigger run, and its ChangeZone body
// is sized by SVar:X:TriggerRemembered$Amount -- so the return ask's Max is
// EXACTLY the number of discarded lands (2), and answering it moves the named
// graveyard permanents to hand. The ETB'd source itself (the fire-time
// capture) must NOT be counted, so Max is 2, never 3.
func TestLoamcrafterFaunWhenYouDoReturnsThatMany(t *testing.T) {
	t.Parallel()
	const discarded = 2
	e, hand, grave := loamcrafterEngine(t, discarded, 2)

	// (1) the optional any-number discard: a real mid-resolution KModes ask
	// over the hand's land cards, Min 0 / Max == the eligible land count.
	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("discard ask = %+v, want the optional any-number discard KModes", d)
	}
	if d.Min != 0 {
		t.Fatalf("discard ask Min = %d, want 0 (Optional$ True)", d.Min)
	}
	if d.Max < discarded {
		t.Fatalf("discard ask Max = %d, want at least %d eligible lands", d.Max, discarded)
	}
	// Answer exactly `discarded` lands. The options are per-card discard
	// options; take the first `discarded` that name a hand land.
	var picks []int
	for _, o := range d.Options {
		if o.Kind != "discard" {
			continue
		}
		inHand := false
		for _, id := range hand {
			if o.Obj == id {
				inHand = true
			}
		}
		if inHand {
			picks = append(picks, o.Index)
			if len(picks) == discarded {
				break
			}
		}
	}
	if len(picks) != discarded {
		t.Fatalf("only %d of the %d hand lands were offered as discard options: %+v", len(picks), discarded, d.Options)
	}
	// Prove the discard side really happened, so the return ask below cannot
	// pass on a board where nothing was discarded.
	before := 0
	for _, ev := range e.L.Events {
		if events.IsDiscard(ev) {
			before++
		}
	}
	submitChoices(t, e, picks...)
	after := 0
	for _, ev := range e.L.Events {
		if events.IsDiscard(ev) {
			after++
		}
	}
	if after-before != discarded {
		t.Fatalf("%d Discard events from the answer, want %d (the discard side did not run)", after-before, discarded)
	}

	// (2) the "when you do" return ask: ONE mid-resolution target ask over the
	// graveyard nonland permanents, with Max EXACTLY the number of lands
	// discarded -- the capture-excluded TriggerRemembered$Amount. Max 3 here
	// would be the fire-time capture (Loamcrafter Faun itself) leaking into
	// the count; the sibling ticket's plain Ctx.Remembered mapping is exactly
	// that bug.
	ret := passUntilAsk(t, e)
	if ret == nil {
		t.Fatal("no return ask after the discard: TriggerRemembered$Amount read zero and changeZoneChosenTargets skipped the ask")
	}
	if ret.Max != discarded {
		t.Fatalf("return ask Max = %d, want %d (the discarded lands, capture excluded): %+v", ret.Max, discarded, ret)
	}
	if ret.Min != 0 {
		t.Fatalf("return ask Min = %d, want 0 (TargetMin$ 0)", ret.Min)
	}
	var answer []int
	for _, o := range ret.Options {
		for _, id := range grave {
			if o.Obj == id {
				answer = append(answer, o.Index)
			}
		}
	}
	if len(answer) != len(grave) {
		t.Fatalf("the graveyard permanents were not all offered by the return ask: %+v", ret.Options)
	}
	submitChoices(t, e, answer...)

	// (3) the answered ChangeZone moved the named graveyard cards to hand.
	for _, id := range grave {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZHand {
			t.Fatalf("the answered return left card %d in %s, want hand (obj %+v)", id, o.Zone, o)
		}
	}
}

// TestLoamcrafterFaunEmptyDiscardIsASilentNoOp pins the brief's empty-set
// requirement: answering the optional discard with ZERO cards leaves the
// ConditionDefined$ Remembered / ConditionCompare$ GE1 gate false, so the
// ImmediateTrigger never runs, no return ask is posed, and the graveyard
// permanents stay put -- and the chain does not wedge (the game keeps
// advancing).
func TestLoamcrafterFaunEmptyDiscardIsASilentNoOp(t *testing.T) {
	t.Parallel()
	e, _, grave := loamcrafterEngine(t, 2, 2)

	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("discard ask = %+v, want the optional any-number discard KModes", d)
	}
	if d.Min != 0 || len(d.Options) == 0 {
		t.Fatalf("discard ask = %+v, want a Min 0 ask with offered cards (so declining is a real answer)", d)
	}
	// Answer zero cards: decline the whole optional discard. This must run
	// the handler (the discard primitive was reached and asked -- asserted by
	// d above being the ask), not skip the chain silently through an
	// unimplemented fallback.
	submitChoices(t, e)

	// The no-return-ask assertion must not pass because the feature is
	// unregistered: assert no unimplemented-API Note for the chain ran.
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && (strings.Contains(ev.Text, "ImmediateTrigger") || strings.Contains(ev.Text, "Discard")) {
			t.Fatalf("the empty-discard chain emitted an unimplemented note: %q", ev.Text)
		}
	}
	// Drive a few priority rounds; a return ask would surface. Assert none
	// appears and the graveyard is untouched.
	for i := 0; i < 8; i++ {
		nd := e.Pending()
		if nd == nil {
			e.Advance()
			continue
		}
		if nd.Kind != decision.KPriority {
			t.Fatalf("empty-discard chain posed an unexpected decision %+v, want no return ask", nd)
		}
		passChoice(t, e)
	}
	for _, id := range grave {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("the empty-discard answer moved card %d out of the graveyard (zone %s)", id, o.Zone)
		}
	}
}
