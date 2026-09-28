package rules

// CR 702.25b — the mana choke point. appendAvailableManaAbilities is the
// shared discovery walk for every zone (the offer walk, payment windows and
// activation rechecks), so its phased-out gate must reject exactly a
// phased-out BATTLEFIELD permanent without excluding hand/graveyard sources
// whose ActivationZone$ puts them there (CR 605.2a). This is a regression
// guard: an earlier round gated this walk with existsOnBattlefield (which
// requires Zone == ZBattlefield) and silently stopped offering and paying
// for every hand- and graveyard-functioning mana ability (Simian/Elvish
// Spirit Guide, Jack-o'-Lantern). Both directions are asserted here, on
// inline fixtures, so the only variable is the zone/flag.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// handGuide is a Spirit-Guide shape: an AB$ Mana the card text lets function
// from the hand.
const handGuide = "Name:Hand Guide\nManaCost:no cost\nTypes:Creature Spirit\nPT:1/1\n" +
	"A:AB$ Mana | Cost$ 0 | ActivationZone$ Hand | Produced$ G | SpellDescription$ Add {G}.\n" +
	"Oracle:Add {G}.\n"

// TestCR702PhasedOutBattlefieldManaAbilityNotPayable pins the phase-out
// direction of the choke point itself: a phased-out battlefield source yields
// no mana abilities through availableManaAbilities, so it can never be used
// to pay a cost. The same object while phased in is the control.
func TestCR702PhasedOutBattlefieldManaAbilityNotPayable(t *testing.T) {
	t.Parallel()
	e, _ := phasesGame(t, 601, "Forest")
	forest := moveSeededCard(t, e, 0, tokenReplCorpusCard(t, "Forest"), state.ZBattlefield)
	o := e.G.Obj(forest)
	if o == nil || o.Zone != state.ZBattlefield || o.PhasedOut {
		t.Fatalf("precondition: Forest not a phased-in battlefield permanent: %+v", o)
	}
	// Control: while phased in the choke point really does surface the
	// ability (a vacuous setup must fail loudly).
	if got := e.availableManaAbilities(0, forest); len(got) == 0 {
		t.Fatal("precondition: the phased-in Forest's mana ability was not discovered")
	}
	e.emit(events.Event{Kind: events.PhaseOut, Obj: forest, Amount: 1})
	if o := e.G.Obj(forest); o == nil || !o.PhasedOut {
		t.Fatal("precondition: the Forest was not phased out")
	}
	if got := e.availableManaAbilities(0, forest); len(got) != 0 {
		t.Fatalf("CR 702.25b: a phased-out permanent contributed %d mana abilities, want 0", len(got))
	}
}

// TestCR702HandManaAbilityStillPayablePhasedIn is the other direction: the
// choke point must not exclude a NON-battlefield source the card's
// ActivationZone$ lets function there. The object sits in hand for the whole
// test, so a no-offer can only be the zone gate, never a missing card.
func TestCR702HandManaAbilityStillPayablePhasedIn(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 602, handGuide)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Hand Guide not in hand: %+v", o)
	}
	if got := e.availableManaAbilities(0, id); len(got) == 0 {
		t.Fatal("CR 605.2a: a hand-functioning mana ability was excluded by the choke point")
	}
}
