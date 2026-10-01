package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// T3, the converter gate (cardfuzz batch1 lines 3/5/6/16/17): a mana
// ability that spends pool mana without tapping its source (Farrelite
// Priest's "{1}: Add {W}", Bog Initiate, Initiates of the Ebon Hand) is
// activated only when it provably moves the intended card closer to
// castable. The T2 gate priced sources only by production, so the priest
// ranked tier 0 for a white card and was re-activated once per decision
// forever: the engine pays the {1} with the pool's only {W} and adds it
// straight back.
func TestTapGateNeverLoopsANetZeroConverter(t *testing.T) {
	white := cards.ManaProduction{Colour: [6]int32{1, 0, 0, 0, 0, 0}}
	priority := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Label: "Activate Farrelite Priest for mana", Obj: 7, Cost: "1"},
			{Index: 1, Kind: "pass", Label: "Pass priority"},
		}}
	// Line 3's exact window: pool {W}, the intended card {2}{W}{W}. Paying
	// {1} spends the {W} and adds a {W}: the same pool, so the pass.
	b := Board{IsMain: true,
		Cards: TableOf(map[state.ObjID]Card{
			3: {CMC: 4, ManaCost: "2 W W", Castable: true},
			7: {Produces: white},
		}),
		Pool: state.Mana{1, 0, 0, 0, 0, 0}}
	if in := Decide(b, &priority, rng(1)); priority.Options[in.Choices[0]].Kind != "pass" {
		t.Fatalf("net-zero conversion = %+v, want the pass", in)
	}
	// A real fix: pool {W}{C}{C}{C} against {2}{W}{W}. The engine pays the
	// generic {1} with {C} first, and the {W} it adds closes the second
	// pip: the deficit falls, so the gate converts.
	fix := b
	fix.Pool = state.Mana{1, 0, 0, 0, 0, 3}
	if in := Decide(fix, &priority, rng(1)); priority.Options[in.Choices[0]].Kind != "activate" {
		t.Fatalf("colour-fixing conversion = %+v, want the activation", in)
	}
	// Line 5's window: a cast is already offered, so a conversion (Bog
	// Initiate turning the four {B} paying for Frogmite into {B}) can only
	// undo it -- the gate falls through to the cast.
	withCast := decision.Decision{Seq: 2, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "cast", Label: "Cast Frogmite", Obj: 4},
			{Index: 1, Kind: "activate", Label: "Activate Bog Initiate for mana", Obj: 7, Cost: "1"},
			{Index: 2, Kind: "pass", Label: "Pass priority"},
		}}
	bog := Board{IsMain: true,
		Cards: TableOf(map[state.ObjID]Card{
			3: {CMC: 5, ManaCost: "3 B B", Castable: true},
			4: {CMC: 4, ManaCost: "4", Castable: true},
			7: {Produces: cards.ManaProduction{Colour: [6]int32{0, 0, 1, 0, 0, 0}}},
		}),
		Pool: state.Mana{0, 0, 4, 0, 0, 0}}
	if in := Decide(bog, &withCast, rng(1)); withCast.Options[in.Choices[0]].Kind != "cast" {
		t.Fatalf("conversion beside an offered cast = %+v, want the cast", in)
	}
	// A mana-costed ability that TAPS its source is bounded by the tap and is
	// not a converter: Celestial Prism's "{2}, {T}" stays a T2 tap.
	prism := decision.Decision{Seq: 3, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Label: "Activate Celestial Prism for mana", Obj: 7, Cost: "2 T"},
			{Index: 1, Kind: "pass", Label: "Pass priority"},
		}}
	if in := Decide(b, &prism, rng(1)); prism.Options[in.Choices[0]].Kind != "activate" {
		t.Fatalf("tapping mana-costed source = %+v, want the T2 tap", in)
	}
}

// TestConverterGateTerminates drives the gate against its own simulated
// pool: whatever the pool, repeatedly applying the converter the gate picks
// ends in a pass within a bounded number of activations (the deficit is a
// strictly decreasing non-negative integer).
func TestConverterGateTerminates(t *testing.T) {
	priority := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Label: "Activate Priest", Obj: 7, Cost: "1"},
			{Index: 1, Kind: "pass", Label: "Pass priority"},
		}}
	for _, start := range []state.Mana{{1, 0, 0, 0, 0, 0}, {0, 0, 0, 0, 0, 5}, {2, 1, 1, 1, 1, 1}, {0, 3, 0, 0, 0, 0}} {
		b := Board{IsMain: true,
			Cards: TableOf(map[state.ObjID]Card{
				3: {CMC: 6, ManaCost: "2 W W W W", Castable: true},
				7: {Produces: cards.ManaProduction{Colour: [6]int32{1, 0, 0, 0, 0, 0}}},
			}),
			Pool: start}
		steps := 0
		for ; steps < 50; steps++ {
			in := Decide(b, &priority, rng(1))
			if priority.Options[in.Choices[0]].Kind == "pass" {
				break
			}
			// The engine's payment: generic from C, then W, U, B, R, G.
			for _, i := range [...]int{state.MC, state.MW, state.MU, state.MB, state.MR, state.MG} {
				if b.Pool[i] > 0 {
					b.Pool[i]--
					break
				}
			}
			b.Pool[state.MW]++
		}
		if steps >= 50 {
			t.Fatalf("pool %v: converter still activated after 50 steps (pool now %v)", start, b.Pool)
		}
	}
}

