package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestColourPipsASCIIMatchesSlow holds colourPips' byte-loop path to the
// braceForm/FieldsSeq parse over every printed cost in the corpus plus the
// separator and symbol shapes a cost string can take.
func TestColourPipsASCIIMatchesSlow(t *testing.T) {
	costs := []string{"", " ", "U", "{U}", "{2}{U}{U}", "2 U U", "W/U W/U", "{W/U}{B}", "UP", "X X G",
		"\tR\nG\r", "{{G}}", "G}{", "C", "no cost", "1 W W W", "WU", "{R}{G} {W}", "S", "{X}{R}"}
	reg := testutil.CorpusRegistry(t)
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if f != nil {
				costs = append(costs, f.ManaCost)
			}
		}
	}
	n := 0
	for _, mc := range costs {
		fast, ok := colourPipsASCII(mc)
		if !ok {
			continue
		}
		n++
		if want := colourPipsSlow(mc); fast != want {
			t.Fatalf("colourPips(%q): fast %v, slow %v", mc, fast, want)
		}
	}
	if n < 1000 {
		t.Fatalf("only %d costs compared", n)
	}
}
