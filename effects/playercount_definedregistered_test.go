package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// PlayerCountDefinedRegistered$<Property> -- the group of registered players
// -- was an unimplemented count head: the dispatch had no arm for the two
// spellings, so every body fell through to the documented fail-closed
// (0, false) verdict and the four corpus carriers' intervening-ifs could not
// be evaluated. These tests pin the eval-level behaviour; the rules package
// pins the real corpus cards end to end.

// TestPlayerCountDefinedRegisteredLifeExtremes pins the two life-extreme
// properties over the bare group (every living player, the controller
// INCLUDED) and the `.Other` group (living players minus the resolving
// controller). Knight of the Ebon Legion and Y'shtola both spell the bare
// form; Ludevic spells the `.Other` form.
func TestPlayerCountDefinedRegisteredLifeExtremes(t *testing.T) {
	h, c := fixtureHost(t) // 2 seats, controller 0
	h.lifeLost = map[state.PlayerID]int32{0: 2, 1: 5}

	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HighestLifeLostThisTurn"); !ok || got != 5 {
		t.Errorf("bare HighestLifeLostThisTurn = (%d, %v), want (5, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered.Other$HighestLifeLostThisTurn"); !ok || got != 5 {
		t.Errorf(".Other HighestLifeLostThisTurn = (%d, %v), want (5, true)", got, ok)
	}
	// The bare group includes the CONTROLLER: with seat 0 holding the higher
	// loss the bare extreme is seat 0's, while `.Other` reads seat 1's.
	h.lifeLost = map[state.PlayerID]int32{0: 6, 1: 2}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HighestLifeLostThisTurn"); !ok || got != 6 {
		t.Errorf("bare HighestLifeLostThisTurn (controller highest) = (%d, %v), want (6, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered.Other$HighestLifeLostThisTurn"); !ok || got != 2 {
		t.Errorf(".Other HighestLifeLostThisTurn (controller highest) = (%d, %v), want (2, true)", got, ok)
	}
	// Lowest is the mirror property.
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$LowestLifeLostThisTurn"); !ok || got != 2 {
		t.Errorf("bare LowestLifeLostThisTurn = (%d, %v), want (2, true)", got, ok)
	}
}

// TestPlayerCountDefinedRegisteredHasPropertyLostLifeThisTurn pins Ludevic's
// property: the count of group members who lost any life this turn. The bare
// group counts the controller too; `.Other` excludes them.
func TestPlayerCountDefinedRegisteredHasPropertyLostLifeThisTurn(t *testing.T) {
	h, c := fixtureHost(t)

	h.lifeLost = map[state.PlayerID]int32{0: 6, 1: 0}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HasPropertyLostLifeThisTurn"); !ok || got != 1 {
		t.Errorf("bare (controller only) = (%d, %v), want (1, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered.Other$HasPropertyLostLifeThisTurn"); !ok || got != 0 {
		t.Errorf(".Other (controller only) = (%d, %v), want (0, true)", got, ok)
	}

	h.lifeLost = map[state.PlayerID]int32{0: 0, 1: 3}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HasPropertyLostLifeThisTurn"); !ok || got != 1 {
		t.Errorf("bare (opponent only) = (%d, %v), want (1, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered.Other$HasPropertyLostLifeThisTurn"); !ok || got != 1 {
		t.Errorf(".Other (opponent only) = (%d, %v), want (1, true)", got, ok)
	}

	// Nobody lost life: a readable zero, not unresolvable.
	h.lifeLost = nil
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered.Other$HasPropertyLostLifeThisTurn"); !ok || got != 0 {
		t.Errorf("nobody lost life = (%d, %v), want (0, true)", got, ok)
	}

	// The same property on the RegisteredOpponents$ group (the already-
	// implemented arm now routes through the shared HasProperty helper):
	// kaito_bane_of_nightmares's carrier.
	h.lifeLost = map[state.PlayerID]int32{0: 0, 1: 4}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountRegisteredOpponents$HasPropertyLostLifeThisTurn"); !ok || got != 1 {
		t.Errorf("RegisteredOpponents HasPropertyLostLifeThisTurn = (%d, %v), want (1, true)", got, ok)
	}
}

