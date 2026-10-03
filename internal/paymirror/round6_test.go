package paymirror

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestWitnessReadsPayLifeOnlySources pins the witness over a source whose
// cost neither taps nor moves it (round-6 commander4-fresh: 5x
// a_witness:unexecuted_activation on Treasonous Ogre's "Pay 3 life: Add
// {R}"). Its activation starts at the payer's payment of exactly its
// disclosed Consequence.Life immediately followed by mana; two such sources
// claim two payments; a payment not followed by mana, or of another amount,
// starts nothing.
func TestWitnessReadsPayLifeOnlySources(t *testing.T) {
	const payer state.PlayerID = 2
	life := func(n int32) events.Event { return events.Event{Kind: events.LifeChange, Player: payer, Amount: -n} }
	mana := func(sym string) events.Event {
		return events.Event{Kind: events.ManaAdd, Player: payer, Amount: 1, Counter: sym}
	}
	ogre := func(id state.ObjID) decision.PaymentActivation {
		return decision.PaymentActivation{Source: id, Produces: decision.ManaAmount{0, 0, 0, 1, 0, 0},
			Consequence: &decision.PaymentConsequence{Life: 3}}
	}
	sol := decision.PaymentActivation{Source: 5, Produces: decision.ManaAmount{0, 0, 0, 0, 0, 2}}
	plan := decision.PaymentPlan{Activations: []decision.PaymentActivation{sol, ogre(7), ogre(8)}}
	// Sol Ring taps for CC, then each Ogre pays 3 and adds R.
	evs := []events.Event{{Kind: events.Tap, Obj: 5}, mana("C"), mana("C"), life(3), mana("R"), life(3), mana("R")}
	starts := lifeOnlyStarts(evs, plan, payer)
	if starts[0] != -1 || starts[1] != 3 || starts[2] != 5 {
		t.Fatalf("starts = %v, want [-1 3 5]", starts)
	}
	for i, j := range starts[1:] {
		if got := manaRunAfter(evs, j); got != plan.Activations[i+1].Produces {
			t.Fatalf("ogre %d produced %v", i, got)
		}
	}
	// A payment of another amount, or one not followed by mana, is no start.
	for _, bad := range [][]events.Event{
		{life(2), mana("R")},
		{life(3), {Kind: events.Draw, Player: payer}, mana("R")},
	} {
		if s := lifeOnlyStarts(bad, decision.PaymentPlan{Activations: []decision.PaymentActivation{ogre(7)}}, payer); s[0] != -1 {
			t.Fatalf("%v read as an activation start at %d", bad, s[0])
		}
	}
	// A source that taps is never read through the life fallback.
	tapper := ogre(9)
	if s := lifeOnlyStarts([]events.Event{{Kind: events.Tap, Obj: 9}, life(3), mana("R")},
		decision.PaymentPlan{Activations: []decision.PaymentActivation{tapper}}, payer); s[0] != -1 {
		t.Fatalf("a tapping source took the life fallback: %v", s)
	}
}

