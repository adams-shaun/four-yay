package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Avalanche of Sector 7's "Whenever an opponent activates an ability of an
// artifact they control" (ValidCard$ Artifact.inZoneBattlefield, ValidSA$
// Activated.OppCtrl, ValidSAonCard$ Activated.YouCtrl): p1 activates Sensei's
// Divining Top's rearrange ability and the row fires. CR 603.10 (the ValidCard$ read is
// the ability's source permanent, whose battlefield zone the inZone$
// qualifier names).
const oracleAbilityCastArtifactScenario = `{"name":"ability-cast-artifact-oppctrl","cr":["603.10"],"why":"p1's Sensei's Divining Top activation fires Avalanche's row",
 "setup":{"p0":{"battlefield":["Avalanche of Sector 7"]},"p1":{"battlefield":["Sensei's Divining Top"]}},
 "steps":[{"op":"pass","seat":0},{"op":"activate","seat":1,"card":"p1:Sensei's Divining Top","mana":"C","ability_index":0},{"op":"pass_to","decision":"priority"}]}`

func TestOracleAbilityCastArtifactOppCtrlFires(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(oracleAbilityCastArtifactScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("precondition: scenario did not play cleanly: %v", res.Fails)
	}
	activated := false
	for _, s := range res.Snapshots {
		for _, e := range s.Stack {
			if e.Kind == "ability" && strings.Contains(strings.ToLower(e.Source), "divining top") {
				activated = true
			}
		}
	}
	if !activated {
		t.Fatal("precondition: Sensei's Divining Top's ability was never activated")
	}
	onStack := false
	for _, s := range res.Snapshots {
		for _, e := range s.Stack {
			if e.Kind == "ability" && e.Trigger == "0" && strings.Contains(strings.ToLower(e.Source), "avalanche") {
				onStack = true
			}
		}
	}
	if !onStack {
		t.Fatalf("Avalanche's AbilityCast trigger never appeared on the stack; last stack=%v",
			res.Snapshots[len(res.Snapshots)-1].Stack)
	}
}
