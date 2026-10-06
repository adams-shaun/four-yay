package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// costCompoundRows is every activate row whose verdict recorded the joined
// "^" answer as an XMage anomaly ("xmage did not pose a decision gorge did:
// ... found [A^B^C]"): the cost was paid from the singles, so the compound is
// a leftover the next dialog must not consume. Measured over
// compliance/verdicts/*.jsonl on 2026-10-06: 11 rows, all of them activate.
// Supportive Parents is the one that instead threw, because ee44a1747 queues a
// mana-colour answer after the compound.
var costCompoundRows = []struct {
	name string
	key  string
}{
	{"Adaptive Gemguard", "activate#0.0"},
	{"Baylen, the Haymaker", "activate#0.1"},
	{"Dutiful Griffin", "activate#0.0"},
	{"Goldfury Strider", "activate#0.0"},
	{"High Perfect Morcant", "activate#0.0"},
	{"Kithkeeper", "activate#0.0"},
	{"Magda, the Hoardmaster", "activate#0.0"},
	{"Rat King, Verminister", "activate#0.0"},
	{"Sunshot Militia", "activate#0.0"},
	{"The Gold Saucer", "activate#0.2"},
	{"Warden of the Inner Sky", "activate#0.0"},
	{"Supportive Parents", "activate#0.0"},
}

// TestActivateCostCompoundClassDropped proves dropCostCompound covers every
// subtitle with a recorded multi-pick tap/sacrifice cost batch, not just the
// three cases in TestActivateCostCompoundDropped. Each row is regenerated from
// the live corpus and asserted to hold no "^" answer whose every part is a
// cost pick.
func TestActivateCostCompoundClassDropped(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range costCompoundRows {
		t.Run(tc.name, func(t *testing.T) {
			it, _ := activateRequirement(t, reg, tc.name, tc.key)
			step := activateStepIndex(it.Steps)
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok {
				t.Fatal("precondition: generated activation must play through")
			}
			// Precondition: the row really carries a multi-pick all-cost
			// decision, so a row that stopped posing one fails loudly here
			// instead of passing vacuously.
			costDecisions := 0
			var compounds []string
			for _, d := range res.Decisions {
				if d.Step != step || d.Seat != 0 || d.Kind != "choose_n" {
					continue
				}
				if len(d.Picks) >= 2 && allCostPicks(d) {
					costDecisions++
					compounds = append(compounds, strings.Join(d.Picks, "^"))
				}
			}
			if costDecisions == 0 {
				t.Fatalf("precondition: %s %s poses no multi-pick cost decision", tc.name, tc.key)
			}
			answers := it.XAnswers[step]
			for _, want := range compounds {
				for _, a := range answers {
					if strings.EqualFold(a.Value, want) {
						t.Errorf("compound cost answer %q left in %v", a.Value, answers)
					}
				}
			}
			for _, a := range answers {
				if strings.Contains(a.Value, "^") {
					t.Errorf("a joined answer %q survives in %v", a.Value, answers)
				}
			}
			// The singles that actually pay the cost must all still be queued.
			for _, d := range res.Decisions {
				if d.Step != step || d.Seat != 0 || d.Kind != "choose_n" || len(d.Picks) < 2 || !allCostPicks(d) {
					continue
				}
				for _, pick := range d.Picks {
					found := false
					for _, a := range answers {
						if strings.EqualFold(a.Value, pick) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("cost pick %q missing from %v", pick, answers)
					}
				}
			}
		})
	}
}
