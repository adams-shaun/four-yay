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

// TestRoundSixFindingsMirror replays round-6 finding games end to end: no
// cast is a mismatch, the live-vs-clone control is equivalent everywhere,
// and each named cast gets its root-caused verdict.
//
//   - 4130: control contChain.len (a replacement-order answer's body leaked a
//     continuation report; rules fix, resolveReplacementBody/answerParked);
//   - 2138: G.Stack reorder by a float-triggered ability (expected);
//   - 4098: the float's sacrifice triggered Rakdos, the Muscle's target ask
//     at priority (expected placement);
//   - 4139: damageSourceLKI on the Incubator's cast trigger (cost-move mask);
//   - 4129: Treasonous Ogre's pay-life-only activation (witness).
func TestRoundSixFindingsMirror(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d, err := LoadDecks(reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		seed  uint64
		decks []string
		seq   uint64
		want  string // the named cast's verdict key ("" = equivalent)
	}{
		{4130, []string{"vivi-ornitier-cedh", "foundations-reign-of-dragons", "avengers-assemble", "valgavoth-endless-punishment"}, 7213, ""},
		{2138, []string{"vivi-ornitier-cedh", "hearthhull-worldseed-landfall", "pro-shaper", "foundations-keen-engineering"}, 1488, "expected:float_then_cast:float_trigger_precedes_cast"},
		{4098, []string{"foundations-reign-of-dragons", "hearthhull-worldseed-landfall", "avengers-assemble", "rakdos-muscle-scam-exe"}, 5592, "expected:float_then_cast:float_trigger_placement"},
		{4139, []string{"foundations-wretched-ranks", "deadly-disguise", "foundations-reign-of-dragons", "ulalek-eldrazi"}, 6752, ""},
		{4129, []string{"rakdos-muscle-scam-exe", "pro-shaper", "foundations-reign-of-dragons", "foundations-wretched-ranks"}, 4118, ""},
	} {
		reports := round6Game(t, d, GameSpec{Seed: tc.seed, Decks: tc.decks, Commander: true, Policy: "bot"})
		found := false
		for _, r := range reports {
			st, key := r.Verdict()
			if st == Mismatch {
				t.Errorf("seed %d seq %d %q: %s (witness %q)", tc.seed, r.Seq, r.Card, key, r.AWitness)
			}
			if r.Control == nil || r.Control.Status != Equivalent {
				t.Errorf("seed %d seq %d %q: control %+v", tc.seed, r.Seq, r.Card, r.Control)
			}
			if r.Seq != tc.seq {
				continue
			}
			found = true
			if key != tc.want {
				t.Errorf("seed %d seq %d %q: verdict %s %q, want %q", tc.seed, r.Seq, r.Card, st, key, tc.want)
			}
		}
		if !found {
			t.Errorf("seed %d: no planned cast at seq %d (the game no longer reaches the finding)", tc.seed, tc.seq)
		}
	}
}

// TestRoundSixGainedMemberSeed3589 is random2-fresh seed 3589 (Lorcan,
// Warlock Collector): a Manascape Refractor had gained "Add {R}" from a
// Mountain and from a Vivid Crag; the float route picked the Crag's while
// run A activated the Mountain's, and the ManaActivate markers differed.
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
	found := false
	for _, r := range round6Game(t, d, spec) {
		if st, key := r.Verdict(); st == Mismatch {
			t.Errorf("seq %d %q: %s", r.Seq, r.Card, key)
		}
		if r.Seq == 1553 {
			found = true
		}
	}
	if !found {
		t.Fatal("no planned cast at seq 1553 (the game no longer reaches the finding)")
	}
}
