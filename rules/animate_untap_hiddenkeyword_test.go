// Animate's HiddenKeywords$ grant of CR 611.2b's next-untap-step restriction
// is the SECOND delivery site of the same runtime keyword Pump/PumpAll
// delivers (Frost Lynx). Both route through the ONE shared reader
// cards.IsHiddenUntapNextStepKeyword; the Pump site stamps the one-shot state
// flag at grant time, and the Animate registration path must do the same or
// the derived keyword reaches the layer-6 list but nothing consumes it.
//
// The corpus carries this exact delivery 0 times (measured:
// `grep -rlE "HiddenKeywords.*doesn't untap" .cards/cardsfolder` = 0;
// `grep -rlE "AB\$ Animate.*HiddenKeywords" .cards/cardsfolder` = 0), so the
// test authors its `Animate` script inline (never a committed corpus .txt,
// per the licensing rule) and drives the real engine path against a real
// corpus creature target.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// animateUntapSrc is a freely-authored fixture sorcery (never a corpus .txt)
// whose single SpellAbility is the exact Animate + HiddenKeywords$ shape this
// ticket arms. It carries no P/T/type rider, so the only behaviour it
// registers is the keyword grant -- the layer-6 list and the grant-time
// stamp.
const animateUntapSrc = "Name:Animate Untap Test\nManaCost:1 U\nTypes:Sorcery\n" +
	"A:SP$ Animate | Cost$ 0 | ValidTgts$ Creature | HiddenKeywords$ This card doesn't untap during your next untap step. | SpellDescription$ Target creature doesn't untap during your next untap step.\n" +
	"Oracle:x\n"

// animateUntapEngine builds a two-seat engine whose seat-0 deck leads with the
// synthetic Animate sorcery and the authored untap artifact (untapWandSrc,
// shared with the Pump test's fixture harness), and whose seat-1 deck leads
// with the REAL corpus Centaur Courser (the animate target). It returns the
// engine at a fresh turn-1 priority ask with every card in its opening zone.
func animateUntapEngine(t *testing.T, reg *cards.Registry) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	centaur, ok := reg.Lookup("Centaur Courser")
	if !ok {
		t.Fatal("corpus fixture: Centaur Courser missing")
	}
	animate := card(t, animateUntapSrc)
	wand := card(t, untapWandSrc)
	cfg := seatZeroStart(Config{Seed: 7311, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{animate, wand}, mountainDeck(t, 38)...),
			append([]*cards.Card{centaur}, mountainDeck(t, 39)...),
		},
		Tokens: map[string]*cards.Card{},
	})
	e := New(cfg)
	e.Advance()
	animID := moveByName(t, e, 0, "Animate Untap Test", state.ZHand)
	wandID := moveByName(t, e, 0, "Untap Wand", state.ZBattlefield)
	centaurID := moveByName(t, e, 1, "Centaur Courser", state.ZBattlefield)
	return e, animID, wandID, centaurID
}

