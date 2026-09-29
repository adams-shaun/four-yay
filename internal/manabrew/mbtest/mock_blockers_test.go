package mbtest

import (
	"testing"

	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// Tests for mock.go's blocker policy (MBX-6 fix round, seed 20260929): the
// mock's declarations must be legal under the CR 509.1a/509.1c bounds the
// prompt itself carries (MinBlockers/MaxBlockers/MustBeBlocked), or the game
// fails the lane with an illegal block declaration.

func blockersInputFor(attackers ...mb.BlockableAttackerDto) mb.ChooseBlockersInput {
	return mb.ChooseBlockersInput{Attackers: attackers}
}

func intPtr(n int) *int { return &n }

func menace() mb.BlockableAttackerDto {
	return mb.BlockableAttackerDto{AttackerID: "a1", ValidBlockerIDs: []string{"b1", "b2", "b3", "b4"},
		MinBlockers: 2, MustBeBlocked: true}
}

func countFor(dec mb.DeclareBlockersDecision, attacker string) int {
	n := 0
	for _, a := range dec.Assignments {
		if a.AttackerID == attacker {
			n++
		}
	}
	return n
}

func TestMockMenaceAttackerGetsItsQuota(t *testing.T) {
	in := blockersInputFor(menace())
	for name, client := range map[string]*MockClient{
		"first-legal": NewFirstLegalClient(),
		"random":      NewSeededRandomClient(99),
	} {
		out := client.answerBlockers(in)
		dec, ok := out.(mb.DeclareBlockersDecision)
		if !ok {
			t.Fatalf("%s: answer type %T, want DeclareBlockersDecision", name, out)
		}
		if got := countFor(dec, "a1"); got != 2 {
			t.Fatalf("%s: menace attacker got %d blockers, want MinBlockers 2 (assignments %v)", name, got, dec.Assignments)
		}
	}
}

func TestMockOptionalQuotaAttackerIsAllOrNothing(t *testing.T) {
	// MinBlockers 2, NOT must-be-blocked, only two valid blockers, the
	// mandatory attacker beside it takes b1: in first-legal mode the coin
	// chooses to block, and the fill must be either 0 or 2 -- never a
	// partial 1.
	in := blockersInputFor(
		mb.BlockableAttackerDto{AttackerID: "opt", ValidBlockerIDs: []string{"b2", "b3"}, MinBlockers: 2},
		menace())
	dec := NewFirstLegalClient().answerBlockers(in).(mb.DeclareBlockersDecision)
	// The mandatory menace ask is served first.
	if got := countFor(dec, "a1"); got != 2 {
		t.Fatalf("menace attacker got %d blockers, want 2 (assignments %v)", got, dec.Assignments)
	}
	switch got := countFor(dec, "opt"); got {
	case 0, 2:
	default:
		t.Fatalf("optional quota attacker got %d blockers, want 0 or 2, never a partial 1 (assignments %v)", got, dec.Assignments)
	}
	// A quota the valid blockers cannot fill: all-or-nothing yields none.
	small := blockersInputFor(mb.BlockableAttackerDto{AttackerID: "big", ValidBlockerIDs: []string{"b1", "b2"}, MinBlockers: 3})
	dec = NewFirstLegalClient().answerBlockers(small).(mb.DeclareBlockersDecision)
	if len(dec.Assignments) != 0 {
		t.Fatalf("unfillable quota produced %v, want no assignments", dec.Assignments)
	}
}

func TestMockBlockerAssignmentsStayDistinctAndCapped(t *testing.T) {
	// Two must-be-blocked attackers with overlapping valid-blocker pools:
	// each blocker may serve only one attacker.
	in := blockersInputFor(
		mb.BlockableAttackerDto{AttackerID: "a1", ValidBlockerIDs: []string{"b1", "b2"}, MinBlockers: 2, MustBeBlocked: true},
		mb.BlockableAttackerDto{AttackerID: "a2", ValidBlockerIDs: []string{"b1", "b2"}, MinBlockers: 2, MustBeBlocked: true})
	dec := NewFirstLegalClient().answerBlockers(in).(mb.DeclareBlockersDecision)
	seen := map[string]string{}
	for _, a := range dec.Assignments {
		if _, dup := seen[a.BlockerID]; dup {
			t.Fatalf("blocker %s assigned twice: %v", a.BlockerID, dec.Assignments)
		}
		seen[a.BlockerID] = a.AttackerID
	}
	if countFor(dec, "a1") != 2 {
		t.Fatalf("a1 got %d blockers, want its full quota served first (assignments %v)", countFor(dec, "a1"), dec.Assignments)
	}
	// MaxBlockers caps a fill: a must-be-blocked attacker with Max 3 gets 2
	// (Min) blockers, never more.
	capped := blockersInputFor(mb.BlockableAttackerDto{AttackerID: "c", ValidBlockerIDs: []string{"b1", "b2", "b3", "b4"},
		MinBlockers: 2, MaxBlockers: intPtr(3), MustBeBlocked: true})
	dec = NewFirstLegalClient().answerBlockers(capped).(mb.DeclareBlockersDecision)
	if got := countFor(dec, "c"); got != 2 {
		t.Fatalf("capped attacker got %d blockers, want MinBlockers 2 (assignments %v)", got, dec.Assignments)
	}
}
