package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// caseSolvedReadCensus is the measured population of every corpus "Solved"
// read, by the reader that now models it (cli-20261006T024354Z-e5dc4abb):
//
//   - grant: `AlterAttribute | Attributes$ Solved`, the "To solve --"
//     body (effects.effAlterAttribute -> events.AlterAttribute -> Object.Solved);
//   - filter: an `IsSolved` / `!IsSolved` filter property (IsPresent$,
//     IsPresent2$, Affected$), the effects "IsSolved" predicate;
//   - activation: `Activation$ Solved`, rules.activationConditionOK;
//   - trigger: `Mode$ CaseSolved`, trigmatch.caseSolvedMatches.
//
// Descriptions, SVar names and Execute$ targets that merely spell "Solved"
// are not reads and are not counted.
var caseSolvedReadCensus = map[string]int{
	"grant":      15,
	"filter":     28,
	"activation": 3,
	"trigger":    1,
}

func classifySolvedRead(key, val string) string {
	switch {
	case key == "Attributes" && strings.EqualFold(strings.TrimSpace(val), "Solved"):
		return "grant"
	case key == "Activation" && strings.EqualFold(strings.TrimSpace(val), "Solved"):
		return "activation"
	case strings.Contains(val, "IsSolved"):
		return "filter"
	}
	return ""
}

func solvedReadsOf(params map[string]string, sink func(string)) {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if c := classifySolvedRead(k, params[k]); c != "" {
			sink(c)
		}
	}
}

// TestCaseSolvedCorpusCensus walks every corpus face and counts its Solved
// reads by reader. Every read must land on a modelled reader, the counts
// are exact in both directions (a new corpus shape or a lost one fails),
// and no card carrying a Solved read is unsupported because of it.
func TestCaseSolvedCorpusCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	got := map[string]int{}
	var carriers []string
	for _, c := range reg.AllCards() {
		before := 0
		for _, n := range got {
			before += n
		}
		sink := func(class string) { got[class]++ }
		for _, f := range c.Faces {
			for _, ab := range f.Abilities {
				// Sub-abilities are SVar bodies, counted once below.
				solvedReadsOf(ab.Params, sink)
			}
			for _, tr := range f.Triggers {
				if tr.Mode == "CaseSolved" {
					sink("trigger")
				}
				solvedReadsOf(tr.Params, sink)
			}
			for _, st := range f.Statics {
				solvedReadsOf(st.Params, sink)
			}
			for _, rp := range f.Repls {
				solvedReadsOf(rp.Params, sink)
			}
			svars := make([]string, 0, len(f.SVars))
			for k := range f.SVars {
				svars = append(svars, k)
			}
			sort.Strings(svars)
			for _, k := range svars {
				for _, part := range strings.Split(f.SVars[k], "|") {
					kv := strings.SplitN(strings.TrimSpace(part), "$", 2)
					if len(kv) == 2 {
						if cl := classifySolvedRead(strings.TrimSpace(kv[0]), kv[1]); cl != "" {
							sink(cl)
						}
					}
				}
			}
		}
		after := 0
		for _, n := range got {
			after += n
		}
		if after == before {
			continue
		}
		carriers = append(carriers, c.Faces[0].Name)
		for _, m := range reg.Unsupported(c, supported) {
			if strings.Contains(m, "Solved") || strings.Contains(m, "AlterAttribute") {
				t.Errorf("%s: Solved read still unsupported: %s", c.Faces[0].Name, m)
			}
		}
	}
	if len(carriers) == 0 {
		t.Fatal("no corpus card carries a Solved read: the corpus is missing")
	}
	for class, want := range caseSolvedReadCensus {
		if got[class] != want {
			t.Errorf("Solved %s reads = %d, want %d", class, got[class], want)
		}
	}
	for class := range got {
		if _, ok := caseSolvedReadCensus[class]; !ok {
			t.Errorf("unclassified Solved read class %q", class)
		}
	}
	t.Logf("%d carriers: %v", len(carriers), carriers)
	if auditor, ok := reg.Lookup("Case File Auditor"); !ok {
		t.Fatal("corpus card Case File Auditor not found")
	} else if m := reg.Unsupported(auditor, supported); len(m) != 0 {
		t.Errorf("Case File Auditor still unsupported: %v", m)
	}
}
