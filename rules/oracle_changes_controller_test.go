package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Zidane's "Whenever an opponent gains control of a permanent from you"
// (ValidCard$ Card.OppCtrl | ValidOriginalController$ You): the two filters
// hold only POST-change, so the trigger fires on p1's Threaten taking a p0
// creature and its Treasure resolves. CR 603.10 (the trigger reads the card
// as the event left it, beside the original-controller filter).
const oracleChangesControllerScenario = `{"name":"changes-controller-oppctrl","cr":["603.10"],"why":"p1's Threaten takes a p0 creature; Zidane's row fires",
 "setup":{"p0":{"battlefield":["Zidane, Tantalus Thief","Grizzly Bears"]},"p1":{"hand":["Threaten"]}},
 "steps":[{"op":"pass_to","step":"main1","active":"p1"},{"op":"cast","seat":1,"card":"p1:Threaten","mana":"CCR","targets":["p0:Grizzly Bears"]},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0}]}`

func TestOracleChangesControllerOppCtrlFires(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(oracleChangesControllerScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("precondition: scenario did not play cleanly: %v", res.Fails)
	}
	// Precondition: the Threaten resolution actually moved the bears to p1.
	last := res.Snapshots[len(res.Snapshots)-1]
	taken := false
	for _, p := range last.Permanents {
		taken = taken || (p.Controller == 1 && p.Name == "Grizzly Bears")
	}
	if !taken {
		t.Fatalf("precondition: p1 never gained the bears: %+v", last.Permanents)
	}
	// The row trigger went on the stack: an ability sourced by Zidane at
	// trigger slot 1 (its second trigger line).
	onStack := false
	for _, s := range res.Snapshots {
		for _, e := range s.Stack {
			if e.Kind == "ability" && e.Trigger == "1" && strings.Contains(strings.ToLower(e.Source), "zidane") {
				onStack = true
			}
		}
	}
	if !onStack {
		t.Fatalf("Zidane's ChangesController trigger never appeared on the stack; last stack=%v", last.Stack)
	}
	// Its resolution: Zidane's controller made the Treasure.
	made := false
	for _, p := range last.Permanents {
		made = made || (p.Controller == 0 && p.Token && strings.Contains(strings.ToLower(p.Name), "treasure"))
	}
	if !made {
		t.Fatalf("Treasure never entered: permanents %+v", last.Permanents)
	}
}
