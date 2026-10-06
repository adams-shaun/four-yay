package rules

import (
	"fmt"
	"math/bits"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules/pay"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Boards for aph-rank-search (spec 5, amended 2026-09-26): the planner
// chooses a COUNT per class of interchangeable units, solves coloured pips
// before generic, and prunes by branch-and-bound on the rank, so ordinary wide
// boards return the rank-best plan well inside the node budget. The
// brute-force oracle below is the old unit-by-unit walk with no budget: it is
// the reference every search answer is compared with.

const (
	srchIsland   = "Name:Island\nTypes:Basic Land Island\nOracle:x\n"
	srchMountain = "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n"
	srchForest   = "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"
	srchPlains   = "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n"
	srchSwamp    = "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n"
	srchVolcanic = "Name:Volcanic Test\nTypes:Land Island Mountain\nOracle:x\n"
	srchSavannah = "Name:Savannah Test\nTypes:Land Forest Plains\nOracle:x\n"
	srchBadlands = "Name:Badlands Test\nTypes:Land Swamp Mountain\nOracle:x\n"
	srchCombo    = "Name:Combo Test\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Combo U B | SpellDescription$ x\nOracle:x\n"
	srchDork     = "Name:Dork Test\nTypes:Creature Elf\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ x\nOracle:x\n"
	srchAnyRock  = "Name:Any Rock\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ Any | SpellDescription$ x\nOracle:x\n"
	srchWastes   = "Name:Wastes Test\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ x\nOracle:x\n"
	srchTwoRock  = "Name:Two Rock\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ C | Amount$ 2 | SpellDescription$ x\nOracle:x\n"
)

// srchSpell is an ordinary instant with the given Forge mana cost.
func srchSpell(cost string) string {
	return "Name:Search Spell\nManaCost:" + cost + "\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n"
}

// paymentPlanSearchOracle is the brute-force reference: every subset of the
// planner's own source units x every alternative of each chosen unit, in
// battlefield (unit) order, each complete candidate settled by the ordinary
// solver and ranked by pay.RankPlan, the best kept by pay.Rank.less.
// It has no node budget and no rank-based pruning. It never keeps a set
// whose summed life + damage would kill the caster (the lethal guard). It
// skips only sets that provably cannot pay or provably cannot win, by facts
// that hold for every rank key order:
//
//   - a set that already pays is not extended: any superset has strictly more
//     sources and no smaller irreversible cost or creature count, so it ranks
//     strictly lower (keys 1-3) than the set it extends;
//   - a set of more units than the cost has symbols is such a superset: in
//     any settlement some unit's mana pays no symbol, and without that unit
//     the rest still pays;
//   - a prefix whose pool plus everything the rest of the board could add is
//     below the cost's total, or below one colour's requirement, cannot pay.
func paymentPlanSearchOracle(e *Engine, p state.PlayerID, cast state.ObjID, cost Cost) (*decision.PaymentPlan, int) {
	return paymentPlanSearchOracleOver(e, p, cost, e.paymentPlanQueryChoices(p), pay.PaymentPlanHandDemand(asPayer(e), p, cast))
}

// paymentPlanSearchOracleOver is the oracle over an explicit alternative
// table (one entry per unit, in unit order) and hand demand (rank key 6;
// the planner's is paymentPlanHandDemand of the acting player without the
// cast card).
func paymentPlanSearchOracleOver(e *Engine, p state.PlayerID, cost Cost, choices [][]pay.Alt, demand [5]int) (*decision.PaymentPlan, int) {
	ctx := pay.NewRankContext(choices, demand)
	pool := e.G.Players[p].Pool
	life := e.G.Players[p].Life
	need := cost.Generic + cost.Colored.Total()
	rest := make([]state.Mana, len(choices)+1) // most of each type the units from i on can add
	restTotal := make([]int32, len(choices)+1)
	for i := len(choices) - 1; i >= 0; i-- {
		rest[i] = rest[i+1]
		var most int32
		var mostOf state.Mana
		for _, a := range choices[i] {
			most = max(most, a.Mana.Total())
			for k, n := range a.Mana {
				mostOf[k] = max(mostOf[k], n)
			}
		}
		rest[i] = pay.ManaAdd(rest[i+1], mostOf)
		restTotal[i] = restTotal[i+1] + most
	}
	var best *decision.PaymentPlan
	var bestRank pay.Rank
	visited := 0
	var walk func(int, state.Mana, []pay.Alt)
	walk = func(at int, produced state.Mana, chosen []pay.Alt) {
		visited++
		// The lethal guard (spec 5): a set whose summed life + damage is at
		// least the caster's life is never a plan, nor is any superset.
		var pain int64
		for _, a := range chosen {
			pain += pay.ConsequencePain(a.Consequence)
		}
		if pain > 0 && pain >= int64(life) {
			return
		}
		if paid, ok := resolveManaWith(cost, pay.ManaAdd(pool, produced), state.Mana{}, [7]state.Mana{}, life, false, pipRider{}, nil); ok {
			plan := pay.Witness(cost, pool, produced, chosen, paid.Pool)
			r := pay.RankPlan(ctx, plan, chosen, paid.Pool)
			if best == nil || r.Less(bestRank) {
				best, bestRank = &plan, r
			}
			return
		}
		if at == len(choices) || len(chosen) == decision.MaxPaymentActivations || int32(len(chosen)) >= need {
			return
		}
		if pool.Total()+produced.Total()+restTotal[at] < need {
			return
		}
		for k := range cost.Colored {
			if pool[k]+produced[k]+rest[at][k] < cost.Colored[k] {
				return
			}
		}
		walk(at+1, produced, chosen)
		for _, a := range choices[at] {
			// The DFS shares one backing array: a sibling overwrites slot
			// len(chosen) only after the previous sibling's subtree has
			// returned, and nothing keeps chosen (pay.Witness copies it,
			// pay.RankPlan only reads it). A fresh copy per branch was
			// this test oracle's whole allocation, gigabytes per suite run.
			walk(at+1, pay.ManaAdd(produced, a.Mana), append(chosen, a))
		}
	}
	walk(0, state.Mana{}, make([]pay.Alt, 0, decision.MaxPaymentActivations))
	return best, visited
}

// paymentPlanQueryChoices is the planner's per-unit alternative table.
func (e *Engine) paymentPlanQueryChoices(p state.PlayerID) [][]pay.Alt {
	units := e.paymentPlanManaUnits(p)
	choices := make([][]pay.Alt, len(units))
	for i, u := range units {
		choices[i] = pay.PaymentPlanUnitAlternatives(asPayer(e), u)
	}
	return choices
}

// srchCost parses a Forge mana-cost string into the planner's Cost.
func srchCost(t *testing.T, s string) Cost {
	t.Helper()
	var c Cost
	for _, tok := range strings.Fields(s) {
		switch {
		case tok[0] >= '0' && tok[0] <= '9':
			var n int32
			if _, err := fmt.Sscanf(tok, "%d", &n); err != nil {
				t.Fatalf("cost token %q: %v", tok, err)
			}
			c.Generic += n
		case len(tok) == 1 && strings.ContainsRune("WUBRGC", rune(tok[0])):
			c.Colored[state.ManaIndex(tok[0])]++
		default:
			t.Fatalf("cost token %q unsupported", tok)
		}
	}
	return c
}

func srchDuals(plan *decision.PaymentPlan, duals map[state.ObjID]bool) int {
	n := 0
	for _, a := range plan.Activations {
		if duals[a.Source] {
			n++
		}
	}
	return n
}

// Done-means 1 (research probe P10, spec 5's PP-26 boards): basics plus as
// many typed duals, the duals seated FIRST so battlefield order favours them.
// Enough Islands exist every time, so the rank-best plan taps no dual; the
// old walk hit the node limit and tapped 1, 3 and 4 duals.
func TestPaymentPlanSearchBasicsBeforeDualsOnWideBoards(t *testing.T) {
	t.Parallel()
	for i, tc := range []struct {
		basics int
		cost   string
		want   int
	}{
		{8, "6 U", 7},
		{10, "8 U", 9},
		{12, "9 U", 10},
	} {
		t.Run(tc.cost, func(t *testing.T) {
			e, _, spell := newFixtureDeck(t, 9950+uint64(i), srchSpell(tc.cost))
			duals := map[state.ObjID]bool{}
			for k := 0; k < tc.basics; k++ {
				duals[onBoard(t, e, 0, srchVolcanic)] = true
				onBoard(t, e, 0, srchIsland)
			}
			got := e.PlanCastPayment(0, paymentCast(spell))
			if got.Plan == nil || got.Reason != "" {
				t.Fatalf("outcome reason=%q detail=%q nodes=%d plan=%v, want a clean plan", got.Reason, got.Detail, got.Nodes, got.Plan != nil)
			}
			if n := len(got.Plan.Activations); n != tc.want {
				t.Fatalf("activations = %d, want %d", n, tc.want)
			}
			if n := srchDuals(got.Plan, duals); n != 0 {
				t.Fatalf("plan taps %d duals, want 0: %+v", n, got.Plan.Activations)
			}
			if got.Nodes >= 5000 {
				t.Fatalf("nodes = %d, want < 5000", got.Nodes)
			}
			if err := e.ValidateCastPayment(0, paymentCast(spell), *got.Plan); err != nil {
				t.Fatalf("validate: %v", err)
			}
			t.Logf("%d+%d duals at {%s}: nodes=%d", tc.basics, tc.basics, tc.cost, got.Nodes)
		})
	}
}

// Done-means 2 (the gaps audit's search-limit proof): a 12-generic spell over
// 30 Mountains and 6 duals used to exhaust the budget with no plan at all.
func TestPaymentPlanSearchThirtyMountainsTwelveGeneric(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9960, srchSpell("12"))
	duals := map[state.ObjID]bool{}
	for k := 0; k < 30; k++ {
		onBoard(t, e, 0, srchMountain)
	}
	for k := 0; k < 6; k++ {
		duals[onBoard(t, e, 0, srchVolcanic)] = true
	}
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan == nil || got.Reason != "" {
		t.Fatalf("outcome reason=%q nodes=%d plan=%v, want a clean plan", got.Reason, got.Nodes, got.Plan != nil)
	}
	if len(got.Plan.Activations) != 12 || srchDuals(got.Plan, duals) != 0 {
		t.Fatalf("plan = %+v, want 12 Mountains", got.Plan.Activations)
	}
	if got.Nodes >= 1000 {
		t.Fatalf("nodes = %d, want < 1000", got.Nodes)
	}
	again := e.PlanCastPayment(0, paymentCast(spell))
	if !reflect.DeepEqual(got, again) {
		t.Fatalf("repeated query differs:\n%#v\n%#v", got, again)
	}
	t.Logf("30 Mountains + 6 duals at {12}: nodes=%d", got.Nodes)
}

