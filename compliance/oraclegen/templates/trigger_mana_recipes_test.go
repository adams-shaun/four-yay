package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// permByName returns the named battlefield permanent from a snapshot.
func permByName(t *testing.T, s rules.OracleSnapshot, name string) rules.OracleSnapPerm {
	t.Helper()
	for _, p := range s.Permanents {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("precondition: %s not on the battlefield in snapshot %q", name, s.Checkpoint)
	return rules.OracleSnapPerm{}
}

// TestManaExpendRecipes: a Mode$ ManaExpend trigger fires on a probe cast whose
// mana spend crosses Amount$, and its effect lands in the final snapshot. One
// real card per effect shape (a P/T pump, a damage-to-each-opponent and a
// +1/+1 counter), each asserted against the value the card's own text names.
func TestManaExpendRecipes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key string
		check     func(t *testing.T, final rules.OracleSnapshot)
	}{
		{"Bakersbane Duo", "trigger#0.1", func(t *testing.T, final rules.OracleSnapshot) {
			if p := permByName(t, final, "Bakersbane Duo"); p.PT != "3/3" {
				t.Fatalf("Bakersbane Duo = %s, want 3/3 (the expend-4 pump)", p.PT)
			}
		}},
		{"Teapot Slinger", "trigger#0.0", func(t *testing.T, final rules.OracleSnapshot) {
			if final.Players[1].Life != 18 {
				t.Fatalf("opponent life = %d, want 18 (2 damage to each opponent)", final.Players[1].Life)
			}
		}},
		{"Wandertale Mentor", "trigger#0.0", func(t *testing.T, final rules.OracleSnapshot) {
			if p := permByName(t, final, "Wandertale Mentor"); p.Counters["P1P1"] != 1 {
				t.Fatalf("Wandertale Mentor counters = %v, want one P1P1", p.Counters)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, levelb.ManaExpendSub)
			// Precondition: the trigger source is on the battlefield, so the
			// effect assertion rides on the card being the one that fired.
			onField := false
			for _, bf := range it.Scenario.Setup["p0"].Battlefield {
				onField = onField || bf == tc.name
			}
			if !onField {
				t.Fatalf("precondition: %s not on p0's battlefield: %v", tc.name, it.Scenario.Setup["p0"].Battlefield)
			}
			res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Fails) != 0 {
				t.Fatalf("scenario failed: %v", res.Fails)
			}
			if len(res.Snapshots) == 0 {
				t.Fatal("no snapshot")
			}
			tc.check(t, res.Snapshots[len(res.Snapshots)-1])
		})
	}
}

// TestTapsForManaRecipes: a Static$ True TapsForMana triggered mana ability
// adds mana to the pool when a probe land is tapped. The item generates and the
// activate checkpoint's pool holds the land's own mana plus the trigger's.
func TestTapsForManaRecipes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, key, probe string }{
		{"Lavaleaper", "trigger#0.0", "Forest"},
		{"Groundchuck & Dirtbag", "trigger#0.0", "Forest"},
		{"Shimmerwilds Growth", "trigger#0.0", "Forest"},
		{"Badgermole Cub", "trigger#0.1", "Llanowar Elves"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, levelb.TapsForManaSub)
			// Precondition: the tapped probe source is in the setup, so the pool
			// assertion below is about a real tap.
			tapped := false
			for _, bf := range it.Scenario.Setup["p0"].Battlefield {
				tapped = tapped || bf == tc.probe
			}
			if !tapped {
				t.Fatalf("precondition: probe %s not in setup: %v", tc.probe, it.Scenario.Setup["p0"].Battlefield)
			}
			if p := poolAtCheckpoint(t, reg, it, "(activate)"); len(p) < 2 {
				t.Fatalf("%s: pool at the activate checkpoint = %q, want the probe's mana plus the trigger's added mana", tc.name, p)
			}
		})
	}
}
