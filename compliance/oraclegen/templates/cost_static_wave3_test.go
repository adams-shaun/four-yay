package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestCostStaticWave3Probes covers the wave-3 cost-static rows that needed a
// prerequisite fixture the probe could not build:
//
//   - Bolt Bend and Spectral Denial are spells whose own target slot draws
//     from the stack (a retargeter and a counterspell), so the reduced-price
//     probe could not be cast with an empty stack. The probe now casts a
//     fixture spell first and holds priority (CR 117.3c).
//   - Rowdy Research and Witchstalker Frenzy reduce their cost by the number
//     of creatures that attacked this turn (Amount$ X over
//     PlayerCountPlayers$AttackersDeclared). The probe now attacks with a
//     fixture creature and casts in the second main.
//
// Each row's probe must play through gorge at the exact reduced price, and the
// same cast must fail once the card's own statics are removed, so a probe that
// pays the printed cost (or an engine that ignores the reduction) cannot pass.
func TestCostStaticWave3Probes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, probe, mana string
		printed                string
		reduction              int
		// wantAttack is true for the attacker-count rows, which must carry an
		// attack prelude; the stack-target rows must carry a precast instead.
		wantAttack bool
	}{
		{"Bolt Bend", "static#0.0", "Bolt Bend", "R", "CCCR", 3, false},
		{"Spectral Denial", "static#0.0", "Spectral Denial", "CU", "CCU", 1, false},
		{"Rowdy Research", "static#0.0", "Rowdy Research", "CCCCCU", "CCCCCCU", 1, true},
		{"Witchstalker Frenzy", "static#0.0", "Witchstalker Frenzy", "CCR", "CCCR", 1, true},
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
			foundSource := false
			for _, card := range it.Scenario.Setup["p0"].Hand {
				foundSource = foundSource || card == tc.name
			}
			if !foundSource {
				t.Fatalf("precondition: reduction source %q absent from hand (Card.Self is its own probe)", tc.name)
			}
			if tc.wantAttack {
				if !hasStep(it.Steps, "attack") {
					t.Fatalf("precondition: attacker-count probe has no attack prelude: %+v", it.Steps)
				}
			} else if !hasPrecast(it.Steps, tc.probe) {
				t.Fatalf("precondition: stack-target probe has no precast spell: %+v", it.Steps)
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

// hasStep reports whether steps carries an op of the given name.
func hasStep(steps []oraclegen.Step, op string) bool {
	for _, s := range steps {
		if s.Op == op {
			return true
		}
	}
	return false
}

// hasPrecast reports whether a cast of a spell other than the probe precedes
// the probe's own cast, the fixture that puts a spell on the stack.
func hasPrecast(steps []oraclegen.Step, probe string) bool {
	for _, s := range steps {
		if s.Op == "cast" && s.Card != "p0:"+probe {
			return true
		}
	}
	return false
}
