package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// pendingGreen is a forest's {T}: add {G} production summary.
func pendingGreen() cards.ManaProduction {
	var p cards.ManaProduction
	p.Colour[state.MG] = 1
	return p
}

// pendingTargetDecision is effectTargetDecision's twin for the pending-payment
// path: it sets Decision.Source so chooseTargets can read the unpaid spell's
// cost off the stack, exactly as rules/cast.go does at a cast-target ask
// (Source = pc.card, the card already pushed as pc.stackObj).
func pendingTargetDecision(b Board, src state.ObjID, dmg int, options []tgt) ([]int, *decision.Decision) {
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1, Source: src,
		TargetEffect: &decision.TargetEffect{API: "DealDamage",
			Damage: &decision.DamageEffect{Amount: &dmg}}}
	for _, o := range options {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: o.kind, Obj: o.obj, Player: o.pl})
	}
	return Decide(b, d, rng(1)).Choices, d
}

// pendingKillBoard is the claim-A shape: `forests` untapped Forests, a
// {G}{G} instant reserve the seat wants to keep, and a 3-damage burn pending
// on the stack whose cost the caller supplies. The opponent has a killable
// 2/2 (the tier-3 value kill) and an unkillable 5/5 (the board-first
// fallback the policy picks when tierValue does NOT fire).
func pendingKillBoard(t *testing.T, pendingCost string, forests int) Board {
	t.Helper()
	green := pendingGreen()
	b := boardOf(def(1, 2, 2), def(2, 5, 5))
	b.Life.Set(0, 20)
	b.Life.Set(1, 20) // neither lethal nor a threat: only tiers 1/2 are excluded by design
	b.Cards = TableOf(map[state.ObjID]Card{
		// The instant-speed reserve: cost {G}{G} (Giant Growth's sibling shape;
		// the printed Giant Growth itself is {G}, see the report).
		9: {Castable: true, InstantSpeed: true, ManaCost: "G G", CMC: CmcOf("G G")},
	})
	for i := 1; i <= forests; i++ {
		b.Cards.Set(state.ObjID(i), Card{OnBattlefield: true, Basic: true, Produces: green})
	}
	b.Stack = []StackEntry{{ID: 50, Controller: 0, IsSpell: true, CMC: CmcOf(pendingCost), ManaCost: pendingCost}}
	// Preconditions the ranking depends on: the requested number of live
	// untapped basic green sources, a real coloured reserve, and a pending
	// spell with the exact printed cost under test. A vacuous board must fail
	// here, not pass silently downstream.
	for i := 1; i <= forests; i++ {
		c := b.Cards.Get(state.ObjID(i))
		if !c.OnBattlefield || !c.Basic || c.Tapped || c.Produces.Colour[state.MG] != 1 {
			t.Fatalf("source %d is not a live untapped basic Forest: %+v", i, c)
		}
	}
	if forests < 1 {
		t.Fatal("two creature values under comparison need at least one source")
	}
	if r := b.Cards.Get(9); !r.Castable || !r.InstantSpeed || colourPips(r.ManaCost)[state.MG] != 2 {
		t.Fatalf("reserve 9 is not a {G}{G} instant: %+v", r)
	}
	if len(b.Stack) != 1 || b.Stack[0].ManaCost != pendingCost {
		t.Fatalf("pending stack entry cost %q, want %q", b.Stack[0].ManaCost, pendingCost)
	}
	if b.Creatures.Get(201).Toughness == b.Creatures.Get(202).Toughness {
		t.Fatal("the two creature values under comparison must differ")
	}
	return b
}