// srchMixedBoard seats the 20-source mixed board of Done-means 3: basics,
// typed duals, a Combo dual, a creature dork, an Any rock, a {C} land and a
// two-mana rock, interleaved so neither battlefield order nor object IDs group
// a class together.
func srchMixedBoard(t *testing.T, e *Engine) {
	t.Helper()
	for _, src := range []string{
		srchIsland, srchVolcanic, srchMountain, srchForest, srchCombo,
		srchIsland, srchPlains, srchMountain, srchSavannah, srchDork,
		srchSwamp, srchIsland, srchBadlands, srchForest, srchAnyRock,
		srchMountain, srchVolcanic, srchWastes, srchIsland, srchTwoRock,
	} {
		if src == srchDork {
			onBoardReady(t, e, 0, src)
			continue
		}
		onBoard(t, e, 0, src)
	}
}

var srchOracleCosts = []string{
	"U", "R", "G", "W", "B", "C", "1", "3", "5",
	"1 U", "2 R R", "U B", "W G", "U R G", "2 W W", "B B B", "G G G",
	"4 U U", "6 G", "1 C C", "W U B R G", "2 U B", "3 R G", "U U U U",
	"1 W U", "2 B R", "R R R", "4", "7", "G W U",
	// infeasible: the search must agree there is no plan
	"W W W W", "C C C", "B B B B B B",
}

