package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestActivateCostCompoundDropped pins that a multi-pick tapXType or Sac
// cost is scripted for XMage as one answer per paid permanent, never also as
// the joined "^" answer. XMage's cost consumes the singles, so a compound left
// in the queue is consumed by the next dialog: Supportive Parents' colour
// choice threw "Choice key [Supportive Parents^Grizzly Bears] not found in
// [White, Blue, Black, Red, Green]" (DRIFT, driver-batch-20261006T165451Z),
// and Kithkeeper's and Rat King's compounds were the unused leftover choice.
func TestActivateCostCompoundDropped(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		name   string
		kind   string
		picks  []string
		colour bool
	}{
		{"Supportive Parents", "tapcost", []string{"Supportive Parents", "Grizzly Bears"}, true},
		{"Kithkeeper", "tapcost", []string{"Kithkeeper", "Grizzly Bears", "Llanowar Elves"}, false},
		{"Rat King, Verminister", "sacrifice", []string{"Rat King, Verminister", "Bog Rats", "Typhoid Rats"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it, _ := activateRequirement(t, reg, tc.name, "activate#0.0")
			step := activateStepIndex(it.Steps)
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok {
				t.Fatal("precondition: generated activation must play through")
			}
			observed := false
			for _, d := range res.Decisions {
				if d.Step == step && d.Seat == 0 && d.Kind == "choose_n" && hasPickKind(d, tc.kind) && len(d.Picks) == len(tc.picks) {
					observed = true
				}
			}
			if !observed {
				t.Fatalf("precondition: no observed %d-pick %s decision at step %d: %+v", len(tc.picks), tc.kind, step, res.Decisions)
			}
			answers := it.XAnswers[step]
			if len(answers) < len(tc.picks) {
				t.Fatalf("activate step answers %+v hold fewer than the %d tap picks", answers, len(tc.picks))
			}
			for i, pick := range tc.picks {
				if a := answers[i]; a.Seat != 0 || a.Kind != "choice" || !strings.EqualFold(a.Value, pick) {
					t.Fatalf("answer %d = %+v, want cost pick %q first (cost before effect): %+v", i, a, pick, answers)
				}
			}
			for _, a := range answers {
				if strings.Contains(a.Value, "^") {
					t.Errorf("compound cost answer %q left in the queue: %+v", a.Value, answers)
				}
			}
			rest := answers[len(tc.picks):]
			if tc.colour {
				if len(rest) != 1 || !isColourName(rest[0].Value) {
					t.Errorf("after the cost picks want exactly the mana colour answer, got %+v", rest)
				}
			} else if len(rest) != 0 {
				t.Errorf("after the cost picks want no answer, got %+v", rest)
			}
		})
	}
}
