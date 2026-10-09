package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// answerAt is the answer list of step i, or nil.
func answerAt(it oraclegen.Item, i int) []oraclegen.XAnswer {
	if i < 0 || i >= len(it.XAnswers) {
		return nil
	}
	return it.XAnswers[i]
}

// TestLevelBPerPlayerTargetAsk: Kaya, Spirits' Justice's -2 poses the
// per-opponent "exile up to one target creature that player controls" ask,
// which the engine records as a grouped KChoose. The generated item must
// close XMage's target ask with a target skip on the TARGET queue, not the
// [choice_skip] the generic choose routing used to emit.
func TestLevelBPerPlayerTargetAsk(t *testing.T) {
	it := levelBItem(t, "Kaya, Spirits' Justice", "activate#0.2")
	if len(it.Scenario.Steps) != 2 || it.Scenario.Steps[0].Op != "activate" || it.Scenario.Steps[1].Op != "resolve" {
		t.Fatalf("steps = %v, want activate then resolve", it.Scenario.Steps)
	}
	cast := it.Scenario.Steps[0]
	if strings.Join(cast.Targets, "|") != "p0:Grizzly Bears" {
		t.Fatalf("precondition: activate targets = %v, want the controller's own creature", cast.Targets)
	}
	as := answerAt(it, 1)
	if len(as) != 1 || as[0].Kind != "target" || as[0].Value != "[target_skip]" {
		t.Fatalf("resolve answers = %+v, want one target skip on the target queue", as)
	}
	for i, list := range it.XAnswers {
		for _, a := range list {
			if a.Kind == "choice" && a.Value == "[choice_skip]" {
				t.Fatalf("step %d still scripts a choice skip: %+v", i, list)
			}
		}
	}
}

// TestLevelBTransformedTargetRef: Jill, Shiva's Dominant's activation returns
// her transformed (Shiva, Warden of Ice), whose Saga chapter targets a
// creature. The pick's option label names the current face while its ref is
// the front name, so the answer must be the exact ref (the driver binds it by
// identity) rather than the front name, which matches no XMage object.
func TestLevelBTransformedTargetRef(t *testing.T) {
	it := levelBItem(t, "Jill, Shiva's Dominant", "activate#0.0")
	as := answerAt(it, 1)
	if len(as) != 1 || as[0].Kind != "target" {
		t.Fatalf("resolve answers = %+v, want one target", as)
	}
	if as[0].Value != "p0:Jill, Shiva's Dominant" {
		t.Fatalf("target answer = %q, want the exact ref p0:Jill, Shiva's Dominant (the alias selects the transformed object)", as[0].Value)
	}
	if !strings.Contains(as[0].Value, ":") {
		t.Fatal("the answer is not a scenario ref the driver can bind")
	}
}

// TestLevelBTapOrUntapAnswer: Inverted Iceberg's back-face attack trigger
// targets an artifact or creature and then asks XMage's chooseUse to tap or
// untap it. The target is the transformed permanent (answered by its exact
// ref) and the election is the boolean XMage asks, not gorge's option label.
func TestLevelBTapOrUntapAnswer(t *testing.T) {
	it := levelBItem(t, "Inverted Iceberg", "trigger#1.0")
	first := answerAt(it, 0)
	if len(first) != 1 || first[0].Kind != "target" || first[0].Value != "p0:Inverted Iceberg" {
		t.Fatalf("attack answers = %+v, want the exact ref of the transformed permanent", first)
	}
	second := answerAt(it, 1)
	if len(second) != 1 || second[0].Kind != "choice" || (second[0].Value != "yes" && second[0].Value != "no") {
		t.Fatalf("resolve answers = %+v, want the tap/untap boolean", second)
	}
	for _, a := range second {
		if strings.HasPrefix(a.Value, "Untap ") || strings.HasPrefix(a.Value, "Tap ") {
			t.Fatalf("tap/untap answer is gorge's label, not XMage's boolean: %+v", a)
		}
	}
}

// TestLevelBSurveyMechanQueueOrder pins the generator input the driver's
// later-player hiding fixes: the activation names the creature (the any-target
// damage slot) before the player (the TargetPlayer draw slot), and scripts no
// separate answers.
func TestLevelBSurveyMechanQueueOrder(t *testing.T) {
	it := levelBItem(t, "Survey Mechan", "activate#0.0")
	if len(it.Scenario.Steps) != 2 || it.Scenario.Steps[0].Op != "activate" {
		t.Fatalf("steps = %v, want activate then resolve", it.Scenario.Steps)
	}
	if got := strings.Join(it.Scenario.Steps[0].Targets, "|"); got != "p1:Grizzly Bears|p1" {
		t.Fatalf("activate targets = %q, want the creature then the player in slot order", got)
	}
	for i, list := range it.XAnswers {
		if len(list) != 0 {
			t.Fatalf("step %d carries answers %+v; the driver queues the step targets itself", i, list)
		}
	}
}
