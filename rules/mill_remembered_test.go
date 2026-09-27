package rules

// The Mill-remember fix (task agent-20260918T201731Z-86b4ef09): effMill never
// recorded which cards it milled, so a chained "put a card from among them
// into your hand" pickup -- filtered on Card.IsRemembered -- found nothing and
// silently no-oped after the mill. Six is the shape end to end: it mills
// three on attack (RememberMilled$ True), picks a milled land into hand
// (ChangeType$ Land.IsRemembered), then DB$ Cleanup | ClearRemembered$ True.
//
// Six is the corpus's only repo-deck RememberMilled$ carrier (pro-shaper.json)
// and, crucially, uses a TYPED base (Land.IsRemembered) -- the bare
// Permanent.IsRemembered family hits a separate zone-blind matchesBase defect
// (see the report's Issues section) that would mask this fix.
//
// No Forge script text is committed: Six is fetched from the corpus registry.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// millRememberedEngine seats Six on the battlefield for seat 0 and puts four
// lands on top of seat 0's library -- the mill's window (seat 0 draws one at
// the start of its next turn, leaving three) -- by emitting a real
// LibraryOrder event (the deck itself is shuffled, so deck-order seeding
// would not survive the deal). It returns the engine, the config for
// replayCheck, Six's object id, and the ids of the three milled library cards
// in top-to-bottom order.
func millRememberedEngine(t *testing.T) (*Engine, Config, state.ObjID, []state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	six := searchCorpusCard(t, reg, "Six")
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")

	// A land-heavy deck so seat 0's library holds plenty of lands after the
	// shuffled opening hand.
	deck := []*cards.Card{six}
	for i := 0; i < 12; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := mountainDeck(t, 40)
	cfg := seatZeroStart(Config{Seed: 6201, Names: []string{"miller", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	// Put four lands on top of seat 0's library through a real (logged)
	// LibraryOrder event -- the deck is shuffled, so the top of the library
	// must be set explicitly. Four, not three: seat 0 draws one at the start
	// of its next turn, leaving three lands in the mill window.
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	var top, rest []state.ObjID
	want := []*cards.Card{forest, forest, mountain, forest}
	for _, wantCard := range want {
		for i, id := range lib {
			if id == 0 {
				continue
			}
			if e.G.Obj(id).Card == wantCard {
				top = append(top, id)
				lib[i] = 0
				break
			}
		}
	}
	if len(top) != 4 {
		t.Fatalf("seat 0's library held only %d of the 4 wanted lands", len(top))
	}
	for _, id := range lib {
		if id != 0 {
			rest = append(rest, id)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: append(top, rest...)})

	// Six in play for seat 0. It just entered, so it is summoning sick; drive
	// to seat 0's NEXT declare-attackers step (turn 3), where the untap step
	// has cleared the sickness through the ordinary logged flow -- a direct
	// SummonSick=false would replay differently.
	sixID := searchMoveByName(t, e, "Six", state.ZBattlefield)
	driveToStep(t, e, 3, 0, state.StepDeclareAttackers)

	// The three cards the mill will move: the top three of seat 0's library
	// after its turn-3 draw (all lands).
	var milled []state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if len(milled) == 3 {
			break
		}
		milled = append(milled, id)
	}
	if len(milled) != 3 {
		t.Fatalf("library held %d cards, want at least 3", len(milled))
	}
	for _, id := range milled {
		f := e.G.Obj(id).Face()
		if f == nil || (f.Name != "Forest" && f.Name != "Mountain") {
			t.Fatalf("library top %d is %q, want a land (the deal changed)", id, nameOf(f))
		}
	}
	return e, cfg, sixID, milled
}

func nameOf(f *cards.Face) string {
	if f == nil {
		return "<nil>"
	}
	return f.Name
}

// TestMillRememberMilledPicksUpARememberedLand drives Six's real attack
// trigger end to end: the mill records its three cards, the chained hidden
// pick offers the milled lands and moves the answered one to hand, the
// cleanup clears the remembered list, and the game replays byte-identically.
func TestMillRememberMilledPicksUpARememberedLand(t *testing.T) {
	e, cfg, sixID, milled := millRememberedEngine(t)

	// The helper already drove to seat 0's declare-attackers step; attack
	// with Six so the mill trigger fires.
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, sixID)

	// The attack trigger mills three, then the pickup suspends on a
	// hidden_pick ask over the milled lands.
	d := passUntilNonPriority(t, e, 40)
	// The explicit Optional$ marker poses Forge's confirm-before-pick gate
	// first (optional_confirm1): accept it, then the hidden_pick over the
	// milled lands follows.
	if d == nil || d.ResumeKind != "hidden_pick_confirm" {
		t.Fatalf("pending = %+v, want the hidden_pick_confirm gate (Optional$ True asks before the pick)", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hidden_pick" {
		t.Fatalf("pending = %+v, want a hidden_pick KChoose over the milled lands", d)
	}

	// The ask is Optional$ (Min 0, Max 1) over every milled land -- all three
	// milled cards are lands, and every offered option must be one of them.
	if d.Min != 0 || d.Max != 1 {
		t.Fatalf("pick = %d..%d, want the Optional$ 0..1", d.Min, d.Max)
	}
	if len(d.Options) != 3 {
		t.Fatalf("options = %+v, want the three milled lands", d.Options)
	}
	var landOpt *decision.Option
	for i := range d.Options {
		o := &d.Options[i]
		obj := e.G.Obj(o.Obj)
		if obj == nil || obj.Zone != state.ZGraveyard {
			t.Fatalf("offered card %d is not in the graveyard: %+v", o.Obj, obj)
		}
		inMilled := false
		for _, id := range milled {
			if id == o.Obj {
				inMilled = true
			}
		}
		if !inMilled {
			t.Fatalf("offered card %d was not milled this trigger", o.Obj)
		}
		if obj.Face() != nil && obj.Face().Name == "Forest" && landOpt == nil {
			landOpt = o
		}
	}
	if landOpt == nil {
		t.Fatalf("no milled Forest offered: %+v", d.Options)
	}

	// The mill itself is recorded on the source's event-backed remembered
	// list before any pickup.
	sawRemembered := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Obj == sixID && ev.Counter == "remembered" {
			sawRemembered = true
		}
	}
	if !sawRemembered {
		t.Fatal("no Choose/remembered event on Six after the mill")
	}

	// Answer the pick with the first Forest; it must move to hand.
	submitChoices(t, e, landOpt.Index)
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(landOpt.Obj); o == nil || o.Zone != state.ZHand {
		t.Fatalf("picked Forest = %+v, want in hand", e.G.Obj(landOpt.Obj))
	}
	// The other two milled cards stayed in the graveyard.
	kept := 0
	for _, id := range milled {
		if id == landOpt.Obj {
			continue
		}
		if o := e.G.Obj(id); o != nil && o.Zone == state.ZGraveyard {
			kept++
		}
	}
	if kept != 2 {
		t.Fatalf("milled-but-unpicked cards in graveyard = %d, want 2", kept)
	}

	// DB$ Cleanup | ClearRemembered$ True ran after the pickup: the source's
	// remembered list is empty and a clear-remembered event was emitted.
	if o := e.G.Obj(sixID); o == nil {
		t.Fatal("Six vanished")
	} else if len(o.Remembered) != 0 {
		t.Fatalf("Six's remembered list = %+v, want empty after DBCleanup", o.Remembered)
	}
	sawClear := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Obj == sixID && ev.Counter == "clear-remembered" {
			sawClear = true
		}
	}
	if !sawClear {
		t.Fatal("no Choose/clear-remembered event on Six after DBCleanup")
	}

	replayCheck(t, e, cfg)
}
