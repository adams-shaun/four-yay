package oraclediff

import (
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func TestCompareHandCountIgnoresCompositionButKeepsCount(t *testing.T) {
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{snap("setup")}}
	x := XResult{Snapshots: []rules.OracleSnapshot{xsnap("setup")}}
	g.Snapshots[0].Players[0].Hand = []string{"Ornithopter", "Wastes"}
	x.Snapshots[0].Players[0].Hand = []string{"Forest", "Island"}
	if v := CompareOpts(g, nil, x, []string{CompareHandCount}); v.Status != Agree {
		t.Fatalf("same-size random hands diverged: %+v", v)
	}
	x.Snapshots[0].Players[0].Hand = []string{"Forest"}
	if v := CompareOpts(g, nil, x, []string{CompareHandCount}); v.Status != Diverge || v.Field != "p0.hand" {
		t.Fatalf("different hand sizes: got %+v, want p0.hand divergence", v)
	}
}
