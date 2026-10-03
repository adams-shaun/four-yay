package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// untapWandSrc is a freely-authored fixture artifact (never a corpus .txt, per
// the licensing rule): the untap-EFFECT contrast for the one-shot keyword
// window. Activating it untaps every creature, the exert test's
// TestExertedCreatureSkipsUntapStepButEffectsUntapIt pattern.
const untapWandSrc = "Name:Untap Wand\nManaCost:1\nTypes:Artifact\nOracle:x\n" +
	"A:AB$ UntapAll | Cost$ 0 | ValidCards$ Creature | SpellDescription$ Untap all creatures.\n"

// frostLynxEngine builds a two-seat engine whose seat-0 deck leads with the
// REAL corpus Frost Lynx and a synthetic untap artifact, and whose seat-1 deck
// leads with the REAL corpus Centaur Courser (a vanilla 3/3 for the trigger to
// tap), padded with authored basic Mountains. It returns the engine at a fresh
// turn-1 priority ask with all three cards in their decks' opening zones.
func frostLynxEngine(t *testing.T, reg *cards.Registry) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	frost, ok := reg.Lookup("Frost Lynx")
	if !ok {
		t.Fatal("corpus fixture: Frost Lynx missing")
	}
	centaur, ok := reg.Lookup("Centaur Courser")
	if !ok {
		t.Fatal("corpus fixture: Centaur Courser missing")
	}
	wand := card(t, untapWandSrc)
	cfg := seatZeroStart(Config{Seed: 4242, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{frost, wand}, mountainDeck(t, 38)...),
			append([]*cards.Card{centaur}, mountainDeck(t, 39)...),
		},
		Tokens: map[string]*cards.Card{},
	})
	e := New(cfg)
	e.Advance()
	frostID := moveByName(t, e, 0, "Frost Lynx", state.ZHand)
	wandID := moveByName(t, e, 0, "Untap Wand", state.ZBattlefield)
	centaurID := moveByName(t, e, 1, "Centaur Courser", state.ZBattlefield)
	return e, frostID, wandID, centaurID
}

// TestFrostLynxHiddenUntapKeywordIsRead is the ticket's end-to-end leaf on the
// REAL corpus Frost Lynx. Its enter trigger's Pump sub-body grants
// `KW$ HIDDEN This card doesn't untap during your next untap step.` as runtime
// keyword TEXT (never a printed K: line). The test drives the whole flow
// through the engine -- cast, resolve, the controller's next untap step and the
// one after -- and asserts:
//
//  1. the tap half runs (control: the trigger itself is fine);
//  2. the target does NOT untap at its controller's next untap step (the
//     runtime keyword is read);
//  3. it DOES untap at the following untap step (the window is one-shot,
//     consumed at use -- the crux of the ticket);
//  4. an untap EFFECT untaps it while the window is armed (CR 611.2b names
//     only the untap step, so the gate must not live in effects.TryUntap).
func TestFrostLynxHiddenUntapKeywordIsRead(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, frostID, wandID, centaurID := frostLynxEngine(t, reg)

	// Preconditions: the engine put each card where the flow needs it.
	if e.G.Obj(centaurID).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Centaur Courser is in %v, not the battlefield", e.G.Obj(centaurID).Zone)
	}
	if e.G.Obj(centaurID).Tapped {
		t.Fatal("precondition: Centaur Courser started tapped; the untap flow needs it untapped first")
	}
	// The one shared reader is exact-match: the runtime sentence is recognised
	// (with or without the HIDDEN marker), and Undiscovered Paradise's
	// overlapping "During your next untap step, ..." rider is NOT -- a whole-
	// sentence match, so a longer rider cannot borrow the restriction meaning.
	if !cards.IsHiddenUntapNextStepKeyword("HIDDEN This card doesn't untap during your next untap step.") {
		t.Fatal("the shared reader does not recognise the runtime untap sentence")
	}
	if !cards.IsHiddenUntapNextStepKeyword("This card doesn't untap during your next untap step.") {
		t.Fatal("the shared reader does not recognise the bare untap sentence")
	}
	if cards.IsHiddenUntapNextStepKeyword("During your next untap step, as you untap your permanents, return this card to its owner's hand.") {
		t.Fatal("the shared reader matched Undiscovered Paradise's rider (the phrase was not matched whole)")
	}
	// The rules reader delegates to that shared reader, so a future equivalent
	// spelling is one arm (the must-be-blocked control shape).
	if !parseHiddenKeyword("HIDDEN This card doesn't untap during your next untap step.").UntapNextStep {
		t.Fatal("rules' parseHiddenKeyword no longer recognises the runtime untap sentence")
	}
	addMana(t, e, 0, "CCU")
	// Cast Frost Lynx. The creature spell itself has no target; its enter
	// trigger does, so the target ask arrives only once the permanent is on the
	// battlefield and the trigger resolves.
	submitChoices(t, e, castOptionFor(t, e, frostID).Index)
	td := passUntilAskKind(t, e, decision.KTarget, 40)
	tgt := -1
	for _, o := range td.Options {
		if o.Obj == centaurID {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("the trigger did not offer Centaur Courser as a target: %+v", td.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 40)

	// (1) Control: the enter trigger tapped the target.
	if e.G.Obj(frostID).Zone != state.ZBattlefield {
		t.Fatalf("Frost Lynx resolved to %v, not the battlefield", e.G.Obj(frostID).Zone)
	}
	if !e.G.Obj(centaurID).Tapped {
		t.Fatal("the enter trigger did not tap the target (the control half failed)")
	}
	if !e.G.Obj(centaurID).CantUntapNextStep {
		t.Fatalf("the runtime keyword grant did not arm the one-shot window; derived keywords = %v",
			e.Derived(centaurID).Keywords)
	}

	// (4) An untap EFFECT untaps it while the window is armed: the skip is a
	// turn-step gate, never a TryUntap gate. This must run BEFORE any untap
	// step consumes the window.
	e.beginActivation(0, abilityOption(t, e, wandID, 0))
	e.resolveTop()
	if e.G.Obj(centaurID).Tapped {
		t.Fatal("an untap effect could not untap a creature carrying the next-untap-step keyword")
	}
	if !e.G.Obj(centaurID).CantUntapNextStep {
		t.Fatal("the untap effect wrongly consumed the one-shot window (only the untap step may)")
	}

	// Re-tap so the next untap step has a real skip to show.
	e.emit(events.Event{Kind: events.Tap, Obj: centaurID})

	// (2) p1's next untap step (turn 2) skips the untap.
	driveToStepAny(t, e, 2, 1, state.StepUpkeep)
	if !e.G.Obj(centaurID).Tapped {
		t.Fatal("the creature untapped during its controller's next untap step (the keyword was not read)")
	}
	if e.G.Obj(centaurID).CantUntapNextStep {
		t.Fatal("the one-shot window was not consumed at its first untap step")
	}

	// (3) The FOLLOWING untap step (turn 4) untaps it normally: the window is
	// one-shot, not permanent.
	driveToStepAny(t, e, 4, 1, state.StepUpkeep)
	if e.G.Obj(centaurID).Tapped {
		t.Fatal("the creature never untapped again: the window was not one-shot")
	}
}