// TestAnimateHiddenUntapNextStepIsRead is the end-to-end leaf for the Animate
// delivery. It drives cast -> resolve -> the target's controller's next untap
// step -> the following untap step, and asserts:
//
//  1. the shared reader matches the sentence with/without the HIDDEN marker
//     and rejects the overlapping non-"next" riders (the reader contract);
//  2. the Animate grant stamps the one-shot window (the fix);
//  3. the animated creature does NOT untap at the next untap step;
//  4. it DOES untap at the following one (the window is one-shot);
//  5. an untap EFFECT untaps it while the window is armed (CR 611.2b names
//     only the untap step, so the gate must not live in effects.TryUntap).
func TestAnimateHiddenUntapNextStepIsRead(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, animID, wandID, centaurID := animateUntapEngine(t, reg)

	// Preconditions: each card is where the flow needs it, and the target is
	// an untapped battlefield creature so a later "stays tapped" is the
	// keyword's doing, not an incidental tap/zone gate.
	if e.G.Obj(animID).Zone != state.ZHand {
		t.Fatalf("precondition: Animate Untap Test is in %v, not hand", e.G.Obj(animID).Zone)
	}
	if o := e.G.Obj(centaurID); o == nil || o.Zone != state.ZBattlefield || o.Tapped || !e.IsCreature(centaurID) {
		t.Fatalf("precondition: Centaur Courser state wrong: %+v creature=%v", o, e.IsCreature(centaurID))
	}

	// (1) The ONE shared reader is exact-match: the runtime sentence is
	// recognised with and without the leading HIDDEN marker, and neither the
	// Undiscovered Paradise rider ("During your next untap step, ...") nor
	// Ethereal Grasp's no-"next" sentence ("This creature doesn't untap
	// during your untap step") may borrow the one-shot meaning -- a
	// whole-sentence match.
	if !cards.IsHiddenUntapNextStepKeyword("HIDDEN This card doesn't untap during your next untap step.") {
		t.Fatal("the shared reader does not recognise the HIDDEN runtime untap sentence")
	}
	if !cards.IsHiddenUntapNextStepKeyword("This card doesn't untap during your next untap step.") {
		t.Fatal("the shared reader does not recognise the bare untap sentence")
	}
	if cards.IsHiddenUntapNextStepKeyword("During your next untap step, as you untap your permanents, return this card to its owner's hand.") {
		t.Fatal("the shared reader matched Undiscovered Paradise's rider (the phrase was not matched whole)")
	}
	if cards.IsHiddenUntapNextStepKeyword("This creature doesn't untap during your untap step") {
		t.Fatal("the shared reader matched Ethereal Grasp's no-\"next\" sentence, which is a different (perpetual) restriction")
	}

	// Cast the authored Animate, choosing the corpus Centaur Courser at its
	// KTarget ask, then drain the stack. Assert each step so an uncast spell
	// or an unoffered target is a loud failure.
	addMana(t, e, 0, "1U")
	castCardNow(t, e, "Animate Untap Test")
	td := passToTargetAsk(t, e)
	tgt := -1
	for _, o := range td.Options {
		if o.Obj == centaurID {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("Animate did not offer Centaur Courser as a target: %+v", td.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 40)

	// (2) The fix: the grant stamped the one-shot window. The derived keyword
	// alone would be inert, so assert the FLAG, not just the keyword list.
	if e.G.Obj(animID).Zone != state.ZGraveyard {
		t.Fatalf("Animate resolved to %v, not the graveyard", e.G.Obj(animID).Zone)
	}
	if !e.G.Obj(centaurID).CantUntapNextStep {
		t.Fatalf("the Animate HiddenKeywords$ grant did not arm the one-shot window; derived keywords = %v",
			e.Derived(centaurID).Keywords)
	}

	// (5) An untap EFFECT untaps it while the window is armed: the skip is a
	// turn-step gate, never a TryUntap gate. Run BEFORE any untap step
	// consumes the window.
	e.beginActivation(0, abilityOption(t, e, wandID, 0))
	e.resolveTop()
	if e.G.Obj(centaurID).Tapped {
		t.Fatal("an untap effect could not untap a creature carrying the next-untap-step keyword")
	}
	if !e.G.Obj(centaurID).CantUntapNextStep {
		t.Fatal("the untap effect wrongly consumed the one-shot window (only the untap step may)")
	}

	// Tap it so the next untap step has a real skip to show.
	e.emit(events.Event{Kind: events.Tap, Obj: centaurID})
	if !e.G.Obj(centaurID).Tapped {
		t.Fatal("precondition: the tap event did not tap the creature")
	}

	// (3) p1's next untap step (turn 2) skips the untap.
	driveToStepAny(t, e, 2, 1, state.StepUpkeep)
	if !e.G.Obj(centaurID).Tapped {
		t.Fatal("the animated creature untapped during its controller's next untap step (the keyword was not read)")
	}
	if e.G.Obj(centaurID).CantUntapNextStep {
		t.Fatal("the one-shot window was not consumed at its first untap step")
	}

	// (4) The FOLLOWING untap step (turn 4) untaps it normally: the window is
	// one-shot, not permanent.
	driveToStepAny(t, e, 4, 1, state.StepUpkeep)
	if e.G.Obj(centaurID).Tapped {
		t.Fatal("the creature never untapped again: the window was not one-shot")
	}
}
