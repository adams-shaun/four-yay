package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// TestTitanOfLittjaraDrawXCost drives the ticket's canonical carrier end to
// end through the real trigger window, with the shared creature type
// CONFIGURED: Titan enters over a Grizzly Bears, its as-enters ChooseType is
// answered "Bear", so `SVar:X:Count$Valid
// Creature.YouCtrl+Other+sharesCreatureTypeWith` folds to exactly 1 (the one
// other Bear). Paying must draw exactly that one card and then run the
// `Mode$ TgtChoose` discard; declining must do neither; and the window must
// pose exactly ONE pay/decline election per trigger (the round-1 pin paid
// every election it was shown, which masked the duplicate-window shape).
func TestTitanOfLittjaraDrawXCost(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, titan := kr5TitanBearFixture(t, reg)

	// Precondition: the fold's own verdict on this board is EXACTLY the one
	// other Bear — a zero here would mean the shared-type read is broken and
	// every assertion below would pass vacuously.
	n, ok := pay.DrawCostCount(asPayer(e), titan, 0, drawCostPart())
	if !ok || n != 1 {
		t.Fatalf("drawCostCount(Titan) = %d, %v; want exactly 1 (the one other Bear sharing the chosen type)", n, ok)
	}

	// The ETB trigger must have pushed and posed the cost election. A
	// decline-only ask would mean the Draw part was withheld, and a priority
	// ask would mean the trigger window never opened.
	d := passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected Titan's trigger-cost pay ask, got %+v", d)
	}
	if d.Source != titan {
		t.Fatalf("the pay ask's source is %d, want Titan %d", d.Source, titan)
	}
	payIdx, declineIdx := -1, -1
	for _, op := range d.Options {
		switch op.Kind {
		case "trigger_cost_pay":
			payIdx = op.Index
		case "trigger_cost_decline":
			declineIdx = op.Index
		}
	}
	if payIdx < 0 || declineIdx < 0 {
		t.Fatalf("Titan's Draw<X/You> cost was not offered as a pay/decline election: %+v", d.Options)
	}

	// Paying: the window settles exactly one election's draw and then runs
	// the Discard body. The very next non-priority ask must be that body's
	// discard — a second pay ask here is a duplicate cost window (round 1's
	// finding), a priority ask means the body never ran.
	mark := len(e.L.Events)
	submitChoices(t, e, payIdx)
	discard := passUntilNonPriority(t, e, 40)
	if discard == nil || discard.ResumeKind != "discard" {
		t.Fatalf("after paying, the next ask is %+v; want the discard body's ask (exactly one payment election per trigger)", discard)
	}
	draws := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
	}
	if draws != 1 {
		t.Fatalf("paying Titan's Draw<X/You> cost drew %d cards, want exactly the folded count 1", draws)
	}

	// The discard body then discards exactly one card.
	mark2 := len(e.L.Events)
	submitChoices(t, e, discard.Options[0].Index)
	discards := 0
	for _, ev := range e.L.Events[mark2:] {
		if events.IsDiscard(ev) && ev.Player == 0 {
			discards++
		}
	}
	if discards != 1 {
		t.Fatalf("the paid body's discard discarded %d cards, want 1", discards)
	}
}

// TestTitanOfLittjaraDrawXDecline is the decline arm on the same configured
// board: the election is still posed (the feature is registered, not
// unimplemented), and declining leaves the body unrun — no draw, no discard,
// no hand or library movement.
func TestTitanOfLittjaraDrawXDecline(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, titan := kr5TitanBearFixture(t, reg)
	if n, ok := pay.DrawCostCount(asPayer(e), titan, 0, drawCostPart()); !ok || n != 1 {
		t.Fatalf("drawCostCount(Titan) = %d, %v; want exactly 1", n, ok)
	}

	d := passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected Titan's trigger-cost pay ask, got %+v", d)
	}
	declineIdx := -1
	for _, op := range d.Options {
		if op.Kind == "trigger_cost_decline" {
			declineIdx = op.Index
		}
	}
	if declineIdx < 0 {
		t.Fatalf("no decline option on the decline fixture: %+v", d.Options)
	}
	mark := len(e.L.Events)
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	handBefore := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, declineIdx)
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			t.Fatalf("a declined Draw<X/You> cost drew a card")
		}
		if events.IsDiscard(ev) {
			t.Fatalf("a declined Draw<X/You> cost still ran the Discard body")
		}
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != libBefore {
		t.Fatalf("library = %d after a declined cost, want unchanged %d", got, libBefore)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore {
		t.Fatalf("hand = %d after a declined cost, want unchanged %d", got, handBefore)
	}
}

// kr5TitanBearFixture is titanBearFixture under the resolution kernel: the
// Titan's entry runs as a kernel probe from a quiet engine, so its as-enters
// ChooseType ask is posed and answered "Bear"; the engine then continues
// (kr5Settle) so the queued ETB trigger reaches the stack.
func kr5TitanBearFixture(t *testing.T, reg *cards.Registry) (*Engine, state.ObjID) {
	t.Helper()
	e, _ := searchEngine(t, reg, "Titan of Littjara")
	bears, _ := cleanMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	if bears == 0 {
		t.Fatal("no Grizzly Bears fixture")
	}
	var titan state.ObjID
	var from state.Zone
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Titan of Littjara" && titan == 0 {
				titan, from = id, z
			}
		}
	}
	if titan == 0 {
		t.Fatal("no Titan of Littjara fixture")
	}
	e.pending = nil
	e.probe(func() {
		e.emit(events.Event{Kind: events.MoveZone, Obj: titan, From: from, To: state.ZBattlefield})
	})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Titan's entry posed no ChooseType ask: %+v", d)
	}
	idx := -1
	for _, op := range d.Options {
		if op.Kind == "type" && op.Label == "Bear" {
			idx = op.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the ChooseType ask offered no Bear option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit ChooseType Bear: %v", err)
	}
	if got := e.G.Obj(titan).ChosenType; got != "Bear" {
		t.Fatalf("Titan's chosen type = %q, want Bear (the shared-type precondition)", got)
	}
	kr5Settle(e)
	return e, titan
}
