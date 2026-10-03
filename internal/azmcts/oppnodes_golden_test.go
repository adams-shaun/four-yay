package azmcts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// oppGoldenPath is the Results golden that pins Options.OpponentNodes'
// default: captured at the commit BEFORE opponent nodes existed, so a
// search with the switch off is byte-identical to the search as it was.
// Regenerate (only for a deliberate search change, or a corpus pin move)
// with AZ_OPPNODES_GOLDEN_WRITE=1.
const oppGoldenPath = "testdata/oppnodes_off_golden.json"

// oppGoldenCase is one pinned Search: its readable choice and visits, and
// the sha256 of the whole Result's JSON (keys, labels, candidates, visits,
// avail, prior, Q, root value, choice, intent and every Stats counter, the
// cost counters included).
type oppGoldenCase struct {
	Name   string `json:"name"`
	Choice int    `json:"choice"`
	Visits []int  `json:"visits"`
	SHA    string `json:"sha256"`
}

// goldenHeuristicLeaf is a caller-supplied leaf (Options.Leaf) equal to the
// frozen heuristic, so the golden covers the LeafFunc path mzplay takes.
func goldenHeuristicLeaf(e *rules.Engine, actor state.PlayerID) float64 {
	return heuristicLeafValue(e, actor)
}

// oppStatsJSON is the opponent-node counters as a switched-off search's
// Result encodes them (Stats field order), which the golden, captured
// before they existed, does not hold.
var oppStatsJSON = []byte(`,"OppPoints":0,"OppExpanded":0`)

// oppGoldenRun runs every pinned configuration over a fixed root list.
func oppGoldenRun(t *testing.T) []oppGoldenCase {
	t.Helper()
	roots := measureRoots(t, 8)
	e, d, land := landPlayRoot(t)
	roots = append(roots, measureRoot{name: "land-play", e: e, d: d, bot: land})
	type config struct {
		name   string
		source string
		set    func(o *Options)
		bench  bool // the full root with land macros (the benchmark / mzplay root)
	}
	configs := []config{
		{"default-clairvoyant", "clairvoyant", func(o *Options) {}, false},
		{"default-pimc-nocache", "pimc", func(o *Options) { o.NodeCache = 0; o.Noise = true }, false},
		{"bench-clairvoyant-action", "clairvoyant", func(o *Options) {
			o.CPUCT, o.AbsoluteUnvisitedQ, o.UnvisitedQ = 0.5, true, 0.5
			o.Limit, o.AutoPayment, o.UniformPrior, o.NameKeys = BenchCandidateLimit, true, true, true
			o.Discount, o.DiscountUnit = 0.99, DiscountAction
			o.Leaf = goldenHeuristicLeaf
		}, true},
		{"bench-pimc-ply-nocache", "pimc", func(o *Options) {
			o.CPUCT, o.AbsoluteUnvisitedQ, o.UnvisitedQ = 0.5, true, 0.5
			o.Limit, o.AutoPayment, o.UniformPrior, o.NameKeys = BenchCandidateLimit, true, true, true
			o.Discount, o.DiscountUnit = 0.97, DiscountPly
			o.NodeCache = 0
		}, true},
	}
	var out []oppGoldenCase
	for i, r := range roots {
		for _, c := range configs {
			opts := DefaultOptions()
			opts.Sims, opts.Seed = 120, uint64(i)*13+5
			c.set(&opts)
			obs := searchprobe.NewCollector(r.d.Player)
			root := Root{Engine: r.e, Decision: r.d, Bot: r.bot, Observer: obs}
			if c.bench {
				root.Macros, root.BotKey = landMacros(r.d, r.bot)
			}
			res, err := Search(context.Background(), root, measureSource(c.source, r.e, obs), nil, opts)
			if err != nil {
				t.Fatalf("%s %s: %v", r.name, c.name, err)
			}
			raw, err := json.Marshal(res)
			if err != nil {
				t.Fatalf("%s %s: %v", r.name, c.name, err)
			}
			// The golden predates the opponent-node counters: with the
			// switch off they must be zero, and the Result's encoding
			// without them must be the one captured.
			if n := bytes.Count(raw, oppStatsJSON); n != 1 {
				t.Fatalf("%s %s: the opponent-node counters are not both zero exactly once (%d) in %s", r.name, c.name, n, raw)
			}
			raw = bytes.Replace(raw, oppStatsJSON, nil, 1)
			sum := sha256.Sum256(raw)
			out = append(out, oppGoldenCase{
				Name: fmt.Sprintf("%s|%s", r.name, c.name), Choice: res.Choice,
				Visits: res.Visits, SHA: hex.EncodeToString(sum[:]),
			})
		}
	}
	return out
}

// Test 5 of the opponent-node design: with Options.OpponentNodes off (the
// default) every Search is byte-identical to the search before opponent
// nodes existed.
func TestOpponentNodesOffIsByteIdentical(t *testing.T) {
	if testing.Short() {
		t.Skip("the golden runs 36 searches")
	}
	got := oppGoldenRun(t)
	if os.Getenv("AZ_OPPNODES_GOLDEN_WRITE") == "1" {
		raw, err := json.MarshalIndent(got, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(oppGoldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(oppGoldenPath, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d cases to %s", len(got), oppGoldenPath)
		return
	}
	raw, err := os.ReadFile(oppGoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var want []oppGoldenCase
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("%d cases, golden has %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("case %d differs from the golden:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}
