package rules

// Kernel-era restorations of the tests W3 removed from prompt_text_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestDecisionTextHasNoEngineSyntax plays the repo-deck sample to completion
// and checks every decision posed along the way: its Prompt and every
// option Label must be free of raw Forge cost/filter syntax.
func TestDecisionTextHasNoEngineSyntaxKernel(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	type hit struct{ where, pattern string }
	var mu sync.Mutex
	violations := map[string]hit{} // offending text -> first place seen
	decisions, labels := 0, 0
	kinds := map[decision.Kind]int{}
	// Each game is independent, so the games run as parallel subtests: this
	// test is the rules package's longest single body and the module gate's
	// critical-path tail, and its one goroutine used a single CPU while the
	// gate cgroup's other three sat idle. Per-decision failures stay
	// attributed to their own game; the unique-text report waits for all of
	// them.
	for _, g := range promptTextGames(t, reg) {
		t.Run(g.label, func(t *testing.T) {
			t.Parallel()
			e := New(g.cfg)
			// where is built only for a violation: formatting it for every
			// decision and option label was most of this checker's own cost.
			check := func(d *decision.Decision, suffix, optKind, text string) {
				p := engineSyntaxIn(text)
				if p == "" {
					return
				}
				where := fmt.Sprintf("%s: turn %d, %s decision", g.label, e.G.Turn, d.Kind) + suffix + optKind
				mu.Lock()
				// Keep the lexicographically first place for a repeated text so
				// the report is deterministic however the games interleave.
				if prev, seen := violations[text]; !seen || where < prev.where {
					violations[text] = hit{where, p}
				}
				mu.Unlock()
			}
			b := newTestBot(g.bot)
			e.Advance()
			n := 0
			gameDecisions, gameLabels := 0, 0
			gameKinds := map[decision.Kind]int{}
			for !e.G.Over && e.Pending() != nil && n < 60000 {
				d := e.Pending()
				gameDecisions++
				gameKinds[d.Kind]++
				check(d, " prompt", "", d.Prompt)
				for _, o := range d.Options {
					gameLabels++
					check(d, " option ", o.Kind, o.Label)
				}
				if err := e.Submit(b.answer(e, d)); err != nil {
					t.Fatalf("%s, intent %d: %v", g.label, n, err)
				}
				n++
			}
			mu.Lock()
			decisions, labels = decisions+gameDecisions, labels+gameLabels
			for k, c := range gameKinds {
				kinds[k] += c
			}
			mu.Unlock()
		})
	}
	t.Cleanup(func() {
		var ks []string
		for k, c := range kinds {
			ks = append(ks, fmt.Sprintf("%s=%d", k, c))
		}
		sort.Strings(ks)
		t.Logf("checked %d decisions (%s) and %d option labels", decisions, strings.Join(ks, " "), labels)
		var texts []string
		for s := range violations {
			texts = append(texts, s)
		}
		sort.Strings(texts)
		for _, s := range texts {
			t.Errorf("%s: %q contains engine syntax (%s)", violations[s].where, s, violations[s].pattern)
		}
	})
}
