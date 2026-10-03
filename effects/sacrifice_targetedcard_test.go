package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestSacValidTargetedCardSelf is the focused candidate-pool regression: a
// player-targeted Sacrifice whose SacValid$ names the resolution's card target
// (`TargetedCard.Self`) must offer exactly that target -- and only when it sits
// in the asked player's battlefield. Enchanter's Bane's compiled DBSac is this
// shape. Before the fix the target-referent base was unrecognised by the filter
// grammar (a bare Self reads relative to the resolving SOURCE), so the pool was
// empty and the optional ask was never posed.
//
// Board: the targeted enchantment and an unrelated enchantment both belong to
// seat 1, so the pool has two candidates and the compared values (which option
// is offered) genuinely differ.
func TestSacValidTargetedCardSelf(t *testing.T) {
	h := newHost(t, 2)
	target := putBattlefield(h, 1, "Name:Target Enchantment\nTypes:Enchantment\nOracle:x\n")
	unrelated := putBattlefield(h, 1, "Name:Other Enchantment\nTypes:Enchantment\nOracle:x\n")
	// Precondition: both candidates are live battlefield permanents controlled
	// by the player the target names, and are distinct objects.
	if o := h.g.Obj(target); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: target = %+v; want a battlefield permanent controlled by seat 1", h.g.Obj(target))
	}
	if o := h.g.Obj(unrelated); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: unrelated = %+v; want a battlefield permanent controlled by seat 1", h.g.Obj(unrelated))
	}
	if target == unrelated {
		t.Fatalf("precondition: target and unrelated are the same id %d", target)
	}

	c := &Ctx{Source: 0, Controller: 0, Targets: []state.Target{{Obj: target}}}
	h.askResult = true
	effSacrifice(h, c, sacrificeParams(map[string]string{
		"Defined": "TargetedController", "SacValid": "TargetedCard.Self", "Optional": "True",
	}))

	// Precondition: the target resolves to seat 1 (its controller), so the ask
	// goes to the right seat. If Defined$ TargetedController stopped resolving,
	// the ask would go elsewhere and the option assertion below is meaningless.
	if h.lastAsk == nil {
		t.Fatal("no ask was posed; want a KChoose offering the targeted card")
	}
	if h.lastAsk.Kind != decision.KChoose || h.lastAsk.Player != 1 {
		t.Fatalf("ask = %+v; want a KChoose to seat 1", h.lastAsk)
	}
	if h.lastAsk.Min != 0 || h.lastAsk.Max != 1 {
		t.Fatalf("ask range = %d..%d; want 0..1 (Optional$ True)", h.lastAsk.Min, h.lastAsk.Max)
	}
	if len(h.lastAsk.Options) != 1 || h.lastAsk.Options[0].Obj != target {
		t.Fatalf("options = %+v; want only targeted card %d (unrelated %d excluded)", h.lastAsk.Options, target, unrelated)
	}
}
