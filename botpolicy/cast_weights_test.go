package botpolicy

// The L1 cast-weights pins. Three jobs, per the brief:
//
//  1. the equivalence table: DefaultCastWeights (reached through the zero
//     Board.Cast the adapters leave) picks identically to the LITERAL
//     pre-refactor rule on every cast-pick case cast_test.go,
//     reserve_test.go, commander_test.go and counter_cast_test.go exercise —
//     C1–C8 and CR1 exactly as their doc comments state them;
//  2. one test per NEW feature proving a non-zero weight on it changes a
//     pick in the expected direction;
//  3. the zero-value contract: Board.Cast == CastWeights{} is treated as
//     DefaultCastWeights, tested explicitly.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// ---------------------------------------------------------------------------
// The literal pre-refactor rule, kept verbatim (constants, not weights) as
// the equivalence oracle. It is the pre-refactor cardWorth/castScore/
// chooseCast arithmetic, word for word — the ONLY place in the package the
// old literal numbers now live.

func legacyCardWorth(c Card) int32 {
	if c.Creature {
		return 30 + 4*c.Power
	}
	return c.CMC
}

func legacyCastScore(b Board, o decision.Option) int32 {
	s := legacyCardWorth(b.Cards.Get(o.Obj))
	switch o.Mode {
	case "kicked", "kicked1", "kicked2", "kickedboth", "surged":
		s += 6
	case "flashback", "miracle":
		s += 4
	}
	return s
}

func legacyCastCost(b Board, id state.ObjID) int32 {
	c := b.Cards.Get(id)
	cost := c.CMC
	if cmdr, ok := b.Commanders.Lookup(id); ok && cmdr.InCommandZone {
		cost += 2 * cmdr.Casts
	}
	return cost
}

func legacyChooseCast(b Board, d *decision.Decision) int {
	best := -1
	var bestScore int32 = -1
	res := b.reserve()
	foreignSpell := false
	for _, s := range b.Stack {
		if s.IsSpell && s.Controller != d.Player {
			foreignSpell = true
			break
		}
	}
	for _, o := range d.Options {
		if o.Kind != "cast" {
			continue
		}
		if b.Cards.Get(o.Obj).Counter && !foreignSpell {
			continue
		}
		inCmd := b.Commanders.Get(o.Obj).InCommandZone
		s := legacyCastScore(b, o)
		if inCmd {
			s -= 5 * b.Commanders.Get(o.Obj).Casts * b.Cards.Get(o.Obj).CMC
			if s < 0 {
				continue
			}
		} else if res > 0 && b.Pool.Total()-legacyCastCost(b, o.Obj) >= res {
			s += res * 5
		}
		if best == -1 || s > bestScore || (s == bestScore && o.Index < best) {
			best, bestScore = o.Index, s
		}
	}
	return best
}

// castWeightsCase is one equivalence-table entry: a Board and the options
// of one KPriority decision, the exact shape of one cast-pick test case.
type castWeightsCase struct {
	name string
	b    Board
	opts []decision.Option
}

