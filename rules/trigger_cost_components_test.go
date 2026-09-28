package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The trigcost2 pins: the triggered-cost window's pay arm settles the
// Sac/Exile/Discard cost components for real (the walk the mandatory arm
// already runs), so a "you may pay <cost>. If you do, ..." trigger whose
// cost carries a choice-bearing component is genuinely payable instead of
// either silently skipping the component on a Draw-bearing cost or offering
// decline-only on a component-only one. All fixtures are inline synthetic
// scripts (no Forge text committed); the cost specs are the real carriers'
// verbatim shapes. None of the named real carriers (Ambergris Citadel Agent,
// Springbloom Druid, Sanctum of Ugin) is in any legacy golden deck.

// discardCostLooter is the Ambergris-shape fixture: an ETB trigger whose
// body carries `Cost$ Discard<1/Hand> Draw<2/You>` verbatim (the SILENT-SKIP
// carrier's exact cost) behind a one-card body draw.
const discardCostLooter = "Name:Ambergris Looter\nManaCost:2 U\nTypes:Creature Human Wizard\nPT:1/1\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCost | TriggerDescription$ When CARDNAME enters, you may discard your hand. If you do, draw two cards.\n" +
	"SVar:TrigCost:AB$ Draw | Cost$ Discard<1/Hand> Draw<2/You> | NumCards$ 1\n" +
	"Oracle:x\n"

// etbCostWindow places src in hand with extra hand cards, emits its ETB move
// (firing the ChangesZone trigger) and returns the pending trigger-cost
// window ask.
func etbCostWindow(t *testing.T, e *Engine, script string, handExtra ...*cards.Card) *decision.Decision {
	t.Helper()
	for _, c := range handExtra {
		o := e.G.AddObject(c, 0)
		o.Zone = state.ZHand
		e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), o.ID))
	}
	src := e.G.AddObject(card(t, script), 0)
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

// windowPayDecline returns the pay and decline option indices of a pending
// trigger-cost window ask.
func windowPayDecline(t *testing.T, d *decision.Decision) (pay, decline int) {
	t.Helper()
	pay, decline = -1, -1
	for _, o := range d.Options {
		switch o.Kind {
		case "trigger_cost_pay":
			pay = o.Index
		case "trigger_cost_decline":
			decline = o.Index
		}
	}
	if decline < 0 {
		t.Fatalf("no trigger-cost window options: %+v", d.Options)
	}
	return pay, decline
}

// TestAmbergrisStyleDiscardDrawCostPays pins the silent-skip class end to
// end: paying the `Cost$ Discard<1/Hand> Draw<2/You>` body drains the whole
// hand (the Hand spec pays every hand card), emits the cost-discard events
// BEFORE any draw, draws the cost's two cards plus the body's one, and the
// body runs. The decline moves nothing.
func TestAmbergrisStyleDiscardDrawCostPays(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, discardCostJunk), card(t, discardCostJunk))

	d := etbCostWindow(t, e, discardCostLooter)
	if got := len(e.G.Zone(state.ZHand, 0)); got != 2 {
		t.Fatalf("fixture hand = %d cards, want the 2 junk cards", got)
	}
	pay, _ := windowPayDecline(t, d)
	if pay < 0 {
		t.Fatalf("the Discard<1/Hand> Draw<2/You> cost was not offered as payable: %+v", d.Options)
	}

	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	mark := len(e.L.Events)
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	// 2 discarded, then 3 drawn (the cost's two plus the body's one).
	if got := len(e.G.Zone(state.ZHand, 0)); got != 3 {
		t.Fatalf("hand after pay = %d, want 3 (the drawn cards; the whole hand was discarded first)", got)
	}
	for _, z := range e.G.Zone(state.ZGraveyard, 0) {
		if o := e.G.Obj(z); o != nil && o.Face() != nil && o.Face().Name == "Discard Fodder" && o.Zone != state.ZGraveyard {
			t.Fatalf("a junk hand card did not reach the graveyard")
		}
	}
	discards, draws := 0, 0
	lastDiscard, firstDraw := -1, len(e.L.Events)
	for i, ev := range e.L.Events[mark:] {
		if events.IsDiscardCost(ev) {
			discards++
			lastDiscard = i
		}
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
			if i < firstDraw {
				firstDraw = i
			}
		}
	}
	if discards != 2 {
		t.Fatalf("discard-cost events = %d, want 2 (the whole hand)", discards)
	}
	if draws != 3 {
		t.Fatalf("draw events = %d, want 3 (the cost's two plus the body's one)", draws)
	}
	if lastDiscard >= firstDraw {
		t.Fatal("a draw event was recorded before the cost's discards were paid")
	}
	if got := libBefore - len(e.G.Zone(state.ZLibrary, 0)); got != 3 {
		t.Fatalf("library shrank by %d cards, want 3", got)
	}
	for _, z := range e.G.Zone(state.ZGraveyard, 0) {
		if o := e.G.Obj(z); o != nil && o.Face() != nil && o.Face().Name == "Ambergris Looter" {
			t.Fatalf("the source was discarded by its own cost; only the hand pays")
		}
	}
}

