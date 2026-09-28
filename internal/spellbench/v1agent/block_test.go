package v1agent

import (
	"encoding/json"
	"strconv"
	"testing"
)

func testCreature(arena uint32, name, ctrl string, p, tough int, tapped bool) *KCard {
	c := &KCard{Name: name, Tapped: tapped}
	c.Stable = KRef{ArenaID: arena, Owner: ctrl, Controller: ctrl, Zone: "Battlefield"}
	c.Characteristics.Types.Creature = true
	c.Characteristics.Power, c.Characteristics.Toughness = &p, &tough
	return c
}

// TestChumpsALethalPumpedAttacker: at 11 life facing a pumped 11/13 and a
// 1/3, the plan chump-blocks the big attacker with the cheapest creature.
func TestChumpsALethalPumpedAttacker(t *testing.T) {
	big := testCreature(0, "Masked Vandal", "p1", 11, 13, true)
	small := testCreature(2, "Masked Vandal", "p1", 1, 3, true)
	b := &Board{Seat: "p0", OppSeat: "p1", Me: 0, Opp: 1, Life: [2]int{11, 26}, byArena: map[uint32]*KCard{}}
	b.Theirs = []*KCard{big, small, testCreature(3, "Timberwatch Elf", "p1", 1, 2, true)}
	b.Mine = []*KCard{
		testCreature(10, "Priest of Titania", "p0", 1, 1, false),
		testCreature(11, "Fyndhorn Elves", "p0", 1, 1, false),
		testCreature(12, "Quirion Ranger", "p0", 1, 1, false),
		testCreature(13, "Masked Vandal", "p0", 1, 3, true),
	}
	for _, c := range append(append([]*KCard{}, b.Mine...), b.Theirs...) {
		b.byArena[c.Stable.ArenaID] = c
	}
	b.Combat.Attackers = []KRef{big.Stable, small.Stable}
	plan := NewTactical(TacticalOptions{}).planBlocks(b)
	chumped := false
	for _, a := range plan {
		if a == 0 {
			chumped = true
		}
	}
	if !chumped {
		t.Fatalf("no chump on the lethal 11/13: plan %v", plan)
	}
	// and the scan answers it: arena id 0 is a real attacker (measured:
	// seat p0's first card), not "no plan"
	tac := NewTactical(TacticalOptions{})
	tac.blockPlan, tac.blockGrp = plan, 7
	var blocker uint32
	for bl, a := range plan {
		if a == 0 {
			blocker = bl
		}
	}
	d := &Decision{Group: Group{GroupID: 7}, Kernel: &KernelView{}}
	for i, inc := range []bool{false, true} {
		raw := []byte(`{"kind":"choose_blocker_inclusion","include":false}`)
		if inc {
			raw = []byte(`{"kind":"choose_blocker_inclusion","include":true}`)
		}
		sem := Semantic{Kind: "choose_blocker_inclusion", Raw: raw, Fields: map[string]json.RawMessage{"include": json.RawMessage(strconv.FormatBool(inc))}}
		d.Candidates = append(d.Candidates, Candidate{ID: i, Semantic: sem})
		d.Kernel.Actions = append(d.Kernel.Actions, KAction{Kind: "choose_blocker_inclusion", Attacker: &KRef{ArenaID: 0}, Blocker: &KRef{ArenaID: blocker}})
	}
	if got := tac.block(d, b); got != 1 {
		t.Fatalf("scan answered %d, want the planned include (1)", got)
	}
}
