package adopt

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/internal/testutil"
)

const root = "../.."

// census memoises Build for the process: five tests (TournamentScoping,
// CorpusImpact, TicketsAtHead, and the two ratchets) each used to re-pay the
// 589-manifest parse, the printed-list load and the corpus name fold, and
// the gate runs this package in the affected phase's $others wave. Build
// only reads the registry and the committed compliance data, and no method
// writes the returned Census, so one copy is safe for the parallel tests
// that share it. Measured 2026-10-09 at 4 vCPU: compliance/adopt 30.0s ->
// 17.7s wall with the memo plus the parallel corpus tests below.
var censusMemo struct {
	sync.Once
	cs  *Census
	err error
}

func census(t *testing.T) *Census {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	censusMemo.Do(func() { censusMemo.cs, censusMemo.err = Build(reg, root) })
	if censusMemo.err != nil {
		t.Fatal(censusMemo.err)
	}
	return censusMemo.cs
}

func TestConfigIncludes(t *testing.T) {
	cfg := &Config{NonTournamentSetTypes: []string{"JOKE_SET"}}
	m := compliance.Manifest{Code: "XYZ", SetType: "EXPANSION", Released: "2015-01-01"}
	for _, c := range []struct {
		f    Format
		want bool
	}{
		{Format{Sets: []string{"XYZ"}}, true},
		{Format{Sets: []string{"ABC"}}, false},
		{Format{From: "2012-10-05", SetTypes: []string{"EXPANSION"}}, true},
		{Format{From: "2016-01-01", SetTypes: []string{"EXPANSION"}}, false},
		{Format{From: "2012-10-05", SetTypes: []string{"CORE"}}, false},
		{Format{Tournament: true}, true},
	} {
		if got := cfg.Includes(c.f, m); got != c.want {
			t.Errorf("Includes(%+v) = %v, want %v", c.f, got, c.want)
		}
	}
	if cfg.Includes(Format{Tournament: true}, compliance.Manifest{SetType: "JOKE_SET"}) {
		t.Error("a joke set is in the tournament target")
	}
}

// TestTournamentScoping is section 11.3 C7: the All target is tournament
// Magic. A card printed only in joke sets, or one that needs a
// non-tournament primitive, is outside it; a reprinted staple is inside
// every format.
func TestTournamentScoping(t *testing.T) {
	t.Parallel()
	cs := census(t)
	for name, why := range map[string]string{
		"Booster Tutor":       "primitive api:MakeCard", // also promoted in PAL04, so the set rule alone misses it
		"Blast from the Past": "printed only in non-tournament sets",
	} {
		c, ok := cs.Lookup(name)
		if !ok {
			t.Errorf("%s: not in the census", name)
			continue
		}
		if !strings.HasPrefix(c.NonTournament, why) {
			t.Errorf("%s: NonTournament %q, want %q", name, c.NonTournament, why)
		}
	}
	bolt, ok := cs.Lookup("Lightning Bolt")
	if !ok || bolt.NonTournament != "" {
		t.Fatalf("Lightning Bolt: %+v", bolt)
	}
	if !contains(bolt.Formats, "Modern") || !contains(bolt.Formats, "All") {
		t.Errorf("Lightning Bolt formats %v", bolt.Formats)
	}
	for _, s := range cs.FormatSets("All") {
		if !cs.Config.TournamentSetType(cs.Sets[s].SetType) {
			t.Errorf("All covers %s (%s)", s, cs.Sets[s].SetType)
		}
	}
}

// TestFormatSetsHavePrintedLists is the honest-claim half of C7 for the
// first target: every set of a format that lists its sets explicitly has
// a printed list, so none of them can fall back to the XMage manifest
// (which the gate now refuses).
func TestFormatSetsHavePrintedLists(t *testing.T) {
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range cfg.Formats {
		for _, s := range f.Sets {
			if _, err := compliance.LoadPrinted(filepath.Join(root, "compliance", "printed"), s); err != nil {
				t.Errorf("%s set %s: no printed list (%v)", f.Name, s, err)
			}
		}
	}
}
