package templates

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// A setup-placed "As ~ enters, choose a creature type" permanent: XMage poses
// its dialog while placing the card, so gorge's type leads step 0 as a
// setup_choice and the first gameplay answer (the mana colour) follows it.
func TestSetupCreatureTypeChoiceIsScripted(t *testing.T) {
	reg := loadGenRegistry(t)
	it, _ := activateRequirement(t, reg, "Patchwork Banner", "activate#0.0")
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	var setupType string
	for _, d := range res.Decisions {
		if d.Step < 0 && d.Resume == "etb" && len(d.PickKinds) == 1 && d.PickKinds[0] == "type" && len(d.Picks) == 1 {
			setupType = d.Picks[0]
		}
	}
	if setupType == "" {
		t.Fatalf("precondition: Patchwork Banner records no setup creature type pick: %+v", res.Decisions)
	}
	if len(it.XAnswers) == 0 || len(it.XAnswers[0]) != 2 {
		t.Fatalf("step 0 answers = %+v, want setup_choice then the colour choice", it.XAnswers)
	}
	first, second := it.XAnswers[0][0], it.XAnswers[0][1]
	if first.Kind != "setup_choice" || first.Value != setupType {
		t.Errorf("step 0 leads with %+v, want setup_choice %s", first, setupType)
	}
	if second.Kind != "choice" || second.Value != "White" {
		t.Errorf("first gameplay answer = %+v, want choice White", second)
	}
}

// The other three rows of the class: each carries the setup type ahead of its
// own first gameplay answer.
func TestSetupCreatureTypeChoiceLeadsSiblingRows(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, c := range []struct{ card, key, gameplay string }{
		{"Eclipsed Realms", "activate#0.1", "White"},
		{"Lifecraft Engine", "activate#0.0", "Colossal Dreadmaw"},
		{"Gathering Stone", "trigger#0.1", "yes"},
	} {
		c2, ok := reg.Lookup(c.card)
		if !ok {
			t.Fatalf("%s not in corpus", c.card)
		}
		var it oraclegen.Item
		found := false
		for _, r := range levelb.Requirements(c2) {
			if r.Key != c.key {
				continue
			}
			item, skip := GenerateB(reg, c.card, r)
			if skip != nil {
				t.Fatalf("%s %s skipped: %s", c.card, c.key, skip.Reason)
			}
			it, found = item, true
		}
		if !found {
			t.Fatalf("precondition: %s has no %s", c.card, c.key)
		}
		if len(it.XAnswers) == 0 || len(it.XAnswers[0]) == 0 || it.XAnswers[0][0].Kind != "setup_choice" {
			t.Errorf("%s: step 0 answers = %+v, want a leading setup_choice", c.card, it.XAnswers)
			continue
		}
		var gameplay []oraclegen.XAnswer
		for _, step := range it.XAnswers {
			for _, a := range step {
				if a.Kind != "setup_choice" {
					gameplay = append(gameplay, a)
				}
			}
		}
		if len(gameplay) == 0 || gameplay[0].Value != c.gameplay {
			t.Errorf("%s: first gameplay answer = %+v, want %s", c.card, gameplay, c.gameplay)
		}
	}
}

// wantSetupTypeCensus pins, per declared set, the number of generated
// scenarios whose setup places a permanent with an as-enters type ask and how
// many have the answer queued before setup placement (want all).
var wantSetupTypeCensus = map[string][2]int{
	"BIG": {0, 0},
	"EOE": {0, 0},
	"FDN": {2, 2},
	"FRA": {0, 0},
}

// The printed lists are frozen copies under testdata/printed so the test reads
// only its own package directory and stays cacheable.
func TestSetupCreatureTypeCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)
	got := map[string][2]int{}
	for _, set := range activateCensusSets {
		printed, err := compliance.LoadPrinted(filepath.Join("testdata", "printed"), set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		var counts [2]int
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
				wantSetup := 0
				for _, d := range result.Decisions {
					if oraclegen.IsSetupChoice(d) && d.PickKinds[0] == "type" {
						wantSetup++
					}
				}
				if wantSetup == 0 {
					continue
				}
				counts[0]++
				scripted := 0
				if len(item.XAnswers) > 0 {
					for _, a := range item.XAnswers[0] {
						if a.Kind == "setup_choice" {
							scripted++
						}
					}
				}
				if scripted >= wantSetup {
					counts[1]++
				}
			}
		}
		got[set] = counts
		t.Logf("%s setup creature-type scenarios=%d scripted=%d", set, counts[0], counts[1])
	}
	for _, set := range activateCensusSets {
		if got[set] != wantSetupTypeCensus[set] {
			t.Errorf("%s setup type census = %s, want %v", set, fmt.Sprint(got[set]), wantSetupTypeCensus[set])
		}
	}
}
