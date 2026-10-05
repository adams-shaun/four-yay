package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// The generator's battlefield setup is put into play on the first turn in
// XMage. In a generated compliance scenario a target of Malamet Battle Glyph
// therefore entered this turn even though gorge stages its setup before the
// initial TurnChange; a hand-authored Oracle scenario (Sentinel Sarah Lyons,
// Dark Fortress) keeps its setup present from before the turn.
func TestOracleSetupEntryHistoryForConditionDefined(t *testing.T) {
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
	if _, ok := snapPerm(run.snaps[0], "p0:Grizzly Bears"); !ok {
		t.Fatal("target was not on the battlefield at setup")
	}
	if len(fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", fails, strings.Join(transcript, "\n"))
	}
	target, ok := snapPerm(run.snaps[2], "p0:Grizzly Bears")
	if !ok || target.PT != "3/3" || target.Counters["P1P1"] != 1 {
		t.Fatalf("target after first-turn condition: %+v (present=%v); want 3/3 with counter\n%s", target, ok, strings.Join(transcript, "\n"))
	}
	if diff := diffGames(run.e.G, replayFromLog(t, run.cfg, run.e.L.Events)); diff != "" {
		t.Fatalf("entry history did not replay from log: %s", diff)
	}
}
