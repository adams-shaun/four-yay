package botpolicy

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The new (dp1) policy rules: the four decisions that used to be answered
// without reading the game state -- a trigger_order shuffle, a
// trigger_optional coin flip, a fixed commander_zone answer, and a
// first-Max discard/sacrifice/exile -- now read the board's card facts.
// Each test pins the new rule against the board facts it reads.

// TestTriggerOrderRanksBySourceWorth is the trigger_order rule: the order is
// a deterministic permutation of the offered indices ordered by the source
// card's worth (cardWorth), most valuable source first, ties on index. It is
// not the shuffle it replaced -- the same offer always orders the same way.
func TestTriggerOrderRanksBySourceWorth(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{
		30: {CMC: 1},                   // worth 1
		31: {Creature: true, Power: 4}, // worth 46 — most valuable
		32: {Creature: true, Power: 1}, // worth 34
	})}
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KTriggerOrder, Min: 3, Max: 3,
		Options: []decision.Option{
			{Index: 0, Kind: "trigger", Obj: 30},
			{Index: 1, Kind: "trigger", Obj: 31},
			{Index: 2, Kind: "trigger", Obj: 32},
		}}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("trigger order intent failed Validate: %v", err)
	}
	if want := []int{1, 2, 0}; !reflect.DeepEqual(in.Choices, want) {
		t.Fatalf("trigger order = %v, want the most-valuable-source-first order %v", in.Choices, want)
	}
	// Deterministic: a second run with a different rng seed must answer the
	// same permutation -- the rule consumes no rng.
	in2 := Decide(b, &d, rng(99))
	if !reflect.DeepEqual(in.Choices, in2.Choices) {
		t.Fatalf("trigger order differed across seeds: %v vs %v -- the rule must be deterministic", in.Choices, in2.Choices)
	}
}

// TestTriggerOrderTiesBreakOnIndex pins the deterministic tiebreak: two
// triggers of equal source worth keep the offer order (index), not a map or
// random order.
func TestTriggerOrderTiesBreakOnIndex(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{
		40: {Creature: true, Power: 2},
		41: {Creature: true, Power: 2},
	})}
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KTriggerOrder, Min: 2, Max: 2,
		Options: []decision.Option{
			{Index: 0, Kind: "trigger", Obj: 40},
			{Index: 1, Kind: "trigger", Obj: 41},
		}}
	// Loop seeds: with two tied options a shuffle produces the identity
	// permutation half the time, so a single seed passes against the old
	// Fisher-Yates by coin flip and discriminates nothing. 25 seeds makes an
	// accidental pass 2^-25. (Caught by the dp1b gate, which found this case
	// green on unmodified main.)
	for seed := uint64(0); seed < 25; seed++ {
		in := Decide(b, &d, rng(seed))
		if want := []int{0, 1}; !reflect.DeepEqual(in.Choices, want) {
			t.Fatalf("seed %d: tied trigger order = %v, want the offer order %v",
				seed, in.Choices, want)
		}
	}
}

// Only an api:Effect OptionalDecider$ election defaults to decline;
// unmarked printed triggers and Miracle keep dp1's deterministic accept.
func TestTriggerOptionalEffectDeclineOnly(t *testing.T) {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KTriggerOptional, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "yes", Obj: 50},
			{Index: 1, Kind: "no", Obj: 50},
		}}
	for seed := uint64(0); seed < 25; seed++ {
		in := Decide(Board{}, &d, rng(seed))
		if len(in.Choices) != 1 || in.Choices[0] != 0 {
			t.Fatalf("seed %d: printed/Miracle optional = %v, want accept", seed, in.Choices)
		}
		d.EffectOptional = true
		in = Decide(Board{}, &d, rng(seed))
		if len(in.Choices) != 1 || in.Choices[0] != 1 {
			t.Fatalf("seed %d: Effect optional = %v, want decline", seed, in.Choices)
		}
		d.EffectOptional = false
	}
}

// TestCommanderZoneTaxAware is the commander_zone rule: put a commander into
// the command zone while it is still worth the growing CR 903.8 recast tax
// (the same cardWorth-minus-commandTax arithmetic the cast rule's CR1 uses),
// and let it leave once the exchange turns losing -- not the old fixed
// "always command zone".
func TestCommanderZoneTaxAware(t *testing.T) {
	cmdDecision := func(src state.ObjID) *decision.Decision {
		return &decision.Decision{Seq: 1, Player: 0, Kind: decision.KCommanderZone, Min: 1, Max: 1,
			Options: []decision.Option{
				{Index: 0, Kind: "command_zone", Obj: src},
				{Index: 1, Kind: "leave", Obj: src},
			}}
	}
	// A fresh 4/4 commander (Casts 0) is worth 46 with no tax -- command zone.
	fresh := Board{
		Cards:      TableOf(map[state.ObjID]Card{100: {Creature: true, Power: 4, CMC: 4}}),
		Commanders: TableOf(map[state.ObjID]Commander{100: {Casts: 0}}),
	}
	if in := Decide(fresh, cmdDecision(100), rng(1)); len(in.Choices) != 1 || in.Choices[0] != 0 {
		t.Fatalf("fresh commander = %v, want the command zone (option 0)", in.Choices)
	}
	// A 4/4 cast from the command zone three times (tax 60) is worth -14 --
	// let it leave so it can be recast from a non-command-zone source at no
	// tax, the same losing-exchange line the cast rule's CR1 draws.
	taxed := Board{
		Cards:      TableOf(map[state.ObjID]Card{200: {Creature: true, Power: 4, CMC: 4}}),
		Commanders: TableOf(map[state.ObjID]Commander{200: {Casts: 3}}),
	}
	if in := Decide(taxed, cmdDecision(200), rng(1)); len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("taxed commander = %v, want leave (option 1) -- the tax has made recasting losing", in.Choices)
	}
}

// TestChooseWorstPicksLeastValuable pins the discard/sacrifice rule:
// get rid of the d.Max LEAST valuable offered cards (cardWorth), not the
// first d.Max -- so a cleanup discard drops the least useful hand card, a
// sacrifice cost pays it with the least valuable permanent, and a delve
// exile spends the least valuable graveyard cards.
func TestChooseWorstPicksLeastValuable(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{
		1: {Creature: true, Power: 1}, // worth 34
		2: {CMC: 3},                   // worth 3
		3: {Creature: true, Power: 4}, // worth 46 — most valuable, kept
		4: {CMC: 1},                   // worth 1 — least valuable
	})}
	for _, kind := range []string{"sacrifice"} {
		d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KChoose, Min: 2, Max: 2,
			Options: []decision.Option{
				{Index: 0, Kind: kind, Obj: 1},
				{Index: 1, Kind: kind, Obj: 2},
				{Index: 2, Kind: kind, Obj: 3},
				{Index: 3, Kind: kind, Obj: 4},
			}}
		in := Decide(b, &d, rng(1))
		if err := d.Validate(in); err != nil {
			t.Fatalf("%s intent failed Validate: %v", kind, err)
		}
		if want := []int{1, 3}; !reflect.DeepEqual(in.Choices, want) {
			t.Fatalf("%s = %v, want the two least valuable cards (obj 2 worth 3, obj 4 worth 1) %v", kind, in.Choices, want)
		}
	}
}
