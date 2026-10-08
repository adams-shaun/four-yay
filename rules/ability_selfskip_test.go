package rules

import (
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestAbilitySelfSkipTurnsCensus is the class census for the activation
// self-harm rider decision.Option.SelfSkipTurns carries to the bot policy
// (botpolicy A6): every corpus activated ability whose SubAbility$ chain
// makes its activator skip turns. The table is exact in both directions, so
// a corpus pin that adds a carrier (or a reader change that drops one)
// names it here; Ral Zarek's SkipTurn names its target, not the activator,
// so it reads 0 and stays out.
func TestAbilitySelfSkipTurnsCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	want := []string{
		"Chronatog Avatar=1",
		"Chronatog Totem=1",
		"Chronatog=1",
		"Chronosavant=1",
		"Lethal Vapors=1",
		"Magosi, the Waterveil=1",
	}
	var got []string
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			for _, ab := range f.Abilities {
				if ab.Kind != "AB" {
					continue
				}
				if n := abilitySelfSkipTurns(ab); n > 0 {
					got = append(got, f.Name+"="+strconv.Itoa(int(n)))
				}
			}
		}
	}
	sort.Strings(got)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("self-skip activated abilities = %q, want %q", got, want)
	}
}