// TestTargetSpareManaDeductsPendingPayment is claim A: the spare-mana claim
// is made at target-choice time, BEFORE the spell's own mana is paid (CR
// 601.2b/c), so the pending payment can destroy the reserve the answer just
// leaned on -- and, because the pending payment is a real future spend, the
// ordinary single-unit/source probe must still apply to what is left over.
//
// Three Forests and a {G}{G} reserve: a {1}{G} pending removal eats two of
// them and the reserve is dead, so tierValue must NOT fire. A one-mana {G}
// pending removal on the same three Forests ALSO eats one and then the probe
// eats another, so it too must not fire (the composed model). One more
// Forest -- four -- keeps the one-mana case spare and tier 3 fires, so the
// promotion path stays demonstrably live.
func TestTargetSpareManaDeductsPendingPayment(t *testing.T) {
	// {1}{G}: two units, worst-cased onto the reserve's colour.
	b := pendingKillBoard(t, "1 G", 3)
	if b.hasSpareManaAfter("1 G") {
		t.Fatal("{1}{G} pending payment must not read spare against a {G}{G} reserve it can eat two units of")
	}
	got, d := pendingTargetDecision(b, 50, 3, []tgt{face(), opp(201), opp(202)})
	if len(got) != 1 || objAt(d, got[0]) != 202 {
		t.Fatalf("pending {1}{G} target = obj %d (options %v), want the board-first 5/5 (obj 202): tier 3 must not fire when the pending payment breaks the reserve", objAt(d, got[0]), choicesObj(got, d))
	}

	// {G} on three Forests: the payment eats one unit and the probe eats a
	// second, so the {G}{G} reserve is not actually protected.
	b = pendingKillBoard(t, "G", 3)
	if b.hasSpareManaAfter("G") {
		t.Fatal("a {G} pending payment plus one further unit spend must not read spare against a {G}{G} reserve on three Forests")
	}
	got, d = pendingTargetDecision(b, 50, 3, []tgt{face(), opp(201), opp(202)})
	if len(got) != 1 || objAt(d, got[0]) != 202 {
		t.Fatalf("pending {G} on three Forests target = obj %d (options %v), want the board-first 5/5 (obj 202): the composed spend breaks the reserve", objAt(d, got[0]), choicesObj(got, d))
	}

	// {G} on four Forests: after the payment and one further spend two Forests
	// remain, which still pay the {G}{G} reserve, so tier 3 fires.
	b = pendingKillBoard(t, "G", 4)
	if !b.hasSpareManaAfter("G") {
		t.Fatal("a one-mana {G} pending payment must leave the {G}{G} reserve payable from four Forests")
	}
	got, d = pendingTargetDecision(b, 50, 3, []tgt{face(), opp(201), opp(202)})
	if len(got) != 1 || objAt(d, got[0]) != 201 {
		t.Fatalf("pending {G} on four Forests target = obj %d (options %v), want the value kill 2/2 (obj 201): tier 3 fires when the reserve survives the pending payment and a further spend", objAt(d, got[0]), choicesObj(got, d))
	}
}

// TestTargetSpareManaUnpayablePipsFailClosed pins the coloured-pip corner of
// the worst-case deduction: a pending {G}{G} cannot be paid from one Forest
// plus two Islands, so the second green pip is unpayable from modelled
// sources. Capping the pip deduction at availability and pricing the rest as
// generic would spend an Island for that green pip and invent a legal
// payment; the correct answer is to fail closed (the units covering the gap
// are invisible, so no remainder can be proven).
func TestTargetSpareManaUnpayablePipsFailClosed(t *testing.T) {
	green := pendingGreen()
	blue := cards.ManaProduction{}
	blue.Colour[state.MU] = 1
	// Reserve {U}: minCost 1, so a deduction that wrongly spends both Islands
	// for the {G}{G} cost would still leave nothing to pay it with.
	b := Board{Cards: TableOf(map[state.ObjID]Card{
		1: {OnBattlefield: true, Basic: true, Produces: green},
		2: {OnBattlefield: true, Basic: true, Produces: blue},
		3: {OnBattlefield: true, Basic: true, Produces: blue},
		9: {Castable: true, InstantSpeed: true, ManaCost: "U", CMC: CmcOf("U")},
	})}
	b.Stack = []StackEntry{{ID: 50, Controller: 0, IsSpell: true, CMC: CmcOf("G G"), ManaCost: "G G"}}
	// Preconditions: the pool can cover the reserve, but only ONE green pip of
	// the pending {G}{G}; the two facts under test must actually differ.
	if got := colourPips("G G")[state.MG]; got != 2 {
		t.Fatalf("pending cost green pips = %d, want 2", got)
	}
	if !b.hasSpareMana() {
		t.Fatal("precondition: the {U} reserve is payable and survives a probe before any pending cost")
	}
	if b.hasSpareManaAfter("G G") {
		t.Fatal("an unpayable {G}{G} pip requirement must fail closed, not spend a different colour for the unmet pip")
	}
}

