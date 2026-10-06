package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// A `cast` step is not an ability activation, so `ability_index` on it has no
// meaning an index could honour. Before this fix the decoder accepted the
// field and the cast branch never read it, so the step silently cast through
// ordinary cast-mode selection -- the scenario author got no signal that the
// index meant nothing. The fix rejects the step with a harness error naming
// the index, worded exactly like the wrong-index activate path.
//
// The card and step are the report's own example: Lightning Bolt with
// "ability_index":0 aimed at a Grizzly Bears.
const castAbilityIndexScenario = `{"name":"cast-ability-index","cr":["601.2"],"why":"a cast step must reject ability_index","setup":{"p0":{"hand":["Lightning Bolt"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Lightning Bolt","mana":"R","ability_index":0,"targets":["p1:Grizzly Bears"]}]}`

func TestOracleAbilityIndexCastFails(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(castAbilityIndexScenario))
	if err != nil {
		t.Fatal(err)
	}
	setup := res.Snapshots[0]
	// Precondition: the Bolt really is in hand, so the step is a cast the
	// runner would otherwise perform -- the rejection is about the index,
	// not a missing card.
	if h := setup.Players[0].Hand; len(h) != 1 || h[0] != "Lightning Bolt" {
		t.Fatalf("precondition: p0 hand = %v, want [Lightning Bolt]", h)
	}
	if _, ok := snapPerm(setup, "p1:Grizzly Bears"); !ok {
		t.Fatal("precondition: p1:Grizzly Bears missing")
	}
	if len(res.Fails) == 0 {
		t.Fatalf("cast with ability_index 0 succeeded: the harness silently ignored the index\n%s", strings.Join(res.Transcript, "\n"))
	}
	named := false
	for _, f := range res.Fails {
		if strings.Contains(f, "harness:") && strings.Contains(f, "ability_index: 0") {
			named = true
		}
	}
	if !named {
		t.Fatalf("expected a harness failure naming ability_index 0, got %v", res.Fails)
	}
}

// The guard must be specific to the field: the identical scenario without
// ability_index still casts the Bolt. Lightning Bolt deals 3 to the Grizzly
// Bears, killing it (a 2/2), so the final graveyard pins that the cast
// really happened.
const castWithoutAbilityIndexScenario = `{"name":"cast-no-ability-index","cr":["601.2"],"why":"a plain cast step still casts","setup":{"p0":{"hand":["Lightning Bolt"],"battlefield":["Mountain"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Lightning Bolt","mana":"R","targets":["p1:Grizzly Bears"]},{"op":"resolve","seat":0}]}`

func TestOracleAbilityIndexCastAbsentStillCasts(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(castWithoutAbilityIndexScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("plain cast failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	setup := res.Snapshots[0]
	if h := setup.Players[0].Hand; len(h) != 1 || h[0] != "Lightning Bolt" {
		t.Fatalf("precondition: p0 hand = %v, want [Lightning Bolt]", h)
	}
	if _, ok := snapPerm(setup, "p1:Grizzly Bears"); !ok {
		t.Fatal("precondition: p1:Grizzly Bears missing")
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	if _, ok := snapPerm(final, "p1:Grizzly Bears"); ok {
		t.Fatal("Grizzly Bears survived: the Bolt did not resolve (the cast was not performed)")
	}
	if g := final.Players[1].Graveyard; len(g) != 1 || g[0] != "Grizzly Bears" {
		t.Fatalf("p1 graveyard = %v, want [Grizzly Bears] (3 damage from the resolved Bolt)", g)
	}
}