// TestFloatTriggerOnlyAllowsOnlyTriggerTiming pins the proof that gates
// float_trigger_precedes_cast (round-6 G.Stack[*], G.Entered/ability-object
// and trigger_order mirror_missing_decision mismatches): the float route's
// own triggers reaching the stack below the spell instead of above it, under
// permuted ObjIDs, is allowed; any other difference is still reported.
func TestFloatTriggerOnlyAllowsOnlyTriggerTiming(t *testing.T) {
	base := fixtureEngine(t)
	n := len(base.G.Objs)
	spell := state.ObjID(1)
	// Two objects created "since the fork": trigger abilities X and Y.
	x, y := state.ObjID(n-1), state.ObjID(n)
	rep := &Report{Object: spell, forkObjs: n - 2}
	a, b := base.Clone(), base.Clone()
	a.G.Stack = []state.ObjID{spell, x, y} // A: triggers above the spell
	b.G.Stack = []state.ObjID{y, x, spell} // float: below it, created in the other order
	if why := floatTriggerOnly(a, b, len(base.L.Events), rep); why != "" {
		t.Fatalf("a pure trigger-timing difference is rejected: %s", why)
	}
	// The spell's place among the older objects differs: reported.
	a2, b2 := base.Clone(), base.Clone()
	a2.G.Stack = []state.ObjID{spell, 2, x}
	b2.G.Stack = []state.ObjID{x, 2, spell}
	if why := floatTriggerOnly(a2, b2, len(base.L.Events), rep); why != "stack" {
		t.Fatalf("an older object's reorder = %q, want stack", why)
	}
	// Anything else about the game differs: reported.
	c := b.Clone()
	c.G.Players[0].Life--
	if why := floatTriggerOnly(a, c, len(base.L.Events), rep); !strings.HasPrefix(why, "state:") {
		t.Fatalf("a life difference = %q, want a state difference", why)
	}
	// The since-fork objects are not the same multiset: reported.
	d := b.Clone()
	d.G.Objs[y-1].Controller = 1 - d.G.Objs[y-1].Controller
	if why := floatTriggerOnly(a, d, len(base.L.Events), rep); why != "new_objects" {
		t.Fatalf("a changed since-fork object = %q, want new_objects", why)
	}
}

// round6Game plays one round-6 finding game and returns every report.
func round6Game(t *testing.T, d *Decks, spec GameSpec) []*Report {
	t.Helper()
	var full []*Report
	g := PlayGame(d, spec, DriverOptions{Control: true, Resolve: true,
		OnReport: func(_ GameSpec, r *Report) { full = append(full, r) }})
	if g.Err != "" {
		t.Fatalf("seed %d: game error %s", spec.Seed, g.Err)
	}
	return full
}

// TestRoundSixGainedMemberSeed3589 is random2-fresh seed 3589 (Lorcan,
// Warlock Collector): a Manascape Refractor had gained "Add {R}" from a
// Mountain and from a Vivid Crag; the float route picked the Crag's while
// run A activated the Mountain's, and the ManaActivate markers differed.
//
// Re-pinned to seq 0 (the round-10 convention) twice, for the same reason:
// first by the kw:Backup ticket (CR 702.165), then again by sb-job-select
// (kw:Job select, CR 702.182). Each newly registered keyword made its corpus
// carriers eligible for the random pool (NewRandomPool indexes only cards
// with reg.Unsupported(c, sup) == nil), so pool.Generate returned different
// decks for this seed and the moved game no longer casts Manascape Refractor
// at all. The seed keeps an empty pin and asserts the whole game is
// mismatch-free and control-equivalent; the finding's own shape stays pinned
// by the marker unit tests above, and the gained-member witness itself stays
// live in paymirror.go (gainedMember/answerManaAsks) for the next game that
// shows the shape.
func TestRoundSixGainedMemberSeed3589(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d, err := LoadDecks(reg)
	if err != nil {
		t.Fatal(err)
	}
	pool := NewRandomPool(reg)
	const seed, seats = 3589, 2
	spec := GameSpec{Seed: seed, Policy: "bot"}
	for i := 0; i < seats; i++ {
		label, list := pool.Generate(seed*7919 + uint64(seats)*131 + uint64(i))
		spec.Decks = append(spec.Decks, label)
		spec.Lists = append(spec.Lists, list)
	}
	reports := round6Game(t, d, spec)
	if len(reports) == 0 {
		t.Fatal("no planned-cast reports; the clean-game assertions would be vacuous")
	}
	for _, r := range reports {
		st, key := r.Verdict()
		if st == Mismatch {
			t.Errorf("seq %d %q: %s", r.Seq, r.Card, key)
		}
		if r.Control == nil || r.Control.Status != Equivalent {
			t.Errorf("seq %d %q: control %+v", r.Seq, r.Card, r.Control)
		}
	}
}
