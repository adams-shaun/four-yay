package rules

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
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
	sc, err := decodeOracleScenario([]byte(boltTheBears))
	if err != nil {
		t.Fatal(err)
	}
	_, _, run := runOracleScenario(reg, sc)
	// Permanents are listed in ObjID order, which need not be setup order,
	// so compare the ref set and pin each ref to the object setup bound.
	setup := run.snaps[0]
	var refs []string
	for _, p := range setup.Permanents {
		if p.Controller == 1 {
			refs = append(refs, p.Ref)
		}
	}
	sort.Strings(refs)
	if strings.Join(refs, "|") != "p1:Grizzly Bears|p1:Grizzly Bears#2" {
		t.Fatalf("p1 refs %q", refs)
	}
	// The scenario Bolted the object bound to #2; #2 must be the ref that
	// left, and the survivor must still be the object bound to #1.
	if _, ok := snapPerm(run.snaps[len(run.snaps)-1], "p1:Grizzly Bears#2"); ok {
		t.Fatal("#2 still on the battlefield")
	}
	if id := run.refs["p1:Grizzly Bears"]; run.e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("the object bound to p1:Grizzly Bears is in %v", run.e.G.Obj(id).Zone)
	}
	assertSnapRefsResolve(t, run)
}

// A snapshot ref is the object's identity, not its position: it is the ref
// the runner's resolve maps back to that object. Killing bears #1 must not
// rename the survivor, and a bears cast as #2 is still #2 on the field.
func TestOracleSnapshotRefsAreStableIdentities(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	killFirst := strings.Replace(boltTheBears, `"targets": ["p1:Grizzly Bears#2"]`, `"targets": ["p1:Grizzly Bears"]`, 1)
	killFirst = strings.Replace(killFirst, `"expect": [{"card": "p1:Grizzly Bears#2"`, `"expect": [{"card": "p1:Grizzly Bears"`, 1)
	castSecond := `{
  "name": "cast-the-second-bears", "cr": ["608.2"], "why": "snapshot fixture",
  "setup": {
    "p0": {"hand": ["Grizzly Bears", "Grizzly Bears"]}
  },
  "steps": [
    {"op": "cast", "seat": 0, "card": "p0:Grizzly Bears#2", "mana": "GG"},
    {"op": "resolve"}
  ],
  "expect": [{"card": "p0:Grizzly Bears#2", "zone": "battlefield"}]
}`
	for _, tc := range []struct {
		name, scenario, ref string
		ctrl                int
	}{
		{"kill-first", killFirst, "p1:Grizzly Bears#2", 1},
		{"cast-second", castSecond, "p0:Grizzly Bears#2", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc, err := decodeOracleScenario([]byte(tc.scenario))
			if err != nil {
				t.Fatal(err)
			}
			fails, transcript, run := runOracleScenario(reg, sc)
			if len(fails) != 0 {
				t.Fatalf("fails %v\n%s", fails, strings.Join(transcript, "\n"))
			}
			final := run.snaps[len(run.snaps)-1]
			var refs []string
			for _, p := range final.Permanents {
				if p.Controller == tc.ctrl && p.Name == "Grizzly Bears" {
					refs = append(refs, p.Ref)
				}
			}
			if len(refs) != 1 || refs[0] != tc.ref {
				t.Fatalf("final bears refs %q, want [%s]", refs, tc.ref)
			}
			assertSnapRefsResolve(t, run)
		})
	}
}

// assertSnapRefsResolve checks, on the run's final state, that every
// permanent's and stack source's ref resolves back to that same object.
func assertSnapRefsResolve(t *testing.T, r *oracleRun) {
	t.Helper()
	g := r.e.G
	for i := range g.Players {
		for _, id := range g.Zone(state.ZBattlefield, state.PlayerID(i)) {
			ref := r.objRef(g.Obj(id))
			if got, err := r.resolve(ref); err != nil || got != id {
				t.Errorf("permanent %d ref %q resolves to %d (%v)", id, ref, got, err)
			}
		}
	}
}