// TestTargetSpareManaCountsJustPlayedBasicLand is the CR 302.6 regression:
// summoning sickness only forbids a CREATURE's tap ability (rules/legal.go
// refuses the tap cost on SummonSick only when the derived types contain
// Creature and no Haste), and no basic land is a creature, so a Forest played
// this turn is fully tappable for mana and MUST count toward the reserve.
//
// The Board carries no sickness fact at all: a basic source is disqualified
// only by being tapped, off the battlefield, or lacking a single fixed
// production colour. This test fails if a skip is ever re-added that treats a
// fresh basic land as dead mana -- the over-conservative mistake a prior
// revision of the pending-payment fix made. The source that flips the answer
// here is Tapped, so the comparison is non-vacuous.
func TestTargetSpareManaCountsJustPlayedBasicLand(t *testing.T) {
	green := pendingGreen()
	// A land that entered this turn carries no botpolicy-visible mark: the
	// adapters fill no sickness fact, so this Board is exactly what the bot
	// sees for a Forest played this turn.
	b := Board{Cards: TableOf(map[state.ObjID]Card{
		1: {OnBattlefield: true, Basic: true, Produces: green},
		2: {OnBattlefield: true, Basic: true, Produces: green},
		3: {OnBattlefield: true, Basic: true, Produces: green},
		9: {Castable: true, InstantSpeed: true, ManaCost: "G G", CMC: CmcOf("G G")},
	})}
	if r := b.Cards.Get(9); !r.Castable || !r.InstantSpeed || colourPips(r.ManaCost)[state.MG] != 2 {
		t.Fatalf("reserve 9 is not a {G}{G} instant: %+v", r)
	}
	// Precondition: all three sources are live untapped basics, so a fresh
	// land is represented exactly as any other land.
	for i := 1; i <= 3; i++ {
		c := b.Cards.Get(state.ObjID(i))
		if !c.OnBattlefield || !c.Basic || c.Tapped || c.Produces.Colour[state.MG] != 1 {
			t.Fatalf("source %d is not a live untapped basic Forest: %+v", i, c)
		}
	}
	if !b.hasSpareMana() {
		t.Fatal("three live basic Forests must preserve the {G}{G} reserve; a just-played land is tappable, unlike a sick creature")
	}

	// The one fact that DOES disqualify a basic source is being tapped: tap
	// one and the same board is no longer spare, so the answer above is not
	// vacuous.
	tapped := b.Cards.Get(3)
	tapped.Tapped = true
	b.Cards.Set(3, tapped)
	if b.hasSpareMana() {
		t.Fatal("two live plus one tapped Forest must not read spare for a {G}{G} reserve")
	}
}

// TestTargetSpareManaXPaymentFailsClosed pins the {X} corner: CmcOf counts a
// printed X as 0, but the payment it stands for is unbounded, so a pending {X}
// cost cannot be proven to leave the reserve. It fails closed rather than
// reading the X as free.
func TestTargetSpareManaXPaymentFailsClosed(t *testing.T) {
	b := pendingKillBoard(t, "X G", 3)
	if !costHasX("X G") {
		t.Fatal("costHasX must see the X in a printed {X}{G}")
	}
	if CmcOf("X G") != 1 {
		t.Fatalf("CmcOf(X G) = %d, want 1: the X is printed 0, which is exactly why the cost is unbounded", CmcOf("X G"))
	}
	if b.hasSpareManaAfter("X G") {
		t.Fatal("an unbounded {X} pending payment must not read spare")
	}
}

// TestTargetSpareManaFreePendingKeepsProbe pins the zero-cost corner: a
// pending "0" spends nothing, so hasSpareManaAfter must reproduce
// hasSpareMana's single-unit probe rather than reading the free cost as a
// licence to skip it. Two healthy Forests plus a third -- a {G}{G} reserve --
// is the shape the probe rejects, and the free pending cost must reject it too.
func TestTargetSpareManaFreePendingKeepsProbe(t *testing.T) {
	green := pendingGreen()
	b := Board{Cards: TableOf(map[state.ObjID]Card{
		1: {OnBattlefield: true, Basic: true, Produces: green},
		2: {OnBattlefield: true, Basic: true, Produces: green},
		9: {Castable: true, InstantSpeed: true, ManaCost: "G G", CMC: CmcOf("G G")},
	})}
	if CmcOf("0") != 0 || colourPips("0") != [5]int32{} {
		t.Fatalf("a printed 0 must be a free cost: CmcOf=%d pips=%v", CmcOf("0"), colourPips("0"))
	}
	if b.hasSpareMana() {
		t.Fatal("precondition: two Forests cannot preserve a {G}{G} reserve under the single-unit probe")
	}
	if b.hasSpareManaAfter("0") != b.hasSpareMana() {
		t.Fatal("a free pending cost must reproduce hasSpareMana, probes included")
	}

	// And a live pending cost still tightens it: three Forests are probe-spare
	// only until a {1}{G} payment is accounted for.
	b.Cards.Set(3, Card{OnBattlefield: true, Basic: true, Produces: green})
	if !b.hasSpareMana() {
		t.Fatal("precondition: three Forests should be probe-spare for a {G}{G} reserve")
	}
	if b.hasSpareManaAfter("1 G") {
		t.Fatal("a {1}{G} pending payment must tighten the three-Forest board to not-spare")
	}
}