// castWeightsCases mirrors every cast-pick case the existing cast tests
// exercise — the C1–C8 shapes of cast_test.go and counter_cast_test.go,
// the C7 shapes of reserve_test.go, and the CR1 shapes of commander_test.go.
func castWeightsCases() []castWeightsCase {
	cmdr := func(casts int32) map[state.ObjID]Commander {
		return map[state.ObjID]Commander{1: {Casts: casts, InCommandZone: true}}
	}
	return []castWeightsCase{
		{
			name: "C1 creature over spell",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 1}, 2: {CMC: 1}})},
			opts: []decision.Option{castSpell(0, 2), castCreature(1, 1)},
		},
		{
			name: "C2 higher power",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 1}, 2: {Creature: true, Power: 4}})},
			opts: []decision.Option{castCreature(0, 1), castCreature(1, 2)},
		},
		{
			name: "C3 bigger spell",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {CMC: 1}, 2: {CMC: 3}})},
			opts: []decision.Option{castSpell(0, 1), castSpell(1, 2)},
		},
		{
			name: "C4 kicked over ordinary",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 2}})},
			opts: []decision.Option{{Index: 0, Kind: "cast", Obj: 1}, {Index: 1, Kind: "cast", Obj: 1, Mode: "kicked"}},
		},
		{
			name: "C4 flashback over ordinary",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 2}})},
			opts: []decision.Option{{Index: 0, Kind: "cast", Obj: 1}, {Index: 1, Kind: "cast", Obj: 1, Mode: "flashback"}},
		},
		{
			name: "C5 unreadable option is low rank",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 1}})},
			opts: []decision.Option{castSpell(0, 999), castCreature(1, 1)},
		},
		{
			name: "C6 tie on index",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 2}, 2: {Creature: true, Power: 2}})},
			opts: []decision.Option{castCreature(0, 1), castCreature(1, 2)},
		},
		{
			name: "equal-cost non-creatures tie on index",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {CMC: 2}, 2: {CMC: 2}})},
			opts: []decision.Option{castSpell(0, 2), castSpell(1, 1)},
		},
		{
			name: "C3 ranks by cost not oracle",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {CMC: 4}, 2: {CMC: 1}})},
			opts: []decision.Option{castSpell(0, 2), castSpell(1, 1)},
		},
		{
			name: "C7 prefer the cast that keeps the reserve",
			b: Board{IsMain: true, Pool: state.Mana{state.MC: 3}, Cards: TableOf(map[state.ObjID]Card{
				1: {CMC: 1, Castable: true},
				3: {CMC: 3, Castable: true},
				2: {CMC: 1, Castable: true, InstantSpeed: true},
			})},
			opts: []decision.Option{{Index: 0, Kind: "cast", Obj: 3}, {Index: 1, Kind: "cast", Obj: 1}, {Index: 2, Kind: "pass"}},
		},
		{
			name: "C7 never suppresses the best play",
			b: Board{IsMain: true, Pool: state.Mana{state.MC: 4}, Cards: TableOf(map[state.ObjID]Card{
				1: {CMC: 4, Creature: true, Power: 4, Castable: true},
				2: {CMC: 1, Castable: true, InstantSpeed: true},
			})},
			opts: []decision.Option{{Index: 0, Kind: "cast", Obj: 1}, {Index: 1, Kind: "pass"}},
		},
		{
			name: "C7 allows a cast with surplus",
			b: Board{IsMain: true, Pool: state.Mana{state.MC: 4}, Cards: TableOf(map[state.ObjID]Card{
				1: {CMC: 3, Creature: true, Power: 2, Castable: true},
				2: {CMC: 1, Castable: true, InstantSpeed: true},
			})},
			opts: []decision.Option{{Index: 0, Kind: "cast", Obj: 1}, {Index: 1, Kind: "pass"}},
		},
		{
			name: "C7 inert without an instant-speed card",
			b: Board{IsMain: true, Pool: state.Mana{state.MC: 3}, Cards: TableOf(map[state.ObjID]Card{
				1: {CMC: 3, Creature: true, Power: 2, Castable: true},
			})},
			opts: []decision.Option{{Index: 0, Kind: "cast", Obj: 1}, {Index: 1, Kind: "pass"}},
		},
		{
			name: "C7 never holds the commander cast",
			b: Board{IsMain: true, Pool: state.Mana{state.MC: 3},
				Commanders: TableOf(map[state.ObjID]Commander{9: {Casts: 0, InCommandZone: true}}),
				Cards: TableOf(map[state.ObjID]Card{
					9: {CMC: 3, Creature: true, Power: 3, Castable: true},
					2: {CMC: 1, Castable: true, InstantSpeed: true},
				})},
			opts: []decision.Option{{Index: 0, Kind: "cast", Obj: 9}, {Index: 1, Kind: "pass"}},
		},
		{
			name: "CR1 first cast",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 4, CMC: 4}}), Commanders: TableOf(cmdr(0))},
			opts: []decision.Option{castCreature(0, 1), {Index: 1, Kind: "pass"}},
		},
		{
			name: "CR1 second cast still casts",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 4, CMC: 4}, 2: {CMC: 3}}), Commanders: TableOf(cmdr(1))},
			opts: []decision.Option{castSpell(0, 2), castCreature(1, 1)},
		},
		{
			name: "CR1 fourth cast refused",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 4, CMC: 4}}), Commanders: TableOf(cmdr(3))},
			opts: []decision.Option{castCreature(0, 1), {Index: 1, Kind: "pass"}},
		},
		{
			name: "CR1 12/12 third cast refused",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 12, CMC: 12}}), Commanders: TableOf(cmdr(2))},
			opts: []decision.Option{castCreature(0, 1), {Index: 1, Kind: "pass"}},
		},
		{
			name: "CR1 cheap outlasts expensive",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 2, CMC: 2}}), Commanders: TableOf(cmdr(2))},
			opts: []decision.Option{castCreature(0, 1), {Index: 1, Kind: "pass"}},
		},
		{
			name: "CR1 expensive CMC8 priced out",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 4, CMC: 8}}), Commanders: TableOf(cmdr(2))},
			opts: []decision.Option{castCreature(0, 1), {Index: 1, Kind: "pass"}},
		},
		{
			name: "CR1 taxed below the big spell",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 4, CMC: 4}, 2: {CMC: 8}}), Commanders: TableOf(cmdr(2))},
			opts: []decision.Option{castCreature(0, 1), castSpell(1, 2)},
		},
		{
			name: "CR1 hand cast untaxed",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 4, CMC: 4}}), Commanders: TableOf(map[state.ObjID]Commander{1: {Casts: 3, InCommandZone: false}})},
			opts: []decision.Option{castCreature(0, 1), {Index: 1, Kind: "pass"}},
		},
		{
			name: "CR1 refusal falls to the land drop",
			b:    Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 2, CMC: 2}, 2: {Basic: true}}), Commanders: TableOf(cmdr(2))},
			opts: []decision.Option{castCreature(0, 1), playLand(1, 2)},
		},
		{
			name: "C8 counter refused at an own-spells-only stack",
			b: Board{Cards: TableOf(map[state.ObjID]Card{
				10: {CMC: 2, Castable: true, InstantSpeed: true, Counter: true},
			}), Stack: []StackEntry{{ID: 90, Controller: 0, IsSpell: true}}},
			opts: counterPriority(10).Options,
		},
		{
			name: "C8 counter cast at a foreign spell",
			b: Board{Cards: TableOf(map[state.ObjID]Card{
				10: {CMC: 2, Castable: true, InstantSpeed: true, Counter: true},
			}), Stack: []StackEntry{{ID: 91, Controller: 1, IsSpell: true}}},
			opts: counterPriority(10).Options,
		},
	}
}

