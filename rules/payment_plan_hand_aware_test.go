package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Hand-aware payment plans (spec 5 amended, key 6): the rank now reads the
// acting player's OWN hand and prefers, among plans that tie on irreversible
// cost, creature sources, newly activated sources, surplus and flexibility
// consumed, the one whose untapped normal remainder best covers the hand's
// per-colour pip demand. Key 6 sits between flexibility (key 5) and remainder
// diversity (key 7), and is MORE coverage first, so it only breaks ties the
// placeholder used to leave to the typed witness.
//
// Boards here follow the aph-hand-reserve design (the spec §5 key 6 text),
// which this ticket implements. The brief's own Done-means board is pinned in
// TestPaymentPlanHandAwareBriefBoard; its stated pre-change premise
// ("today taps Forest+Forest by the canonical tie-break") was measured FALSE
// at this base: remainder diversity (key 7) already picks Forest+Wastes
// there, because the Wastes' {C} contributes no colour to key 7's census.
// The gap key 6 actually closes is the object-ID tie below, measured
// Forest+Plains before this change.

const handAwarePlains = "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n"
const handAwareWastes = "Name:Wastes\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ C\nOracle:x\n"

// handAwareSources asserts the plan's source set, order-insensitively.
func handAwareSources(t *testing.T, got PaymentPlanOutcome) []state.ObjID {
	t.Helper()
	if got.Plan == nil {
		t.Fatalf("no plan: reason=%q detail=%q nodes=%d", got.Reason, got.Detail, got.Nodes)
	}
	if got.Reason != "" {
		t.Fatalf("plan outcome reason = %q, want a clean plan", got.Reason)
	}
	out := make([]state.ObjID, 0, len(got.Plan.Activations))
	for _, a := range got.Plan.Activations {
		out = append(out, a.Source)
	}
	return out
}

func handAwareSameSources(a, b []state.ObjID) bool {
	return len(a) == len(b) && reflect.DeepEqual(sortedObjIDs(a), sortedObjIDs(b))
}

func sortedObjIDs(in []state.ObjID) []state.ObjID {
	out := append([]state.ObjID(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// oppHand adds an authored card straight to seat 1's hand (the opponent's
// hand must never move the acting seat's plan, so the test needs one there).
func oppHand(t *testing.T, e *Engine, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), 1)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, append(e.G.Zone(state.ZHand, 1), o.ID))
	return o.ID
}

// Done-means (aph-hand-reserve 1, the P5 probe): {1}{W} over Forest, Island,
// Plains with a {G} instant in hand. Every plan ties on keys 1-5 and key 7
// (each leaves exactly one colour), so before key 6 the typed witness's
// object-ID compare picked Forest+Plains (Forest holds the lowest id) and
// stranded the green card. Hand reserve must keep the Forest's green.
func TestPaymentPlanHandAwareKeepsHandGreenCastable(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9721, "Name:White Two\nManaCost:1 W\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	forest := onBoard(t, e, 0, rankForest)
	island := onBoard(t, e, 0, rankIsland)
	plains := onBoard(t, e, 0, handAwarePlains)
	if !(forest < island && island < plains) {
		t.Fatalf("precondition: source ids %d %d %d not ascending (the tie the key must break)", forest, island, plains)
	}
	green := choiceHand(t, e, "Name:Green One\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	if z := e.G.Obj(green).Zone; z != state.ZHand {
		t.Fatalf("precondition: green card zone = %s, want hand", z)
	}
	rankWantSteps(t, e.PlanCastPayment(0, paymentCast(spell)),
		rankStep{island, rankMana('U', 1)}, rankStep{plains, rankMana('W', 1)})
	before := len(e.L.Events)
	rankSubmit(t, e, spell)
	if e.G.Obj(forest).Tapped {
		t.Fatal("the Forest was tapped although Island+Plains paid; the hand's green card is stranded")
	}
	for _, id := range []state.ObjID{island, plains} {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("source %d untapped after the planned payment", id)
		}
	}
	if n := ppTaps(e, before, forest); n != 0 {
		t.Fatalf("Forest tapped %d times in the log", n)
	}
}

