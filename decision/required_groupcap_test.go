package decision

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestRequiredQuotaHonoursGroupCaps is the cardfuzz fuzz-1003 engine error
// (seed 13168609742352887994): three creatures that must attack (Required)
// against a defender whose AttackRestrict static lets at most two creatures
// attack them (GroupLimits["attack-restrict:1"] = 2, the Crawlspace shape).
// CR 508.1d maximises requirements WITHOUT violating restrictions, so only two
// of the three can be required. The quota counted all three, so the engine's
// requirement check demanded three while Validate's group cap allowed two: no
// declaration could pass both, Submit refused every answer, and the game
// stopped ("choice 2 exceeds the per-group limit of 2").
func TestRequiredQuotaHonoursGroupCaps(t *testing.T) {
	d := Decision{Kind: KAttackers, Min: 0, Max: 3,
		GroupLimits: map[string]int{"attack-restrict:1": 2}}
	for i, obj := range []state.ObjID{37, 55, 128} {
		d.Options = append(d.Options, Option{Index: i, Kind: "attacker", Obj: obj, Player: 1,
			Required: true, Group: "attack-restrict:1"})
	}
	if got := d.RequiredQuota(); got != 2 {
		t.Fatalf("RequiredQuota = %d, want 2 (the group cap)", got)
	}
	fit := d.FitRequired([]int{0, 1, 2})
	if err := d.Validate(Intent{Choices: fit}); err != nil {
		t.Fatalf("FitRequired(%v) = %v, which Validate rejects: %v", []int{0, 1, 2}, fit, err)
	}
	if got := d.RequiredChosen(fit); got < d.RequiredQuota() {
		t.Fatalf("FitRequired = %v covers %d required, quota %d", fit, got, d.RequiredQuota())
	}
}

// TestRequiredCoreUsesAnOpenGroupOption: a required attacker offered at two
// defenders, one of them already at its group cap, is required at the OTHER
// defender -- the cap removes one option, not the creature's requirement.
func TestRequiredCoreUsesAnOpenGroupOption(t *testing.T) {
	d := Decision{Kind: KAttackers, Min: 0, Max: 3,
		GroupLimits: map[string]int{"attack-restrict:1": 1}}
	d.Options = []Option{
		{Index: 0, Kind: "attacker", Obj: 10, Player: 1, Required: true, Group: "attack-restrict:1"},
		{Index: 1, Kind: "attacker", Obj: 11, Player: 1, Required: true, Group: "attack-restrict:1"},
		{Index: 2, Kind: "attacker", Obj: 11, Player: 2, Required: true},
	}
	if got := d.RequiredQuota(); got != 2 {
		t.Fatalf("RequiredQuota = %d, want 2 (creature 11 attacks seat 2)", got)
	}
	fit := d.FitRequired(nil)
	if err := d.Validate(Intent{Choices: fit}); err != nil {
		t.Fatalf("FitRequired(nil) = %v, which Validate rejects: %v", fit, err)
	}
	if got := d.RequiredChosen(fit); got != 2 {
		t.Fatalf("FitRequired = %v covers %d required, want 2", fit, got)
	}
}
