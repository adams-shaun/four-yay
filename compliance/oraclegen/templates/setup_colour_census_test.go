package templates

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// wantSetupColourCensus pins, per declared set, the number of generated setup
// ETB colour asks and how many have an answer queued before setup placement.
var wantSetupColourCensus = map[string][2]int{
	"BIG": {2, 2},
	"EOE": {0, 0},
	"FDN": {4, 4},
	"FRA": {2, 2},
}

func TestSetupColourCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..", "..")
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)
	got := map[string][2]int{}
	for _, set := range activateCensusSets {
		printed, err := compliance.LoadPrinted(filepath.Join(root, "compliance", "printed"), set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		var counts [2]int // setup-colour decisions, decisions with scripted answers
		for _, name := range printed.Cards {
			card, ok := compliance.CorpusNameFold(has, folded, name)
			if !ok {
				t.Errorf("%s: %q not in corpus", set, name)
				continue
			}
			c, _ := reg.Lookup(card)
			for _, req := range levelb.Requirements(c) {
				item, skip := GenerateB(reg, card, req)
				if skip != nil {
					continue
				}
				result, err := rules.RunOracleScenarioJSON(reg, item.Raw())
				if err != nil {
					t.Errorf("%s %s: replay generated setup: %v", set, req.Key, err)
					continue
				}
				needsAnswer := false
				for _, d := range result.Decisions {
					if d.Step == -1 && d.Resume == "etb" && d.Kind == "choose_n" && len(d.PickKinds) == 1 && d.PickKinds[0] == "color" {
						needsAnswer = true
					}
				}
				if needsAnswer {
					counts[0]++
					for _, answers := range item.XAnswers {
						for _, answer := range answers {
							if answer.Kind == "setup_choice" {
								counts[1]++
							}
						}
					}
				}
			}
		}
		got[set] = counts
		t.Logf("%s setup colour scenarios=%d scripted=%d", set, counts[0], counts[1])
	}
	for _, set := range activateCensusSets {
		if got[set] != wantSetupColourCensus[set] {
			t.Errorf("%s setup colour census = %s, want %v", set, fmt.Sprint(got[set]), wantSetupColourCensus[set])
		}
	}
}
