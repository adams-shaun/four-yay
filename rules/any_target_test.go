package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestAnyTargetOffersOnlyCR1154Targets exercises the player-visible target
// census with an inline-authored Lightning Bolt equivalent. Forge's scripts
// remain outside the repository; this fixture contains only the minimum IR
// source needed to reach askTarget.
func TestAnyTargetOffersOnlyCR1154Targets(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	bolt := card(t, "Name:Bolt Fixture\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n")

	addPermanent := func(name, types string) state.ObjID {
		o := e.G.AddObject(card(t, "Name:"+name+"\nTypes:"+types+"\nOracle:x\n"), 1)
		o.Zone = state.ZBattlefield
		e.G.SetZone(state.ZBattlefield, 1,
			append(e.G.Zone(state.ZBattlefield, 1), o.ID))
		return o.ID
	}

	type fixture struct {
		kind string
		id   state.ObjID
	}
	valid := []fixture{
		{kind: "creature", id: addPermanent("Bear", "Creature Bear")},
		{kind: "planeswalker", id: addPermanent("Walker", "Legendary Planeswalker Test")},
		{kind: "battle", id: addPermanent("Siege", "Battle Siege")},
	}
	invalid := []fixture{
		{kind: "land", id: addPermanent("Forest", "Basic Land Forest")},
		{kind: "non-creature artifact", id: addPermanent("Relic", "Artifact")},
		{kind: "enchantment", id: addPermanent("Blessing", "Enchantment")},
	}

	e.askTarget(0, 0, bolt.Faces[0].SpellAbility())
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want target decision", d)
	}

	offeredObjects := make(map[state.ObjID]bool)
	offeredPlayers := make(map[state.PlayerID]bool)
	for _, option := range d.Options {
		if option.Obj != 0 {
			offeredObjects[option.Obj] = true
		} else if option.Kind == "player" {
			offeredPlayers[option.Player] = true
		}
	}
	for _, candidate := range valid {
		if !offeredObjects[candidate.id] {
			t.Errorf("%s %d was not offered as an Any target", candidate.kind, candidate.id)
		}
	}
	for _, candidate := range invalid {
		if offeredObjects[candidate.id] {
			t.Errorf("%s permanent %d was offered as an Any target", candidate.kind, candidate.id)
		}
	}
	for p := state.PlayerID(0); p < 2; p++ {
		if !offeredPlayers[p] {
			t.Errorf("player %d was not offered as an Any target", p)
		}
	}
}