// TestAmbergrisStyleDiscardDrawCostDeclineMovesNothing pins the decline arm:
// declining the pay election discards nothing and draws nothing.
func TestAmbergrisStyleDiscardDrawCostDeclineMovesNothing(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, discardCostJunk), card(t, discardCostJunk))

	d := etbCostWindow(t, e, discardCostLooter)
	_, decline := windowPayDecline(t, d)
	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)

	if got := len(e.G.Zone(state.ZHand, 0)); got != 2 {
		t.Fatalf("hand after decline = %d, want the untouched 2", got)
	}
	for _, ev := range e.L.Events[mark:] {
		if events.IsDiscard(ev) || ev.Kind == events.Draw {
			t.Fatalf("a declined cost still moved a card: %+v", ev)
		}
	}
}

// TestTriggerCostEmptyHandDeclinesNoHalfPayment pins the gate's empty-hand
// half: with an empty hand the Hand-spec discard cannot pay, so the window
// offers DECLINE ONLY -- the cost's draw half is never paid on an unpaid
// component.
func TestTriggerCostEmptyHandDeclinesNoHalfPayment(t *testing.T) {
	t.Parallel()
	e := handEngine(t)

	d := etbCostWindow(t, e, discardCostLooter)
	pay, decline := windowPayDecline(t, d)
	if pay >= 0 {
		t.Fatalf("an empty-hand Discard<1/Hand> cost was offered as payable: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)

	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw || events.IsDiscard(ev) {
			t.Fatalf("the unpayable cost still moved a card: %+v", ev)
		}
	}
}

// TestTriggerCostEmptyHandOrdinarySpecDeclines is the ordinary-spec twin: a
// `Cost$ Discard<1/Card>` body with no hand card is decline-only too.
func TestTriggerCostEmptyHandOrdinarySpecDeclines(t *testing.T) {
	t.Parallel()
	const script = "Name:Ordinary Discarder\nManaCost:1 B\nTypes:Creature Zombie\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCost | TriggerDescription$ When CARDNAME enters, you may discard a card. If you do, draw a card.\n" +
		"SVar:TrigCost:AB$ Draw | Cost$ Discard<1/Card> | NumCards$ 1\n" +
		"Oracle:x\n"
	e := handEngine(t)

	d := etbCostWindow(t, e, script)
	pay, decline := windowPayDecline(t, d)
	if pay >= 0 {
		t.Fatalf("an empty-hand Discard<1/Card> cost was offered as payable: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw {
			t.Fatalf("the unpayable cost still drew: %+v", ev)
		}
	}
}

// TestTriggerCostDiscardChoiceAsksAndSettles pins the multi-candidate
// Discard component: with more eligible cards than the count the window
// poses a real exact-N KChoose (the chooseTriggeredMandatory walk), the
// picked cards go to the graveyard as cost discards, the unpicked card
// stays, and the body runs.
func TestTriggerCostDiscardChoiceAsksAndSettles(t *testing.T) {
	t.Parallel()
	const script = "Name:Choosy Discarder\nManaCost:1 B\nTypes:Creature Zombie\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCost | TriggerDescription$ When CARDNAME enters, you may discard two cards. If you do, draw a card.\n" +
		"SVar:TrigCost:AB$ Draw | Cost$ Discard<2/Card> | NumCards$ 1\n" +
		"Oracle:x\n"
	junk := card(t, discardCostJunk)
	e := handEngine(t, junk, junk, junk)

	d := etbCostWindow(t, e, script)
	pay, _ := windowPayDecline(t, d)
	submitChoices(t, e, pay)

	pick := e.Pending()
	if pick == nil || pick.Kind != decision.KChoose || pick.Min != 2 || pick.Max != 2 {
		t.Fatalf("expected the exact-2 discard pick, got %+v", pick)
	}
	if len(pick.Options) != 3 || pick.Options[0].Kind != "discard" {
		t.Fatalf("discard pick options = %+v, want 3 discard-kind options", pick.Options)
	}
	staying := pick.Options[2].Obj
	mark := len(e.L.Events)
	submitChoices(t, e, pick.Options[0].Index, pick.Options[1].Index)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(staying); o == nil || o.Zone != state.ZHand {
		t.Fatalf("the unpicked card must stay in hand, got %v", o)
	}
	// 2 discarded, then the body's one drawn.
	if got := len(e.G.Zone(state.ZHand, 0)); got != 2 {
		t.Fatalf("hand after pay = %d, want 2 (the unpicked card plus the body's draw)", got)
	}
	discards := 0
	for _, ev := range e.L.Events[mark:] {
		if events.IsDiscardCost(ev) {
			discards++
		}
	}
	if discards != 2 {
		t.Fatalf("discard-cost events = %d, want 2", discards)
	}
}

// TestTriggerCostSacComponentPays pins the Sac-component loot shape
// (Springbloom Druid's `Cost$ Sac<1/Land>`): with exactly one eligible land
// the settle needs no ask, the land is sacrificed by a real events.Sacrifice
// before the body runs, and the body draws.
func TestTriggerCostSacComponentPays(t *testing.T) {
	t.Parallel()
	const script = "Name:Ramp Elf\nManaCost:1 G\nTypes:Creature Elf Druid\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCost | TriggerDescription$ When CARDNAME enters, you may sacrifice a land. If you do, draw a card.\n" +
		"SVar:TrigCost:AB$ Draw | Cost$ Sac<1/Land> | NumCards$ 1\n" +
		"Oracle:x\n"
	const landScript = "Name:Test Land\nTypes:Land\nOracle:x\n"
	e := handEngine(t)
	land := onBoard(t, e, 0, landScript)

	d := etbCostWindow(t, e, script)
	pay, _ := windowPayDecline(t, d)
	if pay < 0 {
		t.Fatalf("the Sac<1/Land> cost was not offered as payable: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(land); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the paying land zone = %v, want the graveyard", o)
	}
	sawSac := false
	for _, ev := range e.L.Events[mark:] {
		if events.IsSacrifice(ev) && ev.Obj == land {
			sawSac = true
		}
	}
	if !sawSac {
		t.Fatal("no events.Sacrifice for the paying land")
	}
	draws := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
	}
	if draws != 1 {
		t.Fatalf("draw events = %d, want 1 (the paid body)", draws)
	}
}

