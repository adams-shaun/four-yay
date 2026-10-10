package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestOracleCanAttackDefenderExpectation: a can_attack expectation that names
// a defender reads the option attacking THAT permanent, not merely the
// attacker's presence. p0's Lions may attack p1's planeswalker (its option
// carries the planeswalker as its Battle) but have no option against p1's
// creature, so claiming one is a fail naming exactly that claim.
func TestOracleCanAttackDefenderExpectation(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	run := func(defender, want string) []string {
		sc := `{"name":"can-attack-defender","cr":["508.1b"],"why":"can_attack defender","setup":{"p0":{"battlefield":["Savannah Lions"]},"p1":{"battlefield":["Jace Beleren","Grizzly Bears"]}},"steps":[{"op":"pass_to","seat":0,"step":"declare-attackers","decision":"attackers","expect":[{"can_attack":{"attacker":"p0:Savannah Lions","defender":"` + defender + `"},"want":` + want + `}]}]}`
		res, err := RunOracleScenarioJSON(reg, []byte(sc))
		if err != nil {
			t.Fatal(err)
		}
		return res.Fails
	}
	if fails := run("p1:Jace Beleren", "true"); len(fails) != 0 {
		t.Fatalf("the attack on the planeswalker is offered: %v", fails)
	}
	if fails := run("p1:Grizzly Bears", "false"); len(fails) != 0 {
		t.Fatalf("no option attacks a creature, so want=false must hold: %v", fails)
	}
	fails := run("p1:Grizzly Bears", "true")
	if len(fails) != 1 || !strings.Contains(fails[0], "can attack = false, want true") {
		t.Fatalf("claiming an attack on a creature must fail on exactly that claim: %v", fails)
	}
}