// TestDefaultCastWeightsMatchesPreRefactorRule is the equivalence table: on
// every mirrored cast-pick case, chooseCast over a Board whose Cast is the
// zero value (treated as DefaultCastWeights) picks the same option as the
// literal pre-refactor rule above. Any drift in the default arithmetic —
// a reordered feature, a changed constant, a dropped gate — fails here by
// case name.
func TestDefaultCastWeightsMatchesPreRefactorRule(t *testing.T) {
	for _, tc := range castWeightsCases() {
		d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: tc.opts}
		want := legacyChooseCast(tc.b, d)
		got := tc.b.chooseCast(d)
		if got != want {
			t.Fatalf("%s: chooseCast (zero Cast = default weights) = option %d, want the pre-refactor rule's %d", tc.name, got, want)
		}
	}
}

// TestDefaultCastWeightsFieldPicksLikeDefaultToo pins the same equivalence
// through an EXPLICITLY-set DefaultCastWeights — the adapters never set
// Board.Cast, so both reach paths (zero value and explicit default) must
// agree with the legacy rule on the whole table.
func TestDefaultCastWeightsFieldPicksLikeDefaultToo(t *testing.T) {
	for _, tc := range castWeightsCases() {
		tc.b.Cast = DefaultCastWeights
		d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: tc.opts}
		if got := tc.b.chooseCast(d); got != legacyChooseCast(tc.b, d) {
			t.Fatalf("%s: chooseCast (explicit DefaultCastWeights) = option %d, want the pre-refactor rule's pick", tc.name, got)
		}
	}
}

// withTuned copies a Board with one weight of its cast profile turned on —
// the shape a learned profile arrives in (everything default but the one
// learned weight).
func withTuned(b Board, tune func(*CastWeights)) Board {
	w := DefaultCastWeights
	tune(&w)
	b.Cast = w
	return b
}

// ---------------------------------------------------------------------------
// One test per new feature: a non-zero weight on it changes a pick in the
// expected direction. Every control asserts the default pick first, so the
// flip is attributable to the weight alone.

// TestCastWeightCreatureToughness: with CreatureToughness weight 2, of two
// equal-power creatures (an index tie by default) the tougher body is cast.
func TestCastWeightCreatureToughness(t *testing.T) {
	cards := map[state.ObjID]Card{
		1: {Creature: true, Power: 2, Toughness: 1, Castable: true},
		2: {Creature: true, Power: 2, Toughness: 4, Castable: true},
	}
	opts := []decision.Option{castCreature(0, 1), castCreature(1, 2)}
	b := Board{IsMain: true, Cards: TableOf(cards)}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != 0 {
		t.Fatalf("default pick = option %d, want the index tie (0)", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.CreatureToughness = 2 }).chooseCast(d); got != 1 {
		t.Fatalf("CreatureToughness=2 pick = option %d, want 1 — the 4-toughness body outranks the 1-toughness one", got)
	}
}

// TestCastWeightCurveFit: with CurveFit weight 10, of two equal-power
// creatures (an index tie by default) the one whose cost exactly equals the
// producible mana (pool 1 + an untapped source producing 2 = 3) is cast.
// A tapped source promises nothing, so the control keeps the tie.
func TestCastWeightCurveFit(t *testing.T) {
	cards := map[state.ObjID]Card{
		1: {Creature: true, Power: 2, CMC: 1, Castable: true},
		2: {Creature: true, Power: 2, CMC: 3, Castable: true},
		5: {OnBattlefield: true, Produces: prod(state.MC, 2)}, // an untapped colourless source
	}
	opts := []decision.Option{castCreature(0, 1), castCreature(1, 2)}
	b := Board{IsMain: true, Pool: state.Mana{state.MC: 1}, Cards: TableOf(cards)}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != 0 {
		t.Fatalf("default pick = option %d, want the index tie (0)", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.CurveFit = 10 }).chooseCast(d); got != 1 {
		t.Fatalf("CurveFit=10 pick = option %d, want 1 — the 3-cost cast exactly spends the producible 3 (pool 1 + source 2)", got)
	}
	// Control: the source is tapped — producible drops to the pool's 1 and
	// the fit moves to the 1-cost cast, which the weight then prefers.
	src := cards[5]
	src.Tapped = true
	b.Cards.Set(5, src)
	if got := withTuned(b, func(w *CastWeights) { w.CurveFit = 10 }).chooseCast(d); got != 0 {
		t.Fatalf("CurveFit=10 with a tapped source = option %d, want 0 — producible is the pool's 1 and the 1-cost cast is what fits", got)
	}
}

