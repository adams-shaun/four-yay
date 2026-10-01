package seat

// Tests for the auto-pay adapter's unplanned-preferred-cast fallback
// (autopay-bot-unplanned-intent): when the tap gate's intended card has no
// V1 plan, the adapter must answer with the manual policy so that card can
// still be paid, instead of spending the window on a lesser planned spell.
// Companion halves pin that the fallback stays scoped: a dead counter (C8)
// and an out-of-window card keep the plan path.

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func forestCard() botpolicy.Card {
	var p cards.ManaProduction
	p.Colour[state.MG] = 1 // G
	return botpolicy.Card{OnBattlefield: true, Basic: true, Produces: p}
}

func islandCard() botpolicy.Card {
	var p cards.ManaProduction
	p.Colour[state.MU] = 1 // U
	return botpolicy.Card{OnBattlefield: true, Basic: true, Produces: p}
}

// TestAutoPayPaysManuallyForUnplannedPreferredCast is the brief's headline
// case: a command-zone commander (5/5 for {3}{G}{G}, which V1 can never plan
// -- cast.Origin != "hand") is the policy's preferred cast, and a 2/2 for
// {1}{G} is the only offered plan. Five Forests, own main1, empty stack: the
// shipped adapter must answer with a manual "activate" option (tapping toward
// the commander) rather than the 2/2's payment plan.
func TestAutoPayPaysManuallyForUnplannedPreferredCast(t *testing.T) {
	brd := botpolicy.Board{IsMain: true, FirstMain: true, MyTurn: true,
		Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{
			1: forestCard(), 2: forestCard(), 3: forestCard(), 4: forestCard(), 5: forestCard(),
			42: {Creature: true, Power: 2, Toughness: 2, CMC: 2, Castable: true, ManaCost: "1 G"},
			50: {Creature: true, Power: 5, Toughness: 5, CMC: 5, Castable: true, ManaCost: "3 G G"},
		}),
		Commanders: botpolicy.TableOf(map[state.ObjID]botpolicy.Commander{50: {InCommandZone: true}}),
	}
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1}
	for _, id := range []state.ObjID{1, 2, 3, 4, 5} {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "activate", Obj: id})
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "pass"})
	d.PaymentActions = []decision.PaymentAction{paymentAction("pay", 42)}

	// Precondition: the intended card really is the unplanned commander, and
	// the manual policy would tap toward it. If either fails the assertion
	// below is vacuous.
	if id, ok := brd.TapIntent(&d); !ok || id != 50 {
		t.Fatalf("precondition: TapIntent = (%d, %v), want the commander 50", id, ok)
	}
	manual := NewBot(19).decide(brd, &d)
	if len(manual.Choices) != 1 || d.Options[manual.Choices[0]].Kind != "activate" {
		t.Fatalf("precondition: the manual bot should tap toward the commander, got %+v", manual)
	}

	in := NewBot(19).EnableAutoPayMana().decide(brd, &d)
	if in.Payment != nil {
		t.Fatalf("auto-pay spent the window on the 2/2 plan %s while the preferred commander cast has no plan", in.Payment.ActionID)
	}
	if len(in.Choices) != 1 || d.Options[in.Choices[0]].Kind != "activate" {
		t.Fatalf("intent = %+v, want a manual activate option toward the commander", in)
	}
}

// TestAutoPayKeepsPlanForDeadCounterIntent is the brief's companion: the
// intended card is a Counter with NO foreign spell on b.Stack, so it is dead
// (C8 refuses it outright) and falling back would spend the window on
// nothing. The plan path is kept and the offered plan is selected.
func TestAutoPayKeepsPlanForDeadCounterIntent(t *testing.T) {
	cardsMap := map[state.ObjID]botpolicy.Card{
		1: islandCard(), 2: islandCard(), 3: islandCard(),
		// The preferred non-creature is the counter (CMC 3); the planned
		// card is a cheaper CMC-1 non-creature, so the counter outscores it.
		42: {CMC: 1, Castable: true, ManaCost: "U"},
		50: {CMC: 3, Castable: true, ManaCost: "1 U U", Counter: true, InstantSpeed: true},
	}
	brd := botpolicy.Board{IsMain: true, FirstMain: true, MyTurn: true, Cards: botpolicy.TableOf(cardsMap)}
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1}
	for _, id := range []state.ObjID{1, 2, 3} {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "activate", Obj: id})
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "pass"})
	d.PaymentActions = []decision.PaymentAction{paymentAction("pay", 42)}

	if id, ok := brd.TapIntent(&d); !ok || id != 50 {
		t.Fatalf("precondition: TapIntent = (%d, %v), want the counter 50", id, ok)
	}
	if brd.ForeignSpell(d.Player) {
		t.Fatalf("precondition: the stack must hold no foreign spell for the counter to be dead")
	}

	in := NewBot(19).EnableAutoPayMana().decide(brd, &d)
	if in.Payment == nil || in.Payment.ActionID != "pay" {
		t.Fatalf("intent = %+v, want the offered plan kept (a dead counter must not trigger the fallback)", in)
	}
}

// TestAutoPayPaysManuallyForLiveUnplannedCounter is the other side of C8: a
// counter WITH a foreign spell on the stack is a real cast, so an unplanned
// counter intent must fall back to the manual policy exactly like any other
// unplanned preferred cast. It proves the helper reads C8's census rather than
// excluding every Counter (the prototype's coarser check).
func TestAutoPayPaysManuallyForLiveUnplannedCounter(t *testing.T) {
	cardsMap := map[state.ObjID]botpolicy.Card{
		1: islandCard(), 2: islandCard(), 3: islandCard(),
		42: {CMC: 1, Castable: true, ManaCost: "U"},
		50: {CMC: 3, Castable: true, ManaCost: "1 U U", Counter: true, InstantSpeed: true},
	}
	brd := botpolicy.Board{IsMain: true, FirstMain: true, MyTurn: true, Cards: botpolicy.TableOf(cardsMap),
		Stack: []botpolicy.StackEntry{{ID: 91, Controller: 1, IsSpell: true, CMC: 4}},
	}
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1}
	for _, id := range []state.ObjID{1, 2, 3} {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "activate", Obj: id})
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "pass"})
	d.PaymentActions = []decision.PaymentAction{paymentAction("pay", 42)}

	if id, ok := brd.TapIntent(&d); !ok || id != 50 {
		t.Fatalf("precondition: TapIntent = (%d, %v), want the counter 50", id, ok)
	}
	if !brd.ForeignSpell(d.Player) {
		t.Fatalf("precondition: the stack must hold a foreign spell for the counter to be live")
	}

	in := NewBot(19).EnableAutoPayMana().decide(brd, &d)
	if in.Payment != nil {
		t.Fatalf("auto-pay kept the 2/2 plan %s while the preferred live counter cast has no plan", in.Payment.ActionID)
	}
}