// TestPlayerCountDefinedRegisteredCombatDamageProperty pins Lost Monarch of
// Ifnir's and Blitzball's property: the count of group members dealt combat
// damage this turn by a source matching the Forge spec, with the trailing
// threshold applied to the per-player hit count.
func TestPlayerCountDefinedRegisteredCombatDamageProperty(t *testing.T) {
	h, c := fixtureHost(t)
	zombie := mkCard(t, "Name:Zombie Fixture\nTypes:Creature Zombie\nPT:2/2\nOracle:x\n")
	dragon := mkCard(t, "Name:Dragon Fixture\nTypes:Creature Dragon\nPT:4/4\nOracle:x\n")
	self := c.Source
	h.combatHits = []CombatDamageHit{
		{Player: 1, Source: 7, Card: zombie, Controller: 1, Amount: 2},
	}

	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HasPropertywasDealtCombatDamageThisTurnBy Zombie GE1"); !ok || got != 1 {
		t.Errorf("Zombie GE1 = (%d, %v), want (1, true)", got, ok)
	}
	// A spec the hit does not match: no player satisfies it.
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HasPropertywasDealtCombatDamageThisTurnBy Dragon GE1"); !ok || got != 0 {
		t.Errorf("Dragon GE1 (no match) = (%d, %v), want (0, true)", got, ok)
	}
	// The `.Other` group excludes the controller; the hit is on seat 1 and
	// the controller is seat 0, so it still counts.
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered.Other$HasPropertywasDealtCombatDamageThisTurnBy Zombie GE1"); !ok || got != 1 {
		t.Errorf(".Other Zombie GE1 = (%d, %v), want (1, true)", got, ok)
	}
	// A hit on the CONTROLLER counts for the bare group but not `.Other`.
	h.combatHits = []CombatDamageHit{{Player: 0, Source: 7, Card: zombie, Controller: 1, Amount: 2}}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HasPropertywasDealtCombatDamageThisTurnBy Zombie GE1"); !ok || got != 1 {
		t.Errorf("bare hit on controller = (%d, %v), want (1, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered.Other$HasPropertywasDealtCombatDamageThisTurnBy Zombie GE1"); !ok || got != 0 {
		t.Errorf(".Other hit on controller = (%d, %v), want (0, true)", got, ok)
	}

	// The threshold applies PER PLAYER: two hits to seat 1 satisfy GE2 but
	// not GE3.
	two := []CombatDamageHit{
		{Player: 1, Source: 7, Card: zombie, Controller: 1, Amount: 2},
		{Player: 1, Source: 8, Card: zombie, Controller: 1, Amount: 1},
	}
	h.combatHits = two
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HasPropertywasDealtCombatDamageThisTurnBy Zombie GE2"); !ok || got != 1 {
		t.Errorf("Zombie GE2 (two hits) = (%d, %v), want (1, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HasPropertywasDealtCombatDamageThisTurnBy Zombie GE3"); !ok || got != 0 {
		t.Errorf("Zombie GE3 (two hits) = (%d, %v), want (0, true)", got, ok)
	}
	// A missing threshold means "any hit" (>= 1).
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HasPropertywasDealtCombatDamageThisTurnBy Zombie"); !ok || got != 1 {
		t.Errorf("Zombie (no threshold) = (%d, %v), want (1, true)", got, ok)
	}
	// A COMPOUND spec matches either alternative.
	h.combatHits = []CombatDamageHit{{Player: 1, Source: 9, Card: dragon, Controller: 1, Amount: 4}}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HasPropertywasDealtCombatDamageThisTurnBy Card.Self,Dragon GE1"); !ok || got != 1 {
		t.Errorf("Card.Self,Dragon GE1 (dragon) = (%d, %v), want (1, true)", got, ok)
	}
	// Card.Self anchors on the RESOLVING trigger's source: a hit by that very
	// object matches, a different dragon does not.
	h.combatHits = []CombatDamageHit{{Player: 1, Source: self, Card: zombie, Controller: 0, Amount: 2}}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HasPropertywasDealtCombatDamageThisTurnBy Card.Self,Dragon GE1"); !ok || got != 1 {
		t.Errorf("Card.Self anchor (self hit) = (%d, %v), want (1, true)", got, ok)
	}
	h.combatHits = []CombatDamageHit{{Player: 1, Source: self + 100, Card: zombie, Controller: 0, Amount: 2}}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HasPropertywasDealtCombatDamageThisTurnBy Card.Self,Dragon GE1"); !ok || got != 0 {
		t.Errorf("Card.Self anchor (other source) = (%d, %v), want (0, true)", got, ok)
	}

	// Blitzball's RegisteredOpponents carrier routes through the same helper.
	h.combatHits = []CombatDamageHit{{Player: 1, Source: 7, Card: zombie, Controller: 1, Amount: 2}}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountRegisteredOpponents$HasPropertywasDealtCombatDamageThisTurnBy Creature.Zombie GE1"); !ok || got != 1 {
		t.Errorf("RegisteredOpponents combat property = (%d, %v), want (1, true)", got, ok)
	}

	// A bare `Permanent` base reads the captured source's battlefield zone
	// (the synthesized snapshot must carry Zone = ZBattlefield, or the base
	// fails closed): the captured source dealt combat damage as a
	// battlefield permanent.
	h.combatHits = []CombatDamageHit{{Player: 1, Source: 7, Card: zombie, Controller: 1, Amount: 2}}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HasPropertywasDealtCombatDamageThisTurnBy Permanent.Zombie GE1"); !ok || got != 1 {
		t.Errorf("Permanent.Zombie GE1 = (%d, %v), want (1, true)", got, ok)
	}

	// An empty argument (the property with no spec at all) is unresolvable,
	// never a match-everything.
	h.combatHits = []CombatDamageHit{{Player: 1, Source: 7, Card: zombie, Controller: 1, Amount: 2}}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered$HasPropertywasDealtCombatDamageThisTurnBy"); ok {
		t.Errorf("empty spec = (%d, %v), want unresolvable (0, false)", got, ok)
	}
}

