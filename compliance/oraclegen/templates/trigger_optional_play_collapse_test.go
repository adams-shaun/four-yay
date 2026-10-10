package templates

import (
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// pairedOptionalPlays counts the trigger-level optional booleans that the
// Play decision of the same resolution follows at once, from gorge's own
// recorded decisions of the generated scenario.
func pairedOptionalPlays(t *testing.T, it oraclegen.Item) int {
	t.Helper()
	res, ok := oraclegen.PlaysThrough(loadGenRegistry(t), it.Scenario)
	if !ok {
		t.Fatalf("%s: gorge cannot play the scenario", it.ID)
	}
	n := 0
	for i := 0; i+1 < len(res.Decisions); i++ {
		a, b := res.Decisions[i], res.Decisions[i+1]
		if a.GorgeKind == "trigger_optional" && a.Resume == "optional" && b.Resume == "play" {
			n++
		}
	}
	return n
}

func answerKinds(as []oraclegen.XAnswer) string {
	var parts []string
	for _, a := range as {
		parts = append(parts, fmt.Sprintf("%s %s", a.Kind, a.Value))
	}
	return strings.Join(parts, ", ")
}

// TestTriggerOptionalPlayIsOneXMageAsk: Forge models a trigger's single
// "you may cast target card" twice (OptionalDecider$ on the trigger, Optional$
// on the Play it executes); gorge poses both, XMage's MayCastTargetCardEffect
// poses ONE chooseUse. The generated answers must carry the play decision's
// boolean once per trigger, never the trigger-level "yes" ahead of it: that
// "yes" was consumed by the "Cast Shock?" ask, which cast the card and then
// met the next trigger's queued Shock target at its own any-target ask
// ("Targets list was setup by addTarget with [Shock], but not used").
func TestTriggerOptionalPlayIsOneXMageAsk(t *testing.T) {
	for _, card := range []string{"Seifer Almasy", "Efreet Flamepainter"} {
		it := levelBItem(t, card, "trigger#0.0")
		if card == "Seifer Almasy" {
			it = levelBItem(t, card, "trigger#0.1")
		}
		// Double strike: two combat damage steps, two triggers.
		if got := pairedOptionalPlays(t, it); got != 2 {
			t.Fatalf("%s: precondition: %d optional+play pairs recorded, want 2", card, got)
		}
		as := answerAt(it, 1)
		if got, want := answerKinds(as), "target Shock, target Shock, choice no, choice no"; got != want {
			t.Errorf("%s: step-1 answers = %q, want %q", card, got, want)
		}
	}
}

// TestTriggerOptionalPlayWithTwoMaysKeepsBoth: Neera, Wild Mage's text has
// two "may"s ("you may put it on the bottom ... You may cast that card"),
// which XMage poses as two chooseUse asks (an optional trigger, then the
// cast), so the pair stays as recorded: yes then the play's no.
func TestTriggerOptionalPlayWithTwoMaysKeepsBoth(t *testing.T) {
	it := levelBItem(t, "Neera, Wild Mage", "trigger#0.0")
	if got := pairedOptionalPlays(t, it); got != 1 {
		t.Fatalf("precondition: %d optional+play pairs recorded, want 1", got)
	}
	if got, want := answerKinds(answerAt(it, 1)), "choice yes, choice no"; got != want {
		t.Errorf("step-1 answers = %q, want %q", got, want)
	}
}

// TestPlayWithoutTriggerOptionalIsUnchanged: The Dawning Archaic's Play
// carries only Optional$, so there is no trigger-level boolean to drop and its
// one "no" stays (its verdict row agrees).
func TestPlayWithoutTriggerOptionalIsUnchanged(t *testing.T) {
	it := levelBItem(t, "The Dawning Archaic", "trigger#0.0")
	if got := pairedOptionalPlays(t, it); got != 0 {
		t.Fatalf("precondition: %d optional+play pairs recorded, want 0", got)
	}
	if got, want := answerKinds(answerAt(it, 1)), "choice no"; got != want {
		t.Errorf("step-1 answers = %q, want %q", got, want)
	}
}
