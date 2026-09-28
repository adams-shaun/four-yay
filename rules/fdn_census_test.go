package rules

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestFDNParamCensusReport runs the parameter census (paramcensus_test.go)
// over the SpellBench FDN Limited card universe
// (internal/spellbench/fdn-cards.tsv) instead of the repo decks: the FDN
// cards whose registered primitives leave a parameter unread or whose cost
// carries an unmodelled token, weighted by 17lands wr60 deck copies. A
// report only -- it never fails on a finding; the FDN backlog tickets
// (docs/superpowers/reports/2026-09-28-spellbench-fdn-limited.md) own the
// rows.
func TestFDNParamCensusReport(t *testing.T) {
	_, d := measureParamCensus(t, nil)
	reg := testutil.CorpusRegistry(t)
	raw, err := os.ReadFile("../internal/spellbench/fdn-cards.tsv")
	if err != nil {
		t.Skipf("no FDN universe: %v", err)
	}
	type row struct {
		name   string
		w      int
		labels []string
	}
	var rows []row
	byLabel := map[string]int{}
	cardsBy := map[string][]string{}
	total, flaggedW := 0, 0
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 6 || strings.HasPrefix(line, "#") || f[0] == "name" {
			continue
		}
		w, _ := strconv.Atoi(f[3])
		total += w
		c, ok := reg.Lookup(f[0])
		if !ok {
			t.Errorf("%q not in the corpus", f[0])
			continue
		}
		ls := cardCensusLabels(c, d, nil)
		if len(ls) == 0 {
			continue
		}
		flaggedW += w
		rows = append(rows, row{f[0], w, ls})
		for _, l := range ls {
			byLabel[l] += w
			cardsBy[l] = append(cardsBy[l], f[0])
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].w != rows[j].w {
			return rows[i].w > rows[j].w
		}
		return rows[i].name < rows[j].name
	})
	t.Logf("FDN param census: %d cards carry an unread param or unmodelled cost token; %.1f%% of wr60 deck copies", len(rows), 100*float64(flaggedW)/float64(max(total, 1)))
	for _, r := range rows {
		t.Logf("  card %-36s wr60 %5d  %v", r.name, r.w, r.labels)
	}
	var labels []string
	for l := range byLabel {
		labels = append(labels, l)
	}
	sort.Slice(labels, func(i, j int) bool {
		if byLabel[labels[i]] != byLabel[labels[j]] {
			return byLabel[labels[i]] > byLabel[labels[j]]
		}
		return labels[i] < labels[j]
	})
	for _, l := range labels {
		t.Logf("  label %-50s wr60 %5d  %v", l, byLabel[l], cardsBy[l])
	}
}
