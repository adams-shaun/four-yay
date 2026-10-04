package rules

// Kernel-era restorations of the krrik_upkeep_window_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestKrrikCumulativeUpkeepPosesManaWindowWhenSourceCanPay(t *testing.T) {
	t.Parallel()
	e, cfg, bear, swamp := krrikUpkeepWindowEngine(t, krrikCumulativeFixture)
	krrikUpkeepPreconditions(t, e, bear, swamp, "Cumulative upkeep")
	if c := ParseCost("B"); c.Colored[state.MB] != 1 || c.Generic != 0 || !c.Priceable() {
		t.Fatalf("precondition: ParseCost(B) = %+v, want one plain {B} pip", c)
	}

	kr6ResolveUpkeepCumulative(t, e)

	// The window must be posed: the pool alone cannot pay the {B} pip, but
	// the untapped Swamp can. Before the fix the grant made the gate count
	// the life route as paid and this ask was SKIPPED entirely.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear {
		t.Fatalf("the cumulative-upkeep mana window must be posed, got %+v", d)
	}
	swampOpt := krrikFindWindowOption(t, d, "activate", swamp)
	krrikFindWindowOption(t, d, "done", 0)
	submitChoices(t, e, swampOpt) // tap the Swamp

	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear {
		t.Fatalf("expected the cumulative election after tapping the Swamp, got %+v", d)
	}
	if len(d.Options) < 2 || d.Options[0].Kind != "cumulative_pay" {
		t.Fatalf("cumulative election = %+v, want a pay option once black is in the pool", d.Options)
	}
	if pool := e.G.Players[0].Pool; pool[state.MB] != 1 {
		t.Fatalf("pool after tapping = %+v, want exactly one black floating", pool)
	}
	submitChoices(t, e, 0) // pay

	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("paid the upkeep but the permanent is %+v", o)
	}
	if life := e.G.Players[0].Life; life != 20 {
		t.Fatalf("life=%d, want 20 (the pip was paid with the Swamp's mana, not K'rrik's life)", life)
	}
	if !e.G.Obj(swamp).Tapped {
		t.Fatal("the Swamp must be tapped to pay the pip")
	}
	replayCheck(t, e, cfg)
}

func TestKrrikCumulativeUpkeepWindowDonePaysLife(t *testing.T) {
	t.Parallel()
	e, cfg, bear, swamp := krrikUpkeepWindowEngine(t, krrikCumulativeFixture)
	krrikUpkeepPreconditions(t, e, bear, swamp, "Cumulative upkeep")

	kr6ResolveUpkeepCumulative(t, e)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear {
		t.Fatalf("the cumulative-upkeep mana window must be posed, got %+v", d)
	}
	doneOpt := krrikFindWindowOption(t, d, "done", 0)
	submitChoices(t, e, doneOpt)

	// The offer gate that follows keeps the grant, so a pay option is still
	// offered; paying it spends the 2 granted life.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear {
		t.Fatalf("expected the cumulative election after \"done\", got %+v", d)
	}
	if len(d.Options) < 2 || d.Options[0].Kind != "cumulative_pay" {
		t.Fatalf("cumulative election = %+v, want a pay option (the grant keeps the offer alive)", d.Options)
	}
	submitChoices(t, e, 0) // pay

	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("paid the upkeep with life but the permanent is %+v", o)
	}
	if life := e.G.Players[0].Life; life != 18 {
		t.Fatalf("life=%d, want 18 (\"done\" spends the {B} pip's 2 granted life)", life)
	}
	if e.G.Obj(swamp).Tapped {
		t.Fatal("the Swamp must stay untapped when the payer answers \"done\"")
	}
	replayCheck(t, e, cfg)
}
