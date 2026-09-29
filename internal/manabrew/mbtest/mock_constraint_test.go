package mbtest

import (
	"testing"

	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// Tests for mock.go's set-level target constraint policy: when the prompt's
// presentation description carries one of the translator's constraint
// sentences (prompt_target_set.go's targetSetSentences), the client must
// answer with the offered PREFIX (the translator guarantees the first
// MinTargets candidates form one legal set) in both modes, and must keep its
// ordinary policy for an unconstrained ask. The sentence wording is the
// contract; the tests exercise both directions of the fence.

func boardTargetInputFor(cands ...string) mb.ChooseBoardTargetsInput {
	in := mb.ChooseBoardTargetsInput{
		Candidates:    make([]mb.TargetRef, 0, len(cands)),
		ChosenTargets: []mb.TargetRef{},
	}
	for _, id := range cands {
		in.Candidates = append(in.Candidates, mb.TargetRef{Kind: mb.RefCard, ID: id})
	}
	return in
}

func TestMockConstraintBoundAsksTakeThePrefix(t *testing.T) {
	in := boardTargetInputFor("c1", "c2", "c3", "c4", "c5")
	in.MinTargets, in.MaxTargets = 2, 2
	in.Presentation.Description = "Choose 2 to 2. All chosen targets must share one controller."
	for name, client := range map[string]*MockClient{
		"first-legal": NewFirstLegalClient(),
		"random":      NewSeededRandomClient(99),
	} {
		out := client.answerBoardTargets(in)
		dec, ok := out.(mb.BoardTargetsDecision)
		if !ok {
			t.Fatalf("%s: answer type %T, want BoardTargetsDecision", name, out)
		}
		if len(dec.Chosen) != 2 {
			t.Fatalf("%s: chosen = %d refs, want MinTargets", name, len(dec.Chosen))
		}
		if dec.Chosen[0].ID != "c1" || dec.Chosen[1].ID != "c2" {
			t.Fatalf("%s: chosen ids = %v,%v, want the offered prefix c1,c2", name, dec.Chosen[0].ID, dec.Chosen[1].ID)
		}
	}
}

func TestMockConstraintSentencesAreTheTrigger(t *testing.T) {
	for _, desc := range []string{
		"All chosen targets must share a property.",
		"No two chosen targets may share a property.",
		"Options that share a group are mutually exclusive.",
	} {
		if !setConstraintBound(desc) {
			t.Fatalf("sentence not recognised: %q", desc)
		}
	}
	for _, desc := range []string{"", "Choose 1 to 2.", "Total value must not exceed 3."} {
		if setConstraintBound(desc) {
			t.Fatalf("unrelated description read as constrained: %q", desc)
		}
	}
}

func TestMockUnconstrainedBoardTargetsKeepTheirPolicy(t *testing.T) {
	in := boardTargetInputFor("c1", "c2", "c3", "c4", "c5")
	in.MinTargets, in.MaxTargets = 2, 2
	in.Presentation.Description = "Choose 2 to 2."
	out := NewFirstLegalClient().answerBoardTargets(in)
	dec := out.(mb.BoardTargetsDecision)
	if len(dec.Chosen) != 2 || dec.Chosen[0].ID != "c1" || dec.Chosen[1].ID != "c2" {
		t.Fatalf("first-legal chosen = %v, want the first MinTargets candidates", dec.Chosen)
	}
	// Random mode must stay a random (sorted) sample: assert it answers two
	// distinct candidates, never more than MinTargets.
	out = NewSeededRandomClient(7).answerBoardTargets(in)
	dec = out.(mb.BoardTargetsDecision)
	if len(dec.Chosen) != 2 {
		t.Fatalf("random chosen = %d refs, want 2", len(dec.Chosen))
	}
	if dec.Chosen[0].ID == dec.Chosen[1].ID {
		t.Fatalf("random chosen repeated a candidate: %v", dec.Chosen)
	}
}
