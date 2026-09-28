package main

// The sb-search cost report: internal/spellbench/sbsearch may not read a
// clock (internal/archtest), so this command installs sbsearch.Millis and
// collects one sbsearch.Diag per searched decision through sbsearch.Watch.
// The report is printed after a -spellbench run that seats an sb-search*
// policy; it never reaches an answer.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/internal/spellbench/sbsearch"
)

var sbSearchStats struct {
	mu    sync.Mutex
	diags []sbsearch.Diag
	flush func()
}

func isSBSearchPolicy(spec string) bool { return strings.HasPrefix(basePolicy(spec), "sb-search") }

func installSBSearchCostStats() {
	sbSearchStats.mu.Lock()
	sbSearchStats.diags = nil
	sbSearchStats.mu.Unlock()
	t0 := time.Now()
	sbsearch.Millis = func() float64 { return float64(time.Since(t0).Microseconds()) / 1000 }
	// GORGE_SBSEARCH_DIAGS names a JSONL file that receives every Diag with
	// its per-world root values (offline analysis of budgets and margins).
	var dump *bufio.Writer
	if path := os.Getenv("GORGE_SBSEARCH_DIAGS"); path != "" {
		if f, err := os.Create(path); err == nil {
			dump = bufio.NewWriterSize(f, 1<<20)
			sbSearchStats.flush = func() { dump.Flush(); f.Close() }
		}
	}
	sbsearch.Watch = func(dg sbsearch.Diag) {
		sbSearchStats.mu.Lock()
		if dump != nil {
			fin := func(x float64) *float64 {
				if math.IsNaN(x) || math.IsInf(x, 0) {
					return nil
				}
				return &x
			}
			vals := make([][]*float64, len(dg.Values))
			for i, row := range dg.Values {
				vals[i] = make([]*float64, len(row))
				for j, x := range row {
					vals[i][j] = fin(x)
				}
			}
			b, _ := json.Marshal(map[string]any{"policy": dg.Policy, "turn": dg.Turn, "kind": dg.Kind, "cands": dg.Candidates, "worlds": dg.Worlds,
				"failed": dg.Failed, "override": dg.Override, "gap": dg.Gap, "lead": fin(dg.Lead), "ms": dg.MS, "values": vals})
			dump.Write(append(b, '\n'))
		}
		dg.Values = nil
		sbSearchStats.diags = append(sbSearchStats.diags, dg)
		sbSearchStats.mu.Unlock()
	}
}

// sbSearchCostReports is one sbSearchCostReport per sb-search policy of
// the run (games: each policy's seat-games).
func sbSearchCostReports(policies []string, games []int) string {
	sbSearchStats.mu.Lock()
	all := append([]sbsearch.Diag(nil), sbSearchStats.diags...)
	if sbSearchStats.flush != nil {
		sbSearchStats.flush()
		sbSearchStats.flush = nil
	}
	sbSearchStats.mu.Unlock()
	var b strings.Builder
	for i, p := range policies {
		var ds []sbsearch.Diag
		for _, d := range all {
			if d.Policy == basePolicy(p) || len(policies) == 1 {
				ds = append(ds, d)
			}
		}
		b.WriteString(sbSearchCostReport(p, ds, games[i]))
	}
	return b.String()
}

// sbSearchCostReport summarises the searched decisions ds of games seated
// games (the sb-search seat-games of the run).
func sbSearchCostReport(policy string, ds []sbsearch.Diag, games int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "sb-search cost [%s]: %d searched decisions over %d seat-games\n", policy, len(ds), games)
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
	fmt.Fprintf(&b, "  kinds: priority %d attackers %d blockers %d target %d; overrides %d (%.1f%%); per game %.1f searched, %.2f overrides\n",
		kinds["priority"], kinds["attackers"], kinds["blockers"], kinds["target"], over, 100*float64(over)/n, n/float64(max(games, 1)), float64(over)/float64(max(games, 1)))
	{
		kov := map[string]int{}
		kms := map[string]float64{}
		for _, d := range ds {
			if d.Override {
				kov[d.Kind]++
			}
			kms[d.Kind] += d.MS
		}
		fmt.Fprintf(&b, "  per kind (overrides / mean ms):")
		for _, k := range []string{"priority", "attackers", "blockers", "target"} {
			if kinds[k] > 0 {
				fmt.Fprintf(&b, " %s %d/%.0f", k, kov[k], kms[k]/float64(kinds[k]))
			}
		}
		fmt.Fprintln(&b)
	}
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
