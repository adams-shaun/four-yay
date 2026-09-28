package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestPaymentPlanEnsureIsLazyCloneableAndSubmittable(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 94101, "Name:Lazy Plan Spell\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	island := onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending=%#v, want priority", d)
	}
	if d.PaymentActionsBuilt || len(d.PaymentActions) != 0 {
		t.Fatalf("ask eagerly populated offers: built=%v actions=%#v", d.PaymentActionsBuilt, d.PaymentActions)
	}
	cloneBeforeBuild := e.Clone()
	seq, head, eventCount, rng := d.Seq, e.L.Head(), len(e.L.Events), e.rng
	want := e.PaymentActionsForPriority(d.Player, d.Seq)
	got := e.EnsurePaymentActions()
	if !reflect.DeepEqual(got, want) || len(got) != 1 || got[0].Cast.Object != spell {
		t.Fatalf("lazy offers=%#v, eager reference=%#v", got, want)
	}
	if !d.PaymentActionsBuilt || d.Seq != seq || e.L.Head() != head || len(e.L.Events) != eventCount || e.rng != rng {
		t.Fatal("ensuring payment actions changed event, sequence, RNG, or failed to mark built")
	}
	cloned := e.Clone()
	if !cloned.Pending().PaymentActionsBuilt || !reflect.DeepEqual(cloned.Pending().PaymentActions, got) {
		t.Fatal("clone did not preserve the built payment extension")
	}
	if !e.derivedMemoTailLive() {
		t.Fatal("ensuring payment actions invalidated the resumable derived-read tail")
	}
	e.BeginDerivedReads()
	if e.derivedMemoDepth != 1 {
		t.Fatalf("BeginDerivedReads depth=%d, want resumed scope", e.derivedMemoDepth)
	}
	e.EndDerivedReads()
	if !e.derivedMemoTailLive() {
		t.Fatal("ending resumed derived reads invalidated the payment ask tail")
	}
	cloneActions := cloneBeforeBuild.EnsurePaymentActions()
	if !reflect.DeepEqual(cloneActions, got) {
		t.Fatalf("clone-before-build offers=%#v, original offers=%#v", cloneActions, got)
	}
	selection := decision.PaymentSelection{ActionID: got[0].ID, Plan: got[0].Plans[0]}
	if err := cloneBeforeBuild.Submit(decision.Intent{Seq: seq, Player: d.Player, Payment: &selection}); err != nil {
		t.Fatalf("Submit builds and accepts an unbuilt payment selector: %v", err)
	}
	if cloneBeforeBuild.G.Obj(island).Zone != state.ZBattlefield || !cloneBeforeBuild.G.Obj(island).Tapped {
		t.Fatal("planned submit did not execute the offered Island activation")
	}
}
