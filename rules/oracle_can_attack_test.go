package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestOracleCanAttackExpectation: a Defender is absent from the declare
// attackers decision's options and a vanilla creature is present, and the
// `can_attack` expectation reads exactly that, in both directions.
func TestOracleCanAttackExpectation(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	run := func(wallWant, lionsWant string) []string {
		sc := `{"name":"can-attack","cr":["508.1a"],"why":"can_attack expectation","setup":{"p0":{"battlefield":["Wall of Omens","Savannah Lions"]}},"steps":[{"op":"pass_to","seat":0,"step":"declare-attackers","decision":"attackers","expect":[{"can_attack":{"attacker":"p0:Savannah Lions"},"want":` + lionsWant + `},{"can_attack":{"attacker":"p0:Wall of Omens"},"want":` + wallWant + `}]}]}`
		res, err := RunOracleScenarioJSON(reg, []byte(sc))
		if err != nil {
			t.Fatal(err)
		}
		return res.Fails
	}
	if fails := run("false", "true"); len(fails) != 0 {
		t.Fatalf("Defender absent / vanilla present must hold: %v", fails)
	}
	fails := run("true", "true")
	if len(fails) != 1 || !strings.Contains(fails[0], "p0:Wall of Omens can attack = false, want true") {
		t.Fatalf("claiming the Defender may attack must fail on exactly that claim: %v", fails)
	}
	fails = run("false", "false")
	if len(fails) != 1 || !strings.Contains(fails[0], "p0:Savannah Lions can attack = true, want false") {
		t.Fatalf("claiming the vanilla creature may not attack must fail: %v", fails)
	}
}