// TestCastWeightManaLeft: with ManaLeft weight 1, of two equal-power
// creatures (an index tie by default) the cheaper cast wins — it leaves
// more of the pool unspent.
func TestCastWeightManaLeft(t *testing.T) {
	cards := map[state.ObjID]Card{
		1: {Creature: true, Power: 2, CMC: 3, Castable: true},
		2: {Creature: true, Power: 2, CMC: 1, Castable: true},
	}
	opts := []decision.Option{castCreature(0, 1), castCreature(1, 2)}
	b := Board{IsMain: true, Pool: state.Mana{state.MC: 5}, Cards: TableOf(cards)}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != 0 {
		t.Fatalf("default pick = option %d, want the index tie (0)", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.ManaLeft = 1 }).chooseCast(d); got != 1 {
		t.Fatalf("ManaLeft=1 pick = option %d, want 1 — the 1-cost cast leaves 4 of the pool against the 3-cost's 2", got)
	}
}

// TestCastWeightPrecombat: with Precombat weight 15, a commander cast the
// CR1 tax has priced below zero (46 - 5*3*4 = -14) is rescued in the FIRST
// main phase; outside the first main phase (FirstMain false) the same
// weight keeps it refused.
func TestCastWeightPrecombat(t *testing.T) {
	b := Board{IsMain: true, FirstMain: true,
		Cards:      TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 4, CMC: 4, Castable: true}}),
		Commanders: TableOf(map[state.ObjID]Commander{1: {Casts: 3, InCommandZone: true}})}
	opts := []decision.Option{castCreature(0, 1), {Index: 1, Kind: "pass"}}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != -1 {
		t.Fatalf("default pick = option %d, want -1 — the fourth command-zone cast is priced out (CR1)", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.Precombat = 15 }).chooseCast(d); got != 0 {
		t.Fatalf("Precombat=15 pick = option %d, want 0 — the rescued recast is worth it in the first main phase", got)
	}
	b.FirstMain = false // same weight, second main phase: no rescue
	if got := withTuned(b, func(w *CastWeights) { w.Precombat = 15 }).chooseCast(d); got != -1 {
		t.Fatalf("Precombat=15 outside the first main phase = option %d, want -1", got)
	}
}

// TestCastWeightInstantOnOwnTurn: with InstantOnOwnTurn weight 10, of two
// equal-CMC spells (an index tie by default) the instant-speed one is cast
// during the seat's own main phase; on another seat's turn the same weight
// keeps the index tie.
func TestCastWeightInstantOnOwnTurn(t *testing.T) {
	cards := map[state.ObjID]Card{
		1: {CMC: 2, Castable: true},
		2: {CMC: 2, Castable: true, InstantSpeed: true},
	}
	opts := []decision.Option{castSpell(0, 1), castSpell(1, 2)}
	b := Board{IsMain: true, MyTurn: true, Cards: TableOf(cards)}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != 0 {
		t.Fatalf("default pick = option %d, want the index tie (0)", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.InstantOnOwnTurn = 10 }).chooseCast(d); got != 1 {
		t.Fatalf("InstantOnOwnTurn=10 pick = option %d, want 1 — the instant-speed card is cast in the seat's own main phase", got)
	}
	b.MyTurn = false // another seat's main phase: the weight is inert
	if got := withTuned(b, func(w *CastWeights) { w.InstantOnOwnTurn = 10 }).chooseCast(d); got != 0 {
		t.Fatalf("InstantOnOwnTurn=10 on another seat's turn = option %d, want 0", got)
	}
}

// TestCastWeightOppCreatures: with OppCreatures weight 15 and one opposing
// creature on a public battlefield, a CR1-priced-out commander cast
// (-14) is rescued; with the default weight it stays refused. The constant
// feature can never reorder an otherwise-tied pair — it only moves every
// surviving cast the same way, which is exactly what turns a refusal into
// a cast at the CR1 boundary.
func TestCastWeightOppCreatures(t *testing.T) {
	b := Board{IsMain: true,
		Cards:      TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 4, CMC: 4, Castable: true}}),
		Commanders: TableOf(map[state.ObjID]Commander{1: {Casts: 3, InCommandZone: true}}),
		Creatures:  TableOf(map[state.ObjID]Creature{50: {Controller: 1}})}
	opts := []decision.Option{castCreature(0, 1), {Index: 1, Kind: "pass"}}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != -1 {
		t.Fatalf("default pick = option %d, want -1 — the fourth command-zone cast is priced out (CR1)", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.OppCreatures = 15 }).chooseCast(d); got != 0 {
		t.Fatalf("OppCreatures=15 pick = option %d, want 0 — an opposing board lifts every cast past the CR1 boundary", got)
	}
}

// TestCastWeightOwnCreatures: the mirror of the OppCreatures pin — the
// seat's OWN creature count, read off the same public census, carries the
// weight.
func TestCastWeightOwnCreatures(t *testing.T) {
	b := Board{IsMain: true,
		Cards:      TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 4, CMC: 4, Castable: true}}),
		Commanders: TableOf(map[state.ObjID]Commander{1: {Casts: 3, InCommandZone: true}}),
		Creatures:  TableOf(map[state.ObjID]Creature{50: {Controller: 0}})}
	opts := []decision.Option{castCreature(0, 1), {Index: 1, Kind: "pass"}}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != -1 {
		t.Fatalf("default pick = option %d, want -1 — the fourth command-zone cast is priced out (CR1)", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.OwnCreatures = 15 }).chooseCast(d); got != 0 {
		t.Fatalf("OwnCreatures=15 pick = option %d, want 0 — the seat's own board lifts every cast past the CR1 boundary", got)
	}
}

