package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

const malametBattleGlyphScenario = `{"name":"gen1-cast-resolve","cr":["601.2"],"why":"generated level-A scenario","setup":{"p0":{"battlefield":["Grizzly Bears"],"hand":["Malamet Battle Glyph"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Malamet Battle Glyph","mana":"G","targets":["p0:Grizzly Bears","p1:Grizzly Bears"]},{"op":"resolve","seat":0}]}`

func TestMalametBattleGlyphOracleFixtureAgreesThroughExportedPath(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(malametBattleGlyphScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	if got := len(res.Snapshots); got != 3 {
		t.Fatalf("snapshots: %d, want setup, cast, resolve", got)
	}
	checkpoints := []string{res.Snapshots[0].Checkpoint, res.Snapshots[1].Checkpoint, res.Snapshots[2].Checkpoint}
	if got := strings.Join(checkpoints, "|"); got != "setup|step 0 (cast)|step 1 (resolve)" {
		t.Fatalf("checkpoints %q", got)
	}
	for _, ref := range []string{"p0:Grizzly Bears", "p1:Grizzly Bears"} {
		if _, ok := snapPerm(res.Snapshots[0], ref); !ok {
			t.Fatalf("setup target %s is not on the battlefield", ref)
		}
	}
	final := res.Snapshots[2]
	for _, ref := range []string{"p0:Grizzly Bears", "p1:Grizzly Bears"} {
		if _, ok := snapPerm(final, ref); ok {
			t.Fatalf("setup creature %s survived; neither setup creature entered this turn", ref)
		}
	}
}

func TestMalametBattleGlyphSetupEnteredIsScopedToGeneratedScenarios(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc, err := decodeOracleScenario([]byte(malametBattleGlyphScenario))
	if err != nil {
		t.Fatal(err)
	}
	fails, transcript, run := runOracleScenario(reg, sc)
	if len(run.snaps) != 3 {
		t.Fatalf("snapshots: %d, want setup, cast, resolve", len(run.snaps))
	}
	for _, ref := range []string{"p0:Grizzly Bears", "p1:Grizzly Bears"} {
		if _, ok := snapPerm(run.snaps[0], ref); !ok {
			t.Fatalf("setup target %s is not on the battlefield", ref)
		}
	}
	if len(fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", fails, strings.Join(transcript, "\n"))
	}
	if bear, ok := snapPerm(run.snaps[2], "p0:Grizzly Bears"); ok && bear.PT == "3/3" && bear.Counters["P1P1"] == 1 {
		t.Fatalf("hand-authored scenario incorrectly counted setup entry this turn: %+v", bear)
	}
}
