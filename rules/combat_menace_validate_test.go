package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestMenaceLoneBlockerFailsDecisionValidate: the engine-posed declare-blockers
// decision folds Menace into every block option's published MinBlockers floor
// (CR 702.111b / CR 509.1a), and decision.Validate now rejects a lone blocker
// against it before Submit -- the client gets an error and re-answers, instead
// of the engine's Submit refusing a rules-ignorant answer.
func TestMenaceLoneBlockerFailsDecisionValidate(t *testing.T) {
	t.Parallel()
	e, brute := menaceBlockEngine(t)
	onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	onBoardCard(t, e, 0, corpusCard(t, "Runeclaw Bear"))
	d := declareAndAskBlockers(e, brute)
	if d == nil || d.Kind != decision.KBlockers || len(d.Options) < 2 {
		t.Fatalf("blockers decision = %+v, want at least two block options", d)
	}
	for _, o := range d.Options {
		if o.MinBlockers != 2 {
			t.Fatalf("option %+v does not publish the Menace floor (MinBlockers 2)", o)
		}
	}
	lone := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}
	if err := d.Validate(lone); err == nil {
		t.Fatal("Validate accepted a lone blocker on a Menace attacker")
	}
	both := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index, d.Options[1].Index}}
	if err := d.Validate(both); err != nil {
		t.Fatalf("Validate rejected a legal two-blocker team: %v", err)
	}
}
