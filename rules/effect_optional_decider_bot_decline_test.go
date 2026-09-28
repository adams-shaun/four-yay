package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Only the marked Effect OptionalDecider$ election defaults to decline on
// the unattended bot path. Printed triggers and Miracle retain their former
// accept policy.
//
// The carrier is the same corpus card Beck // Call round 1's test drives, but
// the election here is answered by the BOT's own answer path, never an
// explicit intent.
func TestEffectOptionalDeciderBotDeclines(t *testing.T) {
	t.Parallel()
	beck := corpusAlternativeCard(t, "Beck")
	bear := card(t, "Name:Test FreeBear\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	e := handEngine(t, beck, bear, bear)
	e.G.Players[0].Pool[state.MG], e.G.Players[0].Pool[state.MU] = 1, 1

	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)

	// Precondition: the optional registration is armed, so a creature entry
	// really does reach the election.
	if reg := changesZoneOptionalRegistration(e); reg == nil || reg.OptionalSpec != "You" {
		t.Fatalf("precondition: the OptionalDecider$ registration is not armed: %+v", e.G.Delayed)
	}

	drawsBefore := countDraw(e)
	e.askPriority(0)
	castFirst(t, e, "cast")
	ask := passPriorityUntil(t, e, decision.KTriggerOptional)
	if ask == nil || ask.Player != 0 || !ask.EffectOptional {
		t.Fatalf("precondition: Effect optional ask not posed to seat 0: %+v", ask)
	}
	if optionIndexOfKind(t, ask, "no") < 0 {
		t.Fatalf("precondition: optional ask offers no \"no\": %+v", ask.Options)
	}

	// The no-host path: the BOT's own policy answers, exactly as the fuzz
	// gate and the unattended demo seat do. It must deterministically pick
	// the decline.
	bot := newTestBot(7)
	in := bot.answer(e, ask)
	if len(in.Choices) != 1 || in.Choices[0] != optionIndexOfKind(t, ask, "no") {
		t.Fatalf("bot answered the election with %v, want the deterministic decline (the no option)", in.Choices)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("submit the bot's decline: %v", err)
	}
	passUntilStackEmpty(t, e, 12)
	if got := countDraw(e); got != drawsBefore {
		t.Fatalf("no-host election ran the body mandatorily: drew %d cards", got-drawsBefore)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("declined election left the stack wedged: %v", e.G.Stack)
	}
	if !hasRegistration(e, "ChangesZone") {
		t.Fatalf("declined firing did not complete cleanly (registration gone): %+v", e.G.Delayed)
	}
}

// changesZoneOptionalRegistration returns the Beck-shaped recurring
// ChangesZone registration carrying an OptionalDecider$ spec, or nil.
func changesZoneOptionalRegistration(e *Engine) *state.DelayedTrigger {
	for i := range e.G.Delayed {
		if e.G.Delayed[i].EventMode == "ChangesZone" && e.G.Delayed[i].OptionalSpec != "" {
			return &e.G.Delayed[i]
		}
	}
	return nil
}
