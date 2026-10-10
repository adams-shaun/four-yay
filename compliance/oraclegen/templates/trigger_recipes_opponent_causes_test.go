package templates

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// TestOpponentCauseTriggerRecipes serves the five opponent-side event rows
// the opponent-cause recipes were built for: each row generates an item whose
// scenario plays through gorge and shows the row's trigger on the stack.
func TestOpponentCauseTriggerRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, sub, probe string
		p1Hand, p1Board       string // expected p1 zone holding the probe; "" for p0's board
	}{
		{"Wan Shi Tong, Librarian", "trigger#0.1", "trigger.searched-library", "Demonic Tutor", "Demonic Tutor", ""},
		{"Aclazotz, Deepest Betrayal", "trigger#0.1", "trigger.discarded-opponent", "Mind Rot", "Forest", ""},
		{"Zidane, Tantalus Thief", "trigger#0.1", "trigger.changes-controller", "Threaten", "Threaten", ""},
		{"Avalanche of Sector 7", "trigger#0.0", "trigger.ability-activated-opponent", "Sensei's Divining Top", "", "Sensei's Divining Top"},
		{"Market Gnome", "trigger#0.1", "trigger.exiled-craft", "Braided Net", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			found := false
			for _, step := range it.Scenario.Steps {
				if strings.Contains(step.Card, tc.probe) {
					found = true
				}
			}
			if !found {
				t.Fatalf("cause does not include expected probe %q: %+v", tc.probe, it.Scenario.Steps)
			}
			if tc.p1Hand != "" {
				if !inZone(it.Scenario.Setup["p1"].Hand, tc.p1Hand) {
					t.Fatalf("precondition: %q not in p1's hand: %v", tc.p1Hand, it.Scenario.Setup["p1"].Hand)
				}
			}
			if tc.p1Board != "" {
				if !inZone(it.Scenario.Setup["p1"].Battlefield, tc.p1Board) {
					t.Fatalf("precondition: %q not on p1's battlefield: %v", tc.p1Board, it.Scenario.Setup["p1"].Battlefield)
				}
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("scenario does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if tc.name == "Wan Shi Tong, Librarian" && !hasCompareOption(it.Compare, oraclegen.CompareNoLibraryOrder) {
				// The p1 tutor's search-and-shuffle randomises p1's library in
				// XMage: the compare must ignore its order.
				t.Fatalf("precondition: searched-library item lost CompareNoLibraryOrder: %v", it.Compare)
			}
			if !opponentCauseFiredOnStack(t, reg, it.Scenario, tc.name) {
				t.Fatalf("%s's trigger never appears on stack", tc.name)
			}
		})
	}
}

// opponentCauseFiredOnStack replays the item's cause (its trailing resolves
// dropped) under the tails the opponent-side causes need — a p0-cast cause
// still resolves only on a pass pair — and reports whether a snapshot shows
// an ability sourced by name. The pass pair comes in both orders (the seat
// that holds priority passes first) and the answered tail adds the pass_to
// that stops at the next priority decision, where a mid-resolution ask's
// trigger waits.
func opponentCauseFiredOnStack(t *testing.T, reg *cards.Registry, sc oraclegen.Scenario, name string) bool {
	t.Helper()
	steps := sc.Steps
	for len(steps) > 0 && steps[len(steps)-1].Op == "resolve" {
		steps = steps[:len(steps)-1]
	}
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	reverse := []oraclegen.Step{{Op: "pass", Seat: 1}, {Op: "pass", Seat: 0}}
	var tails [][]oraclegen.Step
	tails = append(tails, nil, passes, reverse,
		append(append([]oraclegen.Step(nil), passes...), oraclegen.Step{Op: "pass_to", Decision: "priority"}),
		append(append([]oraclegen.Step(nil), reverse...), oraclegen.Step{Op: "pass_to", Decision: "priority"}))
	for _, tail := range tails {
		try := append(append([]oraclegen.Step(nil), steps...), tail...)
		sc.Steps = try
		b, _ := json.Marshal(sc)
		res, err := rules.RunOracleScenarioJSON(reg, b)
		if err != nil {
			t.Fatal(err)
		}
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
