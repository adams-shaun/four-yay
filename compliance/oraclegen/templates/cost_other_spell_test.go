package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestCostStaticOtherSpellProfiles(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, mana string }{
		{"Stormcatch Mentor", ""},
		{"The Fire Crystal", ""},
		{"Baron Strucker, HYDRA Overlord", ""},
		{"Dwarven Mauler", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: %s absent", tc.name)
			}
			var req levelb.Requirement
			for _, candidate := range levelb.Requirements(c) {
				if candidate.Key == "static#0.0" {
					req = candidate
				}
			}
			if req.Key == "" {
				t.Fatalf("precondition: %s static#0.0 absent", tc.name)
			}
			it, skip := GenerateB(reg, tc.name, req)
			if tc.name == "Dwarven Mauler" {
				if skip == nil || skip.Reason != "cost static probe not supported: activated-ability probe unsupported" {
					t.Fatalf("ability probe skip = %v, want the explicit activate-operation gap", skip)
				}
				return
			}
			if skip != nil {
				t.Fatalf("GenerateB: %s", skip.Reason)
			}
			cast := probeCast(t, it)
			foundSource := false
			for _, permanent := range it.Scenario.Setup["p0"].Battlefield {
				foundSource = foundSource || permanent == tc.name
			}
			if !foundSource {
				t.Fatalf("precondition: reduction source %q absent from battlefield", tc.name)
			}
			t.Logf("probe=%s mana=%s", cast.Card, cast.Mana)
			if cast.Mana == "" {
				t.Fatalf("precondition: empty reduced-price probe: %+v", cast)
			}
			if tc.mana != "" && cast.Mana != tc.mana {
				t.Fatalf("mana %q, want %q", cast.Mana, tc.mana)
			}
			if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
				t.Fatalf("precondition: generated probe doesn't play through: %+v", it.Steps)
			}
			if res := runSteps(t, reg, it.Scenario, it.Steps); len(res.Fails) != 0 {
				t.Fatalf("reduced-price cast fails with static present: %v", res.Fails)
			}
			if res := runSteps(t, withoutStatics(reg, tc.name), it.Scenario, it.Steps); len(res.Fails) == 0 {
				t.Fatalf("probe is not sensitive to %s's cost reduction", tc.name)
			}
		})
	}
}
