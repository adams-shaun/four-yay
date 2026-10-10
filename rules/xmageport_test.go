package rules

// XMage interaction-scenario port (spec
// docs/superpowers/specs/2026-10-10-xmage-scenario-port-design.md).
//
// Every file under testdata/xmageport/<family>/*.json is a translation of one
// or more XMage Mage.Tests @Test methods (XMage is MIT-licensed; each file's
// "source" names the original class#method, see testdata/xmageport/NOTICE).
// The scenarios use the oracle-audit schema (oracleScenario) unchanged, so
// they run on runOracleScenario exactly like testdata/oracle and could also
// be replayed by the XMage oracle driver. Only card NAMES are written here;
// no Forge script text (the GPL boundary, AGENTS.md).
//
// A scenario that exposes a gorge engine bug keeps its XMage expectation and
// carries "known_bug": "<reason>". It is then skipped with that reason, and a
// sibling assertion makes the row stale-proof: if the scenario starts
// passing, the test fails and asks for the row to be removed.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

type xmagePortFile struct {
	Source    string              `json:"source"`
	Scenarios []xmagePortScenario `json:"scenarios"`
}

type xmagePortScenario struct {
	oracleScenario
	// XMage is the originating test method (Class#method).
	XMage string `json:"xmage"`
	// KnownBug, when set, names the gorge divergence this scenario exposes.
	KnownBug string `json:"known_bug,omitempty"`
}

func loadXMagePortFiles(t *testing.T) map[string]xmagePortFile {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "xmageport", "*", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no xmageport scenario files: %v", err)
	}
	out := map[string]xmagePortFile{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var f xmagePortFile
		if err := dec.Decode(&f); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if f.Source == "" || len(f.Scenarios) == 0 {
			t.Fatalf("%s: source and at least one scenario are required", p)
		}
		for _, sc := range f.Scenarios {
			if sc.XMage == "" || sc.Name == "" {
				t.Fatalf("%s: every scenario needs name and xmage", p)
			}
			for _, st := range sc.Steps {
				if !oracleOps[st.Op] {
					t.Fatalf("%s/%s: unknown op %q", p, sc.Name, st.Op)
				}
			}
		}
		out[p] = f
	}
	return out
}

func TestXMagePortScenarios(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	files := loadXMagePortFiles(t)
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fam := filepath.Base(filepath.Dir(p))
		slug := strings.TrimSuffix(filepath.Base(p), ".json")
		for _, sc := range files[p].Scenarios {
			sc := sc
			t.Run(fam+"/"+slug+"/"+sc.Name, func(t *testing.T) {
				t.Parallel()
				fails, transcript, run := runOracleScenario(reg, sc.oracleScenario)
				if run != nil && run.e != nil {
					if diff := diffGames(run.e.G, replayFromLog(t, run.cfg, run.e.L.Events)); diff != "" {
						t.Errorf("log-only replay differs:\n%s", diff)
					}
				}
				if sc.KnownBug != "" {
					if len(fails) == 0 {
						t.Fatalf("known_bug row is stale (scenario passes now; delete it): %s", sc.KnownBug)
					}
					t.Skipf("known gorge bug: %s\n  observed: %s", sc.KnownBug, strings.Join(fails, "\n  observed: "))
				}
				if len(fails) > 0 {
					t.Errorf("XMage %s CR %v\n  %s\n  FAIL: %s\n  transcript:\n    %s",
						sc.XMage, sc.CR, sc.Why, strings.Join(fails, "\n  FAIL: "), strings.Join(transcript, "\n    "))
				}
			})
		}
	}
}
