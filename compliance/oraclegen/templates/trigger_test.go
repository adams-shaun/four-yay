package templates

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// triggerRequirement finds name's level-B trigger requirement for key and
// generates its item.
func triggerRequirement(t *testing.T, reg *cards.Registry, name, key, sub string) oraclegen.Item {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key != key {
			continue
		}
		if r.Sub != sub {
			t.Fatalf("precondition: %s %s classified %s, want %s", name, key, r.Sub, sub)
		}
		it, skip := GenerateB(reg, name, r)
		if skip != nil {
			t.Fatalf("%s %s: %s", name, key, skip.Reason)
		}
		return it
	}
	t.Fatalf("precondition: %s carries no requirement %s", name, key)
	return oraclegen.Item{}
}

func runSteps(t *testing.T, reg *cards.Registry, sc oraclegen.Scenario, steps []oraclegen.Step) rules.OracleResult {
	t.Helper()
	sc.Steps = steps
	b, _ := json.Marshal(sc)
	res, err := rules.RunOracleScenarioJSON(reg, b)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// TestTriggerRecipes: one real card per recipe. The item plays through gorge,
// uses only cast/attack/resolve/pass_to with every pass_to to main2, and a
// snapshot shows the card's trigger on the stack.
func TestTriggerRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, sub string }{
		{"Weftstalker Ardent", "trigger#0.0", "trigger.etb-other"},
		{"Edge Rover", "trigger#0.0", "trigger.dies"},
		{"Comet Crawler", "trigger#0.0", "trigger.attacks"},
		{"Illvoi Infiltrator", "trigger#0.0", "trigger.combat-damage"},
		{"Oltec Matterweaver", "trigger#0.0", "trigger.spell-cast"},
		{"Ruric Thar, Biomagus", "trigger#0.0", "trigger.becomes-target"},
		{"Ajani's Pridemate", "trigger#0.0", "trigger.life-gained"},
		{"Erudite Wizard", "trigger#0.0", "trigger.drawn"},
	} {
		t.Run(tc.sub, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			if it.Template != tc.key || it.Card != tc.name || len(it.CR) != 1 || it.CR[0] != "603.2" {
				t.Fatalf("identity = (%s, %s, %v)", it.Card, it.Template, it.CR)
			}
			found := false
			for _, bf := range it.Scenario.Setup["p0"].Battlefield {
				found = found || bf == tc.name
			}
			if !found {
				t.Fatalf("precondition: %s not on p0's battlefield: %v", tc.name, it.Scenario.Setup["p0"].Battlefield)
			}
			causes, resolves := 0, 0
			for i, st := range it.Scenario.Steps {
				switch st.Op {
				case "cast", "attack":
					causes++
				case "resolve":
					resolves++
				case "pass_to":
					if st.Step != "main2" {
						t.Fatalf("step %d pass_to %q, want main2", i, st.Step)
					}
				default:
					t.Fatalf("step %d uses op %q outside {cast, attack, resolve, pass_to}", i, st.Op)
				}
			}
			if causes == 0 || resolves == 0 {
				t.Fatalf("want a cause and a resolve, got %d and %d: %+v", causes, resolves, it.Scenario.Steps)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !triggerShownOnStack(t, reg, it.Scenario, tc.name) {
				t.Fatalf("no snapshot shows an ability on the stack sourced by %s", tc.name)
			}
		})
	}
}

// triggerShownOnStack replays the item up to its cause (no resolve), stopping
// combat damage in end-combat where its trigger is still on the stack, and
// with both players passing once for a cast cause, and reports whether a
// snapshot shows an ability sourced by name.
func triggerShownOnStack(t *testing.T, reg *cards.Registry, sc oraclegen.Scenario, name string) bool {
	t.Helper()
	var cause []oraclegen.Step
	for _, st := range sc.Steps {
		if st.Op == "resolve" {
			continue
		}
		if st.Op == "pass_to" {
			st.Step = "end-combat"
		}
		cause = append(cause, st)
	}
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	for _, steps := range [][]oraclegen.Step{cause, append(append([]oraclegen.Step(nil), cause...), passes...)} {
		res := runSteps(t, reg, sc, steps)
		for _, s := range res.Snapshots {
			for _, e := range s.Stack {
				if e.Kind == "ability" && strings.Contains(strings.ToLower(e.Source), strings.ToLower(name)) {
					return true
				}
			}
		}
	}
	return false
}

// TestTriggerSkipsWhatCannotFire: a trigger the recipe's probe cannot cause
// skips, and a non-creature has no dies recipe. Each skip is named, so the
// template (not a missing registration) produced it.
func TestTriggerSkipsWhatCannotFire(t *testing.T) {
	reg := loadGenRegistry(t)
	sawFireSkip := false
	for _, c := range reg.Cards {
		f := c.Faces[0]
		for _, r := range levelb.Requirements(c) {
			if r.Gap != "" || r.Family != "trigger" || r.Sub != "trigger.drawn" {
				continue
			}
			if _, skip := GenerateB(reg, f.Name, r); skip != nil && skip.Reason == "trigger did not fire" {
				sawFireSkip = true
			}
		}
	}
	if !sawFireSkip {
		t.Fatal("no trigger.drawn requirement skipped as did-not-fire; the fire check is not running")
	}
	for _, sub := range []string{"trigger.gap:Foo"} {
		if triggerSubs(sub) {
			t.Fatalf("%s must not be served", sub)
		}
	}
}