// Tokens get positional refs ("p0:token:Soldier Token", "#2"), and each resolves
// to its own token.
func TestOracleSnapshotTokenRefsResolve(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc, err := decodeOracleScenario([]byte(`{
  "name": "raise-the-alarm", "cr": ["111.1"], "why": "snapshot fixture",
  "setup": {"p0": {"hand": ["Raise the Alarm"]}},
  "steps": [
    {"op": "cast", "seat": 0, "card": "p0:Raise the Alarm", "mana": "WW"},
    {"op": "resolve"}
  ],
  "expect": [{"card": "p0:token:Soldier#2", "zone": "battlefield"}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	fails, transcript, run := runOracleScenario(reg, sc)
	if len(fails) != 0 {
		t.Fatalf("fails %v\n%s", fails, strings.Join(transcript, "\n"))
	}
	var refs []string
	for _, p := range run.snaps[len(run.snaps)-1].Permanents {
		if p.Token {
			refs = append(refs, p.Ref)
		}
	}
	if strings.Join(refs, "|") != "p0:token:Soldier Token|p0:token:Soldier Token#2" {
		t.Fatalf("token refs %q", refs)
	}
	assertSnapRefsResolve(t, run)
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

// Snapshotting is read-only: the same scenario run with and without
// snapshots produces the identical event stream. Glorious Anthem puts a
// layer-7c effect on the field, so the snapshot's Derived/Power reads go
// through the layer cache.
func TestOracleSnapshotDoesNotPerturbReplay(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	withAnthem := strings.Replace(boltTheBears, `"battlefield": ["Grizzly Bears", "Grizzly Bears"]`, `"battlefield": ["Grizzly Bears", "Grizzly Bears", "Glorious Anthem"]`, 1)
	withAnthem = strings.Replace(withAnthem, `"steps": [`, `"steps": [
    {"op": "cast", "seat": 0, "card": "p0:Shock", "mana": "R", "targets": ["p1:Grizzly Bears#2"]},
    {"op": "resolve"},`, 1)
	withAnthem = strings.Replace(withAnthem, `"p0": {"hand": ["Lightning Bolt"]}`, `"p0": {"hand": ["Lightning Bolt", "Shock"]}`, 1)
	sc, err := decodeOracleScenario([]byte(withAnthem))
	if err != nil {
		t.Fatal(err)
	}
	run := func(noSnapshot bool) *oracleRun {
		fails, transcript, r := runOracleScenarioWith(reg, sc, noSnapshot)
		if len(fails) != 0 || r.e == nil {
			t.Fatalf("noSnapshot=%v: fails %v\n%s", noSnapshot, fails, strings.Join(transcript, "\n"))
		}
		return r
	}
	with, without := run(false), run(true)
	if len(with.snaps) != 5 || len(without.snaps) != 0 {
		t.Fatalf("%d / %d snapshots, want 5 / 0", len(with.snaps), len(without.snaps))
	}
	// Shock deals 2 to a 3/3 (anthem): the bears survive, which the
	// snapshot must show through the layer system.
	if p, ok := snapPerm(with.snaps[2], "p1:Grizzly Bears#2"); !ok || p.PT != "3/3" || p.Damage != 2 {
		t.Fatalf("after Shock, bears #2 = %+v, %v", p, ok)
	}
	if !reflect.DeepEqual(with.e.L.Events, without.e.L.Events) {
		t.Fatalf("event streams differ: %d events with snapshots, %d without", len(with.e.L.Events), len(without.e.L.Events))
	}
	if diff := diffGames(with.e.G, replayFromLog(t, with.cfg, with.e.L.Events)); diff != "" {
		t.Fatalf("log-only replay differs after snapshotting:\n%s", diff)
	}
}

func TestRunOracleScenarioJSONRejectsUnknownField(t *testing.T) {
	_, err := RunOracleScenarioJSON(nil, []byte(`{"name":"x","steps":[],"bogus":1}`))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v, want an unknown-field error naming bogus", err)
	}
}
