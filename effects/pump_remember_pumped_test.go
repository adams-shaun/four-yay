package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPumpRememberPumped remembers only objects the Pump actually affects,
// making the object available both to a chained Card.IsRemembered selector
// and to later resolutions through the source's event-backed memory.
func TestPumpRememberPumped(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	source, pumped, unpumped := ids["myEnchantment"], ids["myBear"], ids["theirBig"]
	ctx := &Ctx{Source: source, Controller: 0,
		Targets: []state.Target{{Obj: pumped}}, TargetsOffered: true}

	body := sa(t, "DB$ Pump | ValidTgts$ Creature | Defined$ Targeted | NumAtt$ +1 | RememberPumped$ True")
	if body.API != "Pump" || g.Obj(pumped).Zone != state.ZBattlefield || g.Obj(unpumped).Zone != state.ZBattlefield || pumped == unpumped {
		t.Fatal("precondition: fixture must contain distinct battlefield creatures for the targeted pump")
	}
	Resolve(h, ctx, body)

	if len(h.continuous) != 1 || h.continuous[0].Source != pumped || h.continuous[0].AddPower != 1 {
		t.Fatalf("pump effects = %+v, want exactly one +1/+0 effect on target %d", h.continuous, pumped)
	}
	local := ctx.SpecContext(0)
	local.Source = 0 // isolate the resolution-local half from persistent memory
	if !MatchesObjectCtx(g, "Card.IsRemembered", g.Obj(pumped), local) {
		t.Fatal("Card.IsRemembered did not match the pumped object in the resolution-local set")
	}
	if MatchesObjectCtx(g, "Card.IsRemembered", g.Obj(unpumped), local) {
		t.Fatal("Card.IsRemembered matched an unpumped object in the resolution-local set")
	}
	if !MatchesSpecCtx(g, "Card.IsRemembered", pumped, SpecContext{You: 0, Source: source}) {
		t.Fatal("Card.IsRemembered did not match the pumped object through persistent source memory")
	}
	if MatchesSpecCtx(g, "Card.IsRemembered", unpumped, SpecContext{You: 0, Source: source}) {
		t.Fatal("persistent source memory included an unpumped object")
	}
	foundEvent := false
	for _, ev := range h.log {
		if ev.Kind == events.Choose && ev.Obj == source && ev.Counter == "remembered" && len(ev.IDs) == 1 && ev.IDs[0] == pumped {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Fatal("event log has no remembered event for the pumped object")
	}
}