// TestTriggerCostSacComponentMultiCandidateAsks pins the Sac pick path: with
// two eligible lands the walk poses a real exact-1 KChoose and only the
// picked land leaves.
func TestTriggerCostSacComponentMultiCandidateAsks(t *testing.T) {
	t.Parallel()
	const script = "Name:Ramp Elf\nManaCost:1 G\nTypes:Creature Elf Druid\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCost | TriggerDescription$ When CARDNAME enters, you may sacrifice a land. If you do, draw a card.\n" +
		"SVar:TrigCost:AB$ Draw | Cost$ Sac<1/Land> | NumCards$ 1\n" +
		"Oracle:x\n"
	const landScript = "Name:Test Land\nTypes:Land\nOracle:x\n"
	e := handEngine(t)
	landA := onBoard(t, e, 0, landScript)
	landB := onBoard(t, e, 0, landScript)

	d := etbCostWindow(t, e, script)
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
	if pick.Options[0].Obj == landA {
		pickIdx = 0
	}
	if pickIdx < 0 {
		t.Fatalf("the pick does not offer the first land: %+v", pick.Options)
	}
	submitChoices(t, e, pickIdx)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(landA); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the picked land zone = %v, want the graveyard", o)
	}
	if o := e.G.Obj(landB); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the unpicked land must stay on the battlefield")
	}
}