// Done-means 2: the same board with a {U} instant in hand instead. Now the
// Island's blue is the demanded colour, and the Forest must pay.
func TestPaymentPlanHandAwareKeepsHandBlueCastable(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9722, "Name:White Two\nManaCost:1 W\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	forest := onBoard(t, e, 0, rankForest)
	onBoard(t, e, 0, rankIsland)
	plains := onBoard(t, e, 0, handAwarePlains)
	choiceHand(t, e, "Name:Blue One\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	rankWantSteps(t, e.PlanCastPayment(0, paymentCast(spell)),
		rankStep{forest, rankMana('G', 1)}, rankStep{plains, rankMana('W', 1)})
}

// The brief's Done-means board: an empty pool, battlefield in zone order
// Forest, Forest, Wastes, Mountain, hand A = a {2} artifact (the cast) and
// B = a {R}{G} spell. Every 2-source plan for A ties on keys 1-5 (every
// source has one alternative), so the hand-aware key decides; the plan must
// leave one Forest and the Mountain untapped, keeping B's {R}{G} payable.
// (Measured at the pre-change base the plan was ALREADY Forest+Wastes via
// key 7 remainder diversity — the brief's "pins Forest+Forest today" premise
// was false — so this board pins the composed outcome, and the decisive key-6
// behaviour is pinned by the two P5 boards above and the rank unit test.)
func TestPaymentPlanHandAwareBriefBoard(t *testing.T) {
	e, _, artifact := newFixtureDeck(t, 9723, "Name:Gray Artifact\nManaCost:2\nTypes:Artifact\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	toMain1(t, e)
	f1 := onBoard(t, e, 0, rankForest)
	f2 := onBoard(t, e, 0, rankForest)
	wastes := onBoard(t, e, 0, handAwareWastes)
	mountain := onBoard(t, e, 0, ppMountain)
	if !(f1 < f2 && f2 < wastes && wastes < mountain) {
		t.Fatalf("precondition: source ids %d %d %d %d not ascending (Forests lowest)", f1, f2, wastes, mountain)
	}
	rg := choiceHand(t, e, "Name:RG Card\nManaCost:R G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	if z := e.G.Obj(rg).Zone; z != state.ZHand {
		t.Fatalf("precondition: RG card zone = %s, want hand", z)
	}
	g := rankMana('G', 1)
	rankWantSteps(t, e.PlanCastPayment(0, paymentCast(artifact)), rankStep{f1, g}, rankStep{wastes, rankMana('C', 1)})
	before := len(e.L.Events)
	rankSubmit(t, e, artifact)
	if e.G.Obj(f2).Tapped || e.G.Obj(mountain).Tapped {
		t.Fatal("a Forest and the Mountain must stay untapped so the {R}{G} card stays castable")
	}
	if !e.G.Obj(f1).Tapped || !e.G.Obj(wastes).Tapped {
		t.Fatal("the paying sources were not tapped")
	}
	if n := ppTaps(e, before, mountain); n != 0 {
		t.Fatalf("Mountain tapped %d times in the log", n)
	}
}

// Done-means 3: with no other nonland card in hand the demand is zero, key 6
// packs to 0 for every plan, and the rank falls through to remainder
// diversity and the typed witness — here Forest+Plains, exactly as before the
// key existed. The offer must be a pure function of the position: identical
// across a rerun and an engine clone.
func TestPaymentPlanHandAwareWithoutHandDemandIsUnchanged(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9724, "Name:White Two\nManaCost:1 W\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	forest := onBoard(t, e, 0, rankForest)
	onBoard(t, e, 0, rankIsland)
	plains := onBoard(t, e, 0, handAwarePlains)
	for _, nonland := range e.G.Zone(state.ZHand, 0) {
		if f := e.G.Obj(nonland).Face(); f != nil && !f.IsLand() && nonland != spell {
			t.Fatalf("precondition: unexpected nonland %q in hand", f.Name)
		}
	}
	rankWantSteps(t, e.PlanCastPayment(0, paymentCast(spell)),
		rankStep{forest, rankMana('G', 1)}, rankStep{plains, rankMana('W', 1)})
	first := e.PlanCastPayment(0, paymentCast(spell))
	again := e.PlanCastPayment(0, paymentCast(spell))
	if !reflect.DeepEqual(*first.Plan, *again.Plan) {
		t.Fatalf("rerun differs:\n%+v\n%+v", *first.Plan, *again.Plan)
	}
	cloned := e.Clone().PlanCastPayment(0, paymentCast(spell))
	if !reflect.DeepEqual(*first.Plan, *cloned.Plan) {
		t.Fatalf("clone differs:\n%+v\n%+v", *first.Plan, *cloned.Plan)
	}
}

// Done-means 4: the demand reads only the acting player's OWN hand. Adding a
// {G}{G} card to the OPPONENT's hand changes neither the plan nor its ID.
func TestPaymentPlanHandAwareIgnoresOpponentHand(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9725, "Name:White Two\nManaCost:1 W\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, rankForest)
	island := onBoard(t, e, 0, rankIsland)
	plains := onBoard(t, e, 0, handAwarePlains)
	// The acting seat's own hand demands green: Island+Plains wins.
	choiceHand(t, e, "Name:Green One\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	before := *e.PlanCastPayment(0, paymentCast(spell)).Plan
	oppHand(t, e, "Name:Green Three\nManaCost:G G G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	after := *e.PlanCastPayment(0, paymentCast(spell)).Plan
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("the opponent's hand moved the plan:\nbefore %+v\nafter  %+v", before, after)
	}
	if before.ID != "" {
		t.Fatal("precondition: query-built plan carries no ID; asserting the plan body instead")
	}
	rankWantSteps(t, PaymentPlanOutcome{Plan: &after}, rankStep{island, rankMana('U', 1)}, rankStep{plains, rankMana('W', 1)})
}

// Done-means 5: keys 1-5 still dominate key 6. A {1}{W} cast over Forest,
// Plains and a non-sick {C}{C} creature rock with a {G} card in hand: the
// rock+Plains plan would cover the demand (the Forest's green stays), but it
// taps a creature, so the creature key refuses it and Forest+Plains pays.
func TestPaymentPlanHandAwareCostKeysDominate(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9726, "Name:White Two\nManaCost:1 W\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	forest := onBoard(t, e, 0, rankForest)
	plains := onBoard(t, e, 0, handAwarePlains)
	rock := onBoardReady(t, e, 0, "Name:CC Rock\nTypes:Artifact Creature Golem\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ C | Amount$ 2\nOracle:x\n")
	choiceHand(t, e, "Name:Green One\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	if e.G.Obj(rock).Tapped || !e.IsCreature(rock) {
		t.Fatalf("precondition: rock tapped=%v creature=%v", e.G.Obj(rock).Tapped, e.IsCreature(rock))
	}
	for _, a := range handAwareSources(t, e.PlanCastPayment(0, paymentCast(spell))) {
		if a == rock {
			t.Fatal("the creature rock was planned although two lands pay")
		}
	}
	rankWantSteps(t, e.PlanCastPayment(0, paymentCast(spell)),
		rankStep{forest, rankMana('G', 1)}, rankStep{plains, rankMana('W', 1)})
}

// The key-6 computation on constructed rank inputs: the coverage digits pack
// in descending demand order (WUBRG ties), the packed value orders MORE
// first, and key 1 still dominates it.
func TestPaymentPlanHandAwareRankKeyUnit(t *testing.T) {
	// Demand {R:2, G:1}: demand order [R, G], digit base 3.
	ctx := newPaymentPlanRankContext(nil, [5]int{state.MR: 2, state.MG: 1})
	if ctx.demandOrder != [5]int{state.MR, state.MG, state.MW, state.MU, state.MB} {
		t.Fatalf("demand order = %v, want R G then the untouched WUBRG tail", ctx.demandOrder)
	}
	if ctx.reserveBase != 3 {
		t.Fatalf("reserve base = %d, want 3", ctx.reserveBase)
	}
	// A context with colour supply: two red sources, one green, one blue.
	ctx = newPaymentPlanRankContext(nil, [5]int{state.MR: 2, state.MG: 1})
	ctx.colourSources = [5]int{1, 1, 0, 2, 1}
	step := func(id state.ObjID, col int) plannedManaActivation {
		return plannedManaActivation{activation: decision.PaymentActivation{Source: id}, flex: 1, colours: 1 << col}
	}
	rank := func(as ...plannedManaActivation) paymentPlanRank {
		acts := make([]decision.PaymentActivation, len(as))
		for i := range as {
			acts[i] = as[i].activation
		}
		return rankPaymentPlan(ctx, decision.PaymentPlan{Activations: acts}, as, state.Mana{})
	}
	// Consuming both reds leaves green+blue: coverage R=min(0,2)=0, G=min(1,1)=1;
	// the zero-demand W/U/B digits are constant 0, so the packed value is 27.
	bothReds := rank(step(1, state.MR), step(2, state.MR))
	// Consuming one red leaves one red + green + blue: coverage R=1, G=1 -> 108.
	oneRed := rank(step(1, state.MR))
	// Consuming green leaves both reds: coverage R=2, G=0 -> 162.
	green := rank(step(3, state.MG))
	if bothReds.handReserve != 27 || oneRed.handReserve != 108 || green.handReserve != 162 {
		t.Fatalf("packed coverages = %d %d %d, want 27 108 162 (five digits: R G then the zero-demand W U B)",
			bothReds.handReserve, oneRed.handReserve, green.handReserve)
	}
	if !(bothReds.handReserve < oneRed.handReserve && oneRed.handReserve < green.handReserve) {
		t.Fatal("precondition: coverage values not strictly ordered")
	}
	if !green.less(oneRed) || !oneRed.less(bothReds) {
		t.Fatal("key 6 must rank larger coverage first (consumes the least-demanded sources)")
	}
	// Keys 1-5 dominate: a costly single step beats a free two-creature plan
	// with worse coverage.
	costly := rank(plannedManaActivation{activation: decision.PaymentActivation{Source: 9}, flex: 1, colours: 1 << state.MR,
		consequence: paymentConsequence{damage: 1}})
	free := rank(step(4, state.MR), step(5, state.MR))
	if !free.less(costly) || costly.less(free) {
		t.Fatalf("cost key must dominate key 6 (%+v vs %+v)", free, costly)
	}
	// Zero demand packs to 0 for every plan: the tie value the placeholder
	// pinned, so boards without hand demand rank exactly as before.
	plain := newPaymentPlanRankContext(nil, [5]int{})
	plain.colourSources = [5]int{1, 1, 1, 1, 1}
	zero := func(as ...plannedManaActivation) paymentPlanRank {
		acts := make([]decision.PaymentActivation, len(as))
		for i := range as {
			acts[i] = as[i].activation
		}
		return rankPaymentPlan(plain, decision.PaymentPlan{Activations: acts}, as, state.Mana{})
	}
	if z := zero(step(1, state.MW)); z.handReserve != 0 {
		t.Fatalf("zero-demand hand reserve = %d, want 0", z.handReserve)
	}
}

// costPips: plain pips for their colour, hybrid for both, Phyrexian for its
// colour, twobrid for its coloured face, hybrid-Phyrexian for both; generic,
// {X} and {C} for nothing.
func TestPaymentPlanManaPips(t *testing.T) {
	cases := []struct {
		name string
		cost Cost
		want [5]int
	}{
		{"plain pips", Cost{Colored: state.Mana{state.MR: 2, state.MG: 1}}, [5]int{state.MR: 2, state.MG: 1}},
		{"hybrid counts both", Cost{Hybrid: []ManaPair{{A: 'G', B: 'W'}}}, [5]int{state.MW: 1, state.MG: 1}},
		{"phyrexian counts its colour", Cost{Phyrexian: []byte{'U'}}, [5]int{state.MU: 1}},
		{"twobrid counts its face", Cost{Twobrid: []Twobrid{{Generic: 2, Col: 'B'}}}, [5]int{state.MB: 1}},
		{"hybrid phyrexian counts both", Cost{HybridPhyrexian: []HybridPhyrexian{{A: 'R', B: 'B'}}}, [5]int{state.MR: 1, state.MB: 1}},
		{"generic and X and C count nothing", Cost{Generic: 3, X: 1, Colored: state.Mana{state.MC: 2}}, [5]int{}},
	}
	for _, c := range cases {
		if got := costPips(c.cost); got != c.want {
			t.Errorf("%s: costPips = %v, want %v", c.name, got, c.want)
		}
	}
	// Through the real parser: the authored printed costs of the board above.
	e := layerEngine(t)
	if got := costPips(e.rawBaseCost(0, 0)); got != [5]int{} {
		t.Errorf("empty cost: costPips = %v, want zeros", got)
	}
}
