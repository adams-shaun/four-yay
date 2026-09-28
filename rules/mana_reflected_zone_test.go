package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A ManaReflected land with a real opponent land on the battlefield must be
// offered (and activatable) only while it is on the battlefield: CR 605.2a,
// a mana ability functions only while its source object is in the zone its
// ActivationZone$ names -- the battlefield when printed none is. Exotic
// Orchard (feedback 20260917T131507Z-8b95b5a9) was offered as "Activate ...
// for mana" while IN HAND, because the plain AB$ Mana branch gated on
// abilityZoneOK but the ManaReflected walk (considerReflected) did not.
func TestManaReflectedNotActivatableOutsideBattlefield(t *testing.T) {
	t.Parallel()
	orchard := "Name:Exotic Meadow\nManaCost:no cost\nTypes:Land\n" +
		"A:AB$ ManaReflected | Cost$ T | ColorOrType$ Color | Valid$ Land.OppCtrl | ReflectProperty$ Produce | SpellDescription$ {T}: Add one mana of any color that a land an opponent controls could produce.\n" +
		"Oracle:{T}: Add one mana of any color that a land an opponent controls could produce.\n"
	e, cfg, id := newFixtureDeck(t, 42, orchard)
	// A real reflected-mana candidate for seat 0's opponent, on the
	// battlefield for EVERY stage below: a no-offer in hand or graveyard can
	// then only be the zone gate, never a missing candidate set.
	moveSeeded(t, e, 1, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n", state.ZBattlefield)
	e.Advance()
	if hasActivateOption(e, id) {
		t.Fatalf("activate offered while the reflected land is in hand")
	}
	// Graveyard: still no offer.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard})
	e.pending = nil
	e.Advance()
	if hasActivateOption(e, id) {
		t.Fatalf("activate offered while the reflected land is in the graveyard")
	}
	// Battlefield: the option IS offered -- the candidate set was never the
	// blocker, only the zone.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZBattlefield})
	e.pending = nil
	e.Advance()
	if !hasActivateOption(e, id) {
		t.Fatalf("activate not offered on the battlefield (options %+v)", e.Pending().Options)
	}
	replayCheck(t, e, cfg)
}

// The zone gate is per-ability, not per-API: an AB$ Mana whose printed
// ActivationZone$ names the hand (a Spirit-Guide shape) must still be
// offered from hand after the ManaReflected branch started gating too.
func TestManaActivationZoneHandStillOffered(t *testing.T) {
	t.Parallel()
	src := "Name:Hand Guide\nManaCost:no cost\nTypes:Creature Spirit\nPT:1/1\n" +
		"A:AB$ Mana | Cost$ 0 | ActivationZone$ Hand | Produced$ G | SpellDescription$ Add {G}.\n" +
		"Oracle:Add {G}.\n"
	e, cfg, id := newFixtureDeck(t, 43, src)
	e.Advance()
	if !hasActivateOption(e, id) {
		t.Fatalf("ActivationZone$ Hand mana ability not offered from hand")
	}
	replayCheck(t, e, cfg)
}
