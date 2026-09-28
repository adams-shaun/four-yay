package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The trigger-body Cost$ window pinned end to end on the three REAL deck
// cards the trigger-body-cost brief named (the Desert Bloom / OTC precon
// census): Yuma, Proud Protector (commander, `Cost$ Sac<1/Land>` on an
// AB$ Draw ETB/attack body), Bitter Reunion (`Cost$ Discard<1/Card>` on an
// AB$ Draw body with NumCards$ 2) and Springbloom Druid (`Cost$ Sac<1/Land>`
// on an AB$ ChangeZone library search). The synthetic fixtures of
// trigger_cost_components_test.go and trigger_body_cost_test.go pin the
// window's mechanics; these tests prove the REAL compiled cards enter the
// window too -- the election is posed, a decline executes nothing, a pay
// settles the component for real and then runs the body.
//
// Springbloom Druid IS in the foundations-tramplesaurus-rex repo deck, so
// none of this may change engine behaviour that deck exercises; the heads
// and the ratchet are verified unmoved by the gate runs this report cites.

// realCardETBCost places a compiled corpus card in hand with optional extra
// hand cards, emits its ETB move (firing the ChangesZone trigger), drives
// the trigger onto and through the stack's top resolution, and returns the
// pending trigger-cost window ask. The etbCostWindow shape for a *cards.Card.
func realCardETBCost(t *testing.T, e *Engine, c *cards.Card, handExtra ...*cards.Card) *decision.Decision {
	t.Helper()
	for _, x := range handExtra {
		o := e.G.AddObject(x, 0)
		o.Zone = state.ZHand
		e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), o.ID))
	}
	src := e.G.AddObject(c, 0)
	src.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), src.ID))
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZHand, To: state.ZBattlefield})
	e.putTriggersOnStack()
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the trigger-cost window ask, got %+v", d)
	}
	return d
}

// movedEvents counts kinds of interest since mark.
func movedEvents(e *Engine, mark int) (draws, saccs, discards int) {
	for _, ev := range e.L.Events[mark:] {
		switch {
		case ev.Kind == events.Draw:
			draws++
		case events.IsSacrifice(ev):
			saccs++
		case events.IsDiscard(ev):
			discards++
		}
	}
	return
}

// TestYumaEnterCostDeclineDrawsNothing pins Yuma, Proud Protector's real
// corpus card: its ETB trigger's `Cost$ Sac<1/Land>` body poses the
// sacrifice election; declining draws nothing and sacrifices nothing.
func TestYumaEnterCostDeclineDrawsNothing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	yuma := searchCorpusCard(t, reg, "Yuma, Proud Protector")
	forest := searchCorpusCard(t, reg, "Forest")

	e := handEngine(t)
	land := onBoardCard(t, e, 0, forest)
	d := realCardETBCost(t, e, yuma)

	// Preconditions: Yuma itself is on the battlefield (the trigger fired on
	// its own entry) and the paying land is there for the cost to take.
	if o := e.G.Obj(srcID(t, e, "Yuma, Proud Protector")); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Yuma is not on the battlefield after entry")
	}
	if o := e.G.Obj(land); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the paying land must be on the battlefield")
	}
	pay, decline := windowPayDecline(t, d)
	if pay < 0 {
		t.Fatalf("Yuma's Sac<1/Land> cost was not offered as payable: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(land); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the declined cost must not sacrifice the land")
	}
	if draws, saccs, discards := movedEvents(e, mark); draws != 0 || saccs != 0 || discards != 0 {
		t.Fatalf("declining moved cards: draws=%d sacrifices=%d discards=%d", draws, saccs, discards)
	}
}

// srcID finds a battlefield object by face name.
func srcID(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("no battlefield object named %q", name)
	return 0
}

