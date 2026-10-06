package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Generated and hand-authored scenarios both treat setup permanents as
// present before turn 1. Malamet Battle Glyph therefore does not grant its
// entry-this-turn counter to either setup creature.
func TestOracleSetupPermanentsDoNotCountAsEnteredThisTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const scenario = `{"name":"entry-history","setup":{"p0":{"battlefield":["Grizzly Bears"],"hand":["Malamet Battle Glyph"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Malamet Battle Glyph","mana":"G","targets":["p0:Grizzly Bears","p1:Grizzly Bears"]},{"op":"resolve","seat":0}]}`
	sc, err := decodeOracleScenario([]byte(scenario))
	if err != nil {
		t.Fatal(err)
	}
	sc.xmageFixture = true // a generated compliance scenario (RunOracleScenarioJSON)
	fails, transcript, run := runOracleScenario(reg, sc)
	if len(run.snaps) != 3 {
		t.Fatalf("snapshots: %d, want setup, cast, resolve", len(run.snaps))
	}
	for _, ref := range []string{"p0:Grizzly Bears", "p1:Grizzly Bears"} {
		if _, ok := snapPerm(run.snaps[0], ref); !ok {
			t.Fatalf("target %s was not on the battlefield at setup", ref)
		}
	}
	if len(fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", fails, strings.Join(transcript, "\n"))
	}
	for _, ref := range []string{"p0:Grizzly Bears", "p1:Grizzly Bears"} {
		if _, ok := snapPerm(run.snaps[2], ref); ok {
			t.Fatalf("setup creature %s survived the fight; setup permanents must not count as entered this turn\n%s", ref, strings.Join(transcript, "\n"))
		}
	}
	if diff := diffGames(run.e.G, replayFromLog(t, run.cfg, run.e.L.Events)); diff != "" {
		t.Fatalf("entry history did not replay from log: %s", diff)
	}
}
