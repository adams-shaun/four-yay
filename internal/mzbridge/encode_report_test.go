package mzbridge_test

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/mzbridge"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

// TestEncodeFDNReport is a report, not a gate: it plays FDN pool decks bot
// against bot, encodes every priority decision for the deciding seat with
// the opponent's hand hidden, and writes the id-set sizes, the feature
// names that account for the most ids, and the cost of one Encode. Off
// unless MZBRIDGE_FDN_REPORT names the file to write; MZBRIDGE_FDN_GAMES is
// the number of games (default 5).
func TestEncodeFDNReport(t *testing.T) {
	out := os.Getenv("MZBRIDGE_FDN_REPORT")
	if out == "" {
		t.Skip("set MZBRIDGE_FDN_REPORT to write the FDN encoder report")
	}
	games := 5
	if v := os.Getenv("MZBRIDGE_FDN_GAMES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			t.Fatalf("MZBRIDGE_FDN_GAMES %q", v)
		}
		games = n
	}
	reg := testutil.CorpusRegistry(t)
	cat, err := spellbench.CatalogByID("fdn")
	if err != nil {
		t.Fatal(err)
	}

	enc := mzbridge.NewEncoder()
	fs := mzbridge.NewFeatureSet()
	dbg := mzbridge.NewFeatureSet()
	dbg.SetDebug(true)
	var sizes []int
	byName := map[string]int{}   // ids per bare feature name, over all states
	byDepth := map[int]int{}     // ids per tree depth
	inStates := map[string]int{} // states a bare name appears in
	var samples []*rules.Engine
	var b strings.Builder
	turns := 0
	for g := 0; g < games; g++ {
		deckA, err := spellbench.Deck(reg, cat.Dir, cat.Pool[(2*g)%len(cat.Pool)])
		if err != nil {
			t.Fatal(err)
		}
		deckB, err := spellbench.Deck(reg, cat.Dir, cat.Pool[(2*g+1)%len(cat.Pool)])
		if err != nil {
			t.Fatal(err)
		}
		seed := uint64(20261002 + g)
		seats := []seat.Seat{seat.NewBot(seed ^ 1), seat.NewBot(seed ^ 2)}
		cfg := rules.Config{Seed: seed, Names: []string{"p0", "p1"}, Decks: [][]*cards.Card{deckA, deckB},
			Tokens: reg.Tokens, NameUniverse: reg.Cards}
		n := 0
		hook := func(e *rules.Engine, seatIdx int, d *decision.Decision, in decision.Intent) (bool, error) {
			if d == nil || d.Kind != decision.KPriority {
				return false, nil
			}
			who := state.PlayerID(seatIdx)
			if err := enc.Encode(e, who, d, mzbridge.Priority, "priority", fs, mzbridge.EncodeOptions{}); err != nil {
				return false, err
			}
			ids := fs.IDs()
			sizes = append(sizes, len(ids))
			if err := enc.Encode(e, who, d, mzbridge.Priority, "priority", dbg, mzbridge.EncodeOptions{}); err != nil {
				return false, err
			}
			if got := dbg.IDs(); !slices.Equal(got, ids) {
				return false, fmt.Errorf("debug and plain encodings differ: %d vs %d ids", len(got), len(ids))
			}
			seen := map[string]bool{}
			for _, id := range ids {
				paths := dbg.Names(id)
				if len(paths) == 0 {
					continue
				}
				p := paths[0]
				byDepth[strings.Count(p, "/")]++
				name := bareName(p)
				byName[name]++
				if !seen[name] {
					seen[name] = true
					inStates[name]++
				}
			}
			if n%7 == 0 && len(samples) < 400 {
				samples = append(samples, e.Clone())
			}
			n++
			return false, nil
		}
		oc, e, err := gbench.PlayGame(cfg, seats, 200, 20000, gbench.Hooks{Submit: hook})
		if err != nil || e == nil {
			t.Fatalf("game %d: %v", g, err)
		}
		turns += int(e.Game().Turn)
		fmt.Fprintf(&b, "game %d seed %d: %d priority decisions, %d turns, outcome %+v\n", g, seed, n, e.Game().Turn, oc)
	}
	if len(sizes) == 0 {
		t.Fatal("no priority decision was encoded")
	}

	sorted := slices.Clone(sizes)
	slices.Sort(sorted)
	total := 0
	for _, s := range sizes {
		total += s
	}
	fmt.Fprintf(&b, "\nstates %d  ids per state: mean %.1f  min %d  p10 %d  median %d  p90 %d  max %d\n",
		len(sizes), float64(total)/float64(len(sizes)), sorted[0], sorted[len(sorted)/10], sorted[len(sorted)/2],
		sorted[len(sorted)*9/10], sorted[len(sorted)-1])

	depths := make([]int, 0, len(byDepth))
	for d := range byDepth {
		depths = append(depths, d)
	}
	sort.Ints(depths)
	fmt.Fprintf(&b, "\nmean ids per state by tree depth (1 = root):\n")
	for _, d := range depths {
		fmt.Fprintf(&b, "  depth %d: %.1f\n", d, float64(byDepth[d])/float64(len(sizes)))
	}

	type row struct {
		name string
		n    int
	}
	rows := make([]row, 0, len(byName))
	for name, n := range byName {
		rows = append(rows, row{name, n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].name < rows[j].name
	})
	fmt.Fprintf(&b, "\ndistinct feature names %d; the 30 that account for the most ids\n(mean ids per state, share of states the name appears in):\n", len(rows))
	for i := 0; i < 30 && i < len(rows); i++ {
		fmt.Fprintf(&b, "  %7.1f  %5.1f%%  %s\n", float64(rows[i].n)/float64(len(sizes)),
			100*float64(inStates[rows[i].name])/float64(len(sizes)), rows[i].name)
	}

	// One Encode's cost, on a sample of the states above, with the reused
	// encoder and feature set a self-play worker would hold.
	for _, e := range samples {
		if e.Pending() == nil {
			t.Fatal("a sampled engine has no pending decision")
		}
	}
	res := testing.Benchmark(func(tb *testing.B) {
		tb.ReportAllocs()
		for i := 0; i < tb.N; i++ {
			e := samples[i%len(samples)]
			d := e.Pending()
			if err := enc.Encode(e, d.Player, d, mzbridge.Priority, "priority", fs, mzbridge.EncodeOptions{}); err != nil {
				tb.Fatal(err)
			}
			_ = fs.IDs()
		}
	})
	fmt.Fprintf(&b, "\nEncode + IDs over %d sampled states: %d ns/op, %d B/op, %d allocs/op (N=%d)\n",
		len(samples), res.NsPerOp(), res.AllocedBytesPerOp(), res.AllocsPerOp(), res.N)

	// The same with a fresh feature set per state: FeatureSet.Refresh walks
	// every node the set has ever created (as upstream's stateRefresh does),
	// so a long-lived set pays for every card it has seen.
	fresh := testing.Benchmark(func(tb *testing.B) {
		tb.ReportAllocs()
		for i := 0; i < tb.N; i++ {
			e := samples[i%len(samples)]
			d := e.Pending()
			f := mzbridge.NewFeatureSet()
			if err := enc.Encode(e, d.Player, d, mzbridge.Priority, "priority", f, mzbridge.EncodeOptions{}); err != nil {
				tb.Fatal(err)
			}
			_ = f.IDs()
		}
	})
	fmt.Fprintf(&b, "same, a fresh FeatureSet per state: %d ns/op, %d B/op, %d allocs/op (N=%d)\n",
		fresh.NsPerOp(), fresh.AllocedBytesPerOp(), fresh.AllocsPerOp(), fresh.N)

	// One late state's full dump, beside the report (it names real cards
	// and their texts, so it stays out of the repository).
	if e := samples[len(samples)*3/4]; true {
		d := e.Pending()
		lines, err := mzbridge.DescribeState(e, d.Player, d, mzbridge.Priority, "priority", mzbridge.EncodeOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out+".dump.txt", []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("\n%s", b.String())
}

// bareName is a debug path's last component without its occurrence number
// and thermometer step: "root/Player#1/LifeTotal@7#1" -> "LifeTotal@".
func bareName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		path = path[i+1:]
	}
	if i := strings.LastIndexByte(path, '#'); i >= 0 {
		path = path[:i]
	}
	if i := strings.LastIndexByte(path, '@'); i >= 0 {
		if _, err := strconv.Atoi(path[i+1:]); err == nil {
			path = path[:i+1]
		}
	}
	return path
}
