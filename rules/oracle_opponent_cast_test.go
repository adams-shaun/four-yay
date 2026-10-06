package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

const oracleOpponentCastScenario = `{"name":"opponent-cast-priority","cr":["601.2"],"why":"p1 casts an instant after p0 passes","setup":{"p1":{"hand":["Shock"]}},"steps":[{"op":"pass","seat":0},{"op":"cast","seat":1,"card":"p1:Shock","mana":"R","targets":["p0"]},{"op":"resolve","seat":1}]}`

func TestOracleOpponentCastRoutesPriority(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(oracleOpponentCastScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Snapshots) == 0 {
		t.Fatal("precondition: opponent-cast scenario produced no snapshots")
	}
	if hand := res.Snapshots[0].Players[1].Hand; len(hand) != 1 || hand[0] != "Shock" {
		t.Fatalf("precondition: p1 hand = %v, want [Shock]", hand)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("p1 cast after p0 passed failed: %v", res.Fails)
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	if len(final.Players[1].Graveyard) != 1 || final.Players[1].Graveyard[0] != "Shock" {
		t.Fatalf("p1 graveyard = %v, want resolved Shock", final.Players[1].Graveyard)
	}
}
