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
// NEW rows go in the card's own file under oracleDivergentDir
// (testdata/oracle/<family>/known-divergent/<slug>.json), not here and not in
// a family aggregate: every audit ticket appending to one shared file made
// each merge conflict with every other in-flight audit branch editing it, and
// each conflict cost a merge-fix round plus a full re-gate. The three sources
// (this map, the family aggregate, the per-card files) are merged by
// oracleDivergent; a key in two of them, or a row naming a scenario of another
// family, fails the build. Rows here are legacy and may be moved into the
// per-card files.
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

// oracleDivergentFile is a family directory's legacy ratchet aggregate: one
// flat JSON object, "<card>/<scenario>" -> the one-line observed-vs-expected
// reason. It is not a scenario file; loadOracleFiles skips it.
//
// NEW rows never go here. Every audit ticket appending to a family aggregate
// made each merge conflict with every other in-flight audit branch editing
// that family (fdn-fix5 and fdn-fix6 both deleted rows from
// activated-ability/known-divergent.json in 2026-10-02); the aggregate is
// legacy and may only shrink. New rows go in the card's own file under
// oracleDivergentDir, so two audit tickets touch two files.
const oracleDivergentFile = "known-divergent.json"

// oracleDivergentDir is the per-card home for ratchet rows:
// testdata/oracle/<family>/known-divergent/<slug>.json, one file per card,
// {"card": "<Card>", "rows": {"<scenario>": "<reason>"}}. Splitting the
// family aggregate by card is the same move that took this ratchet from the
// single oracleKnownDivergent map to per-family files: the collision always
// follows the coarsest key a family shares, and two audits of different cards
// in one family then collide on the family file.
const oracleDivergentDir = "known-divergent"

// oracleDivergentPerCard is the schema of one file under oracleDivergentDir.
type oracleDivergentPerCard struct {
	Card string            `json:"card"`
	Rows map[string]string `json:"rows"`
}

// oracleDivergentRow is one ratchet row and where it came from: the family of
// the file that holds it, or "" for the legacy oracleKnownDivergent map, plus
// the source path for error messages.
type oracleDivergentRow struct {
	reason, family, src string
}

// oracleDivergent merges the legacy oracleKnownDivergent map, every family's
// oracleDivergentFile aggregate, and every card's oracleDivergentDir file. It
// needs no corpus, so the scenario-schema test holds the files to their shape
// where the author works.
func oracleDivergent(t *testing.T) map[string]oracleDivergentRow {
	t.Helper()
	out := make(map[string]oracleDivergentRow, len(oracleKnownDivergent))
	for k, v := range oracleKnownDivergent {
		out[k] = oracleDivergentRow{reason: v, src: "oracleKnownDivergent"}
	}
	add := func(p, k, reason, fam string) {
		if prev, dup := out[k]; dup {
			t.Errorf("%s: row %q is also listed in %s", p, k, prev.src)
			return
		}
		out[k] = oracleDivergentRow{reason: reason, family: fam, src: p}
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
			add(p, k, v, fam)
		}
	}
	cards, err := filepath.Glob(filepath.Join("testdata", "oracle", "*", oracleDivergentDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	cardFile := map[string]string{}
	for _, p := range cards {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var pc oracleDivergentPerCard
		if err := json.Unmarshal(raw, &pc); err != nil {
			t.Fatalf("%s: want {\"card\": \"<Card>\", \"rows\": {\"<scenario>\": \"<reason>\"}}: %v", p, err)
		}
		fam := filepath.Base(filepath.Dir(filepath.Dir(p)))
		if strings.TrimSpace(pc.Card) == "" {
			t.Errorf("%s: card is required", p)
			continue
		}
		if prev, dup := cardFile[fam+"/"+pc.Card]; dup {
			t.Errorf("%s: card %q already has a divergent file at %s", p, pc.Card, prev)
			continue
		}
		cardFile[fam+"/"+pc.Card] = p
		for sc, reason := range pc.Rows {
			if strings.TrimSpace(sc) == "" || strings.TrimSpace(reason) == "" {
				t.Errorf("%s: row %q needs a non-empty scenario and reason", p, sc)
				continue
			}
			add(p, pc.Card+"/"+sc, reason, fam)
		}
	}
	return out
}

// oracleUnconsumedFile is the shrinking ratchet for step `answers` that no
// decision matched: one flat JSON object, "<card>/<scenario>" -> the
// one-line leftover observation (the step index, op and answer that no
// decision consumed). It is a top-level file, not a family directory, and
// loadOracleFiles's */*.json glob does not match it.
//
// A listed scenario must still leave an answer unconsumed; a fixed one is
// stale and fails the build until its row is deleted. An unlisted scenario
// that leaves one fails loudly. Rows are only ever deleted, never added by
// the scenario author.
const oracleUnconsumedFile = "known-unconsumed-answers.json"

// oracleUnconsumed reads the leftover-answer ratchet. It needs no corpus, so
// TestOracleScenarioFilesWellFormed holds it to its shape where the author
// works. A malformed row fails the build.
func oracleUnconsumed(t *testing.T) map[string]string {
	t.Helper()
	p := filepath.Join("testdata", "oracle", oracleUnconsumedFile)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	var rows map[string]string
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("%s: want one JSON object of \"<card>/<scenario>\": \"reason\" rows: %v", p, err)
	}
	for k, v := range rows {
		if !strings.Contains(k, "/") || strings.TrimSpace(v) == "" {
			t.Errorf("%s: row %q needs a \"<card>/<scenario>\" key and a non-empty reason", p, k)
		}
	}
	return rows
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
			t.Errorf("ratchet row %q is in %s but the scenario is family %q", key, row.src, fam)
		}
	}
	for key := range oracleUnconsumed(t) {
		if _, ok := scenarioFamily[key]; !ok {
			t.Errorf("%s row %q names no scenario", oracleUnconsumedFile, key)
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
	seenUnconsumed := map[string]bool{}
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
				uncReason, isUnc := unconsumed[key]
				seenUnconsumed[key] = true
				if isUnc && len(leftovers) == 0 {
					t.Errorf("stale unconsumed-answer row (the scenario now consumes every step answer; delete its row from %s): %s", oracleUnconsumedFile, uncReason)
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
	for key, row := range divergent {
		if !seen[key] {
			where := row.src
			if where == "" {
				where = "oracleKnownDivergent"
			}
			t.Errorf("%s row %q names no scenario", where, key)
		}
	}
	for key := range unconsumed {
		if !seenUnconsumed[key] {
			t.Errorf("%s row %q names no scenario", oracleUnconsumedFile, key)
		}
	}
}
