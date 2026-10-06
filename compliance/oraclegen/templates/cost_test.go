package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func staticCostItem(t *testing.T, reg *cards.Registry, name string) oraclegen.Item {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s missing from corpus", name)
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key == "static#0.0" {
			if req.Sub != "static.cost" || req.Gap != "" {
				t.Fatalf("precondition: %s requirement = %+v, want supported static.cost", name, req)
			}
			it, skip := GenerateB(reg, name, req)
			if skip != nil {
				t.Fatalf("%s: %s", name, skip.Reason)
			}
			return it
		}
	}
	t.Fatalf("precondition: %s has no static#0.0", name)
	return oraclegen.Item{}
}

func TestCostStaticProbesModifiedPrice(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, spell, mana, permanent string
	}{
		{"Ghalta the Immovable", "Ghalta the Immovable", "CCCCW", "Serra Angel"},
		{"Geist of Saint Thalia", "Shock", "R", "Geist of Saint Thalia"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := staticCostItem(t, reg, tc.name)
			if it.Template != "static#0.0" || len(it.Steps) == 0 {
				t.Fatalf("identity/steps = %s/%+v", it.Template, it.Steps)
			}
			cast := it.Steps[len(it.Steps)-1]
			if cast.Op != "cast" || cast.Card != "p0:"+tc.spell || cast.Mana != tc.mana {
				t.Fatalf("cast probe = %+v, want %s at %s", cast, tc.spell, tc.mana)
			}
			found := false
			for _, p := range it.Scenario.Setup["p0"].Battlefield {
				found = found || p == tc.permanent
			}
			if !found {
				t.Fatalf("precondition: %s not on p0 battlefield: %v", tc.permanent, it.Scenario.Setup["p0"].Battlefield)
			}
			res := runSteps(t, reg, it.Scenario, it.Steps)
			if len(res.Fails) != 0 {
				t.Fatalf("reduced-price cast failed in gorge: %v", res.Fails)
			}
			if len(res.Snapshots) < 2 {
				t.Fatalf("cast did not produce a step snapshot: %v", res.Snapshots)
			}
			pool := ""
			for _, p := range res.Snapshots[1].Players {
				if p.Seat == 0 {
					pool = p.Pool
				}
			}
			if pool != "" {
				t.Errorf("p0.pool after exactly-priced cast = %q, want empty", pool)
			}
		})
	}
}

func TestCostStaticProfilesAndOpponentGap(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Tam, the Possibility", "Ghalta the Unstoppable", "Traxos, Academy Guardian", "Wrath of the Bloodmane"} {
		it := staticCostItem(t, reg, name)
		res := runSteps(t, reg, it.Scenario, it.Steps)
		if len(res.Fails) != 0 {
			t.Errorf("%s reduced-price cast failed: %v", name, res.Fails)
		}
	}
	c, ok := reg.Lookup("Thalia, the Survivor")
	if !ok {
		t.Fatal("precondition: Thalia, the Survivor missing from corpus")
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key == "static#0.0" {
			if req.Sub != "static.cost" || !strings.Contains(req.Gap, "opponent-cast") {
				t.Fatalf("Thalia gap = %+v, want named opponent-cast gap", req)
			}
			if _, skip := GenerateB(reg, "Thalia, the Survivor", req); skip == nil || !strings.Contains(skip.Reason, "opponent-cast") {
				t.Fatalf("Thalia did not remain a named gap: %v", skip)
			}
			return
		}
	}
	t.Fatal("precondition: Thalia has no static#0.0")
}
