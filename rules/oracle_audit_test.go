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

// oracleKnownDivergent is the audit's ratchet (the knownUnsupported pattern
// of acceptance_test.go): "<card>/<scenario>" -> the observed divergence. A
// listed scenario must still FAIL (a fixed one is stale and fails the build
// until its row is deleted); an unlisted failure fails the build. Rows are
// only ever added by the triage step, never by the scenario author.
//
// NEW rows go in the family's own oracleDivergentFile
// (testdata/oracle/<family>/known-divergent.json), not here: every audit
// ticket appending to this one map made each merge conflict with every other
// in-flight audit branch, and each conflict cost a merge-fix round plus a full
// re-gate. The two sources are merged by oracleDivergent; a key in both, or a
// file row naming a scenario of another family, fails the build. Rows here are
// legacy and may be moved into the family files.
var oracleKnownDivergent = map[string]string{
	// (The Purphoros and Mogis rows retired when rules/layers.go's type-static
	// emission read stat:Continuous RemoveType$ -- the devotion gods' "isn't
	// a creature" gate, pinned in rules/remove_type_static_test.go; the
	// paramcensus rows retired with it.)
	// Engine bug (CR 603.10a): a granted "whenever a creature you control
	// dies" trigger is checked AFTER a non-SBA departure, so the departure
	// that ends the grant's IsPresent$ condition loses its own trigger. The
	// SBA path (only-cleric-dies-to-damage-looks-back) uses the pre-batch
	// snapshot and passes.
	// Suspected script translation: Evendo's conditional may-play static uses
	// an exiled-with-source affected zone plus a turn/SVar gate; the Bolt is
	// exiled by its sacrifice trigger but is not offered from exile.
	"Evendo Brushrazer/sacrifice-this-turn-allows-exiled-card-play": "Lightning Bolt was exiled, but casting it from exile was not offered during the turn (conditional may-play static)",
	// Suspected script translation: the Knight graveyard permission is a
	// conditional continuous may-play static; Haakon is present, but the
	// Knight in its controller's graveyard is not offered.
	"Haakon, Stromgald Scourge/haakon-on-battlefield-permits-knight-from-graveyard": "Knight of the Ebon Legion from the graveyard was not offered while Haakon was on the battlefield",
	// Script translation: the once-per-turn permission is tracked per
	// affected spell, so Darksteel Monolith's free-cast grant is available again.
	"Darksteel Monolith/once-each-turn-second-colorless-pays": "cast p0:Runed Servitor offered=true, want false",
	// (Giada row retired: main's 6f256c81e/b250fa68c excluded the entering
	// permanent from replacement counts, so the scenario now passes.)
	// Engine/script gap: max-speed-gated AddAbility is not offered after the
	// three turn-specific speed increases (CR 702.179).
	"Amonkhet Raceway/max-speed-after-opponent-loses-life-on-three-turns": "max-speed haste activation is not offered after reaching speed four",
	// Script translation: Brotherhood Scribe's CounterAddedOnce trigger does
	// not produce the printed team-wide +1/+1 bonus after its energy ability.
	"Brotherhood Scribe/metalcraft-three-artifacts-gives-energy": "observed Scribe 1/3 and Lions 2/1, expected 2/4 and 3/2 after energy",
	// Script translation: Urza's Workshop's conditional Urza-land count is
	// not reflected in its mana ability; the three-land board produces one C.
	"Urza's Workshop/metalcraft-three-artifacts-three-urza-lands": "observed C, expected CCC for three Urza's lands",
	// Engine filter gap: Captain Marvel's script uses Creature...+nonKree,
	// which the trigger matcher fails closed on (also noted in acceptance_test.go).
	"Captain Marvel, Apex Avenger/non-kree-creature-counter-is-copied": "Experiment One gets a counter, but Captain Marvel stays 4/4 with none (nonKree filter fails closed)",
	// Engine trigger-chain gap: Earthbender's script chains ImmediateTrigger
	// with ConditionCheckSVar$ and ConditionPresent$; the mixed condition shape
	// is unresolved in effects/conditions.go, and the fourth-counter follow-up is lost.
	"Earthbender Ascension/fourth-landfall-reaches-quest-threshold": "4 quest counters reached, but target gets no +1/+1 counter or Trample (reflexive trigger chain lost)",
}