// TestTriggerCostUnpayableSacComponentDeclinesOnly pins the gate's
// component-payability half: a Sac component with no eligible candidate on
// the battlefield keeps the decline-only ask (the cost's body never runs,
// nothing is charged).
func TestTriggerCostUnpayableSacComponentDeclinesOnly(t *testing.T) {
	t.Parallel()
	const script = "Name:Ramp Elf\nManaCost:1 G\nTypes:Creature Elf Druid\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCost | TriggerDescription$ When CARDNAME enters, you may sacrifice a land. If you do, draw a card.\n" +
		"SVar:TrigCost:AB$ Draw | Cost$ Sac<1/Land> | NumCards$ 1\n" +
		"Oracle:x\n"
	e := handEngine(t)

	d := etbCostWindow(t, e, script)
	pay, decline := windowPayDecline(t, d)
	if pay >= 0 {
		t.Fatalf("an unpayable Sac<1/Land> cost was offered as payable: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw || events.IsSacrifice(ev) {
			t.Fatalf("the unpayable cost still moved: %+v", ev)
		}
	}
}

// TestAmbergrisXCountsThePaidDiscard is the secondary-scope pin, on the REAL
// carrier's exact body and SVar verbatim (`AB$ DamageAll | Cost$
// Discard<1/Hand> Draw<2/You> | NumDmg$ X | ValidPlayers$ Opponent` with
// `SVar:X:PlayerCountPropertyYou$CardsDiscardedThisTurn`): the cost's
// discards are paid BEFORE the body resolves, so the body's X counts them --
// the opponent takes one damage per discarded card. Without the count head
// the X degraded to 0 and the body dealt nothing.
func TestAmbergrisXCountsThePaidDiscard(t *testing.T) {
	t.Parallel()
	const script = "Name:Ambergris Citadel Agent\nManaCost:2 U\nTypes:Creature Human Wizard\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigDamage | TriggerDescription$ When CARDNAME enters, you may discard your hand. If you do, it deals damage to each opponent equal to the cards discarded this turn.\n" +
		"SVar:TrigDamage:AB$ DamageAll | Cost$ Discard<1/Hand> Draw<2/You> | NumDmg$ X | ValidPlayers$ Opponent\n" +
		"SVar:X:PlayerCountPropertyYou$CardsDiscardedThisTurn\n" +
		"Oracle:x\n"
	junk := card(t, discardCostJunk)
	e := handEngine(t, junk, junk)

	d := etbCostWindow(t, e, script)
	pay, _ := windowPayDecline(t, d)
	if pay < 0 {
		t.Fatalf("the cost was not offered as payable: %+v", d.Options)
	}
	oppLife := e.G.Players[1].Life
	mark := len(e.L.Events)
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if got := e.CardsDiscardedThisTurn(0); got != 2 {
		t.Fatalf("CardsDiscardedThisTurn(0) = %d, want 2 (the paid cost discards count)", got)
	}
	if got := e.G.Players[1].Life; got != oppLife-2 {
		t.Fatalf("opponent life = %d, want %d (X = the paid discards)", got, oppLife-2)
	}
	// 2 discarded, then the cost's two drawn.
	if got := len(e.G.Zone(state.ZHand, 0)); got != 2 {
		t.Fatalf("hand after pay = %d, want 2 (the drawn cards)", got)
	}
	draws := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
	}
	if draws != 2 {
		t.Fatalf("draw events = %d, want 2 (the cost's draw half)", draws)
	}
}

