package decision

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// A raised cap permits two different BlockAllDefined pairs, not two copies
// of the same option. The repair's fast path must obey Validate's index rule.
func TestFitRequiredRejectsDuplicateBlockAllOption(t *testing.T) {
	d := &Decision{Kind: KBlockers, Min: 0, Max: 2,
		GroupLimits: map[string]int{"blocker:10": 2},
		Options: []Option{
			{Index: 0, Obj: state.ObjID(10), Attacker: state.ObjID(20), Required: true, BlockMust: true, BlockMustAll: true, Group: "blocker:10"},
			{Index: 1, Obj: state.ObjID(10), Attacker: state.ObjID(21), Required: true, BlockMust: true, BlockMustAll: true, MinBlockers: 2, Group: "blocker:10"},
		},
	}
	if d.Options[0].Obj != d.Options[1].Obj || d.Options[0].Attacker == d.Options[1].Attacker ||
		!d.Options[0].BlockMustAll || !d.Options[1].BlockMustAll || d.GroupCapFor("blocker:10") != 2 {
		t.Fatal("precondition: distinct BlockAllDefined pairs for one blocker under a raised cap")
	}
	if got := d.RequiredQuota(); got != 1 {
		t.Fatalf("precondition: quota = %d, want 1 (second attacker needs another blocker)", got)
	}
	preferred := []int{0, 0}
	if err := d.Validate(Intent{Choices: preferred}); err == nil || !strings.Contains(err.Error(), "duplicate choice") {
		t.Fatalf("precondition: Validate(%v) = %v, want duplicate choice", preferred, err)
	}
	fit := d.FitRequired(preferred)
	if !slices.Equal(fit, []int{0}) {
		t.Fatalf("FitRequired(%v) = %v, want [0]", preferred, fit)
	}
	if err := d.Validate(Intent{Choices: fit}); err != nil {
		t.Fatalf("FitRequired returned invalid declaration %v: %v", fit, err)
	}
}
