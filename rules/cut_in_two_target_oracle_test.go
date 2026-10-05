package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Real corpus card (WOE 125). Pins commit 42df82652 (the XMage driver's
// per-target binding for a two-target cast): the root's 4 damage must go to
// the FIRST listed target and the Role's bearer is the SECOND, so the dead
// Grizzly lands in its owner's graveyard and the Role attaches to the
// surviving one. The old driver joined both targets into one
// castSpell("p1:Grizzly Bears^p0:Grizzly Bears") string and hit the wrong
// same-named permanent.
const cutInTwoTargetScenario = `{"name":"gen1-cast-resolve","cr":["601.2"],"why":"generated level-A scenario","setup":{"p0":{"battlefield":["Grizzly Bears"],"hand":["Cut In"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Cut In","mana":"CCCR","targets":["p1:Grizzly Bears","p0:Grizzly Bears"]},{"op":"resolve","seat":0}]}`

func TestCutInTwoTargetOracleFixtureAgreesThroughExportedPath(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(cutInTwoTargetScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	// Precondition: both Grizzlies are on the battlefield, one per seat.
	if _, ok := snapPerm(res.Snapshots[0], "p0:Grizzly Bears"); !ok {
		t.Fatal("setup: p0:Grizzly Bears missing")
	}
	if _, ok := snapPerm(res.Snapshots[0], "p1:Grizzly Bears"); !ok {
		t.Fatal("setup: p1:Grizzly Bears missing")
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	// The FIRST listed target (p1's) took the 4 damage and died into its OWN
	// owner's graveyard -- the exact field the report diverged on.
	if g := final.Players[1].Graveyard; len(g) != 1 || g[0] != "Grizzly Bears" {
		t.Fatalf("p1 graveyard = %v, want [Grizzly Bears]", g)
	}
	if g := final.Players[0].Graveyard; len(g) != 1 || g[0] != "Cut In" {
		t.Fatalf("p0 graveyard = %v, want [Cut In] (the p0 Grizzly must survive)", g)
	}
	// The SECOND listed target (p0's) survives and bears the Role.
	bear, ok := snapPerm(final, "p0:Grizzly Bears")
	if !ok || bear.PT != "2/2" {
		t.Fatalf("p0 Grizzly after resolve = %+v (present=%v), want 2/2", bear, ok)
	}
	role, ok := snapPerm(final, "p0:token:Young Hero")
	if !ok || !role.Token || role.AttachedTo != "p0:Grizzly Bears" {
		t.Fatalf("Young Hero Role = %+v (present=%v), want token attached_to p0:Grizzly Bears", role, ok)
	}
}