// TestYumaEnterCostPaysSacrificesChosenLandAndDraws pins the pay arm on the
// real card: with two eligible lands the walk poses the exact-1 sacrifice
// pick, the CHOSEN (not zone-order-first) land is sacrificed by a real
// events.Sacrifice, and the body draws exactly one.
func TestYumaEnterCostPaysSacrificesChosenLandAndDraws(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	yuma := searchCorpusCard(t, reg, "Yuma, Proud Protector")
	forest := searchCorpusCard(t, reg, "Forest")
	island := searchCorpusCard(t, reg, "Island")

	e := handEngine(t)
	landA := onBoardCard(t, e, 0, forest)
	landB := onBoardCard(t, e, 0, island)
	d := realCardETBCost(t, e, yuma)
	pay, _ := windowPayDecline(t, d)
	submitChoices(t, e, pay)

	pick := e.Pending()
	if pick == nil || pick.Kind != decision.KChoose || pick.Min != 1 || pick.Max != 1 {
		t.Fatalf("expected the exact-1 sacrifice pick, got %+v", pick)
	}
	if len(pick.Options) != 2 || pick.Options[0].Kind != "sacrifice" {
		t.Fatalf("sacrifice pick options = %+v, want 2 sacrifice-kind options", pick.Options)
	}
	pickIdx := -1
	for _, o := range pick.Options {
		if o.Obj == landB {
			pickIdx = o.Index
		}
	}
	if pickIdx < 0 {
		t.Fatalf("the pick does not offer the second land: %+v", pick.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, pickIdx)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(landB); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the chosen land zone = %v, want the graveyard", o)
	}
	if o := e.G.Obj(landA); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the unpicked land must stay on the battlefield")
	}
	sawSac := false
	for _, ev := range e.L.Events[mark:] {
		if events.IsSacrifice(ev) && ev.Obj == landB {
			sawSac = true
		}
	}
	if !sawSac {
		t.Fatal("no events.Sacrifice for the chosen land")
	}
	draws, _, _ := movedEvents(e, mark)
	if draws != 1 {
		t.Fatalf("draw events = %d, want 1 (the paid body)", draws)
	}
}