// Done-means 3: on a 20-source mixed board the search's plan equals the
// brute-force oracle's best for every cost, and repeated queries and clones
// give identical plans and IDs.
func TestPaymentPlanSearchMatchesOracleOnMixedBoard(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9970, srchSpell("U"))
	srchMixedBoard(t, e)
	if n := len(e.paymentPlanManaUnits(0)); n != 20 {
		t.Fatalf("mixed board has %d source units, want 20", n)
	}
	cast := paymentCast(spell)
	agreePlans, agreeNone := 0, 0
	for _, s := range srchOracleCosts {
		cost := srchCost(t, s)
		want, visited := paymentPlanSearchOracle(e, 0, cast.Object, cost)
		got := pay.PlanPaymentCost(asPayer(e), 0, cast, cost)
		if got.Reason == "search_limit" {
			t.Fatalf("{%s}: search_limit after %d nodes", s, got.Nodes)
		}
		if want == nil {
			if got.Plan != nil || got.Reason != "insufficient" {
				t.Fatalf("{%s}: search = %+v, oracle found no plan", s, got)
			}
			agreeNone++
			continue
		}
		if got.Plan == nil || !reflect.DeepEqual(*got.Plan, *want) {
			t.Fatalf("{%s}: search plan differs from the oracle (search nodes %d, oracle visited %d)\nsearch: %+v\noracle: %+v", s, got.Nodes, visited, got.Plan, want)
		}
		clone := e.Clone()
		again := pay.PlanPaymentCost(asPayer(e), 0, cast, cost)
		fromClone := pay.PlanPaymentCost(asPayer(clone), 0, cast, cost)
		if !reflect.DeepEqual(got, again) || !reflect.DeepEqual(got, fromClone) {
			t.Fatalf("{%s}: repeated or cloned query differs:\n%#v\n%#v\n%#v", s, got, again, fromClone)
		}
		id1, err1 := decision.PaymentPlanID(7, 0, cast, *got.Plan)
		id2, err2 := decision.PaymentPlanID(7, 0, cast, *fromClone.Plan)
		if err1 != nil || err2 != nil || id1 != id2 {
			t.Fatalf("{%s}: plan IDs %q/%v and %q/%v differ", s, id1, err1, id2, err2)
		}
		agreePlans++
		t.Logf("{%s}: %d steps, search nodes %d, oracle visited %d", s, len(got.Plan.Activations), got.Nodes, visited)
	}
	if agreePlans < 25 {
		t.Fatalf("oracle agreed on %d planned costs, want >= 25", agreePlans)
	}
	t.Logf("oracle agreement: %d planned costs, %d insufficient costs", agreePlans, agreeNone)
}