// T4, the payment-window converter gate (cardfuzz batch3 lines 9/11): inside
// a KChoose payment window (cast payment, UnlessCost$/Ward, cumulative
// upkeep) a non-tapping converter is never taken -- the first tap source is,
// and once only converters remain the window is closed with Done. The window
// carries no fact naming its charge, so a conversion's progress cannot be
// priced; failing closed toward Done bounds every window by its untapped
// sources.
func TestPaymentWindowNeverTakesAConverter(t *testing.T) {
	for _, resume := range []string{"", "unless_mana", "ward_mana"} {
		w := decision.Decision{Seq: 1, Player: 0, Kind: decision.KChoose, Min: 1, Max: 1, ResumeKind: resume,
			Prompt: "Activate mana abilities to pay",
			Options: []decision.Option{
				{Index: 0, Kind: "activate", Label: "Tap Farrelite Priest for mana", Obj: 7, Cost: "1"},
				{Index: 1, Kind: "activate", Label: "Tap Plains for mana", Obj: 8},
				{Index: 2, Kind: "done", Label: "Done"},
			}}
		b := Board{Pool: state.Mana{1, 0, 0, 0, 0, 0}, Cards: TableOf(map[state.ObjID]Card{})}
		if in := Decide(b, &w, rng(1)); len(in.Choices) != 1 || in.Choices[0] != 1 {
			t.Fatalf("%q window with a converter first = %+v, want the Plains (option 1)", resume, in)
		}
		// A mana-costed source that TAPS is bounded by the tap: taken.
		w.Options[1].Cost = "2 T"
		if in := Decide(b, &w, rng(1)); len(in.Choices) != 1 || in.Choices[0] != 1 {
			t.Fatalf("%q window with a tapping filter source = %+v, want it (option 1)", resume, in)
		}
		only := decision.Decision{Seq: 2, Player: 0, Kind: decision.KChoose, Min: 1, Max: 1, ResumeKind: resume,
			Options: []decision.Option{
				{Index: 0, Kind: "activate", Label: "Tap Farrelite Priest for mana", Obj: 7, Cost: "1"},
				{Index: 1, Kind: "done", Label: "Done"},
			}}
		if in := Decide(b, &only, rng(1)); len(in.Choices) != 1 || only.Options[in.Choices[0]].Kind != "done" {
			t.Fatalf("%q window with only a converter = %+v, want Done", resume, in)
		}
	}
}

// TestConverterGateIgnoresRestrictedPoolMana (cardfuzz batch5 line 9): Myr
// Reservoir's floating {C}{C} may only be spent on Myr, so the engine pays
// Initiates of the Ebon Hand's "{1}: Add {B}" with the pool's {B} and adds
// it straight back. The gate simulated the generic {1} out of the {C} and
// saw the {B} it added close a pip -- progress that never happens -- and
// re-activated the converter until the livelock watcher fired. Priced over
// the unrestricted pool only, the conversion is net-zero: the pass.
func TestConverterGateIgnoresRestrictedPoolMana(t *testing.T) {
	priority := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Label: "Activate Initiates of the Ebon Hand for mana", Obj: 7, Cost: "1"},
			{Index: 1, Kind: "pass", Label: "Pass priority"},
		}}
	b := Board{IsMain: true,
		Cards: TableOf(map[state.ObjID]Card{
			3: {CMC: 3, ManaCost: "1 B B", Castable: true},
			7: {Produces: cards.ManaProduction{Colour: [6]int32{0, 0, 1, 0, 0, 0}}},
		}),
		Pool: state.Mana{0, 0, 1, 0, 0, 2}}
	// Control: the same {C}{C} UNRESTRICTED really does fix the colour.
	if in := Decide(b, &priority, rng(1)); priority.Options[in.Choices[0]].Kind != "activate" {
		t.Fatalf("unrestricted colour-fixing conversion = %+v, want the activation", in)
	}
	b.PoolRestricted = RestrictedPool([]state.ManaRestriction{
		{Color: "C", Amount: 2, Valid: "Spell.Myr,Activated.Myr+inZoneBattlefield"}})
	if b.PoolRestricted != (state.Mana{0, 0, 0, 0, 0, 2}) {
		t.Fatalf("RestrictedPool = %v, want the two {C} in the C slot", b.PoolRestricted)
	}
	if in := Decide(b, &priority, rng(1)); priority.Options[in.Choices[0]].Kind != "pass" {
		t.Fatalf("conversion priced over restricted mana = %+v, want the pass", in)
	}
	// An AddsNoCounter$-only provenance batch (empty Valid) is spendable
	// anywhere and is not restricted.
	if got := RestrictedPool([]state.ManaRestriction{{Color: "G", Amount: 1, NoCounter: "True"}}); got != (state.Mana{}) {
		t.Fatalf("RestrictedPool of an unrestricted provenance batch = %v, want empty", got)
	}
}
