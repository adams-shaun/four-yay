package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Setup permanents are present before turn 1, but a permanent that genuinely
// enters during turn 1 (Bitterblossom's upkeep token) still entered this turn.
// The oracle driver must clear only the seeded setup provenance, not these.
func TestOracleSetupKeepsGenuineUpkeepEntry(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const scenario = `{"name":"upkeep-entry","setup":{"p0":{"battlefield":["Bitterblossom","Grizzly Bears"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`
	sc, err := decodeOracleScenario([]byte(scenario))
	if err != nil {
		t.Fatal(err)
	}
	sc.xmageFixture = true
	fails, transcript, run := runOracleScenario(reg, sc)
	if len(fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", fails, strings.Join(transcript, "\n"))
	}
	var sawToken, sawSetup int
	for _, id := range run.e.G.Zone(state.ZBattlefield, 0) {
		o := run.e.G.Obj(id)
		switch {
		case o.IsToken:
			sawToken++
			if !o.EnteredThisTurn {
				t.Errorf("upkeep token %s did not enter this turn", o.Face().Name)
			}
		default:
			sawSetup++
			if o.EnteredThisTurn {
				t.Errorf("setup permanent %s reads as entered this turn", o.Face().Name)
			}
		}
	}
	if sawToken != 1 || sawSetup != 2 {
		t.Fatalf("p0 battlefield: %d tokens, %d setup permanents; want 1 and 2\n%s", sawToken, sawSetup, strings.Join(transcript, "\n"))
	}
}