// The same oracle check over the class-heavy shapes: many identical units
// where the witness must pick the lowest IDs, a floating pool, and seat
// orders that put a class's members out of ID order.
func TestPaymentPlanSearchMatchesOracleOnClassBoards(t *testing.T) {
	t.Parallel()
	boards := []struct {
		name  string
		srcs  []string
		pool  string
		costs []string
	}{
		{"islands-duals", []string{srchVolcanic, srchIsland, srchVolcanic, srchIsland, srchVolcanic, srchIsland, srchMountain, srchMountain},
			"", []string{"U", "1 U", "3 U", "U R", "U U R", "2 R", "5", "U U U R"}},
		{"any-rocks", []string{srchAnyRock, srchIsland, srchAnyRock, srchCombo, srchCombo, srchSwamp, srchTwoRock, srchTwoRock},
			"", []string{"W", "W W", "U B", "B B B", "2", "3", "4 W", "W U B", "5 B"}},
		{"floating", []string{srchMountain, srchIsland, srchVolcanic, srchForest},
			"R G", []string{"R", "1", "2 U", "R R", "G G", "3", "U R G", "C"}},
		{"dorks", []string{srchForest, srchDork, srchDork, srchForest, srchSavannah, srchDork},
			"", []string{"G", "1 G", "2 G", "G G G", "W G", "4", "5 G"}},
	}
	checked := 0
	for bi, b := range boards {
		e, _, spell := newFixtureDeck(t, 9980+uint64(bi), srchSpell("U"))
		for _, src := range b.srcs {
			if src == srchDork {
				onBoardReady(t, e, 0, src)
				continue
			}
			onBoard(t, e, 0, src)
		}
		for _, sym := range strings.Fields(b.pool) {
			e.G.Players[0].Pool[state.ManaIndex(sym[0])]++
		}
		for _, s := range b.costs {
			cost := srchCost(t, s)
			want, _ := paymentPlanSearchOracle(e, 0, spell, cost)
			got := pay.PlanPaymentCost(asPayer(e), 0, paymentCast(spell), cost)
			if want == nil {
				if got.Plan != nil || got.Reason != "insufficient" {
					t.Fatalf("%s {%s}: search = %+v, oracle found no plan", b.name, s, got)
				}
				continue
			}
			if got.Reason != "" || got.Plan == nil || !reflect.DeepEqual(*got.Plan, *want) {
				t.Fatalf("%s {%s}: search (%q, %d nodes) differs from the oracle\nsearch: %+v\noracle: %+v", b.name, s, got.Reason, got.Nodes, got.Plan, want)
			}
			checked++
		}
	}
	t.Logf("oracle agreement on class boards: %d planned costs", checked)
}

// Done-means 4: a dual assigned to one colour must not strand a later pip.
// The existing TestPaymentPlanBacktracksExclusiveSources shape, the dual
// seated first, and a three-colour board where every greedy first choice of
// the Volcanic's colour fails.
func TestPaymentPlanSearchBacktracksDualColours(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9990, srchSpell("U R"))
	dual := onBoard(t, e, 0, srchVolcanic)
	island := onBoard(t, e, 0, srchIsland)
	rankWantSteps(t, e.PlanCastPayment(0, paymentCast(spell)),
		rankStep{dual, rankMana('R', 1)}, rankStep{island, rankMana('U', 1)})

	e2, _, spell2 := newFixtureDeck(t, 9991, srchSpell("U B R"))
	volcanic := onBoard(t, e2, 0, srchVolcanic)
	badlands := onBoard(t, e2, 0, srchBadlands)
	combo := onBoard(t, e2, 0, srchCombo)
	rankWantSteps(t, e2.PlanCastPayment(0, paymentCast(spell2)),
		rankStep{volcanic, rankMana('R', 1)}, rankStep{badlands, rankMana('B', 1)}, rankStep{combo, rankMana('U', 1)})

	e3, _, spell3 := newFixtureDeck(t, 9992, srchSpell("U R"))
	onBoard(t, e3, 0, srchVolcanic)
	if got := e3.PlanCastPayment(0, paymentCast(spell3)); got.Plan != nil || got.Reason != "insufficient" {
		t.Fatalf("one dual for {U}{R} = %+v, want insufficient", got)
	}
}

// A floating pool that already pays needs no activation at all.
func TestPaymentPlanSearchPoolAlonePays(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9993, srchSpell("1 R"))
	onBoard(t, e, 0, srchMountain)
	e.G.Players[0].Pool[state.ManaIndex('R')] = 2
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan == nil || got.Reason != "" || len(got.Plan.Activations) != 0 || got.Nodes != 1 {
		t.Fatalf("pool-only outcome = %+v, want a zero-step plan at the root", got)
	}
}