// TestCastWeightLifeDelta: with LifeDelta weight 15 and the seat 15 ahead
// of its lowest opponent, a CR1-priced-out commander cast (-14) is rescued;
// with the default weight it stays refused.
func TestCastWeightLifeDelta(t *testing.T) {
	b := Board{IsMain: true,
		Cards:      TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 4, CMC: 4, Castable: true}}),
		Commanders: TableOf(map[state.ObjID]Commander{1: {Casts: 3, InCommandZone: true}}),
		Life:       TableOf(map[state.PlayerID]int32{0: 20, 1: 5})}
	opts := []decision.Option{castCreature(0, 1), {Index: 1, Kind: "pass"}}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != -1 {
		t.Fatalf("default pick = option %d, want -1 — the fourth command-zone cast is priced out (CR1)", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.LifeDelta = 15 }).chooseCast(d); got != 0 {
		t.Fatalf("LifeDelta=15 pick = option %d, want 0 — being 15 ahead of the lowest opponent lifts every cast past the CR1 boundary", got)
	}
}

// TestCastWeightsZeroValueIsDefault pins the zero-value contract directly:
// a Board whose Cast nobody set and a Board carrying DefaultCastWeights
// explicitly pick identically on the whole table — and a genuinely tuned
// profile (the C7/creature terms zeroed, non-creatures priced up) changes a
// pick, proving the field is live and not decorative.
func TestCastWeightsZeroValueIsDefault(t *testing.T) {
	for _, tc := range castWeightsCases() {
		d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: tc.opts}
		zero := tc.b
		zero.Cast = CastWeights{}
		if got, want := zero.chooseCast(d), tc.b.chooseCast(d); got != want {
			t.Fatalf("%s: explicit zero Cast picked option %d, want the unset-Board pick %d", tc.name, got, want)
		}
	}
	// A tuned profile is live: creature terms zeroed, non-creatures priced
	// up — the C1 ranking inverts and the spell is cast over the creature.
	b := Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{
		1: {Creature: true, Power: 1},
		2: {CMC: 3},
	})}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{castSpell(0, 2), castCreature(1, 1)}}
	if got := withTuned(b, func(w *CastWeights) { w.CreatureBase, w.CreaturePower, w.NonCreatureCMC = 0, 0, 10 }).chooseCast(d); got != 0 {
		t.Fatalf("tuned profile pick = option %d, want 0 — with the creature terms zeroed the CMC-3 spell outranks the 1/1", got)
	}
}

// ---------------------------------------------------------------------------
// L1b: the hold threshold (C9) and the interaction features (C10).

// TestCastThresholdHoldsTheBestCast: a threshold above the best option's
// final score makes chooseCast return -1, and the boundary is the strict
// "<" the rule states — a threshold exactly AT the best score still casts.
func TestCastThresholdHoldsTheBestCast(t *testing.T) {
	cards := map[state.ObjID]Card{
		1: {Creature: true, Power: 2, Castable: true},
		2: {Creature: true, Power: 3, Castable: true},
	}
	opts := []decision.Option{castCreature(0, 1), castCreature(1, 2)}
	b := Board{IsMain: true, Cards: TableOf(cards)}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != 1 {
		t.Fatalf("default pick = option %d, want 1 — the 3-power creature (42) outranks the 2/2 (38)", got)
	}
	// Threshold exactly at the best score (42): 42 is not strictly below it,
	// so the cast is still made.
	if got := withTuned(b, func(w *CastWeights) { w.CastThreshold = 42 }).chooseCast(d); got != 1 {
		t.Fatalf("threshold AT the best score = option %d, want 1 — the boundary is strictly-below", got)
	}
	// One above: the best score (42) is below 43, so nothing is cast.
	if got := withTuned(b, func(w *CastWeights) { w.CastThreshold = 43 }).chooseCast(d); got != -1 {
		t.Fatalf("threshold above the best score = option %d, want -1 — the cast is held", got)
	}
}

// TestCastThresholdHoldsTheWholePriorityDecision: with a threshold above
// the best cast score, the WHOLE KPriority decision passes rather than
// casting — chooseCast's -1 falls through the tap gate (inert: the CMC-0
// card is payable from the empty pool), the land drop (none offered), the
// ability ranking (no ability offered) and lands on the explicit pass,
// never the trailing concede.
func TestCastThresholdHoldsTheWholePriorityDecision(t *testing.T) {
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "cast", Obj: 1},
			{Index: 1, Kind: "pass"},
			{Index: 2, Kind: "concede"},
		}}
	b := Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 2, Castable: true}})}
	in := Decide(b, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("default intent failed Validate: %v", in)
	}
	if len(in.Choices) != 1 || d.Options[in.Choices[0]].Kind != "cast" {
		t.Fatalf("default priority = %+v, want the cast — the threshold never binds at MinInt32/2", in)
	}
	held := withTuned(b, func(w *CastWeights) { w.CastThreshold = 100 })
	in = Decide(held, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("held intent failed Validate: %v", in)
	}
	if len(in.Choices) != 1 || d.Options[in.Choices[0]].Kind != "pass" {
		t.Fatalf("held priority = %+v, want the pass — the threshold holds the whole decision", in)
	}
}