// TestPlayerCountDefinedRegisteredHasPropertyAcrossGroups pins the class
// fix: the same HasPropertyLostLifeThisTurn property must resolve on the
// Players$ and Opponents$ arms too (reapers_scythe, strefan_maurer_progenitor,
// belbe_corrupted_observer), not just the new group -- before the fix those
// arms fell through to playerCountExtreme and reported (0, false), so the
// property resolved on one sibling group and failed closed on another. It
// also pins Forge's /Op suffix (belbe's /Twice) through applyCountOp.
func TestPlayerCountDefinedRegisteredHasPropertyAcrossGroups(t *testing.T) {
	h, c := fixtureHost(t) // 2 seats, controller 0
	h.lifeLost = map[state.PlayerID]int32{0: 0, 1: 3}

	for _, body := range []string{
		"Count$PlayerCountPlayers$HasPropertyLostLifeThisTurn",
		"Count$PlayerCountOpponents$HasPropertyLostLifeThisTurn",
		"Count$PlayerCountRegisteredOpponents$HasPropertyLostLifeThisTurn",
		"Count$PlayerCountDefinedRegistered$HasPropertyLostLifeThisTurn",
		"Count$PlayerCountDefinedRegistered.Other$HasPropertyLostLifeThisTurn",
	} {
		if got, ok := EvalCountOK(h, c, body); !ok || got != 1 {
			t.Errorf("%s = (%d, %v), want (1, true)", body, got, ok)
		}
	}

	// belbe_corrupted_observer's /Twice suffix doubles the count. The
	// controller (seat 0) lost life too, but the opponents group excludes
	// them, so the count is 1 then doubles to 2.
	h.lifeLost = map[state.PlayerID]int32{0: 5, 1: 3}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountOpponents$HasPropertyLostLifeThisTurn/Twice"); !ok || got != 2 {
		t.Errorf("Opponents HasPropertyLostLifeThisTurn/Twice = (%d, %v), want (2, true)", got, ok)
	}
}

// TestPlayerCountDefinedRegisteredUnknownPropertyFailsClosed pins the
// fallthrough: an unmodelled property on the new group, and every OTHER
// PlayerCountDefined* group, stay (0, false) -- so the wide
// PlayerCountDefined prefix is never cut.
func TestPlayerCountDefinedRegisteredUnknownPropertyFailsClosed(t *testing.T) {
	h, c := fixtureHost(t)
	for _, body := range []string{
		"Count$PlayerCountDefinedRegistered$HasPropertySomeUnmodelledThing",
		"Count$PlayerCountDefinedRegistered.Other$HasPropertySomeUnmodelledThing",
		// The sibling groups the brief scopes OUT: they need referent
		// machinery this head does not build and must stay fail-closed.
		"Count$PlayerCountDefinedRememberedOwner$HasPropertyLostLifeThisTurn",
		"Count$PlayerCountDefinedNonTriggeredTarget$HasPropertyLostLifeThisTurn",
		"Count$PlayerCountDefinedActivePlayer$HighestLifeLostThisTurn",
		// (NonCombatDamageDealtThisTurn, this list's old example, is now
		// modelled -- Chandra's Incinerator -- so an unmodelled property on
		// the same group stands in for it.)
		"Count$PlayerCountRegisteredOpponents$SomeUnmodelledProperty",
		// The life-TOTAL extremes resolve on the Players$/Opponents$ arms but
		// are NOT among the three properties this head offers — they stay
		// (0, false) here (no corpus carrier reads one through this group).
		"Count$PlayerCountDefinedRegistered$HighestLifeTotal",
		"Count$PlayerCountDefinedRegistered$LowestLifeTotal",
	} {
		if got, ok := EvalCountOK(h, c, body); ok {
			t.Errorf("%s reported EVALUATED as %d -- an unmodelled property/group must stay (0, false)", body, got)
		}
	}
}

// TestPlayerCountDefinedRegisteredOtherGroupEmptyFailsClosed pins the
// empty-`.Other`-group edge: with the resolving controller being the only
// living seat there is no other player, so the life extreme has no value and
// the verdict is unresolvable rather than a fake zero.
func TestPlayerCountDefinedRegisteredOtherGroupEmptyFailsClosed(t *testing.T) {
	h := newHost(t, 1)
	c := &Ctx{Controller: 0}
	h.lifeLost = map[state.PlayerID]int32{0: 4}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRegistered.Other$HighestLifeLostThisTurn"); ok {
		t.Errorf("single-seat .Other extreme = (%d, %v), want unresolvable (0, false)", got, ok)
	}
}
