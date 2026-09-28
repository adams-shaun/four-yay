package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestSameControllerTargetDecisionRejectsMixedControllers covers the
// resolution/trigger ask, where there is no pending cast. The same metadata
// also lets Clamp repair a bot answer rather than repeatedly submitting a
// mixed-controller set.
func TestSameControllerTargetDecisionRejectsMixedControllers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Barrin's Spite")
	if !ok {
		t.Fatal("Barrin's Spite missing from corpus")
	}
	sa := card.Faces[0].SpellAbility()
	e := newSeats(t, 2)
	for i := 0; i < 2; i++ {
		bearPermanent(t, e, 0)
		bearPermanent(t, e, 1)
	}
	e.askTarget(0, 0, sa)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("fixture precondition: target decision = %+v", d)
	}
	if !d.TargetsWithSameController {
		t.Fatal("fixture precondition: same-controller metadata missing")
	}
	var first, second int = -1, -1
	for _, o := range d.Options {
		if first < 0 {
			first = o.Index
			continue
		}
		if o.Controller != d.Options[first].Controller {
			second = o.Index
			break
		}
	}
	if first < 0 || second < 0 {
		t.Fatalf("fixture precondition: options do not span controllers: %+v", d.Options)
	}
	mixed := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{first, second}}
	if err := d.Validate(mixed); err == nil {
		t.Fatalf("mixed-controller target answer unexpectedly validated: options=%+v choices=%+v", d.Options, mixed.Choices)
	}
	clamped := botpolicy.Clamp(d, decision.Intent{Seq: mixed.Seq, Player: mixed.Player,
		Choices: append([]int(nil), mixed.Choices...)})
	if err := d.Validate(clamped); err != nil {
		t.Fatalf("Clamp produced an invalid target answer: %v (%+v)", err, clamped)
	}
	if err := e.Submit(mixed); err == nil {
		t.Fatal("mixed-controller target answer unexpectedly submitted")
	}
	if e.Pending() == nil {
		t.Fatal("rejected target answer consumed the pending decision")
	}
}
