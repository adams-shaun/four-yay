package builtins

import (
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestZZDumpDeckIR(t *testing.T) {
	if os.Getenv("SB_DUMP") == "" {
		t.Skip()
	}
	reg, err := testutil.OpenCorpusRegistry("../../../.cards")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var names []string
	for _, id := range spellbench.BenchmarkPool {
		d, err := spellbench.Deck(reg, spellbench.PauperKernel, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range d {
			n := c.Faces[0].Name
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	sort.Strings(names)
	for _, n := range names {
		c, _ := reg.Lookup(n)
		for fi, f := range c.Faces {
			fmt.Printf("== %s [face %d] %s | %v | kw=%v\n", f.Name, fi, f.ManaCost, f.Types, f.Keywords)
			for i, a := range f.Abilities {
				fmt.Printf("  A%d %s %s %v\n", i, a.Kind, a.API, a.Params)
				for s := a.Sub; s != nil; s = s.Sub {
					fmt.Printf("     sub %s %v\n", s.API, s.Params)
				}
			}
			for _, tr := range f.Triggers {
				fmt.Printf("  T %s %v\n", tr.Mode, tr.Params)
				for s := tr.Effect; s != nil; s = s.Sub {
					fmt.Printf("     eff %s %s %v\n", s.Kind, s.API, s.Params)
				}
			}
			for _, st := range f.Statics {
				fmt.Printf("  S %s %v\n", st.Mode, st.Params)
			}
		}
	}
}
