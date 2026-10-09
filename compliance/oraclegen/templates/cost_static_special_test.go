package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestCostStaticSpecialActionProbes covers the special-action and turn-gated
// cost-static rows:
//
//   - Geyser Drake's Condition$ NotPlayerTurn reducer: the probe casts an
//     instant on p1's first main phase, reached by a pass_to, at the printed
//     price one generic short.
//   - Inquisitive Glimmer's ValidSpell$ Static.Unlock reducer: the probe casts
//     the room unlock probe and unlocks its locked right half at the door's
//     printed cost one generic short, the step shape (and XMage rule text) the
//     FullyUnlock recipe already replays in XMage.
//   - Gathering Stone's ValidCard$ Card.ChosenType reducer: the as-enters type
//     choice is answered in setup ("Bear"), and the probe casts a Bear spell
//     at the printed price one generic short.
//
// Each row's probe must play through gorge at the exact reduced price, and
// the same cast must fail once the card's own statics are removed, so a probe
// that pays the printed cost (or an engine that ignores the reduction) cannot
// pass. The two remaining Static.* shapes stay named skips, pinned with their
// reasons: a plotting probe cannot be replayed by the XMage driver, and a
// token's ability has no XMage-proven rule text to activate it by.
func TestCostStaticSpecialActionProbes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, op, probe, mana, ability string
		reduction                           int
	}{
		{"Geyser Drake", "static#0.0", "cast", "", "", "", 1},
		{"Inquisitive Glimmer", "static#0.1", "activate", "Dazzling Theater // Prop Room", "CW", "Unlock Prop Room", 1},
		{"Gathering Stone", "static#0.0", "cast", "Grizzly Bears", "G", "", 1},
	} {
		t.Run(tc.name+" "+tc.key, func(t *testing.T) {
			it, skip := GenerateB(reg, tc.name, costRequirement(t, reg, tc.name, tc.key))
			if skip != nil {
				t.Fatalf("GenerateB: %s", skip.Reason)
			}
			probe := probeStep(t, it)
			if probe.Op != tc.op {
				t.Fatalf("probe op = %s, want %s", probe.Op, tc.op)
			}
			if tc.probe != "" && probe.Card != "p0:"+tc.probe {
				t.Fatalf("probe card = %s, want p0:%s", probe.Card, tc.probe)
			}
			if tc.ability != "" && probe.Ability != tc.ability {
				t.Fatalf("probe ability = %q, want %q", probe.Ability, tc.ability)
			}
			// Precondition: the probe's price is its printed pool one
			// generic short (the static's reduction). A row whose probe is
			// picked from the corpus (Geyser Drake's) pins the fixed mana
			// field empty and asserts only this printed-minus-reduction
			// relation.
			probeName := strings.TrimPrefix(probe.Card, "p0:")
			c, ok := reg.Lookup(probeName)
			if !ok {
				t.Fatalf("precondition: probe %s absent", probeName)
			}
			if tc.mana != "" && probe.Mana != tc.mana {
				t.Fatalf("probe pays %q, want %q", probe.Mana, tc.mana)
			}
			if tc.name == "Inquisitive Glimmer" {
				if door := c.Faces[1].ManaCost; door != "2 W" {
					t.Fatalf("precondition: locked door prints %q, want 2 W", door)
				}
			} else if pool, why := oraclegen.PoolFor(c.Faces[0].ManaCost); why != "" || len(pool)-len(probe.Mana) != tc.reduction {
				t.Fatalf("precondition: %s prints %q (%s), probe pays %q, reduction %d", probeName, pool, why, probe.Mana, tc.reduction)
			}
			switch tc.name {
			case "Geyser Drake":
				// Precondition: the probe is an instant (the only spell
				// legal on an opponent's turn) and a pass_to to p1's main
				// phase precedes it.
				if !c.Faces[0].IsInstant() {
					t.Fatalf("precondition: %s is not an instant", probeName)
				}
				if !specialReachesOpponentTurn(it.Steps, len(it.Steps)-1) {
					t.Fatalf("precondition: no pass_to to p1's main phase before the probe: %+v", it.Steps)
				}
			case "Gathering Stone":
				// Precondition: the probe matches the chosen type the setup
				// answer fixes, and the answer is queued before the steps.
				if !oraclegen.HasType(c.Faces[0], "Bear") {
					t.Fatalf("precondition: %s is not a Bear", probeName)
				}
				if len(it.Scenario.SetupAnswers) != 1 || it.Scenario.SetupAnswers[0].Kind != "choose" || len(it.Scenario.SetupAnswers[0].Pick) != 1 || it.Scenario.SetupAnswers[0].Pick[0] != "Bear" {
					t.Fatalf("precondition: setup answers = %+v, want one choose of Bear", it.Scenario.SetupAnswers)
				}
			case "Inquisitive Glimmer":
				// Precondition: the room is cast and resolved before the
				// unlock, and the unlock step carries the XMage rule text.
				idx := -1
				for i, s := range it.Steps {
					if s.Op == "activate" {
						idx = i
					}
				}
				if idx < 0 {
					t.Fatalf("precondition: no activate step: %+v", it.Steps)
				}
				cast, resolved := false, false
				for _, s := range it.Steps[:idx] {
					cast = cast || (s.Op == "cast" && s.Card == "p0:"+tc.probe)
					resolved = resolved || s.Op == "resolve"
				}
				if !cast || !resolved {
					t.Fatalf("precondition: the room is not cast and resolved before the unlock: %+v", it.Steps[:idx])
				}
				if got := it.XAbility[idx]; got != "{2}{W}: Unlock the right half." {
					t.Fatalf("unlock xmage_ability = %q, want the door's printed rule text", got)
				}
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

// TestCostStaticSpecialActionSkips pins the two named skips the special-action
// work leaves behind, with the exact reasons: a bare "not supported" would
// hide which prerequisite is missing.
func TestCostStaticSpecialActionSkips(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, reason string }{
		{"Doc Aurlock, Grizzled Genius", "static#0.1", "plotting probe unavailable (cast_mode plot is not XMage-replayable)"},
		{"Mutagen Man, Living Ooze", "static#0.0", "token ability fixture unavailable (no XMage-proven token ability text)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, skip := GenerateB(reg, tc.name, costRequirement(t, reg, tc.name, tc.key))
			if skip == nil {
				t.Fatalf("produced a scenario, want the named skip")
			}
			if want := "cost static probe not supported: " + tc.reason; skip.Reason != want {
				t.Fatalf("skip = %q, want %q", skip.Reason, want)
			}
		})
	}
}

// specialReachesOpponentTurn reports whether a pass_to step that stops on
// p1's first main phase precedes the step at index i.
func specialReachesOpponentTurn(steps []oraclegen.Step, i int) bool {
	for j := 0; j < i && j < len(steps); j++ {
		s := steps[j]
		if s.Op == "pass_to" && s.Step == "main1" && s.Active == "p1" {
			return true
		}
	}
	return false
}
