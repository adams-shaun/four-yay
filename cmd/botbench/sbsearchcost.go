package main

// The sb-search cost report: internal/spellbench/sbsearch may not read a
// clock (internal/archtest), so this command installs sbsearch.Millis and
// collects one sbsearch.Diag per searched decision through sbsearch.Watch.
// The report is printed after a -spellbench run that seats an sb-search*
// policy; it never reaches an answer.

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/internal/spellbench/sbsearch"
)

var sbSearchStats struct {
	mu    sync.Mutex
	diags []sbsearch.Diag
}

func isSBSearchPolicy(spec string) bool { return strings.HasPrefix(basePolicy(spec), "sb-search") }

func installSBSearchCostStats() {
	sbSearchStats.mu.Lock()
	sbSearchStats.diags = nil
	sbSearchStats.mu.Unlock()
	t0 := time.Now()
	sbsearch.Millis = func() float64 { return float64(time.Since(t0).Microseconds()) / 1000 }
	sbsearch.Watch = func(dg sbsearch.Diag) {
		sbSearchStats.mu.Lock()
		sbSearchStats.diags = append(sbSearchStats.diags, dg)
		sbSearchStats.mu.Unlock()
	}
}

// sbSearchCostReport summarises the searched decisions of games seated
// games (the sb-search seat-games of the run).
func sbSearchCostReport(games int) string {
	sbSearchStats.mu.Lock()
	ds := append([]sbsearch.Diag(nil), sbSearchStats.diags...)
	sbSearchStats.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "sb-search cost: %d searched decisions over %d seat-games\n", len(ds), games)
	if len(ds) == 0 {
		return b.String()
	}
	kinds := map[string]int{}
	var ms []float64
	over, worlds, failed, rollouts, refusedDecs := 0, 0, 0, 0, 0
	var total float64
	reasons := map[string]int{}
	for _, d := range ds {
		kinds[d.Kind]++
		ms = append(ms, d.MS)
		total += d.MS
		if d.Override {
			over++
		}
		worlds += d.Worlds
		failed += d.Failed
		rollouts += d.Rollouts
		if d.Worlds == 0 {
			refusedDecs++
		}
		if d.Refused != "" {
			r := d.Refused
			if len(r) > 90 {
				r = r[:90]
			}
			reasons[r]++
		}
	}
	sort.Float64s(ms)
	q := func(p float64) float64 { return ms[int(p*float64(len(ms)-1))] }
	n := float64(len(ds))
	fmt.Fprintf(&b, "  kinds: priority %d attackers %d; overrides %d (%.1f%%); per game %.1f searched, %.2f overrides\n",
		kinds["priority"], kinds["attackers"], over, 100*float64(over)/n, n/float64(max(games, 1)), float64(over)/float64(max(games, 1)))
	fmt.Fprintf(&b, "  ms/searched decision: mean %.1f p50 %.1f p90 %.1f p99 %.1f max %.1f; search ms/game %.0f\n",
		total/n, q(0.5), q(0.9), q(0.99), ms[len(ms)-1], total/float64(max(games, 1)))
	fmt.Fprintf(&b, "  worlds valid %d failed %d (decisions with no valid world %d); rollouts %d\n", worlds, failed, refusedDecs, rollouts)
	// Override rate by sb-tactical's score gap between its pick and the
	// runner-up (priority): where the search disagrees with the heuristic.
	edges := []float64{1, 3, 6, 10, 20}
	var bn, bo [6]int
	for _, d := range ds {
		if d.Kind != "priority" {
			continue
		}
		i := 0
		for i < len(edges) && d.Gap >= edges[i] {
			i++
		}
		bn[i]++
		if d.Override {
			bo[i]++
		}
	}
	fmt.Fprintf(&b, "  overrides by score gap:")
	for i := range bn {
		lo := "0"
		if i > 0 {
			lo = fmt.Sprint(edges[i-1])
		}
		fmt.Fprintf(&b, " [%s,) %d/%d", lo, bo[i], bn[i])
	}
	fmt.Fprintln(&b)
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return reasons[keys[i]] > reasons[keys[j]] || (reasons[keys[i]] == reasons[keys[j]] && keys[i] < keys[j])
	})
	for i, k := range keys {
		if i >= 6 {
			break
		}
		fmt.Fprintf(&b, "  first failure %4d x %s\n", reasons[k], k)
	}
	return b.String()
}
