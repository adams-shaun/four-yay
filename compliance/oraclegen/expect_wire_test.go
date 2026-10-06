package oraclegen

import (
	"encoding/json"
	"strings"
	"testing"
)

// The Offered and CanBlock expectations are omitempty: a step that does not
// use them marshals exactly as it did before they existed, so every level-A
// item (and every pinned scenario hash) is unchanged.
func TestExpectOfferedCanBlockAreOmittedWhenUnset(t *testing.T) {
	want := false
	b, err := json.Marshal(Step{Op: "pass", Expect: []Expect{{TriggerOnStack: "p0:X", Want: &want}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "offered") || strings.Contains(string(b), "can_block") || strings.Contains(string(b), "can_attack") {
		t.Fatalf("unset legality expectations leaked into the wire form: %s", b)
	}
	plain, _ := json.Marshal(Step{Op: "pass"})
	if strings.Contains(string(plain), "expect") {
		t.Fatalf("a level-A step grew an expect key: %s", plain)
	}
	// Precondition: the fields do marshal when set, under the runner's keys.
	b, _ = json.Marshal(Expect{Offered: &Offered{Seat: 0, Kind: "cast", Card: "p0:X"}, CanBlock: &CanBlock{Blocker: "p1:A", Attacker: "p0:B"}, CanAttack: &CanAttack{Attacker: "p0:C"}, Want: &want})
	for _, k := range []string{`"offered":`, `"can_block":`, `"blocker":`, `"can_attack":`, `"attacker":`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("missing %s in %s", k, b)
		}
	}
}
