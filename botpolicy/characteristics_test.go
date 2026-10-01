package botpolicy

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

type combinedCharacteristics struct {
	values map[state.ObjID]struct {
		power, toughness int32
		keyword          string
	}
	calls       map[state.ObjID]int
	legacyCalls int
	scratch     []string
}

func (c *combinedCharacteristics) Power(state.ObjID) int32 {
	c.legacyCalls++
	return -1
}

func (c *combinedCharacteristics) Toughness(state.ObjID) int32 {
	c.legacyCalls++
	return -1
}

func (c *combinedCharacteristics) Keywords(state.ObjID) []string {
	c.legacyCalls++
	return []string{"legacy"}
}

func (c *combinedCharacteristics) Characteristics(id state.ObjID) (int32, int32, []string) {
	c.calls[id]++
	v := c.values[id]
	c.scratch[0] = v.keyword
	return v.power, v.toughness, c.scratch
}

// TestBoardFromGameUsesCombinedCharacteristicsOncePerObject catches either
// falling back to the three legacy calls when the optional combined surface
// exists, or retaining its scratch-backed keyword result across the next
// object's query. The literal Board facts are the observable contract.
func TestBoardFromGameUsesCombinedCharacteristicsOncePerObject(t *testing.T) {
	g := state.NewGame([]string{"alice", "bob"})
	hand := g.AddObject(cardFace(t, "Ambusher", "Creature Rogue", 3, 9), 0)
	foe := g.AddObject(cardFace(t, "Sky Terror", "Creature Dragon", 7, 4), 1)
	g.SetZone(state.ZHand, 0, []state.ObjID{hand.ID})
	g.SetZone(state.ZBattlefield, 1, []state.ObjID{foe.ID})

	chars := &combinedCharacteristics{
		values: map[state.ObjID]struct {
			power, toughness int32
			keyword          string
		}{
			hand.ID: {power: 3, toughness: 9, keyword: "Flash"},
			foe.ID:  {power: 7, toughness: 4, keyword: "Flying"},
		},
		calls:   make(map[state.ObjID]int),
		scratch: make([]string, 1),
	}

	got := BoardFromGame(g, chars, 0)
	if creature := got.Creatures.Get(foe.ID); creature.Power != 7 || creature.Toughness != 4 || !slices.Equal(creature.Keywords, []string{"Flying"}) {
		t.Errorf("foe creature = %+v, want literal 7/4 with Flying", creature)
	}
	if card := got.Cards.Get(hand.ID); card.Power != 3 || !card.Castable || !card.InstantSpeed {
		t.Errorf("hand card = %+v, want literal power 3, castable, and instant-speed from Flash", card)
	}
	if chars.calls[foe.ID] != 1 || chars.calls[hand.ID] != 1 {
		t.Errorf("combined calls = %v, want one query for each projected object", chars.calls)
	}
	if chars.legacyCalls != 0 {
		t.Errorf("legacy characteristic calls = %d, want 0 when combined query is supported", chars.legacyCalls)
	}
}