// oracleDivergentFile is a family directory's ratchet rows: one flat JSON
// object, "<card>/<scenario>" -> the one-line observed-vs-expected reason.
// It is not a scenario file; loadOracleFiles skips it.
const oracleDivergentFile = "known-divergent.json"

// oracleDivergentRow is one ratchet row and where it came from: the family of
// the file that holds it, or "" for the legacy oracleKnownDivergent map.
type oracleDivergentRow struct {
	reason, family string
}

// oracleDivergent merges the legacy oracleKnownDivergent map with every
// family's oracleDivergentFile. It needs no corpus, so the scenario-schema
// test holds the files to their shape where the author works.
func oracleDivergent(t *testing.T) map[string]oracleDivergentRow {
	t.Helper()
	out := make(map[string]oracleDivergentRow, len(oracleKnownDivergent))
	for k, v := range oracleKnownDivergent {
		out[k] = oracleDivergentRow{reason: v}
	}
	paths, err := filepath.Glob(filepath.Join("testdata", "oracle", "*", oracleDivergentFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var rows map[string]string
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatalf("%s: want one JSON object of \"<card>/<scenario>\": \"reason\" rows: %v", p, err)
		}
		fam := filepath.Base(filepath.Dir(p))
		for k, v := range rows {
			if !strings.Contains(k, "/") || strings.TrimSpace(v) == "" {
				t.Errorf("%s: row %q needs a \"<card>/<scenario>\" key and a non-empty reason", p, k)
				continue
			}
			if prev, dup := out[k]; dup {
				where := "oracleKnownDivergent"
				if prev.family != "" {
					where = filepath.Join(prev.family, oracleDivergentFile)
				}
				t.Errorf("%s: row %q is also listed in %s", p, k, where)
				continue
			}
			out[k] = oracleDivergentRow{reason: v, family: fam}
		}
	}
	return out
}

func loadOracleFiles(t *testing.T) map[string]oracleFile {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "oracle", "*", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no oracle scenario files: %v", err)
	}
	out := map[string]oracleFile{}
	for _, p := range paths {
		if filepath.Base(p) == oracleDivergentFile {
			continue
		}
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
		case row.family != "" && row.family != fam:
			t.Errorf("ratchet row %q is in %s/%s but the scenario is family %q", key, row.family, oracleDivergentFile, fam)
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
				if isKnown && row.family != "" && row.family != f.Family {
					t.Errorf("ratchet row for %s is in %s/%s but the scenario is family %q", key, row.family, oracleDivergentFile, f.Family)
				}
				if os.Getenv("ORACLE_AUDIT_TRACE") != "" {
					t.Logf("transcript:\n    %s", strings.Join(transcript, "\n    "))
				}
				switch {
				case len(fails) == 0 && isKnown:
					t.Errorf("stale known divergence (the scenario now passes; delete its row from %s/%s or oracleKnownDivergent): %s", f.Family, oracleDivergentFile, known)
				case len(fails) > 0 && isKnown:
					t.Logf("known divergence: %s\n  observed: %s", known, strings.Join(fails, "\n  observed: "))
				case len(fails) > 0:
					t.Errorf("%s [%s] CR %v\n  Oracle-derived: %s\n  FAIL: %s\n  transcript:\n    %s",
						f.Card, f.Family, sc.CR, sc.Why, strings.Join(fails, "\n  FAIL: "), strings.Join(transcript, "\n    "))
				}
			})
		}
	}
	for key, row := range divergent {
		if !seen[key] {
			where := "oracleKnownDivergent"
			if row.family != "" {
				where = filepath.Join(row.family, oracleDivergentFile)
			}
			t.Errorf("%s row %q names no scenario", where, key)
		}
	}
}
