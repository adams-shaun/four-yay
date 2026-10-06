package rules

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// An oracle scenario deals an Adventure card under its PARENT (front) name,
// the physical card both engines deal, and its cast step names the requested
// Adventure spell face. The runner must bind the face-name ref to the
// physical card and cast it through the Adventure offer (CR 715.3), so the
// spell's effect happens and the card ends in exile (CR 715.4) -- not cast
// the creature front, and not fail with "ref names no object".
const adventureFaceScenario = `{"name":"adventure-face","cr":["715.3"],"why":"cast an Adventure face named by the step","setup":{"p0":{"hand":["Bonecrusher Giant"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Stomp","mana":"RR","targets":["p1:Grizzly Bears"]},{"op":"resolve","seat":0}]}`

func TestOracleCastsNamedAdventureFace(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(adventureFaceScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	setup := res.Snapshots[0]
	if h := setup.Players[0].Hand; len(h) != 1 || h[0] != "Bonecrusher Giant" {
		t.Fatalf("precondition: p0 hand = %v, want [Bonecrusher Giant]", h)
	}
	if _, ok := snapPerm(setup, "p1:Grizzly Bears"); !ok {
		t.Fatal("precondition: p1:Grizzly Bears missing")
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	// Stomp's 2 damage killed the Bears: the Adventure face's effect.
	if _, ok := snapPerm(final, "p1:Grizzly Bears"); ok {
		t.Fatal("Grizzly Bears survived: the Adventure spell did not resolve")
	}
	if g := final.Players[1].Graveyard; len(g) != 1 || g[0] != "Grizzly Bears" {
		t.Fatalf("p1 graveyard = %v, want [Grizzly Bears]", g)
	}
	// The card went on an adventure (exile), not to the battlefield or the
	// graveyard.
	if _, ok := snapPerm(final, "p0:Bonecrusher Giant"); ok {
		t.Fatal("the creature front was cast instead of the Adventure face")
	}
	if x := final.Players[0].Exile; len(x) != 1 || x[0] != "Bonecrusher Giant" {
		t.Fatalf("p0 exile = %v, want [Bonecrusher Giant]", x)
	}
	if g := final.Players[0].Graveyard; len(g) != 0 {
		t.Fatalf("p0 graveyard = %v, want empty", g)
	}
	if h := final.Players[0].Hand; len(h) != 0 {
		t.Fatalf("p0 hand = %v, want empty", h)
	}
	// Determinism: the same scenario replays to the same snapshots.
	again, err := RunOracleScenarioJSON(reg, []byte(adventureFaceScenario))
	if err != nil || !reflect.DeepEqual(again.Snapshots, res.Snapshots) {
		t.Fatalf("replay is not deterministic: err=%v", err)
	}
}