// srchIndexAgrees compares the per-query zone-entry index with the backward
// log scan for every battlefield object, returning how many tokens and
// re-entered objects it checked.
func srchIndexAgrees(t *testing.T, e *Engine, where string) (tokens, reentered, checked int) {
	t.Helper()
	entries := map[state.ObjID]int{}
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield {
			entries[ev.Obj]++
		}
	}
	for _, pl := range e.G.Players {
		for _, id := range e.G.Zone(state.ZBattlefield, pl.ID) {
			want := pay.PaymentSourceZoneSeqScan(asPayer(e), id)
			if got := e.zoneEntrySeq(id); got != want {
				t.Fatalf("%s: object %d index seq %d, log scan %d", where, id, got, want)
			}
			if got := pay.PaymentSourceZoneSeq(asPayer(e), id); got != want {
				t.Fatalf("%s: object %d paymentSourceZoneSeq %d, log scan %d", where, id, got, want)
			}
			checked++
			if o := e.G.Obj(id); o != nil && o.IsToken {
				tokens++
			}
			if entries[id] > 1 {
				reentered++
			}
		}
	}
	return tokens, reentered, checked
}

// The zone-entry index agrees with the backward scan it replaces, for tokens
// (whose TokenCreate carries no Obj, so both answer GenesisZoneSeq) and for
// an object that left and re-entered the battlefield.
func TestPaymentPlanSearchZoneSeqIndexFixture(t *testing.T) {
	e, _, _ := newFixtureDeck(t, 9994, srchSpell("U"), srchMountain)
	onBoard(t, e, 0, srchIsland)
	token := putToken(t, e, 0, srchWastes, state.ZBattlefield)
	blinked := moveSeeded(t, e, 0, srchMountain, state.ZBattlefield)
	e.emit(events.Event{Kind: events.MoveZone, Obj: blinked, From: state.ZBattlefield, To: state.ZExile})
	e.emit(events.Event{Kind: events.MoveZone, Obj: blinked, From: state.ZExile, To: state.ZBattlefield})
	if e.G.Obj(token) == nil || !e.G.Obj(token).IsToken || e.G.Obj(blinked).Zone != state.ZBattlefield {
		t.Fatal("fixture did not seat a token and a re-entered object")
	}
	tokens, reentered, checked := srchIndexAgrees(t, e, "fixture")
	if tokens == 0 || reentered == 0 {
		t.Fatalf("checked %d objects with %d tokens and %d re-entered, want both kinds", checked, tokens, reentered)
	}
	if got := pay.PaymentSourceZoneSeq(asPayer(e), token); got != decision.GenesisZoneSeq {
		t.Fatalf("token zone seq = %d, want the genesis sentinel (unchanged token behaviour)", got)
	}
}

// Property test over fixed-seed repo-deck games: at every priority decision
// the index agrees with the scan for every battlefield object.
func TestPaymentPlanSearchZoneSeqIndexAgreesInGames(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	all := testutil.LegacyDeckNames()
	var tokens, reentered, checked, decisions int
	// Seeds 6 and 7 seat Empty the Warrens and flicker effects: the storm
	// deck makes goblin tokens and several permanents leave and re-enter.
	for _, seed := range []uint64{6, 7} {
		names := make([]string, 4)
		decks := make([][]*cards.Card, 4)
		for i := range names {
			names[i] = all[(int(seed)+i*3)%len(all)]
			decks[i] = testutil.RepoDeck(t, reg, names[i])
		}
		e := New(Config{Seed: seed, Names: names, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Cards})
		b := newTestBot(seed)
		e.Advance()
		for n := 0; !e.G.Over && e.Pending() != nil && n < 20000; n++ {
			if d := e.Pending(); d.Kind == decision.KPriority {
				tk, re, ch := srchIndexAgrees(t, e, fmt.Sprintf("seed %d intent %d", seed, n))
				tokens, reentered, checked = tokens+tk, reentered+re, checked+ch
				decisions++
			}
			if err := e.Submit(b.answer(e, e.Pending())); err != nil {
				t.Fatalf("seed %d intent %d: %v", seed, n, err)
			}
		}
	}
	if tokens == 0 || reentered == 0 {
		t.Fatalf("checked %d tokens and %d re-entered objects: the property test must cover both", tokens, reentered)
	}
	t.Logf("%d priority decisions, %d battlefield objects checked (%d token checks, %d re-entered checks)", decisions, checked, tokens, reentered)
}

// srchDistinctRock is a red source whose mana ability sits after k dummy
// abilities, so every k is a different ability identity and therefore a
// different class, although all of them produce the same {R}.
func srchDistinctRock(k int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Name:Distinct Rock %d\nTypes:Artifact\n", k)
	for i := 0; i < k; i++ {
		b.WriteString("A:AB$ Draw | Cost$ 1 | NumCards$ 1 | SpellDescription$ x\n")
	}
	b.WriteString("A:AB$ Mana | Cost$ T | Produced$ R | SpellDescription$ x\nOracle:x\n")
	return b.String()
}

