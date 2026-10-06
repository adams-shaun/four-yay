package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

const attackOpponentTurnFixture = `{
  "name": "attack-opponent-turn", "cr": ["508.1a"],
  "why": "an attack step for seat 1 must advance from seat 0's turn to seat 1's combat",
  "setup": {
    "p0": {"battlefield": ["Grizzly Bears"]},
    "p1": {"battlefield": ["Grizzly Bears"]}
  },
  "steps": [
    {"op": "attack", "seat": 1, "defender": "p0", "attackers": ["p1:Grizzly Bears"]}
  ],
  "expect": [{"card": "p1:Grizzly Bears", "tapped": true}]
}`

func TestOracleAttackOpOpponentTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(attackOpponentTurnFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Snapshots) == 0 {
		t.Fatal("scenario did not produce a setup snapshot")
	}
	var found bool
	for _, p := range res.Snapshots[0].Permanents {
		if p.Ref == "p1:Grizzly Bears" && p.Controller == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("setup precondition failed: p1:Grizzly Bears is not on the battlefield")
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
}
