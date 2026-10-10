package rules

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Simultaneous departures keep every pre-event replacement (CR 614.6,
// 616.1, 603.10): Kalitas, Traitor of Ghet leaving in the SAME batch as the
// opponent's creatures still exiles them. One scenario per batch path the
// fix-masszone change wired: the effect destroy batch is
// xmageport's kalitas-damnation; these cover the SBA lethal-damage batch
// and the per-player sacrifice batch.
const masszoneScenarios = `[
 {"name": "kalitas-blasphemous-act-sba",
  "setup": {"p0": {"battlefield": ["Kalitas, Traitor of Ghet"], "hand": ["Blasphemous Act"]},
            "p1": {"battlefield": ["Bronze Sable", "Wall of Roots", "Sigiled Starfish"]}},
  "steps": [{"op": "cast", "seat": 0, "card": "p0:Blasphemous Act", "mana": "CCCCR"}, {"op": "resolve"}],
  "expect": [
   {"count": {"seat": 0, "zone": "graveyard", "name": "Kalitas, Traitor of Ghet"}, "eq": 1},
   {"count": {"seat": 1, "zone": "exile", "name": "Bronze Sable"}, "eq": 1},
   {"count": {"seat": 1, "zone": "exile", "name": "Wall of Roots"}, "eq": 1},
   {"count": {"seat": 1, "zone": "exile", "name": "Sigiled Starfish"}, "eq": 1},
   {"graveyard_size": {"p1": 0}}]},
 {"name": "kalitas-innocent-blood-sacrifice",
  "setup": {"p0": {"battlefield": ["Kalitas, Traitor of Ghet"], "hand": ["Innocent Blood"]},
            "p1": {"battlefield": ["Bronze Sable"]}},
  "steps": [{"op": "cast", "seat": 0, "card": "p0:Innocent Blood", "mana": "B"}, {"op": "resolve"}],
  "expect": [
   {"count": {"seat": 0, "zone": "graveyard", "name": "Kalitas, Traitor of Ghet"}, "eq": 1},
   {"count": {"seat": 1, "zone": "exile", "name": "Bronze Sable"}, "eq": 1},
   {"graveyard_size": {"p1": 0}}]}
]`

func TestSimultaneousDeparturesKeepReplacements(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	var scs []oracleScenario
	dec := json.NewDecoder(strings.NewReader(masszoneScenarios))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&scs); err != nil {
		t.Fatal(err)
	}
	for _, sc := range scs {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()
			fails, transcript, run := runOracleScenario(reg, sc)
			if run != nil && run.e != nil {
				if diff := diffGames(run.e.G, replayFromLog(t, run.cfg, run.e.L.Events)); diff != "" {
					t.Errorf("log-only replay differs:\n%s", diff)
				}
			}
			if len(fails) > 0 {
				t.Errorf("FAIL: %s\n  transcript:\n    %s", strings.Join(fails, "\n  FAIL: "), strings.Join(transcript, "\n    "))
			}
		})
	}
}
