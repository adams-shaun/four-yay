package oraclegen

import (
	"encoding/json"
	"strings"
	"testing"
)

// The can_attack defender is omitempty: every existing can_attack expectation
// marshals exactly as it did before the field existed, so no pinned scenario
// hash moves; set, it rides the runner's key.
func TestCanAttackDefenderIsOmittedWhenUnset(t *testing.T) {
	b, err := json.Marshal(CanAttack{Attacker: "p1:A"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "defender") {
		t.Fatalf("unset defender leaked into the wire form: %s", b)
	}
	b, _ = json.Marshal(CanAttack{Attacker: "p1:A", Defender: "p0:W"})
	if !strings.Contains(string(b), `"defender":"p0:W"`) {
		t.Fatalf("defender missing under the runner's key: %s", b)
	}
}
