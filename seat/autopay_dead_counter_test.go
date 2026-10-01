package seat

// Test for autopay-bot-dead-counter-blocks-fallback: a payment plan the
// policy will never take (C8's dead counter) must not suppress the manual
// path. Before the fix, `paymentIntent` built `payable` from every offered
// plan, so a window whose ONLY plan was a counter with no foreign spell
// looked plannable: the private candidate carried that plan, chooseCast
// refused it (C8), the pick fell through to pass, and the adapter returned a
// manual pass -- hiding the mana activations the manual policy would have
// spent tapping toward an unplanned instant. With the fix the dead counter's
// plan is excluded from `payable`, leaving it empty, and the window falls
// back to the manual policy as a window with no plans does.

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestAutoPayDeadCounterDoesNotBlockManualPath is the brief's headline case:
// the bot's OWN spell sits on b.Stack in its own main phase (so no foreign
// spell exists and the planned counter is dead by C8), a Counter card is the
// ONLY offered payment plan, and an unplanned instant-speed card (CMC 3, not
// offered as a plan) is what the tap gate wants. The auto-pay adapter must
// answer with a manual "activate" choice -- tapping toward that instant --
// not a pass.
func TestAutoPayDeadCounterDoesNotBlockManualPath(t *testing.T) {
	brd := botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{
			1: islandCard(), 2: islandCard(), 3: islandCard(),
			// 50: the planned counter -- dead, because the only stack spell
			// is the deciding seat's own. Its higher CMC makes it the tap
			// gate's own intended card (cardScore = CMC for a non-creature), so
			// unplannedTapIntent does NOT catch this shape: the dead counter's
			// plan IS the tap intent, and leaving it payable lets the private
			// candidate lose to pass.
			50: {CMC: 4, Castable: true, ManaCost: "3 U", Counter: true, InstantSpeed: true},
			// 60: the unplanned instant the manual policy could pay by hand
			// (lower CMC, so it is not the tap gate's intent).
			60: {CMC: 2, Castable: true, ManaCost: "1 U", InstantSpeed: true},
		}),
		// The bot's OWN spell: Controller 0 == the deciding seat, IsSpell
		// true, so ForeignSpell(0) is false and the counter is dead.
		Stack: []botpolicy.StackEntry{{ID: 90, Controller: 0, IsSpell: true, CMC: 4}},
	}
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1}
	for _, id := range []state.ObjID{1, 2, 3} {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "activate", Obj: id})
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "pass"})
	// The ONLY plan is the dead counter.
	d.PaymentActions = []decision.PaymentAction{paymentAction("pay", 50)}

	// Preconditions: the counter really is dead (no foreign spell), the tap
	// gate's intended card really is the unplanned instant 60, and the
	// manual policy really does tap toward it. Any of these failing makes the
	// assertion below vacuous.
	if brd.ForeignSpell(d.Player) {
		t.Fatalf("precondition: the stack must hold no foreign spell for the counter to be dead")
	}
	if !brd.CounterIsDead(d.Player, 50) {
		t.Fatalf("precondition: CounterIsDead(50) = false, want the planned counter dead")
	}
	if id, ok := brd.TapIntent(&d); !ok || id != 50 {
		t.Fatalf("precondition: TapIntent = (%d, %v), want the dead counter 50 (the shape unplannedTapIntent cannot catch)", id, ok)
	}
	manual := NewBot(19).decide(brd, &d)
	if len(manual.Choices) != 1 || d.Options[manual.Choices[0]].Kind != "activate" {
		t.Fatalf("precondition: the manual bot should tap (toward the dead counter), got %+v", manual)
	}

	in := NewBot(19).EnableAutoPayMana().decide(brd, &d)
	if in.Payment != nil {
		t.Fatalf("auto-pay spent the window on the dead counter's plan %s", in.Payment.ActionID)
	}
	if len(in.Choices) != 1 || d.Options[in.Choices[0]].Kind != "activate" {
		t.Fatalf("intent = %+v, want a manual activate option (not pass) when the only plan is a dead counter", in)
	}
}

// TestAutoPayKeepsPlanForLiveCounter is the other side of the new exclusion:
// with a FOREIGN spell on the stack the counter plan is live (C8 permits it),
// so it must stay in `payable` and be selected exactly as before. It proves
// the filter reads C8's census rather than dropping every Counter plan.
func TestAutoPayKeepsPlanForLiveCounter(t *testing.T) {
	brd := botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{
			1: islandCard(), 2: islandCard(), 3: islandCard(),
			50: {CMC: 4, Castable: true, ManaCost: "3 U", Counter: true, InstantSpeed: true},
			60: {CMC: 1, Castable: true, ManaCost: "U", InstantSpeed: true},
		}),
		// A FOREIGN spell (Controller 1) makes the counter live.
		Stack: []botpolicy.StackEntry{{ID: 91, Controller: 1, IsSpell: true, CMC: 4}},
	}
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1}
	for _, id := range []state.ObjID{1, 2, 3} {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "activate", Obj: id})
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "pass"})
	d.PaymentActions = []decision.PaymentAction{paymentAction("pay", 50)}

	if !brd.ForeignSpell(d.Player) {
		t.Fatalf("precondition: the stack must hold a foreign spell for the counter to be live")
	}
	if brd.CounterIsDead(d.Player, 50) {
		t.Fatalf("precondition: CounterIsDead(50) = true, want the counter live")
	}
	// The live counter is itself the tap gate's intent (higher CMC), so this
	// exercises the plan path rather than the unplanned fallback.
	if id, ok := brd.TapIntent(&d); !ok || id != 50 {
		t.Fatalf("precondition: TapIntent = (%d, %v), want the live counter 50", id, ok)
	}

	in := NewBot(19).EnableAutoPayMana().decide(brd, &d)
	if in.Payment == nil || in.Payment.ActionID != "pay" {
		t.Fatalf("intent = %+v, want the live counter's offered plan kept", in)
	}
}
