package decision

import (
	"strings"
	"testing"
)

// TestValidateRejectsLoneBlockerBelowFloor: the published CR 509.1a /
// CR 702.111b per-attacker floor (Option.MinBlockers, which the rules layer
// folds Menace into) is enforced by Validate, so a lone blocker of a
// MinBlockers=2 attacker is a client-side rejection before it ever reaches
// Submit -- a rules-ignorant client gets the error and re-answers.
func TestValidateRejectsLoneBlockerBelowFloor(t *testing.T) {
	d := &Decision{
		Seq: 1, Player: 0, Kind: KBlockers, Min: 0, Max: 2,
		Options: []Option{
			{Index: 0, Kind: "block", Obj: 10, Attacker: 9, MinBlockers: 2},
			{Index: 1, Kind: "block", Obj: 11, Attacker: 9, MinBlockers: 2},
		},
	}
	err := d.Validate(Intent{Seq: 1, Player: 0, Choices: []int{0}})
	if err == nil {
		t.Fatal("lone blocker of a MinBlockers=2 attacker must be rejected")
	}
	if !strings.Contains(err.Error(), "9") || !strings.Contains(err.Error(), "2") {
		t.Fatalf("rejection must name the attacker and the floor, got %q", err)
	}
	if err := d.Validate(Intent{Seq: 1, Player: 0, Choices: []int{0, 1}}); err != nil {
		t.Fatalf("two blockers meet the floor: %v", err)
	}
	if err := d.Validate(Intent{Seq: 1, Player: 0, Choices: nil}); err != nil {
		t.Fatalf("declining to block is legal: %v", err)
	}
}

// TestValidateRejectsBlockAboveCeiling: MaxBlockers is the mirror ceiling on
// the same published bound.
func TestValidateRejectsBlockAboveCeiling(t *testing.T) {
	d := &Decision{
		Seq: 1, Player: 0, Kind: KBlockers, Min: 0, Max: 3,
		Options: []Option{
			{Index: 0, Kind: "block", Obj: 10, Attacker: 9, MaxBlockers: 1},
			{Index: 1, Kind: "block", Obj: 11, Attacker: 9, MaxBlockers: 1},
		},
	}
	err := d.Validate(Intent{Seq: 1, Player: 0, Choices: []int{0, 1}})
	if err == nil {
		t.Fatal("two blockers of a MaxBlockers=1 attacker must be rejected")
	}
	if err := d.Validate(Intent{Seq: 1, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("one blocker meets the ceiling: %v", err)
	}
}
