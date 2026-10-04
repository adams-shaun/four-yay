package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestKr1EachStructuredOptionsExplicitZeroOffersNothing is the option-builder
// half of the deleted TestEachExplicitZeroChangeNumSelectsNothing (the
// engine halves live in rules/each_changezone_kernel_test.go): an explicit
// per-type ChangeNum$ 0 is a zero ceiling, never the default one, and
// builds no options.
func TestKr1EachStructuredOptionsExplicitZeroOffersNothing(t *testing.T) {
	h := newHost(t, 2)
	crea := h.g.AddObject(mkCard(t, "Name:ZeroCreature\nTypes:Creature Bear\nOracle:x\n"), 0)
	land := h.g.AddObject(mkCard(t, "Name:ZeroLand\nTypes:Land Forest\nOracle:x\n"), 0)
	groups := [][]state.ObjID{{crea.ID}, {land.ID}}
	d := &decision.Decision{}
	if got := eachStructuredOptions(h.g, d, groups, 0, false, 0, "search"); got != 0 {
		t.Fatalf("zero per-type ceiling = %d, want 0", got)
	}
	if len(d.Options) != 0 {
		t.Fatalf("explicit ChangeNum 0 produced options: %+v", d.Options)
	}
}
