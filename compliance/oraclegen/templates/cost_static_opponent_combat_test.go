package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestCostStaticOpponentCombatProbes covers the cost-static rows whose gate or
// target needs scenario state the bare probe could not build:
//
//   - Lashwhip Predator reduces its cost only while the opponents control
//     three or more creatures (IsPresent$ Creature.OppCtrl, GE3): the probe
//     now places those creatures on p1's battlefield.
//   - Static Snare and Swat Away read a combat the main-phase probe was
//     never in (Count$Valid Creature.attacking / Creature.attackingYou): the
//     probe now declares the counted attack and casts inside the
//     declare-attackers priority window (Static Snare is Flash, Swat Away an
//     instant).
//   - Dark Endurance reduces its cost only when it targets a blocking
//     creature (ValidTarget$ Creature.blocking): the probe now attacks,
//     blocks, and targets p1's blocker in the declare-blockers priority
//     window.
//
// Each row's probe must play through gorge at the exact reduced price, and
// the same cast must fail once the card's own statics are removed, so a probe
// that pays the printed cost (or an engine that ignores the reduction) cannot
// pass.
func TestCostStaticOpponentCombatProbes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, probe, mana, printed string
		reduction                       int
		// setupP1 asserts the opponent-board state the probe's gate or target
		// reads; nil for the rows whose fixture lives on p0's side or in
		// steps.
		setupP1 []string
		// wantAttack / wantBlock name the combat prelude the probe must carry.
		wantAttack, wantBlock bool
	}{
		{"Lashwhip Predator", "static#0.0", "Lashwhip Predator", "CCGG", "CCCCGG", 2,
			[]string{"Llanowar Elves", "Grizzly Bears", "Llanowar Elves"}, false, false},
		{"Static Snare", "static#0.0", "Static Snare", "CCCW", "CCCCW", 1,
			nil, true, false},
		{"Swat Away", "static#0.0", "Swat Away", "UU", "CCUU", 2,
			[]string{"Grizzly Bears"}, true, false},
		{"Dark Endurance", "static#0.0", "Dark Endurance", "B", "CB", 1,
			nil, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it, skip := GenerateB(reg, tc.name, costRequirement(t, reg, tc.name, tc.key))
			if skip != nil {
				t.Fatalf("GenerateB: %s", skip.Reason)
			}
			probe := probeStep(t, it)
			if probe.Op != "cast" || probe.Card != "p0:"+tc.probe || probe.Mana != tc.mana {
				t.Fatalf("probe = %s %s paying %q, want cast %s paying %q", probe.Op, probe.Card, probe.Mana, "p0:"+tc.probe, tc.mana)
			}
			c, ok := reg.Lookup(tc.probe)
			if !ok {
				t.Fatalf("precondition: probe %s absent", tc.probe)
			}
			pool, why := oraclegen.PoolFor(c.Faces[0].ManaCost)
			if why != "" || pool != tc.printed {
				t.Fatalf("precondition: %s prints %q (%s), table says %q", tc.probe, pool, why, tc.printed)
			}
			if got := len(tc.printed) - len(tc.mana); got != tc.reduction {
				t.Fatalf("precondition: table prices differ by %d, want the static's reduction %d", got, tc.reduction)
			}
			if tc.setupP1 != nil {
				got := it.Scenario.Setup["p1"].Battlefield
				if len(got) != len(tc.setupP1) {
					t.Fatalf("precondition: p1 setup %v, want the gate's %d fixture permanents", got, len(tc.setupP1))
				}
				for _, name := range tc.setupP1 {
					found := false
					for _, g := range got {
						found = found || g == name
					}
					if !found {
						t.Fatalf("precondition: p1 setup %v misses fixture %q", got, name)
					}
				}
			}
			if tc.wantAttack && !hasStep(it.Steps, "attack") {
				t.Fatalf("precondition: attacking-creature probe has no attack prelude: %+v", it.Steps)
			}
			if tc.wantBlock && !hasStep(it.Steps, "block") {
				t.Fatalf("precondition: blocking-target probe has no block prelude: %+v", it.Steps)
			}
			if res := runSteps(t, reg, it.Scenario, it.Steps); len(res.Fails) != 0 {
				t.Fatalf("reduced-price probe fails with the static present: %v", res.Fails)
			}
			if res := runSteps(t, withoutStatics(reg, tc.name), it.Scenario, it.Steps); len(res.Fails) == 0 {
				t.Fatalf("probe is not sensitive to %s's cost reduction", tc.name)
			}
		})
	}
}
