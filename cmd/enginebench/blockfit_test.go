package main

import (
	"errors"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestMenaceRepairDropsTheLoneBlocker: a blockers answer the engine
// rejected for menace loses exactly the named attacker's lone blocker, and
// a published MinBlockers bound is fitted before the first submit.
func TestMenaceRepairDropsTheLoneBlocker(t *testing.T) {
	d := &decision.Decision{Kind: decision.KBlockers, Options: []decision.Option{
		{Attacker: 7}, {Attacker: 8}, {Attacker: 8},
		{Attacker: 9, MinBlockers: 2},
	}}
	in := decision.Intent{Choices: []int{0, 1, 2}}
	got, ok := menaceRepair(d, in, errors.New("attacker 7 with menace must be blocked by at least two creatures"))
	if !ok || !slices.Equal(got.Choices, []int{1, 2}) {
		t.Fatalf("menace repair = %v, %v", got.Choices, ok)
	}
	if _, ok := menaceRepair(d, in, errors.New("something else")); ok {
		t.Fatal("repaired an unrelated rejection")
	}
	if got := fitBlockBounds(d, []int{0, 3}, 0); !slices.Equal(got, []int{0}) {
		t.Fatalf("MinBlockers fit = %v, want [0]", got)
	}
}