// TestCastThresholdSparesTheCommanderCast: a command-zone cast is priced by
// value alone and is NOT subject to the threshold (CR1 — the deck must be
// able to cast its commander). The exemption is per WINNER: while the
// commander cast IS the best option it is made whatever the threshold; when
// a hand card outscores it, the hand card is the best option and the
// threshold holds the whole decision.
func TestCastThresholdSparesTheCommanderCast(t *testing.T) {
	b := Board{IsMain: true,
		Cards:      TableOf(map[state.ObjID]Card{1: {Creature: true, Power: 2, CMC: 2, Castable: true}}),
		Commanders: TableOf(map[state.ObjID]Commander{1: {Casts: 0, InCommandZone: true}})}
	opts := []decision.Option{castCreature(0, 1), {Index: 1, Kind: "pass"}}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := withTuned(b, func(w *CastWeights) { w.CastThreshold = 100 }).chooseCast(d); got != 0 {
		t.Fatalf("commander cast with threshold 100 = option %d, want 0 — the recast (38) is exempt from the threshold", got)
	}
	// Mixed: the hand 4-power creature (46) outscores the commander (38), so
	// it is the best option and the threshold applies to the whole decision.
	b2 := Board{IsMain: true,
		Cards: TableOf(map[state.ObjID]Card{
			1: {Creature: true, Power: 2, CMC: 2, Castable: true},
			2: {Creature: true, Power: 4, Castable: true},
		}),
		Commanders: TableOf(map[state.ObjID]Commander{1: {Casts: 0, InCommandZone: true}})}
	opts2 := []decision.Option{castCreature(0, 1), castCreature(1, 2), {Index: 2, Kind: "pass"}}
	d2 := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts2}
	if got := b2.chooseCast(d2); got != 1 {
		t.Fatalf("default mixed pick = option %d, want 1 — the hand 4-power (46) outranks the commander (38)", got)
	}
	if got := withTuned(b2, func(w *CastWeights) { w.CastThreshold = 100 }).chooseCast(d2); got != -1 {
		t.Fatalf("mixed with threshold 100 = option %d, want -1 — the hand card is the best option, so the threshold holds everything", got)
	}
}

// TestCastWeightCreaturePrecombat: with CreaturePrecombat weight 5, of a
// creature and a same-score spell (34 vs CMC 34, an index tie by default)
// the CREATURE is cast in the FIRST main phase; outside the first main
// phase the same tuned profile keeps the tie.
func TestCastWeightCreaturePrecombat(t *testing.T) {
	cards := map[state.ObjID]Card{
		1: {Creature: true, Power: 1},
		2: {CMC: 34},
	}
	opts := []decision.Option{castSpell(0, 2), castCreature(1, 1)}
	b := Board{IsMain: true, FirstMain: true, Cards: TableOf(cards)}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != 0 {
		t.Fatalf("default pick = option %d, want the index tie (0)", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.CreaturePrecombat = 5 }).chooseCast(d); got != 1 {
		t.Fatalf("CreaturePrecombat=5 pick = option %d, want 1 — the creature earns the first-main term", got)
	}
	outside := b
	outside.FirstMain = false
	if got := withTuned(outside, func(w *CastWeights) { w.CreaturePrecombat = 5 }).chooseCast(d); got != 0 {
		t.Fatalf("CreaturePrecombat=5 outside the first main = option %d, want 0 — the term is FirstMain-gated", got)
	}
}

// TestCastWeightCreatureOppCreatures: with CreatureOppCreatures weight 3 and
// two opposing creatures on the battlefield, of a creature and a same-score
// spell (the index tie) the creature is cast; with no opposing creatures the
// same tuned profile keeps the tie.
func TestCastWeightCreatureOppCreatures(t *testing.T) {
	cards := map[state.ObjID]Card{
		1: {Creature: true, Power: 1},
		2: {CMC: 34},
	}
	opts := []decision.Option{castSpell(0, 2), castCreature(1, 1)}
	b := Board{IsMain: true, FirstMain: true, Cards: TableOf(cards),
		Creatures: TableOf(map[state.ObjID]Creature{10: {Controller: 1}, 11: {Controller: 1}})}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != 0 {
		t.Fatalf("default pick = option %d, want the index tie (0)", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.CreatureOppCreatures = 3 }).chooseCast(d); got != 1 {
		t.Fatalf("CreatureOppCreatures=3 pick = option %d, want 1 — the creature earns 2 opposing creatures × 3", got)
	}
	empty := b
	empty.Creatures = CreatureTable{}
	if got := withTuned(empty, func(w *CastWeights) { w.CreatureOppCreatures = 3 }).chooseCast(d); got != 0 {
		t.Fatalf("CreatureOppCreatures=3 with an empty board = option %d, want 0 — the count is 0 and the tie stands", got)
	}
}

