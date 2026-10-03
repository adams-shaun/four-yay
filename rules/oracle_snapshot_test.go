package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

const boltTheBears = `{
  "name": "bolt-the-bears", "cr": ["608.2"], "why": "snapshot fixture",
  "setup": {
    "p0": {"hand": ["Lightning Bolt"]},
    "p1": {"battlefield": ["Grizzly Bears", "Grizzly Bears"], "library_top": ["Shock", "Lightning Bolt"]}
  },
  "steps": [
    {"op": "cast", "seat": 0, "card": "p0:Lightning Bolt", "mana": "R", "targets": ["p1:Grizzly Bears#2"]},
    {"op": "resolve"}
  ],
  "expect": [{"card": "p1:Grizzly Bears#2", "zone": "graveyard"}]
}`

func snapPerm(s OracleSnapshot, ref string) (OracleSnapPerm, bool) {
	for _, p := range s.Permanents {
		if p.Ref == ref {
			return p, true
		}
	}
	return OracleSnapPerm{}, false
}

func TestOracleSnapshotSetupAndSteps(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(boltTheBears))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	if got := len(res.Snapshots); got != 3 {
		t.Fatalf("%d snapshots, want 3 (setup + 2 steps)", got)
	}
	names := []string{res.Snapshots[0].Checkpoint, res.Snapshots[1].Checkpoint, res.Snapshots[2].Checkpoint}
	if strings.Join(names, "|") != "setup|step 0 (cast)|step 1 (resolve)" {
		t.Fatalf("checkpoints %q", names)
	}
	setup, final := res.Snapshots[0], res.Snapshots[2]
	bears, ok := snapPerm(setup, "p1:Grizzly Bears#2")
	if !ok || bears.PT != "2/2" || bears.Controller != 1 || bears.Owner != 1 {
		t.Fatalf("setup bears #2 = %+v, %v", bears, ok)
	}
	// p1's library: seat 1 draws nothing during seat 0's turn 1, so the
	// setup order is still intact at every checkpoint of this scenario.
	if got := setup.Players[1].LibraryTop; len(got) < 2 || got[0] != "Shock" || got[1] != "Lightning Bolt" {
		t.Fatalf("setup p1 library_top = %q, want Shock, Lightning Bolt first", got)
	}
	if _, ok := snapPerm(final, "p1:Grizzly Bears#2"); ok {
		t.Fatal("bears #2 still on the battlefield after Bolt resolved")
	}
	if _, ok := snapPerm(final, "p1:Grizzly Bears"); !ok {
		t.Fatal("bears #1 missing after Bolt resolved")
	}
	if g := final.Players[1].Graveyard; len(g) != 1 || g[0] != "Grizzly Bears" {
		t.Fatalf("p1 graveyard = %q", g)
	}
	if g := final.Players[0].Graveyard; len(g) != 1 || g[0] != "Lightning Bolt" {
		t.Fatalf("p0 graveyard = %q", g)
	}
	if len(final.Stack) != 0 {
		t.Fatalf("stack not empty: %+v", final.Stack)
	}
	if cast := res.Snapshots[1]; len(cast.Stack) != 1 || cast.Stack[0].Kind != "spell" || cast.Stack[0].Source != "p0:Lightning Bolt" {
		t.Fatalf("after cast, stack = %+v", cast.Stack)
	}
}

func TestOracleSnapshotRefsMatchScenarioRefs(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(boltTheBears))
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, p := range res.Snapshots[0].Permanents {
		if p.Controller == 1 {
			refs = append(refs, p.Ref)
		}
	}
	if strings.Join(refs, "|") != "p1:Grizzly Bears|p1:Grizzly Bears#2" {
		t.Fatalf("p1 refs %q", refs)
	}
}

func TestOracleSnapshotRecordsNonPriorityDecisions(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(boltTheBears))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res.Decisions {
		if d.Kind == "priority" {
			t.Fatalf("priority decision recorded: %+v", d)
		}
	}
	// Bolt's target is chosen either inside the cast option (no separate
	// decision) or through a target decision. If one is recorded it must be
	// seat 0's, with the bears among its picks.
	for _, d := range res.Decisions {
		if d.Kind == "target" && (d.Seat != 0 || !strings.Contains(strings.Join(d.Picks, ","), "Grizzly Bears")) {
			t.Fatalf("target decision %+v", d)
		}
	}
}

func TestOracleSnapshotDoesNotPerturbReplay(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc, err := decodeOracleScenario([]byte(boltTheBears))
	if err != nil {
		t.Fatal(err)
	}
	fails, transcript, run := runOracleScenario(reg, sc)
	if len(fails) != 0 || run.e == nil {
		t.Fatalf("fails %v\n%s", fails, strings.Join(transcript, "\n"))
	}
	if len(run.snaps) != 3 {
		t.Fatalf("%d snapshots", len(run.snaps))
	}
	if diff := diffGames(run.e.G, replayFromLog(t, run.cfg, run.e.L.Events)); diff != "" {
		t.Fatalf("log-only replay differs after snapshotting:\n%s", diff)
	}
}

func TestRunOracleScenarioJSONRejectsUnknownField(t *testing.T) {
	_, err := RunOracleScenarioJSON(nil, []byte(`{"name":"x","steps":[],"bogus":1}`))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v, want an unknown-field error naming bogus", err)
	}
}
