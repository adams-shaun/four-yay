package decision

import (
	"slices"
	"strings"
	"testing"
)

// The exactly-at-cap boundary of the per-Group rule. GroupAdmits is a
// PRE-add predicate (used[g] < GroupCapFor(g)), so a set holding exactly
// GroupCapFor(g) options of one group is legal and both gates must say so:
// groupCapExceeded (FitRequired's fast path) must report false, so a legal
// answer is returned in the client's submitted order rather than repaired,
// and Validate must accept it. One option past the cap is refused by both.
//
// This is the regression for the round-2 MAJOR: the r1 refactor called
// GroupAdmits AFTER the increment in groupCapExceeded only, so an
// exactly-at-cap answer with a Required pick present took the repair path
// and came back reordered ([3 0 1] below) — a silent misrepair of a legal
// answer, reachable on every raised GroupLimits minted by
// rules' attack-restriction asks.
func TestGroupCapExceededExactlyAtCap(t *testing.T) {
	d := &Decision{
		Kind:        KAttackers,
		Seq:         7,
		Player:      2,
		Min:         3,
		Max:         3,
		GroupLimits: map[string]int{"g": 2},
		Options: []Option{
			{Index: 0, Kind: "attack", Label: "pair0", Obj: 101, Group: "g"},
			{Index: 1, Kind: "attack", Label: "pair1", Obj: 102, Group: "g"},
			{Index: 2, Kind: "attack", Label: "pair2", Obj: 103, Group: "g"},
			{Index: 3, Kind: "attack", Label: "goaded", Obj: 104, Group: "h", Required: true},
		},
	}
	// Precondition: the fixture is what the assertions read -- exactly one
	// Required duty (so the quota is live, not vacuous), the submitted
	// at-cap answer is legal and complete, and the over-cap twin really
	// differs from it.
	if q := d.RequiredQuota(); q != 1 {
		t.Fatalf("precondition: RequiredQuota = %d, want 1 (one Required Obj)", q)
	}
	atCap := []int{0, 1, 3}
	if err := d.Validate(Intent{Seq: d.Seq, Player: d.Player, Choices: atCap}); err != nil {
		t.Fatalf("precondition: Validate(exactly-at-cap answer) = %v, want nil", err)
	}
	if d.groupCapExceeded(atCap) {
		t.Fatalf("groupCapExceeded(%v) = true, want false: a set of exactly GroupCapFor(g)=2 options of one group is legal", atCap)
	}
	if got := d.FitRequired(atCap); !slices.Equal(got, atCap) {
		t.Fatalf("FitRequired(%v) = %v, want the submitted answer unchanged: a legal answer must take the fast path, not be repaired and reordered", atCap, got)
	}

	over := []int{0, 1, 2}
	if !d.groupCapExceeded(over) {
		t.Fatalf("groupCapExceeded(%v) = false, want true: three options of one group exceed the cap of 2", over)
	}
	err := d.Validate(Intent{Seq: d.Seq, Player: d.Player, Choices: over})
	if err == nil {
		t.Fatal("Validate(over-cap answer) = nil, want the per-group-limit error")
	} else if !strings.Contains(err.Error(), "per-group limit of 2") {
		t.Fatalf("Validate(over-cap answer) error = %q, want the raised-cap per-group message", err)
	}
}

// The cap-1 twin: at the default cap the same helper is the historical
// mutual-exclusion rule with its historical message, so the boundary fix
// changed nothing there.
func TestGroupCapExceededCap1Unchanged(t *testing.T) {
	d := &Decision{
		Kind:   KAttackers,
		Seq:    1,
		Player: 0,
		Min:    1,
		Max:    2,
		Options: []Option{
			{Index: 0, Kind: "attack", Label: "a", Obj: 11, Group: "g"},
			{Index: 1, Kind: "attack", Label: "b", Obj: 12, Group: "g"},
		},
	}
	if d.groupCapExceeded([]int{0}) {
		t.Fatal("groupCapExceeded([0]) = true, want false (one of one group is legal at cap 1)")
	}
	if !d.groupCapExceeded([]int{0, 1}) {
		t.Fatal("groupCapExceeded([0 1]) = false, want true (two of one group exceed cap 1)")
	}
	err := d.Validate(Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}})
	if err == nil {
		t.Fatal("Validate(two of one group) = nil, want the mutual-exclusion error")
	} else if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("Validate error = %q, want the historical mutual-exclusion message", err)
	}
}
