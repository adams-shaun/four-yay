package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestAnimateChosenColorResolvesTheRealChooseColorAnswer is the end-to-end pin
// for Puca's Eye's real ETB chain: TrigDraw (draw a card) -> DBChooseColor
// (choose a colour) -> DBAnimate (Colors$ ChosenColor | OverwriteColors$ True).
// It drives the actual mid-resolution ChooseColor ask the card poses and
// answers GREEN -- deliberately not the first WUBRG option -- then reads the
// animation off the battlefield object.
//
// The unit pin (effects.TestAnimateChosenColorGrantsTheChosenColour) pre-sets
// o.ChosenColor; this test is the one that proves the colour the animation
// grants comes from a real ChooseColor answer recorded via the Choose event,
// which is the chain fixed in commit 73c9cc2e5 (parseAnimateGrant's
// ChosenColor guard). ChosenColor is NOT pre-set here.
func TestAnimateChosenColorResolvesTheRealChooseColorAnswer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Puca's Eye")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Puca's Eye", state.ZBattlefield)

	// PRECONDITION: the artifact is really on the battlefield, is colourless,
	// and carries no chosen colour until the card's own ask is answered. A
	// vacuous setup (wrong zone, a pre-existing green colour, or a stale
	// ChosenColor) must fail here, not slip past the assertion below.
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Puca's Eye not on the battlefield: %+v", o)
	}
	if got := e.Colors(id); got != "" {
		t.Fatalf("precondition: Puca's Eye is already coloured %q before any choice; the assertion below would be vacuous", got)
	}
	if got := e.G.Obj(id).ChosenColor; got != "" {
		t.Fatalf("precondition: ChosenColor = %q before the real ChooseColor ask (it must be recorded by the answer, not pre-set)", got)
	}

	// Drive the ETB trigger to the colour ask: putTriggersOnStack places the
	// ChangesZone trigger, priority passes let it resolve through TrigDraw,
	// and DBChooseColor then parks the resolution on a KChoose whose
	// ResumeKind is "choosecolor". Bounded so an engine stall fails the test
	// instead of hanging it. answerGreen picks GREEN, not the first WUBRG
	// option, so a first-colour fallback cannot produce the assertion below.
	answered := false
	for i := 0; i < 40 && !answered; i++ {
		if e.putTriggersOnStack() {
			continue
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch {
		case d.Kind == decision.KChoose && d.ResumeKind == "choosecolor":
			green := optionByLabel(d.Options, "Green")
			if green < 0 {
				t.Fatalf("choosecolor ask offers no Green option: %+v", d.Options)
			}
			submitChoices(t, e, green)
			answered = true
		case d.Kind == decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			submitChoices(t, e, idx)
		default:
			t.Fatalf("unexpected %s decision (resume %q) in Puca's Eye's chain: %+v", d.Kind, d.ResumeKind, d)
		}
	}
	if !answered {
		t.Fatal("the real ChooseColor ask was never posed and answered; the chain did not reach DBChooseColor")
	}
	// Let the chained DBAnimate's resolution finish (it runs immediately after
	// the answer; this drain is empty when it already has).
	passUntilStackEmpty(t, e, 20)

	// The answer must have been recorded as a real choice, and the chained
	// Animate must have overwritten the artifact's colours to the answered
	// GREEN (not the White a first-WUBRG fallback would give).
	if got := e.G.Obj(id).ChosenColor; got != "G" {
		t.Fatalf("ChosenColor = %q, want \"G\" (the answered colour)", got)
	}
	if got := e.Colors(id); got != "G" {
		t.Fatalf("Puca's Eye colours = %q, want \"G\" after choosing Green", got)
	}
}