// TestTriggerCostDiscardPartsReserveAcrossTheGate pins the gate's cross-part
// reservation: two Discard parts of one card over a ONE-card hand pass no
// part independently, so the window offers DECLINE ONLY (the walk would
// decline at the second part -- an offer that cannot be honoured must never
// exist); with two hand cards the same cost is payable.
func TestTriggerCostDiscardPartsReserveAcrossTheGate(t *testing.T) {
	t.Parallel()
	const script = "Name:Twin Discarder\nManaCost:1 B\nTypes:Creature Zombie\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCost | TriggerDescription$ When CARDNAME enters, you may discard a card and discard a card. If you do, draw a card.\n" +
		"SVar:TrigCost:AB$ Draw | Cost$ Discard<1/Card> Discard<1/Card> | NumCards$ 1\n" +
		"Oracle:x\n"

	e := handEngine(t, card(t, discardCostJunk))
	d := etbCostWindow(t, e, script)
	pay, decline := windowPayDecline(t, d)
	if pay >= 0 {
		t.Fatalf("a two-part Discard cost over one shared candidate was offered as payable: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)
	for _, ev := range e.L.Events[mark:] {
		if events.IsDiscard(ev) || ev.Kind == events.Draw {
			t.Fatalf("the unpayable cost still moved a card: %+v", ev)
		}
	}

	// The payable twin: one candidate per part, and the walk asks for the
	// first part (2 candidates > 1) while the second settles from what the
	// first left.
	e2 := handEngine(t, card(t, discardCostJunk), card(t, discardCostJunk))
	d2 := etbCostWindow(t, e2, script)
	pay2, _ := windowPayDecline(t, d2)
	if pay2 < 0 {
		t.Fatalf("the same cost over two candidates was not offered as payable: %+v", d2.Options)
	}
	mark2 := len(e2.L.Events)
	submitChoices(t, e2, pay2)
	pick := e2.Pending()
	if pick == nil || pick.Kind != decision.KChoose || pick.Min != 1 || pick.Max != 1 || len(pick.Options) != 2 {
		t.Fatalf("expected the first part's exact-1 pick over 2 candidates, got %+v", pick)
	}
	submitChoices(t, e2, pick.Options[0].Index)
	passUntilStackEmpty(t, e2, 20)
	if got := len(e2.G.Zone(state.ZHand, 0)); got != 1 {
		t.Fatalf("hand after paying both parts = %d, want 1 (the body's draw)", got)
	}
	discards := 0
	for _, ev := range e2.L.Events[mark2:] {
		if events.IsDiscardCost(ev) {
			discards++
		}
	}
	if discards != 2 {
		t.Fatalf("discard-cost events = %d, want 2 (one per part)", discards)
	}
}

// TestTriggerCostAddCounterComponentDeclinesOnly pins the gate's structural
// rejection of an AddCounter component: it IS parsed into the Cost (rules/
// mana.go) but no settle reads it here, so a cost carrying one keeps the
// decline-only ask rather than offering "pay" and silently skipping it --
// the exact defect class this gate closes (corpus-unreachable today: every
// non-Planeswalker$ Cost$ AddCounter line is an activation/cast cost, never
// a trigger body).
func TestTriggerCostAddCounterComponentDeclinesOnly(t *testing.T) {
	t.Parallel()
	const script = "Name:Loyalty Sinker\nManaCost:1 B\nTypes:Creature Zombie\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCost | TriggerDescription$ When CARDNAME enters, you may sacrifice a land and add two loyalty counters. If you do, draw a card.\n" +
		"SVar:TrigCost:AB$ Draw | Cost$ Sac<1/Land> AddCounter<2/LOYALTY> | NumCards$ 1\n" +
		"Oracle:x\n"
	const landScript = "Name:Test Land\nTypes:Land\nOracle:x\n"
	e := handEngine(t)
	land := onBoard(t, e, 0, landScript)

	d := etbCostWindow(t, e, script)
	pay, decline := windowPayDecline(t, d)
	if pay >= 0 {
		t.Fatalf("a cost carrying an unsettleable AddCounter part was offered as payable: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(land); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the decline must move nothing, land zone = %v", o)
	}
	for _, ev := range e.L.Events[mark:] {
		if events.IsSacrifice(ev) || ev.Kind == events.Draw {
			t.Fatalf("the declined cost still moved a card: %+v", ev)
		}
	}
}

// TestCardsDiscardedThisTurnCostFormCountsTheOwnerOnly pins the cost-form
// attribution fix: an events.DiscardCost carries NO Player field (every
// emitter constructs it without one), so it must count toward the discarded
// card's OWNER alone and never toward seat 0 -- before the fix the
// "ev.Player == p" match counted every seat's cost discard toward seat 0.
// Pinned live: seat 1 pays a window discard cost; seat 0's count stays 0.
func TestCardsDiscardedThisTurnCostFormCountsTheOwnerOnly(t *testing.T) {
	t.Parallel()
	e := handEngine(t)

	// Live half: seat 1 pays a Discard<1/Card> window cost.
	const script = "Name:Seat One Discarder\nManaCost:1 B\nTypes:Creature Zombie\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCost | TriggerDescription$ When CARDNAME enters, you may discard a card. If you do, draw a card.\n" +
		"SVar:TrigCost:AB$ Draw | Cost$ Discard<1/Card> | NumCards$ 1\n" +
		"Oracle:x\n"
	junk := e.G.AddObject(card(t, discardCostJunk), 1)
	junk.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, append(e.G.Zone(state.ZHand, 1), junk.ID))
	src := e.G.AddObject(card(t, script), 1)
	src.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, append(e.G.Zone(state.ZHand, 1), src.ID))
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZHand, To: state.ZBattlefield})
	e.putTriggersOnStack()
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 1 {
		t.Fatalf("expected seat 1's trigger-cost window ask, got %+v", d)
	}
	pay, _ := windowPayDecline(t, d)
	if pay < 0 {
		t.Fatalf("seat 1's Discard<1/Card> cost was not offered as payable: %+v", d.Options)
	}
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if got := e.CardsDiscardedThisTurn(0); got != 0 {
		t.Fatalf("CardsDiscardedThisTurn(0) = %d after seat 1 paid a cost discard, want 0", got)
	}
	if got := e.CardsDiscardedThisTurn(1); got != 1 {
		t.Fatalf("CardsDiscardedThisTurn(1) = %d, want 1 (its own paid cost discard)", got)
	}

	// The ordinary-form twins, on direct emissions: seat 1's ordinary
	// discard counts for seat 1 only, and seat 0's own cost discard counts
	// for seat 0.
	e.emit(events.Discard(junk.ID, 1))
	if got := e.CardsDiscardedThisTurn(1); got != 2 {
		t.Fatalf("after seat 1's ordinary discard, count(1) = %d, want 2", got)
	}
	if got := e.CardsDiscardedThisTurn(0); got != 0 {
		t.Fatalf("after seat 1's ordinary discard, count(0) = %d, want 0", got)
	}
	e.emit(events.DiscardCost(src.ID))
	if got := e.CardsDiscardedThisTurn(0); got != 0 {
		t.Fatalf("a seat-1-owned cost discard still counted toward seat 0: %d", got)
	}
	if got := e.CardsDiscardedThisTurn(1); got != 3 {
		t.Fatalf("count(1) after the seat-1 cost discard = %d, want 3", got)
	}
	own := e.G.AddObject(card(t, discardCostJunk), 0)
	e.emit(events.DiscardCost(own.ID))
	if got := e.CardsDiscardedThisTurn(0); got != 1 {
		t.Fatalf("seat 0's own cost discard must count for seat 0, count(0) = %d", got)
	}
}
