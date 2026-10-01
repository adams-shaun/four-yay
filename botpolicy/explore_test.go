package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestExploreX2ReachesEverySiblingAbility: A2 ranks Brightling's three {W}
// abilities below its {1} mode forever (it reads the first number of the
// DESCRIPTION); the explore pick is uniform, so every offered ability is
// taken for some seed, and a seed always answers the same.
func TestExploreX2ReachesEverySiblingAbility(t *testing.T) {
	b := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{7: {Power: 3, Toughness: 3, Controller: 0}}),
		Cards:     TableOf(map[state.ObjID]Card{7: {}}),
	}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "ability", Obj: 7, Ability: 0, Label: "Brightling: CARDNAME gains vigilance until end of turn."},
			{Index: 1, Kind: "ability", Obj: 7, Ability: 1, Label: "Brightling: CARDNAME gains lifelink until end of turn."},
			{Index: 2, Kind: "ability", Obj: 7, Ability: 2, Label: "Brightling: Return CARDNAME to its owner's hand."},
			{Index: 3, Kind: "ability", Obj: 7, Ability: 3, Label: "Brightling: CARDNAME gets +1/-1 or -1/+1 until end of turn."},
			{Index: 4, Kind: "pass"},
		}}
	if in := Decide(b, d, rng(1)); in.Choices[0] != 3 {
		t.Fatalf("production Decide = %+v, want A2's option 3", in)
	}
	seen := map[int]bool{}
	for seed := uint64(0); seed < 64; seed++ {
		a := ExploreDecide(b, d, rng(seed))
		if again := ExploreDecide(b, d, rng(seed)); again.Choices[0] != a.Choices[0] {
			t.Fatalf("seed %d answered %v then %v", seed, a.Choices, again.Choices)
		}
		seen[a.Choices[0]] = true
	}
	for i := 0; i < 4; i++ {
		if !seen[i] {
			t.Fatalf("explore never took ability option %d (took %v)", i, seen)
		}
	}
}

// TestExploreX5ManaPickIsUniform: a stage-1 "choose a mana ability" wheel
// (a dual land's two abilities) reaches both under explore; production
// always takes the first.
func TestExploreX5ManaPickIsUniform(t *testing.T) {
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KChoose, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "mana", Obj: 9, Ability: 0, Label: "Add W"},
			{Index: 1, Kind: "mana", Obj: 9, Ability: 1, Label: "Add R"},
		}}
	if in := Decide(Board{}, d, rng(1)); in.Choices[0] != 0 {
		t.Fatalf("production mana pick = %+v, want the first", in)
	}
	seen := map[int]bool{}
	for seed := uint64(0); seed < 32; seed++ {
		seen[ExploreDecide(Board{}, d, rng(seed)).Choices[0]] = true
	}
	if !seen[0] || !seen[1] {
		t.Fatalf("explore mana picks = %v, want both", seen)
	}
}

// TestExploreX6FloatsOnlyBareTaps: once mana floats, explore keeps tapping
// plain {T} sources (so an ability costing more than a cast's leftover
// becomes offered) and never takes a costly activation (Cost non-empty: a
// sacrifice or life payment); with nothing plain left it passes.
func TestExploreX6FloatsOnlyBareTaps(t *testing.T) {
	var pool state.Mana
	pool[0] = 1
	b := Board{IsMain: false, Pool: pool}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: 3, Label: "Activate Lotus Petal for mana", Cost: "T Sac<1/CARDNAME>"},
			{Index: 1, Kind: "activate", Obj: 4, Label: "Activate Plains for mana"},
			{Index: 2, Kind: "pass"},
		}}
	for seed := uint64(0); seed < 16; seed++ {
		if in := ExploreDecide(b, d, rng(seed)); in.Choices[0] != 1 {
			t.Fatalf("seed %d: explore with floating mana chose %v, want the plain tap (1)", seed, in.Choices)
		}
	}
	if in := Decide(b, d, rng(1)); d.Options[in.Choices[0]].Kind != "pass" {
		t.Fatalf("production Decide outside a main phase = %+v, want pass", in)
	}
	d.Options = []decision.Option{d.Options[0], {Index: 1, Kind: "pass"}}
	for seed := uint64(0); seed < 16; seed++ {
		if in := ExploreDecide(b, d, rng(seed)); in.Choices[0] != 1 {
			t.Fatalf("seed %d: explore took the costly activation: %v", seed, in.Choices)
		}
	}
}
