package rules

// Oracle-text card audit (docs/superpowers/specs/2026-09-27-oracle-text-card-audit.md).
//
// Every scenario under testdata/oracle/<family>/<card>.json states what a
// card does according to its PRINTED (Oracle) text and the Comprehensive
// Rules, written by an author who never read the card's Forge script. The
// runner below builds a real two-seat game around real corpus cards, drives
// it only through the actions a player has (cast, activate, attack, block,
// pass priority, answer a decision), and asserts observable state: zones,
// life, P/T, keywords, counters, the stack, and what the engine offers. A
// failure therefore means the engine+script disagrees with the printed card,
// whichever side is wrong.
//
// The runner's only non-player actions are SETUP (placing named cards into
// zones and setting life totals before turn 1, as logged MoveZone and
// LifeChange events) and the "mana"/"move"/"life" ops, which stand in for
// an unspecified outside effect. All of them go through e.emit, so every
// scenario still replays from its log.
//
// Nothing here embeds card script or Oracle text: scenarios name cards and
// the corpus is read at run time (the GPL boundary, AGENTS.md).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards/oracletext"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// The audit's two ratchets live one file per card per family, never in a
// shared aggregate (spec 2026-10-03-rules-engine-lasagna-design.md W2): every
// audit ticket editing one shared table made each merge conflict with every
// other in-flight audit branch editing it, so two audits of two cards now
// touch two files.
//
//   - testdata/oracle/<family>/known-divergent/<slug>.json: a listed scenario
//     must still FAIL (a fixed one is stale and fails the build until its row
//     is deleted); an unlisted failure fails the build. Rows are only ever
//     added by the triage step, never by the scenario author.
//   - testdata/oracle/<family>/known-unconsumed/<slug>.json: a listed
//     scenario must still leave a step `answers` entry no decision matched
//     (the step index, op and answer); a fixed one is stale. Rows are only
//     ever deleted.
//
// Both share the schema {"card": "<Card>", "rows": {"<scenario>": "<reason>"}}.
// A row naming a scenario of another family, or a card with two files in one
// family, fails the build.
const (
	oracleDivergentDir  = "known-divergent"
	oracleUnconsumedDir = "known-unconsumed"
)

// oraclePerCard is the schema of one per-card ratchet file.
type oraclePerCard struct {
	Card string            `json:"card"`
	Rows map[string]string `json:"rows"`
}

// oracleRatchetRow is one ratchet row: the family of the file that holds it
// plus the source path for error messages.
type oracleRatchetRow struct {
	reason, family, src string
}