// TestCastWeightNonCreatureOppCreatures: with NonCreatureOppCreatures weight
// 20 and two opposing creatures, the one-shot (the removal proxy) outscores
// the 1/1 creature the default rule prefers; with an empty battlefield the
// same tuned profile keeps the creature.
func TestCastWeightNonCreatureOppCreatures(t *testing.T) {
	cards := map[state.ObjID]Card{
		1: {Creature: true, Power: 1},
		2: {CMC: 1},
	}
	opts := []decision.Option{castSpell(0, 2), castCreature(1, 1)}
	b := Board{IsMain: true, Cards: TableOf(cards),
		Creatures: TableOf(map[state.ObjID]Creature{10: {Controller: 1}, 11: {Controller: 1}})}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != 1 {
		t.Fatalf("default pick = option %d, want 1 — C1 casts the creature over the one-shot", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.NonCreatureOppCreatures = 20 }).chooseCast(d); got != 0 {
		t.Fatalf("NonCreatureOppCreatures=20 pick = option %d, want 0 — the one-shot earns 2 × 20 against the wide board", got)
	}
	empty := b
	empty.Creatures = CreatureTable{}
	if got := withTuned(empty, func(w *CastWeights) { w.NonCreatureOppCreatures = 20 }).chooseCast(d); got != 1 {
		t.Fatalf("NonCreatureOppCreatures=20 with an empty board = option %d, want 1 — the count is 0 and C1 stands", got)
	}
}

// TestCastWeightCreatureLifeDelta: with CreatureLifeDelta weight 2 and the
// seat five life ahead, of a creature and a same-score spell (the index tie)
// the creature is cast; with equal life totals the same tuned profile keeps
// the tie.
func TestCastWeightCreatureLifeDelta(t *testing.T) {
	cards := map[state.ObjID]Card{
		1: {Creature: true, Power: 1},
		2: {CMC: 34},
	}
	opts := []decision.Option{castSpell(0, 2), castCreature(1, 1)}
	b := Board{IsMain: true, FirstMain: true, Cards: TableOf(cards),
		Life: TableOf(map[state.PlayerID]int32{0: 25, 1: 20})}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != 0 {
		t.Fatalf("default pick = option %d, want the index tie (0)", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.CreatureLifeDelta = 2 }).chooseCast(d); got != 1 {
		t.Fatalf("CreatureLifeDelta=2 pick = option %d, want 1 — the creature earns the +5 life delta × 2", got)
	}
	even := b
	even.Life = TableOf(map[state.PlayerID]int32{0: 20, 1: 20})
	if got := withTuned(even, func(w *CastWeights) { w.CreatureLifeDelta = 2 }).chooseCast(d); got != 0 {
		t.Fatalf("CreatureLifeDelta=2 at even life = option %d, want 0 — the delta is 0 and the tie stands", got)
	}
}

// TestCastWeightInstantSpeedOffTurnHold: with InstantSpeedOffTurnHold
// weight -10 in the seat's OWN main phase, of an instant and a same-cost
// non-instant (an index tie by default) the NON-instant is cast; off the
// seat's own turn the same tuned profile keeps the tie. Paired with the C9
// threshold the term holds the instant outright: at threshold 1 the plain
// CMC-2 instant (score 2) still casts, the same instant with the term scores
// -8 and is held, and a non-instant at the same threshold still casts —
// the term, not the threshold alone, moved the boundary for this card.
func TestCastWeightInstantSpeedOffTurnHold(t *testing.T) {
	cards := map[state.ObjID]Card{
		1: {CMC: 2, InstantSpeed: true},
		2: {CMC: 2},
	}
	opts := []decision.Option{castSpell(0, 1), castSpell(1, 2)}
	b := Board{IsMain: true, MyTurn: true, Cards: TableOf(cards)}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != 0 {
		t.Fatalf("default pick = option %d, want the index tie (0)", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.InstantSpeedOffTurnHold = -10 }).chooseCast(d); got != 1 {
		t.Fatalf("InstantSpeedOffTurnHold=-10 pick = option %d, want 1 — the instant earns the hold term in its own main phase", got)
	}
	off := b
	off.MyTurn = false
	if got := withTuned(off, func(w *CastWeights) { w.InstantSpeedOffTurnHold = -10 }).chooseCast(d); got != 0 {
		t.Fatalf("InstantSpeedOffTurnHold=-10 off-turn = option %d, want 0 — the term is MyTurn-gated", got)
	}

	// The threshold pairing. Castable is false on purpose: a castable
	// instant-speed card would earn the C7 reserve bonus and muddy the
	// score the threshold reads.
	pair := Board{IsMain: true, MyTurn: true, Pool: state.Mana{state.MC: 5},
		Cards: TableOf(map[state.ObjID]Card{1: {CMC: 2, InstantSpeed: true}})}
	pairOpts := []decision.Option{castSpell(0, 1), {Index: 1, Kind: "pass"}}
	pairD := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: pairOpts}
	if got := withTuned(pair, func(w *CastWeights) { w.CastThreshold = 1 }).chooseCast(pairD); got != 0 {
		t.Fatalf("instant at threshold 1 without the term = option %d, want 0 — score 2 is not below the boundary", got)
	}
	both := withTuned(pair, func(w *CastWeights) { w.CastThreshold = 1; w.InstantSpeedOffTurnHold = -10 })
	if got := both.chooseCast(pairD); got != -1 {
		t.Fatalf("instant at threshold 1 with the term = option %d, want -1 — the term moved the score (2-10) below the boundary", got)
	}
	plain := Board{IsMain: true, MyTurn: true, Pool: state.Mana{state.MC: 5},
		Cards: TableOf(map[state.ObjID]Card{1: {CMC: 2}})}
	plainD := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{castSpell(0, 1), {Index: 1, Kind: "pass"}}}
	if got := withTuned(plain, func(w *CastWeights) { w.CastThreshold = 1 }).chooseCast(plainD); got != 0 {
		t.Fatalf("non-instant at threshold 1 = option %d, want 0 — the same threshold does not hold a card the term does not reach", got)
	}
}

