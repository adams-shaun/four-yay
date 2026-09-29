package sbheuristic

import (
	"testing"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/view"
)

func TestSBHeuristicRegistrationAndSharedConstructor(t *testing.T) {
	entry, ok := bots.Lookup(Policy)
	if !ok || !entry.Env || entry.Search || entry.Caretaker != "bot" {
		t.Fatalf("entry = %+v, registered=%v; want Env non-search policy with bot caretaker", entry, ok)
	}
	seat, err := bots.New(Policy, bots.Options{Seed: 17, AutoPayMana: true})
	if err != nil {
		t.Fatal(err)
	}
	envSeat, ok := seat.(bots.EnvSeat)
	if !ok {
		t.Fatalf("built %T, want EnvSeat", seat)
	}
	refuser, ok := seat.(bots.RefusalAnswerer)
	if !ok {
		t.Fatalf("built %T, want RefusalAnswerer", seat)
	}
	payment, ok := seat.(interface{ WantsPaymentActions() bool })
	if !ok || !payment.WantsPaymentActions() {
		t.Fatal("AutoPayMana=true did not enable payment actions on the hosted seat")
	}
	if !envSeat.WantsEnv(&decision.Decision{Kind: decision.KPriority}) {
		t.Fatal("priority does not request Env; honest planner would not be installed")
	}
	for _, kind := range decision.Kinds {
		if kind != decision.KPriority && envSeat.WantsEnv(&decision.Decision{Kind: kind}) {
			t.Errorf("WantsEnv(%s) = true; only priority uses the planner", kind)
		}
	}

	shared := NewWithMode(23, builtins.AutoPay)
	if shared.Policy() != builtins.Heuristic || !shared.WantsPaymentActions() {
		t.Fatalf("NewWithMode returned policy %v, wants payment actions %v", shared.Policy(), shared.WantsPaymentActions())
	}
	// Calling the adapter's refusal rung reaches the wrapped builtin handler.
	refuser.AnswerRefused(view.View{}, decision.Decision{}, decision.Intent{})
	if got := shared.Stats.Refusals; got != 0 {
		t.Fatalf("independent shared constructor unexpectedly has %d refusals", got)
	}
	wrapped := seat.(*hostedSeat)
	if wrapped.bot.Stats.Refusals != 1 {
		t.Fatalf("AnswerRefused reached wrapped handler %d times, want 1", wrapped.bot.Stats.Refusals)
	}
}