// Done-means 5: a board that defeats the classes still stops at the node
// budget, deterministically. Twenty one-unit classes that differ only in
// ability identity tie on every rank key before the witness compare, so
// proving the best of C(20,10) witnesses needs more nodes than the budget;
// the outcome is the best complete plan found, recorded as search_limit.
func TestPaymentPlanSearchBudgetExhaustionIsDeterministic(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9995, srchSpell("10"))
	for k := 0; k < 20; k++ {
		onBoard(t, e, 0, srchDistinctRock(k))
	}
	if n := len(pay.Classes(e.paymentPlanQueryChoices(0))); n != 20 {
		t.Fatalf("board has %d classes, want 20", n)
	}
	first := e.PlanCastPayment(0, paymentCast(spell))
	second := e.PlanCastPayment(0, paymentCast(spell))
	fromClone := e.Clone().PlanCastPayment(0, paymentCast(spell))
	if first.Reason != "search_limit" || first.Nodes != decision.MaxPaymentPlanSearchNodes {
		t.Fatalf("outcome reason=%q nodes=%d, want search_limit at the budget", first.Reason, first.Nodes)
	}
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(first, fromClone) {
		t.Fatalf("budget outcome not deterministic:\n%#v\n%#v\n%#v", first, second, fromClone)
	}
	if first.Plan == nil || len(first.Plan.Activations) != 10 {
		t.Fatalf("plan = %+v, want the best complete 10-source plan found", first.Plan)
	}
	if err := e.ValidateCastPayment(0, paymentCast(spell), *first.Plan); err != nil {
		t.Fatalf("limited search returned an unvalidatable plan: %v", err)
	}
}

// Randomised boards (fixed seed) of up to 12 sources from the whole palette,
// with a random floating pool and cost: every search answer equals the
// oracle's.
func TestPaymentPlanSearchMatchesOracleOnRandomBoards(t *testing.T) {
	t.Parallel()
	palette := []string{srchIsland, srchMountain, srchForest, srchPlains, srchSwamp, srchVolcanic, srchSavannah,
		srchBadlands, srchCombo, srchDork, srchAnyRock, srchWastes, srchTwoRock}
	rng := rand.New(rand.NewPCG(20260927, 1))
	symbols := "WUBRGC"
	planned, none := 0, 0
	for board := 0; board < 40; board++ {
		e, _, spell := newFixtureDeck(t, 10000+uint64(board), srchSpell("U"))
		for n := 4 + rng.IntN(9); n > 0; n-- {
			src := palette[rng.IntN(len(palette))]
			if src == srchDork {
				onBoardReady(t, e, 0, src)
				continue
			}
			onBoard(t, e, 0, src)
		}
		for n := rng.IntN(3); n > 0; n-- {
			e.G.Players[0].Pool[rng.IntN(6)]++
		}
		for k := 0; k < 6; k++ {
			var c Cost
			c.Generic = int32(rng.IntN(5))
			for n := rng.IntN(4); n > 0; n-- {
				c.Colored[state.ManaIndex(symbols[rng.IntN(len(symbols))])]++
			}
			want, _ := paymentPlanSearchOracle(e, 0, spell, c)
			got := pay.PlanPaymentCost(asPayer(e), 0, paymentCast(spell), c)
			if want == nil {
				if got.Plan != nil || got.Reason != "insufficient" {
					t.Fatalf("board %d cost %+v: search = %+v, oracle found no plan", board, c, got)
				}
				none++
				continue
			}
			if got.Reason != "" || got.Plan == nil || !reflect.DeepEqual(*got.Plan, *want) {
				t.Fatalf("board %d cost %+v: search (%q, %d nodes) differs from the oracle\nsearch: %+v\noracle: %+v", board, c, got.Reason, got.Nodes, got.Plan, want)
			}
			planned++
		}
	}
	t.Logf("random boards: %d planned and %d insufficient costs agree with the oracle", planned, none)
}

// srchPhaseTwoChoices is the alternative table a phase-2 search sees (spec 5:
// normal plus last-resort alternatives), in paymentPlanUnitAlternatives'
// exact shape but keeping each last-resort alternative with the consequence
// the real classifier (paymentPlanAbilityTier, interference included)
// reports. aph-last-resort-plans wires phase 2; this only lets the search's
// irreversible-cost level be checked against the oracle with real tiers.
func srchPhaseTwoChoices(t *testing.T, e *Engine, p state.PlayerID) [][]pay.Alt {
	t.Helper()
	units := e.paymentPlanManaUnits(p)
	choices := make([][]pay.Alt, len(units))
	for i, u := range units {
		var out []pay.Alt
		for _, alt := range u.Alts {
			tier, consequence, _ := pay.PaymentPlanAbilityTier(asPayer(e), p, u.ID, alt.Ma)
			if tier == pay.TierDeferred || !pay.PaymentPlanTapOnlyCost(e.parseCost(alt.Ma.Params["Cost"])) {
				continue
			}
			ab, ok := pay.PaymentAbility(e.G, u.ID, alt.Ma)
			if !ok {
				continue
			}
			base := pay.Alt{Activation: decision.PaymentActivation{Source: u.ID, SourceZoneSeq: pay.PaymentSourceZoneSeq(asPayer(e), u.ID), Ability: ab},
				Creature: e.IsCreature(u.ID), Ma: alt.Ma, Tier: tier, Consequence: consequence}
			if pay.PlanAltOK(alt) {
				base.Mana = alt.Mana()
				base.Activation.Produces = pay.ManaAmount(base.Mana)
				out = append(out, base)
				continue
			}
			if !alt.Any || alt.Amt <= 0 {
				continue
			}
			for _, col := range pay.PaymentPlanChoiceColours(asPayer(e), u.ID, alt.Ma) {
				a := base
				a.Mana = state.Mana{}
				a.Mana[strings.IndexByte("WUBRG", col[0])] = alt.Amt
				a.Activation.Produces = pay.ManaAmount(a.Mana)
				a.ExecProduced = col
				out = append(out, a)
			}
		}
		var types uint8
		for _, a := range out {
			for k, n := range a.Mana {
				if n > 0 {
					types |= 1 << k
				}
			}
		}
		for k := range out {
			out[k].Flex = bits.OnesCount8(types)
			out[k].Colours = types &^ (1 << state.MC)
		}
		choices[i] = out
	}
	return choices
}

