package v1agent

import "testing"

// TestSpyComboNeedsAnEmptyLandLibrary: the Spy list runs four basics; the
// combo is on only once all four are out of the library, and only with
// three creatures for Dread Return's flashback and a lethal graveyard.
func TestSpyComboNeedsAnEmptyLandLibrary(t *testing.T) {
	deck := deckCounts("Spy")
	if got := countWhere(deck, isLandCard); got != 4 {
		t.Fatalf("Spy lands = %d, want 4 (1 Swamp, 3 Forest)", got)
	}
	tac := NewTactical(TacticalOptions{})
	tac.GameStart(&GameStart{Seat: "p0", CatalogIDs: []string{"Spy", "Burn"}})
	b := &Board{Seat: "p0", OppSeat: "p1", Me: 0, Opp: 1, Life: [2]int{20, 20}, byArena: map[uint32]*KCard{}}
	land := func(id uint32, name string) *KCard {
		c := &KCard{Name: name}
		c.Stable = KRef{ArenaID: id, Owner: "p0", Controller: "p0", Zone: "Battlefield"}
		c.Characteristics.Types.Land = true
		return c
	}
	b.Mine = []*KCard{land(1, "Swamp"), land(2, "Forest"), land(3, "Forest"),
		testCreature(10, "Wall of Roots", "p0", 0, 5, false), testCreature(11, "Saruli Caretaker", "p0", 0, 3, false)}
	if tac.spyCombo(b, false) {
		t.Fatal("combo on with a Forest still in the library")
	}
	b.Mine = append(b.Mine, land(4, "Forest"))
	if !tac.spyCombo(b, false) {
		t.Fatal("combo off with no land left, three creatures and a full graveyard to come")
	}
	b.Mine = b.Mine[:3]
	b.Mine = append(b.Mine, land(4, "Forest"), testCreature(10, "Wall of Roots", "p0", 0, 5, false))
	if tac.spyCombo(b, false) {
		t.Fatal("combo on without three creatures for the flashback")
	}
}
