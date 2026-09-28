package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// api:Effect Triggers$ bodies carrying OptionalDecider$ (Beck's "whenever a
// creature enters this turn, you may draw a card"). Before this landed such a
// trigger was withheld with a loud "unmodelled Effect trigger OptionalDecider$"
// Note, because registering it would fire the body MANDATORILY -- the opposite
// of the card text. It is now registered like any other Effect trigger and the
// election is posed when the minted ability resolves (rules' resolveTop, the
// CR 603.5 optional gate), to the seat the card names.
//
// The carrier is the real corpus card Beck (Beck // Call), whose
// `A:SP$ Effect | Triggers$ CreatureEntered` names the optional ChangesZone
// body. Its registration is an EffectRepeat one, so it survives each firing
// and the SAME game exercises both branches: an accepted election runs the
// body (a draw), a declined one does not.
func TestEffectOptionalDeciderElectionIsPosedAtResolution(t *testing.T) {
	t.Parallel()
	beck := corpusAlternativeCard(t, "Beck")
	bear := card(t, "Name:Test FreeBear\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	e := handEngine(t, beck, bear, bear)
	e.G.Players[0].Pool[state.MG], e.G.Players[0].Pool[state.MU] = 1, 1

	// Cast Beck (the front half): its Effect resolves and arms the recurring
	// "whenever a creature enters this turn" registration.
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)

	// Precondition: the registration reached the delayed set with the
	// OptionalDecider$ spec the card names. Without this the later firings
	// could not happen and the assertions below would pass vacuously.
	var reg *state.DelayedTrigger
	for i := range e.G.Delayed {
		if e.G.Delayed[i].EventMode == "ChangesZone" {
			reg = &e.G.Delayed[i]
		}
	}
	if reg == nil {
		t.Fatalf("no ChangesZone delayed registration after the Effect resolved: %+v", e.G.Delayed)
	}
	if reg.Trigger != "CreatureEntered" || reg.Execute != "TrigDraw" {
		t.Fatalf("registration = %+v, want Trigger CreatureEntered / Execute TrigDraw", *reg)
	}
	if reg.OptionalSpec != "You" {
		t.Fatalf("registration OptionalSpec = %q, want the card's named decider \"You\"", reg.OptionalSpec)
	}
	// The withheld-with-a-Note shape must be gone: the effect was registered,
	// not dropped.
	if notes := effectNotesContaining(e, "unmodelled Effect trigger OptionalDecider$"); len(notes) != 0 {
		t.Fatalf("the optional Effect trigger was still withheld: %v", notes)
	}

	// FIRST FIRING -- ACCEPT. Cast the first free creature; its entry fires
	// the registration. The pending decision must be the CR 603.5 yes/no,
	// posed to the seat the card names ("You" = seat 0).
	drawsBefore := countDraw(e)
	e.askPriority(0)
	castFirst(t, e, "cast")
	ask := passPriorityUntil(t, e, decision.KTriggerOptional)
	if ask.Player != 0 || !ask.EffectOptional {
		t.Fatalf("optional ask = %+v, want the card's named decider (seat 0) and Effect marker", ask)
	}
	if idx := optionIndexOfKind(t, ask, "yes"); idx < 0 {
		t.Fatalf("optional ask offers no yes: %+v", ask.Options)
	}
	submitChoices(t, e, optionIndexOfKind(t, ask, "yes"))
	passUntilStackEmpty(t, e, 12)
	if got := countDraw(e); got != drawsBefore+1 {
		t.Fatalf("accepted election drew %d cards, want exactly one", got-drawsBefore)
	}
	// The registration is an EffectRepeat one and survives its firing.
	if !hasRegistration(e, "ChangesZone") {
		t.Fatalf("the accepted firing consumed the recurring Effect registration: %+v", e.G.Delayed)
	}

	// SECOND FIRING -- DECLINE. Cast the second free creature; the same
	// registration fires again, and a "no" must not run the body while the
	// resolution still completes (the stack drains).
	drawsBefore = countDraw(e)
	e.askPriority(0)
	castFirst(t, e, "cast")
	ask = passPriorityUntil(t, e, decision.KTriggerOptional)
	if ask.Player != 0 || !ask.EffectOptional {
		t.Fatalf("second optional ask = %+v, want seat 0 and Effect marker", ask)
	}
	submitChoices(t, e, optionIndexOfKind(t, ask, "no"))
	passUntilStackEmpty(t, e, 12)
	if got := countDraw(e); got != drawsBefore {
		t.Fatalf("declined election drew %d cards, want none", got-drawsBefore)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("declined optional trigger left the stack wedged: %v", e.G.Stack)
	}
	if !hasRegistration(e, "ChangesZone") {
		t.Fatalf("declined firing did not complete cleanly (registration gone): %+v", e.G.Delayed)
	}
}

// hasRegistration reports whether a delayed registration of the given event
// mode is still pending.
func hasRegistration(e *Engine, eventMode string) bool {
	for i := range e.G.Delayed {
		if e.G.Delayed[i].EventMode == eventMode {
			return true
		}
	}
	return false
}