// The search's irreversible-cost level against the oracle with real
// interference tiers: City of Brass (last resort, damage:1 from its own Taps
// trigger) and Mana Vault (last resort, no_untap from its own Untap
// replacement), classified by the merged classifier, next to normal lands.
// Phase 1 excludes both (TestPaymentPlanInterferenceOwnCityAndVaultAreLastResort);
// a phase-2 table keeps them, and the search must pick exactly the oracle's
// plan, including when a last-resort source is needed and when two Cities
// (3+3) must be preferred to one Vault (25).
func TestPaymentPlanSearchMatchesOracleWithLastResortTiers(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 9996, srchSpell("U"))
	onBoard(t, e, 0, srchIsland)
	onBoardCard(t, e, 0, corpusCard(t, "City of Brass"))
	onBoard(t, e, 0, srchMountain)
	vault := onBoardCard(t, e, 0, corpusCard(t, "Mana Vault"))
	e.G.Obj(vault).SummonSick = false
	onBoardCard(t, e, 0, corpusCard(t, "City of Brass"))
	onBoard(t, e, 0, srchVolcanic)
	choices := srchPhaseTwoChoices(t, e, 0)
	var consequences []pay.Consequence
	for _, alts := range choices {
		for _, a := range alts {
			if a.Tier == pay.TierLastResort && !slices.Contains(consequences, a.Consequence) {
				consequences = append(consequences, a.Consequence)
			}
		}
	}
	if !slices.Contains(consequences, pay.Consequence{Damage: 1}) || !slices.Contains(consequences, pay.Consequence{NoUntap: true}) {
		t.Fatalf("phase-2 table consequences = %+v, want damage:1 (City of Brass) and no_untap (Mana Vault)", consequences)
	}
	pool, life := e.G.Players[0].Pool, e.G.Players[0].Life
	lastResort, vaultUsed := 0, 0
	for _, s := range []string{"U", "W", "W B", "3", "4", "5", "6", "7", "U R", "1 W", "2 G", "W U B", "3 U", "C C", "8", "W W W"} {
		cost := srchCost(t, s)
		demand := pay.PaymentPlanHandDemand(asPayer(e), 0, 0)
		want, _ := paymentPlanSearchOracleOver(e, 0, cost, choices, demand)
		got := pay.Run(cost, pool, life, pay.NewRankContext(choices, demand), choices, pay.Classes(choices), pay.PaymentSearchEnv(walkCacheVerify))
		if got.Limited {
			t.Fatalf("{%s}: search hit the node budget", s)
		}
		if want == nil {
			if got.Best != nil {
				t.Fatalf("{%s}: search found %+v, oracle none", s, got.Best)
			}
			continue
		}
		if got.Best == nil || !reflect.DeepEqual(*got.Best, *want) {
			t.Fatalf("{%s}: search differs from the oracle (%d nodes)\nsearch: %+v\noracle: %+v", s, got.Nodes, got.Best, want)
		}
		if got.BestRank.Cost > 0 {
			lastResort++
		}
		for _, a := range got.Best.Activations {
			if a.Source == vault {
				vaultUsed++
			}
		}
		t.Logf("{%s}: %d steps, irreversible cost %d, nodes %d", s, len(got.Best.Activations), got.BestRank.Cost, got.Nodes)
	}
	if lastResort == 0 || vaultUsed == 0 {
		t.Fatalf("%d plans used a last-resort source, %d used the Vault: the cost level was not exercised", lastResort, vaultUsed)
	}
}

// srchCheckAgainstOracle asserts planPaymentCost's outcome for spell at cost
// equals the oracle's (same hand demand), and reports whether the oracle's
// best would differ without the hand demand -- i.e. whether key 6 decided
// the plan.
func srchCheckAgainstOracle(t *testing.T, e *Engine, spell state.ObjID, cost Cost, what string) (planned, keySixDecided bool) {
	t.Helper()
	want, _ := paymentPlanSearchOracle(e, 0, spell, cost)
	got := pay.PlanPaymentCost(asPayer(e), 0, paymentCast(spell), cost)
	if want == nil {
		if got.Plan != nil || got.Reason != "insufficient" {
			t.Fatalf("%s: search = %+v, oracle found no plan", what, got)
		}
		return false, false
	}
	if got.Reason != "" || got.Plan == nil || !reflect.DeepEqual(*got.Plan, *want) {
		t.Fatalf("%s: search (%q, %d nodes) differs from the oracle\nsearch: %+v\noracle: %+v", what, got.Reason, got.Nodes, got.Plan, want)
	}
	blind, _ := paymentPlanSearchOracleOver(e, 0, cost, e.paymentPlanQueryChoices(0), [5]int{})
	return true, !reflect.DeepEqual(*blind, *want)
}

