package rules

// Inline Oracle-harness regressions: each test is a scenario in the
// rules/testdata/oracle schema, run through runOracleScenario against the
// real corpus and then replayed from its log alone.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// runInlineOracle runs one JSON oracleScenario and fails the test on any
// mismatch or on a log-only replay divergence. It returns the run so a caller
// can assert beyond the scenario's own expectations.
func runInlineOracle(t *testing.T, scenario string) *oracleRun {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	var sc oracleScenario
	if err := json.Unmarshal([]byte(scenario), &sc); err != nil {
		t.Fatalf("scenario JSON: %v", err)
	}
	fails, transcript, run := runOracleScenario(reg, sc)
	if os.Getenv("ORACLE_AUDIT_TRACE") != "" {
		t.Logf("transcript:\n    %s", strings.Join(transcript, "\n    "))
	}
	if len(fails) > 0 {
		t.Fatalf("%s:\n  %s\n  transcript:\n    %s", sc.Name, strings.Join(fails, "\n  "), strings.Join(transcript, "\n    "))
	}
	if run.e != nil {
		if diff := diffGames(run.e.G, replayFromLog(t, run.cfg, run.e.L.Events)); diff != "" {
			t.Fatalf("log-only replay differs:\n%s", diff)
		}
	}
	return run
}

// TestMayPlayChosenCardGrantSurvivesClearChosen: an Effect-delivered
// may-play grant whose Affected$ is Card.ChosenCard (the ChooseCard ->
// Effect -> Cleanup ClearChosenCard$ chain: Strongbox Raider, Chandra,
// Flameshaper, Feldon, End-Blaze Epiphany, Party Thrasher, Case of the
// Burning Masks, Jaya, Fiery Negotiator) names the card chosen when the
// effect was created, after the cleanup wiped the source's chosen list.
// Feldon is the class sibling the Oracle audit does not cover: Shock deals
// it two damage, two cards are exiled, the chosen Bolt is castable from
// exile and the unchosen Forest is not playable.
func TestMayPlayChosenCardGrantSurvivesClearChosen(t *testing.T) {
	t.Parallel()
	runInlineOracle(t, `{
	  "name": "feldon-chosen-card-playable",
	  "setup": {"p0": {"hand": ["Shock"], "battlefield": ["Feldon, Ronom Excavator"],
	                   "library_top": ["Lightning Bolt", "Forest"]}},
	  "steps": [
	    {"op": "cast", "seat": 0, "card": "p0:Shock", "mana": "R", "targets": ["p0:Feldon, Ronom Excavator"]},
	    {"op": "resolve", "answers": [{"kind": "choose", "pick": ["p0:Lightning Bolt"]}],
	     "expect": [{"card": "p0:Lightning Bolt", "zone": "exile"}, {"card": "p0:Forest", "zone": "exile"}]},
	    {"op": "mana", "seat": 0, "mana": "R",
	     "expect": [{"offered": {"seat": 0, "kind": "cast", "card": "p0:Lightning Bolt"}, "want": true},
	                {"offered": {"seat": 0, "kind": "play", "card": "p0:Forest"}, "want": false}]},
	    {"op": "cast", "seat": 0, "card": "p0:Lightning Bolt", "targets": ["p1"]},
	    {"op": "resolve"}
	  ],
	  "expect": [{"life": {"p1": 17}}, {"card": "p0:Lightning Bolt", "zone": "graveyard"},
	             {"card": "p0:Forest", "zone": "exile"}]
	}`)
}
