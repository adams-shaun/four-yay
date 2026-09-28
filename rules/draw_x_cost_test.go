package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// cost:Draw — the X-form Draw cost (Forge's Cost$ Draw<X/Spec>).
//
// The literal form Draw<N/Spec> was already modelled; the dynamic form
// Draw<X/Spec> (9 corpus carriers, all Draw<X/You>, all with a card-level
// SVar:X body) used to fall through nonManaCost to the unrecognised-symbol
// fallback, which substituted one generic mana and reported the head as the
// parameter census's `cost:Draw` label. It is now parsed as a Draw part whose
// Dyn names the source SVar, and resolved at payment by drawCostCount /
// drawCostCountTrig (the twin of fixLifeXCost for PayLife<X>).
//
// These tests pin the three legs the ticket names:
//
//   - the fold (a resolvable SVar:X resolves to its count) and the
//     fail-closed withhold (an absent/unresolvable body is not offered);
//   - the end-to-end real-corpus trigger window on Titan of Littjara, the
//     ticket's canonical carrier, with the shared creature type actually
//     configured (the entry ChooseType answered) and the positive exact
//     draw count asserted.
//
// Champion of Wits (SVar:X:Count$CardPower) is the fold's positive control:
// its body resolves to the permanent's own power. Titan of Littjara
// (SVar:X:Count$Valid Creature.YouCtrl+Other+sharesCreatureTypeWith) is the
// trigger-window pin. Neither card is in a repo deck, so no chain head or
// acceptance-ratchet row depends on these tests.

// drawCostPart is the parsed shape every Draw<X/...> carrier produces.
func drawCostPart() CostPart { return CostPart{Dyn: "X", Spec: "You"} }

// cleanMoveByName is searchMoveByName without the pending-ask wipe: it emits
// the zone move and returns whatever non-priority decision the entry posed
// instead of destroying it. The distinction is load-bearing for any fixture
// whose entry poses a real mid-resolution ask (Titan of Littjara's ChooseCT):
// Engine.Ask parks the asking effect's resume frame on e.resume, and wiping
// only e.pending leaves that frame armed with nothing pending — the next
// answered KChoose is then hijacked by the stale frame (handleChoose's
// mid-resolution arm runs before the e.choosing switch), which is exactly the
// duplicate-pay-ask shape round 1 measured and this round's finding named.
func cleanMoveByName(t *testing.T, e *Engine, name string, to state.Zone) (state.ObjID, *decision.Decision) {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				if z != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				}
				return id, e.Pending()
			}
		}
	}
	t.Fatalf("corpus fixture %q absent from hand/library", name)
	return 0, nil
}

// titanBearFixture builds the shared-type board the card's own text assumes:
// a Grizzly Bears on the battlefield, then Titan of Littjara entering, whose
// as-enters ChooseType ask is answered "Bear" — so Titan IS a Bear (its own
// `AddType$ ChosenType` static) and the one other Bear shares the type.
// Returns the engine and Titan's object id.
func titanBearFixture(t *testing.T, reg *cards.Registry) (*Engine, state.ObjID) {
	t.Helper()
	e, _ := searchEngine(t, reg, "Titan of Littjara")
	bears, _ := cleanMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	if bears == 0 {
		t.Fatal("no Grizzly Bears fixture")
	}
	titan, d := cleanMoveByName(t, e, "Titan of Littjara", state.ZBattlefield)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choosetype" {
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
	return e, titan
}

// TestDrawXCostSVarFoldsAndDraws pins the fold: a Draw<X/You> part on a face
// whose SVar:X resolves yields exactly that count, and the trigger window
// offers the pay election (so the handler is provably reached).
func TestDrawXCostSVarFoldsAndDraws(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Champion of Wits")
	id := searchMoveByName(t, e, "Champion of Wits", state.ZBattlefield)

	// Precondition: the moved permanent really is on the battlefield with a
	// resolvable SVar:X body. A vacuous fixture (wrong zone, absent body)
	// must fail here, not pass the assertion below silently.
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("Champion of Wits has no face after the move")
	}
	if !onBattlefield(e, id) {
		t.Fatalf("Champion of Wits is not on the battlefield; the fold's source is absent")
	}
	if body, ok := o.Face().SVars["X"]; !ok || body == "" {
		t.Fatalf("Champion of Wits has no SVar:X body; the fold has nothing to resolve")
	}

	n, ok := e.drawCostCount(id, 0, drawCostPart())
	if !ok {
		t.Fatalf("drawCostCount refused Champion of Wits' resolvable SVar:X")
	}
	if want := int32(e.Derived(id).Power); n != want {
		t.Fatalf("drawCostCount = %d, want Champion of Wits' derived power %d", n, want)
	}
	if n <= 0 {
		t.Fatalf("drawCostCount = %d; the fixture must resolve to a positive count", n)
	}

	// The window must actually offer the election, or the fold is unreachable.
	pay := witsPayAsk(t, e)
	if witsPayOption(pay) < 0 {
		t.Fatalf("the resolvable Draw<X/You> cost was not offered as payable: %+v", pay.Options)
	}
}

// TestDrawXUnresolvableWithheld pins the fail-closed direction: a cost whose
// source face does NOT define SVar:X is refused by the fold, and the trigger
// window then offers the decline only (never a silent zero draw).
func TestDrawXUnresolvableWithheld(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Grizzly Bears")
	id := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)

	o := e.G.Obj(id)
	if o == nil || o.Face() == nil || !onBattlefield(e, id) {
		t.Fatalf("Grizzly Bears is not a live battlefield source")
	}
	if _, present := o.Face().SVars["X"]; present {
		t.Fatalf("Grizzly Bears unexpectedly defines SVar:X; pick a bodyless source")
	}
	if _, ok := e.drawCostCount(id, 0, drawCostPart()); ok {
		t.Fatalf("drawCostCount accepted a Draw<X/You> part with no SVar:X body")
	}

	// A nil/unknown object is refused too (the same fail-closed contract).
	if _, ok := e.drawCostCount(state.ObjID(0), 0, drawCostPart()); ok {
		t.Fatalf("drawCostCount accepted a Draw<X/You> part for a nil source")
	}
}

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
	e, titan := titanBearFixture(t, reg)

	// Precondition: the fold's own verdict on this board is EXACTLY the one
	// other Bear — a zero here would mean the shared-type read is broken and
	// every assertion below would pass vacuously.
	n, ok := e.drawCostCount(titan, 0, drawCostPart())
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
	e, titan := titanBearFixture(t, reg)
	if n, ok := e.drawCostCount(titan, 0, drawCostPart()); !ok || n != 1 {
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
