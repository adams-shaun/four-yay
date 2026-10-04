package rules

// Kernel-era restorations of the myr_battlesphere_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

func TestMyrBattlesphereAttackTriggerAsksAndPaysTheTapXCost(t *testing.T) {
	t.Parallel()
	e, sphere, myr1, myr2, tappedMyr := battlesphereFixture(t)
	battlesphereTrigger(t, e, sphere)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != 2 {
		t.Fatalf("tap election %+v, want KChoose Min 0 Max 2", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("election options %+v, want only the two untapped Myr (the tapped Myr and the attacking Battlesphere are not offered)", d.Options)
	}
	for _, o := range d.Options {
		if o.Kind != "trigger_cost_tap" || (o.Obj != myr1 && o.Obj != myr2) {
			t.Fatalf("election option %+v, want trigger_cost_tap on myr1/myr2", o)
		}
	}
	// Tap both Myr: X = 2. The answer is deterministic: a second engine
	// built and driven identically logs the same events across the tap-X
	// window's payment and resolution. (The legacy pin cloned the engine at
	// the decision; a kernel probe's pose is bound to the engine that ran
	// it, so the twin is driven from scratch instead.)
	twin, twinSphere, _, _, _ := battlesphereFixture(t)
	battlesphereTrigger(t, twin, twinSphere)
	td := twin.Pending()
	if td == nil || td.Kind != decision.KChoose {
		t.Fatalf("twin tap election %+v", td)
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}
	if err := e.Submit(in); err != nil {
		t.Fatal(err)
	}
	if err := twin.Submit(decision.Intent{Seq: td.Seq, Player: td.Player, Choices: []int{0, 1}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(e.L.Events, twin.L.Events) {
		t.Fatal("twin diverged across the tap-X window's payment and resolution")
	}
	if !e.G.Obj(myr1).Tapped || !e.G.Obj(myr2).Tapped {
		t.Fatal("the elected Myr were not tapped as the cost")
	}
	if !e.G.Obj(tappedMyr).Tapped {
		t.Fatal("the already-tapped Myr untapped")
	}
	if got := e.Power(sphere); got != 6 {
		t.Fatalf("Battlesphere power = %d, want 6 (4 base + X=2 NumAtt$ +X)", got)
	}
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("defender life = %d, want 18 (20 - X=2 NumDmg$ X)", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack not empty after the trigger resolved: %d objects", len(e.G.Stack))
	}
}
