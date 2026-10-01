package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The tap gate's satisfiability filter (the seed-1003 ulalek-eldrazi
// livelock fix): bestUnpayable keeps a card as a tap target only when THIS
// window's offered "activate" sources can actually close its gap. An unmet
// coloured pip no offered source produces is a gap tapping can never close,
// and the old gate re-tapped a generic source (Ugin, Eye of the Storms'
// repeatable [0]) toward it once per intent forever.
func TestTapGateSkipsUnproducibleColourNeed(t *testing.T) {
	priority := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Label: "Activate Ugin for mana", Obj: 7},
			{Index: 1, Kind: "pass", Label: "Pass priority"},
		}}
	// The intended card needs {G}; the only offered source demonstrably
	// produces colourless. Tapping it can never make the card payable, so
	// the gate must fall through to the explicit pass.
	colorScrew := Board{IsMain: true,
		Cards: TableOf(map[state.ObjID]Card{
			3: {CMC: 3, ManaCost: "2 G", Castable: true},
			7: {Produces: cards.ManaProduction{Colour: [6]int32{0, 0, 0, 0, 0, 3}}},
		}),
		Pool: state.Mana{}}
	if in := Decide(colorScrew, &priority, rng(1)); priority.Options[in.Choices[0]].Kind != "pass" {
		t.Errorf("colour-screwed need = %+v, want the pass (the tap can never close the gap)", in)
	}
	// The same shape with a green producer offered: the tap is progress and
	// the gate taps it.
	greenAvailable := colorScrew
	greenAvailable.Cards.Set(7, Card{Produces: cards.ManaProduction{Colour: [6]int32{0, 0, 0, 0, 1, 0}}})
	if in := Decide(greenAvailable, &priority, rng(1)); priority.Options[in.Choices[0]].Kind != "activate" {
		t.Errorf("producible need = %+v, want the tap", in)
	}
	// A KNOWN empty production (a source this build cannot price) stays a
	// non-producer: the unproducible need is still skipped.
	unpriceable := colorScrew
	unpriceable.Cards.Set(7, Card{Produces: cards.ManaProduction{}})
	if in := Decide(unpriceable, &priority, rng(1)); priority.Options[in.Choices[0]].Kind != "pass" {
		t.Errorf("known-empty production = %+v, want the pass", in)
	}
	// A purely generic shortfall qualifies even against a known producer of
	// only one colour: a tier-1 tap adds to the pool total and the progress
	// is finite (the engine clears the pool at the step's end, CR 500.4).
	genericShort := colorScrew
	genericShort.Cards.Set(3, Card{CMC: 3, ManaCost: "3", Castable: true})
	if in := Decide(genericShort, &priority, rng(1)); priority.Options[in.Choices[0]].Kind != "activate" {
		t.Errorf("generic shortfall = %+v, want the tap", in)
	}
}
