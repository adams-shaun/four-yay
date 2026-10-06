package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// A scenario seat's setup "counters" adds counters to a battlefield
// placement on top of what it enters with: Ajani Resolute (printed loyalty 2)
// with 8 more LOYALTY can pay its [-10], and a Grizzly Bears with one P1P1 is
// a 3/3. Both are logged CounterChange events, so the game replays.
func TestOracleSetupCounters(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const scenario = `{"name":"setup-counters","setup":{"p0":{"battlefield":["Ajani Resolute"],"counters":{"Ajani Resolute":{"LOYALTY":8}}},"p1":{"battlefield":["Grizzly Bears"],"counters":{"Grizzly Bears":{"P1P1":1}}}},"steps":[]}`
	sc, err := decodeOracleScenario([]byte(scenario))
	if err != nil {
		t.Fatal(err)
	}
	sc.xmageFixture = true
	fails, transcript, run := runOracleScenario(reg, sc)
	if len(fails) != 0 || len(run.snaps) == 0 {
		t.Fatalf("scenario failed: %v\n%s", fails, strings.Join(transcript, "\n"))
	}
	ajani, ok := snapPerm(run.snaps[0], "p0:Ajani Resolute")
	if !ok {
		t.Fatalf("precondition: Ajani Resolute not on the battlefield at setup\n%s", strings.Join(transcript, "\n"))
	}
	if got := ajani.Counters["LOYALTY"]; got != 10 {
		t.Fatalf("Ajani Resolute loyalty = %d, want printed 2 + setup 8 = 10 (%v)", got, ajani.Counters)
	}
	bears, ok := snapPerm(run.snaps[0], "p1:Grizzly Bears")
	if !ok || bears.Counters["P1P1"] != 1 || bears.PT != "3/3" {
		t.Fatalf("p1 Grizzly Bears at setup: %+v (present=%v), want 3/3 with one P1P1", bears, ok)
	}
	if diff := diffGames(run.e.G, replayFromLog(t, run.cfg, run.e.L.Events)); diff != "" {
		t.Fatalf("setup counters did not replay from log: %s", diff)
	}
}