// Key 6 decides on a class board: {3}{W} over three Forests, three Islands
// and two Plains, with a {G}{G}{G} card in hand. Every 4-source plan ties on
// keys 1-5; key 6 must keep all three Forests (coverage G=3) although that
// spends every Island and so loses a colour on key 7, while without the
// hand the rank keeps every colour and taps a Forest. The search's key-6
// bound (reserve) must not cut the winning branch.
func TestPaymentPlanSearchHandReserveDecidesOnClassBoard(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9997, srchSpell("3 W"))
	var forests []state.ObjID
	for _, src := range []string{srchForest, srchIsland, srchPlains, srchForest, srchIsland, srchForest, srchIsland, srchPlains} {
		id := onBoard(t, e, 0, src)
		if src == srchForest {
			forests = append(forests, id)
		}
	}
	choiceHand(t, e, "Name:Green Three\nManaCost:G G G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	if d := pay.PaymentPlanHandDemand(asPayer(e), 0, spell); d != [5]int{0, 0, 0, 0, 3} {
		t.Fatalf("hand demand = %v, want G=3", d)
	}
	planned, decided := srchCheckAgainstOracle(t, e, spell, srchCost(t, "3 W"), "{3}{W}")
	if !planned || !decided {
		t.Fatalf("planned=%v key6-decided=%v, want a plan that the hand demand changed", planned, decided)
	}
	got := e.PlanCastPayment(0, paymentCast(spell))
	for _, a := range got.Plan.Activations {
		if slices.Contains(forests, a.Source) {
			t.Fatalf("plan %+v taps a Forest the {G}{G}{G} card needs", got.Plan.Activations)
		}
	}
	// The hand-aware shape itself: {1}{W} over Forest, Island, Plains with a
	// {G} card in hand (TestPaymentPlanHandAwareKeepsHandGreenCastable).
	e2, _, spell2 := newFixtureDeck(t, 9998, srchSpell("1 W"))
	onBoard(t, e2, 0, srchForest)
	onBoard(t, e2, 0, srchIsland)
	onBoard(t, e2, 0, srchPlains)
	choiceHand(t, e2, "Name:Green One\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	if planned, decided := srchCheckAgainstOracle(t, e2, spell2, srchCost(t, "1 W"), "{1}{W}"); !planned || !decided {
		t.Fatalf("planned=%v key6-decided=%v on the P5 board", planned, decided)
	}
}

// Randomised boards with a random hand (fixed seed): every search answer
// equals the oracle's under the same nonzero hand demand, and key 6 decides
// a share of them.
func TestPaymentPlanSearchMatchesOracleWithHandDemand(t *testing.T) {
	t.Parallel()
	palette := []string{srchIsland, srchMountain, srchForest, srchPlains, srchSwamp, srchVolcanic, srchSavannah,
		srchBadlands, srchCombo, srchDork, srchAnyRock, srchWastes, srchTwoRock}
	rng := rand.New(rand.NewPCG(20260927, 6))
	symbols := "WUBRG"
	planned, none, decided := 0, 0, 0
	for board := 0; board < 60; board++ {
		e, _, spell := newFixtureDeck(t, 10100+uint64(board), srchSpell("U"))
		for n := 4 + rng.IntN(9); n > 0; n-- {
			src := palette[rng.IntN(len(palette))]
			if src == srchDork {
				onBoardReady(t, e, 0, src)
				continue
			}
			onBoard(t, e, 0, src)
		}
		for n := 1 + rng.IntN(3); n > 0; n-- {
			var pips []string
			for k := 1 + rng.IntN(3); k > 0; k-- {
				pips = append(pips, string(symbols[rng.IntN(len(symbols))]))
			}
			choiceHand(t, e, "Name:Hand Card\nManaCost:"+strings.Join(pips, " ")+"\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		}
		if pay.PaymentPlanHandDemand(asPayer(e), 0, spell) == ([5]int{}) {
			t.Fatalf("board %d: hand demand is zero", board)
		}
		for k := 0; k < 5; k++ {
			var c Cost
			c.Generic = int32(rng.IntN(5))
			for n := rng.IntN(3); n > 0; n-- {
				c.Colored[state.ManaIndex("WUBRGC"[rng.IntN(6)])]++
			}
			ok, sixth := srchCheckAgainstOracle(t, e, spell, c, fmt.Sprintf("board %d cost %+v", board, c))
			switch {
			case !ok:
				none++
			case sixth:
				planned++
				decided++
			default:
				planned++
			}
		}
	}
	if decided == 0 {
		t.Fatal("no random board had key 6 decide the plan")
	}
	t.Logf("hand-demand boards: %d planned (%d decided by key 6) and %d insufficient costs agree with the oracle", planned, decided, none)
}