// TestBitterReunionCostDiscardThenDrawsTwo pins Bitter Reunion's real card:
// the ETB election's pay walks the `Cost$ Discard<1/Card>` pick, the
// unpicked hand card stays, and the body draws its two. The decline moves
// nothing.
func TestBitterReunionCostDiscardThenDrawsTwo(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	reunion := searchCorpusCard(t, reg, "Bitter Reunion")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")

	// Decline arm.
	e := handEngine(t)
	d := realCardETBCost(t, e, reunion, bear, bear)
	pay, decline := windowPayDecline(t, d)
	if pay < 0 {
		t.Fatalf("Bitter Reunion's Discard<1/Card> cost was not offered as payable: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 0)); got != 2 {
		t.Fatalf("hand after decline = %d, want the untouched 2", got)
	}
	if draws, _, discards := movedEvents(e, mark); draws != 0 || discards != 0 {
		t.Fatalf("declining moved cards: draws=%d discards=%d", draws, discards)
	}

	// Pay arm: the exact-1 discard pick over the two hand cards.
	e2 := handEngine(t)
	d2 := realCardETBCost(t, e2, reunion, bear, bear)
	pay2, _ := windowPayDecline(t, d2)
	submitChoices(t, e2, pay2)
	pick := e2.Pending()
	if pick == nil || pick.Kind != decision.KChoose || pick.Min != 1 || pick.Max != 1 {
		t.Fatalf("expected the exact-1 discard pick, got %+v", pick)
	}
	if len(pick.Options) != 2 || pick.Options[0].Kind != "discard" {
		t.Fatalf("discard pick options = %+v, want 2 discard-kind options", pick.Options)
	}
	staying := pick.Options[1].Obj
	mark2 := len(e2.L.Events)
	submitChoices(t, e2, pick.Options[0].Index)
	passUntilStackEmpty(t, e2, 20)

	if o := e2.G.Obj(staying); o == nil || o.Zone != state.ZHand {
		t.Fatalf("the unpicked card must stay in hand")
	}
	// 1 discarded, then the body's two drawn: the staying card plus two draws.
	if got := len(e2.G.Zone(state.ZHand, 0)); got != 3 {
		t.Fatalf("hand after pay = %d, want 3 (the staying card plus the two draws)", got)
	}
	discards := 0
	for _, ev := range e2.L.Events[mark2:] {
		if events.IsDiscardCost(ev) {
			discards++
		}
	}
	if discards != 1 {
		t.Fatalf("discard-cost events = %d, want 1", discards)
	}
	draws, _, _ := movedEvents(e2, mark2)
	if draws != 2 {
		t.Fatalf("draw events = %d, want 2 (NumCards$ 2)", draws)
	}
}

// TestSpringbloomDruidCostPaysThenSearches pins Springbloom Druid's real
// card: paying the `Cost$ Sac<1/Land>` sacrifices the land and THEN the
// body's up-to-two basic-land search asks over the library; the answered
// two basics enter the battlefield tapped. The decline keeps the land and
// never searches.
func TestSpringbloomDruidCostPaysThenSearches(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	spring := searchCorpusCard(t, reg, "Springbloom Druid")
	forest := searchCorpusCard(t, reg, "Forest")

	// Decline arm.
	e := handEngine(t)
	land := onBoardCard(t, e, 0, forest)
	d := realCardETBCost(t, e, spring)
	pay, decline := windowPayDecline(t, d)
	if pay < 0 {
		t.Fatalf("Springbloom's Sac<1/Land> cost was not offered as payable: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(land); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the declined cost must not sacrifice the land")
	}
	if _, saccs, _ := movedEvents(e, mark); saccs != 0 {
		t.Fatalf("declining still sacrificed")
	}
	libAfter := len(e.G.Zone(state.ZLibrary, 0))
	_ = libAfter

	// Pay arm: sacrifice, then the library search.
	e2 := handEngine(t)
	land2 := onBoardCard(t, e2, 0, forest)
	libBefore := len(e2.G.Zone(state.ZLibrary, 0))
	d2 := realCardETBCost(t, e2, spring)
	pay2, _ := windowPayDecline(t, d2)
	// The sacrifice settles at the pay submit (one eligible land, no pick
	// ask), BEFORE the search ask is posed -- mark from there.
	mark2 := len(e2.L.Events)
	submitChoices(t, e2, pay2)

	search := e2.Pending()
	if search == nil || search.Kind != decision.KChoose || search.Min != 0 || search.Max != 2 {
		t.Fatalf("expected the up-to-2 basic-land search ask, got %+v", search)
	}
	if len(search.Options) < 2 {
		t.Fatalf("search options = %d, want at least the two library basics", len(search.Options))
	}
	landsBefore := len(e2.G.Zone(state.ZBattlefield, 0))
	submitChoices(t, e2, search.Options[0].Index, search.Options[1].Index)
	// ShuffleNonMandatory$ True: the moved cards pose the may-shuffle confirm
	// (searchmay1); accept it, then drain.
	ms := e2.Pending()
	if ms == nil || ms.ResumeKind != "search_mayshuffle" {
		t.Fatalf("expected the may-shuffle confirm after the search, got %+v", ms)
	}
	submitChoices(t, e2, ms.Options[0].Index) // yes — shuffle
	passUntilStackEmpty(t, e2, 20)

	if o := e2.G.Obj(land2); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the paying land zone = %v, want the graveyard", o)
	}
	sawSac := false
	for _, ev := range e2.L.Events[mark2:] {
		if events.IsSacrifice(ev) && ev.Obj == land2 {
			sawSac = true
		}
	}
	if !sawSac {
		t.Fatal("no events.Sacrifice for the paying land")
	}
	// Two searched basics on the battlefield, tapped.
	if got := len(e2.G.Zone(state.ZBattlefield, 0)) - landsBefore; got != 2 {
		t.Fatalf("battlefield grew by %d, want the 2 searched basics", got)
	}
	mountains := 0
	tapped := 0
	for _, id := range e2.G.Zone(state.ZBattlefield, 0) {
		o := e2.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == "Mountain" {
			mountains++
			if o.Tapped {
				tapped++
			}
		}
	}
	if mountains != 2 || tapped != 2 {
		t.Fatalf("battlefield Mountains = %d with %d tapped, want 2 and 2", mountains, tapped)
	}
	if got := libBefore - len(e2.G.Zone(state.ZLibrary, 0)); got != 2 {
		t.Fatalf("library shrank by %d, want 2", got)
	}
}
