package statespec

import (
	"strings"
	"testing"
)

func TestParseIsStrictAndAppliesDefaults(t *testing.T) {
	s, err := Parse([]byte(`{"players":{"A":{"battlefield":[{"name":"Forest","id":"f","count":2}]},"B":{}},
		"labels":{"anything":{"goes":1},"blocks":[["A:f",null,true]]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != 1 || s.Players["B"].Life != 20 || s.Players["A"].Battlefield[0].Count != 2 {
		t.Fatalf("defaults %+v", s)
	}
	al := s.Aliases()
	if len(al) != 3 || al["A:f#2"] != (Alias{Seat: "A", Index: 0, Copy: 2}) || al["A:f"] != al["A:f#1"] {
		t.Fatalf("aliases %v", al)
	}
	l, err := s.ParseLabels()
	if err != nil || len(l.Blocks) != 1 || l.Blocks[0].Attacker != "" || !l.Blocks[0].Exact || !l.TurnLabel() {
		t.Fatalf("labels %+v %v", l, err)
	}
	for _, bad := range []string{
		`{"players":{},"extra":1}`,
		`{"players":{"A":{"life":1,"x":2}}}`,
		`{"stack":[{"controller":"A","card":"Shock","x":1}]}`,
		`{"provenance":{"source":"17lands","who":"me"}}`,
	} {
		if _, err := Parse([]byte(bad)); err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