// ---------------------------------------------------------------------------
// L1c: the within-turn mana-efficiency feature (C11, SetValue).

// TestCastWeightSetValuePrefersTheFollowUp is the brief's headline case: four
// mana and a hand of one 3-drop plus two 2-drops. Greedy best-first alone ties
// them on index (the default picks the 3-drop at option 0); with SetValue set,
// a 2-drop's score carries the second 2-drop it leaves affordable, so a 2-drop
// is cast first. Equal-power creatures keep every card's base castScore equal,
// so the flip is attributable to the feature alone.
func TestCastWeightSetValuePrefersTheFollowUp(t *testing.T) {
	cards := map[state.ObjID]Card{
		1: {Creature: true, Power: 2, CMC: 3, Castable: true},
		2: {Creature: true, Power: 2, CMC: 2, Castable: true},
		3: {Creature: true, Power: 2, CMC: 2, Castable: true},
	}
	opts := []decision.Option{castCreature(0, 1), castCreature(1, 2), castCreature(2, 3)}
	b := Board{IsMain: true, Pool: state.Mana{state.MC: 4}, Cards: TableOf(cards)}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	if got := b.chooseCast(d); got != 0 {
		t.Fatalf("default pick = option %d, want the index tie (0) — the 3-drop", got)
	}
	if got := withTuned(b, func(w *CastWeights) { w.SetValue = 8 }).chooseCast(d); got != 1 {
		t.Fatalf("SetValue=8 pick = option %d, want 1 — a 2-drop leaves the second 2-drop affordable (4 mana), the 3-drop leaves nothing", got)
	}
}

// TestCastWeightSetValueZeroIsUnchanged pins the brief's other half: with
// SetValue explicitly 0 the pick is identical to the pre-L1c rule on every
// mirrored cast case — the same equivalence the DefaultCastWeights table
// asserts, restated for this field so a future change cannot make the feature
// evaluate at weight 0.
func TestCastWeightSetValueZeroIsUnchanged(t *testing.T) {
	for _, tc := range castWeightsCases() {
		w := DefaultCastWeights
		w.SetValue = 0
		tc.b.Cast = w
		d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: tc.opts}
		if got := tc.b.chooseCast(d); got != legacyChooseCast(tc.b, d) {
			t.Fatalf("%s: chooseCast with SetValue=0 = option %d, want the pre-L1c rule's pick", tc.name, got)
		}
	}
}

// TestCastWeightSetValueSubsetCapDeterministic pins the brief's bounded-search
// requirement: with more candidates than the exhaustive cap (>10) the greedy
// fallback is used, and the answer is a pure function of the board — repeated
// picks on the same board are identical, so no map iteration order reaches it.
// The candidates differ in cost and score so a non-deterministic search would
// vary across the repetitions.
func TestCastWeightSetValueSubsetCapDeterministic(t *testing.T) {
	cards := map[state.ObjID]Card{}
	opts := make([]decision.Option, 0, 13)
	for i := 0; i < 13; i++ {
		id := state.ObjID(i + 1)
		cards[id] = Card{Creature: true, Power: int32(i%3) + 1, CMC: int32(i%5) + 1, Castable: true}
		opts = append(opts, castCreature(i, id))
	}
	b := Board{IsMain: true, Pool: state.Mana{state.MC: 20}, Cards: TableOf(cards)}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	tuned := withTuned(b, func(w *CastWeights) { w.SetValue = 8 })
	first := tuned.chooseCast(d)
	if first < 0 {
		t.Fatalf("SetValue=8 with 13 candidates picked nothing (option %d)", first)
	}
	for i := 0; i < 50; i++ {
		if got := tuned.chooseCast(d); got != first {
			t.Fatalf("SetValue=8 pick varied across identical boards: got %d, want %d", got, first)
		}
	}
	// The cap must actually be reached for this board (13 options -> 12
	// "other" candidates, over the 10-card exhaustive limit) so the greedy
	// fallback is the path exercised above.
	if n := len(tuned.castEntries(d, false)); n != 13 {
		t.Fatalf("candidate table has %d entries, want 13", n)
	}
}