// oracleRatchet reads every testdata/oracle/<family>/<dir>/*.json file into
// "<card>/<scenario>" rows. It needs no corpus, so the scenario-schema test
// holds the files to their shape where the author works.
func oracleRatchet(t *testing.T, dir string) map[string]oracleRatchetRow {
	t.Helper()
	out := map[string]oracleRatchetRow{}
	paths, err := filepath.Glob(filepath.Join("testdata", "oracle", "*", dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	cardFile := map[string]string{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var pc oraclePerCard
		if err := dec.Decode(&pc); err != nil {
			t.Fatalf("%s: want {\"card\": \"<Card>\", \"rows\": {\"<scenario>\": \"<reason>\"}}: %v", p, err)
		}
		fam := filepath.Base(filepath.Dir(filepath.Dir(p)))
		if strings.TrimSpace(pc.Card) == "" || len(pc.Rows) == 0 {
			t.Errorf("%s: card and at least one row are required", p)
			continue
		}
		if prev, dup := cardFile[fam+"/"+pc.Card]; dup {
			t.Errorf("%s: card %q already has a %s file at %s", p, pc.Card, dir, prev)
			continue
		}
		cardFile[fam+"/"+pc.Card] = p
		for sc, reason := range pc.Rows {
			if strings.TrimSpace(sc) == "" || strings.TrimSpace(reason) == "" {
				t.Errorf("%s: row %q needs a non-empty scenario and reason", p, sc)
				continue
			}
			k := pc.Card + "/" + sc
			if prev, dup := out[k]; dup {
				t.Errorf("%s: row %q is also listed in %s", p, k, prev.src)
				continue
			}
			out[k] = oracleRatchetRow{reason: reason, family: fam, src: p}
		}
	}
	return out
}

// oracleDivergent reads the divergence ratchet.
func oracleDivergent(t *testing.T) map[string]oracleRatchetRow {
	t.Helper()
	return oracleRatchet(t, oracleDivergentDir)
}

// oracleUnconsumed reads the leftover-answer ratchet.
func oracleUnconsumed(t *testing.T) map[string]oracleRatchetRow {
	t.Helper()
	return oracleRatchet(t, oracleUnconsumedDir)
}

func loadOracleFiles(t *testing.T) map[string]oracleFile {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "oracle", "*", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no oracle scenario files: %v", err)
	}
	out := map[string]oracleFile{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var f oracleFile
		if err := dec.Decode(&f); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if f.Card == "" || f.Family == "" || len(f.Scenarios) == 0 {
			t.Fatalf("%s: card, family and at least one scenario are required", p)
		}
		if fam := filepath.Base(filepath.Dir(p)); fam != f.Family {
			t.Fatalf("%s: family %q does not match its directory %q", p, f.Family, fam)
		}
		out[p] = f
	}
	return out
}

// oracleOps is the closed step vocabulary; TestOracleScenarioFilesWellFormed
// holds every scenario file to it.
var oracleOps = map[string]bool{
	"mana": true, "cast": true, "activate": true, "play": true, "resolve": true,
	"attack": true, "block": true, "pass": true, "pass_to": true, "move": true, "life": true,
}

// TestOracleScenarioFilesWellFormed needs no corpus, so it runs where the
// scenario author works -- a worktree deliberately WITHOUT .cards, so the
// author cannot read a script or fit expectations to engine output. It checks
// the schema (unknown fields are rejected by the decoder), the family
// directory, unique scenario names, the step vocabulary, and ref syntax.
func TestOracleScenarioFilesWellFormed(t *testing.T) {
	t.Parallel()
	files := loadOracleFiles(t)
	// The ratchet files are checked here too (shape, duplicates, family), so
	// a triage edit gets feedback without the corpus.
	divergent := oracleDivergent(t)
	scenarioFamily := map[string]string{}
	for _, f := range files {
		for _, sc := range f.Scenarios {
			scenarioFamily[f.Card+"/"+sc.Name] = f.Family
		}
	}
	for key, row := range divergent {
		fam, ok := scenarioFamily[key]
		switch {
		case !ok:
			t.Errorf("ratchet row %q names no scenario", key)
		case row.family != fam:
			t.Errorf("ratchet row %q is in %s but the scenario is family %q", key, row.src, fam)
		}
	}
	for key, row := range oracleUnconsumed(t) {
		fam, ok := scenarioFamily[key]
		switch {
		case !ok:
			t.Errorf("%s row %q names no scenario", row.src, key)
		case row.family != fam:
			t.Errorf("unconsumed row %q is in %s but the scenario is family %q", key, row.src, fam)
		}
	}
	checkRef := func(where, ref string) {
		if ref == "" {
			return
		}
		if _, ok := parseSeatRef(ref); ok {
			return
		}
		if _, _, _, _, err := splitRef(ref); err != nil {
			t.Errorf("%s: %v", where, err)
		}
	}
	for p, f := range files {
		names := map[string]bool{}
		for _, sc := range f.Scenarios {
			where := p + ": " + sc.Name
			if sc.Name == "" || names[sc.Name] {
				t.Errorf("%s: scenario name empty or duplicated", where)
			}
			names[sc.Name] = true
			if sc.Why == "" || len(sc.CR) == 0 {
				t.Errorf("%s: every scenario states its Oracle/CR reasoning (why, cr)", where)
			}
			if len(sc.Expect) == 0 {
				hasStepExpect := false
				for _, st := range sc.Steps {
					hasStepExpect = hasStepExpect || len(st.Expect) > 0
				}
				if !hasStepExpect {
					t.Errorf("%s: asserts nothing", where)
				}
			}
			exps := append([]oracleExpect(nil), sc.Expect...)
			for i, st := range sc.Steps {
				if !oracleOps[st.Op] {
					t.Errorf("%s: step %d: unknown op %q", where, i, st.Op)
				}
				for _, ref := range append(append([]string{st.Card, st.Defender}, st.Targets...), st.Attackers...) {
					checkRef(fmt.Sprintf("%s: step %d", where, i), ref)
				}
				exps = append(exps, st.Expect...)
			}
			for _, x := range exps {
				checkRef(where, x.Card)
				checkRef(where, x.TriggerOnStack)
			}
		}
	}
}

// TestOracleAudit runs every Oracle-text scenario against the real corpus.
// Filter with -run 'TestOracleAudit/<Card>/<scenario>'.
func TestOracleAudit(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	files := loadOracleFiles(t)
	divergent := oracleDivergent(t)
	unconsumed := oracleUnconsumed(t)
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	seen := map[string]bool{}
	for _, p := range paths {
		f := files[p]
		c, ok := reg.Lookup(f.Card)
		if !ok {
			t.Errorf("%s: %q not in the corpus", p, f.Card)
			continue
		}
		if f.OracleSHA != "" && f.OracleSHA != oracletext.Digest(c) {
			t.Errorf("%s: Oracle text changed since the scenarios were written (oracle_sha %s, corpus %s): re-derive them", p, f.OracleSHA, oracletext.Digest(c))
		}
		for _, sc := range f.Scenarios {
			key := f.Card + "/" + sc.Name
			seen[key] = true
			t.Run(key, func(t *testing.T) {
				t.Parallel()
				fails, transcript, run := runOracleScenario(reg, sc)
				// Every scenario must also replay from its log alone: the setup
				// and the stand-in ops are logged events, so a divergence here
				// is an engine replay bug (or a runner write outside emit),
				// independent of the Oracle verdict and never ratcheted.
				if run.e != nil {
					if diff := diffGames(run.e.G, replayFromLog(t, run.cfg, run.e.L.Events)); diff != "" {
						t.Errorf("log-only replay differs:\n%s", diff)
					}
				}
				row, isKnown := divergent[key]
				known := row.reason
				if isKnown && row.family != f.Family {
					t.Errorf("ratchet row for %s is in %s but the scenario is family %q", key, row.src, f.Family)
				}
				if os.Getenv("ORACLE_AUDIT_TRACE") != "" {
					t.Logf("transcript:\n    %s", strings.Join(transcript, "\n    "))
				}
				// Split the fail list: a leftover step answer is excused only by
				// the shrinking unconsumed ratchet, and only the leftover itself.
				// Any other fail on the same scenario is still reported.
				var leftovers, otherFails []string
				for _, f := range fails {
					if strings.Contains(f, oracleUnconsumedMarker) {
						leftovers = append(leftovers, f)
					} else {
						otherFails = append(otherFails, f)
					}
				}
				unc, isUnc := unconsumed[key]
				uncReason := unc.reason
				if isUnc && len(leftovers) == 0 {
					t.Errorf("stale unconsumed-answer row (the scenario now consumes every step answer; delete its row from %s): %s", unc.src, uncReason)
				}
				if isUnc && len(leftovers) > 0 {
					t.Logf("known unconsumed answer: %s\n  observed: %s", uncReason, strings.Join(leftovers, "\n  observed: "))
				} else {
					otherFails = append(otherFails, leftovers...)
				}
				fails = otherFails
				switch {
				case len(fails) == 0 && isKnown:
					t.Errorf("stale known divergence (the scenario now passes; delete its row from %s): %s", row.src, known)
				case len(fails) > 0 && isKnown:
					t.Logf("known divergence: %s\n  observed: %s", known, strings.Join(fails, "\n  observed: "))
				case len(fails) > 0:
					t.Errorf("%s [%s] CR %v\n  Oracle-derived: %s\n  FAIL: %s\n  transcript:\n    %s",
						f.Card, f.Family, sc.CR, sc.Why, strings.Join(fails, "\n  FAIL: "), strings.Join(transcript, "\n    "))
				}
			})
		}
	}
	t.Cleanup(func() {
		for key, row := range divergent {
			if !seen[key] {
				t.Errorf("%s row %q names no scenario", row.src, key)
			}
		}
		for key, row := range unconsumed {
			if !seen[key] {
				t.Errorf("%s row %q names no scenario", row.src, key)
			}
		}
	})
}
