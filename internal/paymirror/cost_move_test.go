package paymirror

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// TestWitnessReadsCostMovedSources pins the witness over the activation
// shapes a last-resort plan step discloses (round-5 cardfuzz mirror:
// 36 wrong_production, 10 unexecuted_activation). A Treasure or Lotus Petal
// taps, is sacrificed, then adds its mana; an Eldrazi Spawn is sacrificed
// with no tap at all; a pay-life source pays its life between the tap and
// the mana. Each is the source's own cost, so the witness reads the mana
// after it. An unrelated event still ends the production window, and a
// source with no activation is still unexecuted.
func TestWitnessReadsCostMovedSources(t *testing.T) {
	const payer state.PlayerID = 1
	sac := func(id state.ObjID) events.Event {
		return events.Event{Kind: events.MoveZone, Obj: id, Player: payer, From: state.ZBattlefield, To: state.ZGraveyard, Text: "sacrificed"}
	}
	mana := func(sym string) events.Event {
		return events.Event{Kind: events.ManaAdd, Player: payer, Amount: 1, Counter: sym}
	}
	for _, tc := range []struct {
		name   string
		source state.ObjID
		evs    []events.Event
		want   decision.ManaAmount
		ok     bool
	}{
		{"tap then sacrifice (Treasure, Lotus Petal)", 5,
			[]events.Event{{Kind: events.Tap, Obj: 5}, sac(5), mana("ArtifactG")},
			decision.ManaAmount{0, 0, 0, 0, 1, 0}, true},
		{"sacrifice without a tap (Eldrazi Spawn)", 6,
			[]events.Event{sac(6), mana("C")},
			decision.ManaAmount{0, 0, 0, 0, 0, 1}, true},
		{"tap then pay life", 7,
			[]events.Event{{Kind: events.Tap, Obj: 7}, {Kind: events.LifeChange, Player: payer, Amount: -1}, mana("B")},
			decision.ManaAmount{0, 0, 1, 0, 0, 0}, true},
		{"an unrelated event ends the window", 8,
			[]events.Event{{Kind: events.Tap, Obj: 8}, {Kind: events.Damage, Obj: 8, Amount: 1}, mana("R")},
			decision.ManaAmount{}, true},
		{"another source's sacrifice is not this source's cost", 9,
			[]events.Event{{Kind: events.Tap, Obj: 9}, sac(10), mana("W")},
			decision.ManaAmount{}, true},
		{"no activation at all", 11,
			[]events.Event{{Kind: events.Tap, Obj: 12}, mana("U")},
			decision.ManaAmount{}, false},
	} {
		got, ok := activationProduction(tc.evs, tc.source, payer)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: activationProduction = %v, %v; want %v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

// fixtureEngine is a started engine over the authored basic-land decks.
func fixtureEngine(t *testing.T) *rules.Engine {
	t.Helper()
	cfg := rules.Config{Seed: 11, Names: []string{"rg", "ub"}, Decks: authoredDecks(t), Tokens: map[string]*cards.Card{}}
	e := rules.New(cfg)
	e.Advance()
	return e
}

func diffsOf(a, b *rules.Engine, relax *floatReorder) []Diff {
	df := newDiffer()
	df.relax = relax
	df.walk("", reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem())
	return df.diffs
}

// TestFloatReorderAllowsOnlyAReorder pins the float route's cost-move
// allowance (diff.go floatReorder; round-5 paymirror float_then_cast
// state_differs on G.Entered[*].Sacrificed/Sacrificer/From/To and
// PreStackEnteredLen). Run A sacrifices a planned Treasure after its spell
// moved to the stack, the float route before: the same entries in another
// order. That reorder is allowed; a different entry is still reported.
// The two engines are clones that are never advanced: the walk only reads.
func TestFloatReorderAllowsOnlyAReorder(t *testing.T) {
	base := fixtureEngine(t)
	var cast state.ObjID = 1
	spell := state.ZoneEntry{Obj: cast, From: state.ZHand, To: state.ZStack, Owner: 0}
	treasure := state.ZoneEntry{Obj: 2, From: state.ZBattlefield, To: state.ZGraveyard, Owner: 0, PermanentCard: true, Sacrificed: true}

	a, b := base.Clone(), base.Clone()
	a.G.Entered = append(append([]state.ZoneEntry(nil), base.G.Entered...), spell, treasure)
	b.G.Entered = append(append([]state.ZoneEntry(nil), base.G.Entered...), treasure, spell)
	a.G.Objs[cast-1].PreStackEnteredLen = len(base.G.Entered)
	b.G.Objs[cast-1].PreStackEnteredLen = len(base.G.Entered) + 1

	if d := diffsOf(a, b, nil); len(d) == 0 {
		t.Fatal("the exact walk reports no difference for a reordered entry list (the fixture proves nothing)")
	}
	if d := diffsOf(a, b, newFloatReorder(a, b, cast, []state.ObjID{2}, 0)); len(d) != 0 {
		t.Fatalf("a float-route reorder of the same entries is reported: %v", d)
	}

	// Not the same multiset: the sacrifice is recorded differently. Every
	// field stays compared, the spell's boundary included.
	other := treasure
	other.Sacrificed = false
	b.G.Entered[len(b.G.Entered)-2] = other
	d := diffsOf(a, b, newFloatReorder(a, b, cast, []state.ObjID{2}, 0))
	seen := map[string]bool{}
	for _, x := range d {
		seen[x.Path] = true
	}
	if !seen["G.Objs[0].PreStackEnteredLen"] || len(d) < 2 {
		t.Fatalf("a changed entry must stay reported with the spell's boundary: %v", d)
	}

	// Identical lists earn no allowance: the spell's boundary is compared.
	b.G.Entered = append([]state.ZoneEntry(nil), a.G.Entered...)
	d = diffsOf(a, b, newFloatReorder(a, b, cast, []state.ObjID{2}, 0))
	if len(d) != 1 || d[0].Path != "G.Objs[0].PreStackEnteredLen" {
		t.Fatalf("identical entry lists must leave PreStackEnteredLen compared: %v", d)
	}
}

// TestControlIgnoresHarnessObservers pins the control's exclusion of the
// harness-only observers Clone deliberately does not copy: cmd/cardfuzz
// installs ManaAbilityHook on its live run A, and every round-5 cardfuzz
// mirror diag read control "ManaAbilityHook <func> vs nil".
func TestControlIgnoresHarnessObservers(t *testing.T) {
	live := fixtureEngine(t)
	live.ManaAbilityHook = func(state.PlayerID, state.ObjID, *cards.SA) {}
	live.SetPaymentPlanStats(&rules.PaymentPlanStats{})
	if d := diffsOf(live, live.Clone(), nil); len(d) != 0 {
		t.Fatalf("live engine with harness observers vs its clone: %v", d)
	}
}

// TestExpectedUnmirrorableVerdict pins the verdict of a cast whose only
// manual route is blocked by the float's own triggers (round-5
// cast_blocked_by_float_trigger, City of Brass): unmirrorable, keyed
// "expected:", and not a cardfuzz failure -- unless run A itself broke its
// contract or another route was not expected.
func TestExpectedUnmirrorableVerdict(t *testing.T) {
	blocked := RouteResult{Route: RouteFloat, Status: Unmirrorable, Reason: "cast_blocked_by_float_trigger", Expected: true}
	r := &Report{Routes: []RouteResult{blocked}}
	if st, key := r.Verdict(); st != Unmirrorable || key != "expected:float_then_cast:cast_blocked_by_float_trigger" {
		t.Fatalf("verdict = %s %q", st, key)
	}
	if !r.ExpectedUnmirrorable() {
		t.Fatal("an expected float-trigger block is reported as a failure")
	}
	unexpected := blocked
	unexpected.Expected = false
	if (&Report{Routes: []RouteResult{unexpected}}).ExpectedUnmirrorable() {
		t.Fatal("an unexpected unmirrorable route is excused")
	}
	if (&Report{AWitness: "wrong_production: x", Routes: []RouteResult{blocked}}).ExpectedUnmirrorable() {
		t.Fatal("run A's own witness violation is excused by an expected route")
	}
	if (&Report{Routes: []RouteResult{blocked, {Route: RouteBase, Status: Mismatch, Signature: "s"}}}).ExpectedUnmirrorable() {
		t.Fatal("a route mismatch is excused by an expected route")
	}
}

// TestPayMirrorSacrificeSourcesSeed1006 is the round-5 paymirror finding end
// to end (constructed seed 1006, the-epic-storm vs uw-control, Cabal Ritual
// paid with a Lotus Petal): every planned cast whose witness sacrifices a
// planned source mirrors equivalently -- witness, float route and control.
func TestPayMirrorSacrificeSourcesSeed1006(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	d, err := LoadDecks(reg)
	if err != nil {
		t.Fatal(err)
	}
	// The driver keeps only a compact copy of an equivalent report (its
	// witness dropped), so the full reports are collected as they are made.
	var full []*Report
	g := PlayGame(d, GameSpec{Seed: 1006, Decks: []string{"the-epic-storm", "uw-control"}, Policy: "bot"},
		DriverOptions{Control: true, Resolve: true, OnReport: func(_ GameSpec, r *Report) { full = append(full, r) }})
	if g.Err != "" {
		t.Fatalf("game error: %s", g.Err)
	}
	sacrificing := 0
	for _, r := range full {
		sac := false
		for _, act := range r.Plan.Activations {
			if act.Consequence != nil && act.Consequence.Sacrifice {
				sac = true
			}
		}
		if !sac {
			continue
		}
		sacrificing++
		if st, key := r.Verdict(); st != Equivalent {
			t.Errorf("seq %d %q: %s %s (witness %q, routes %+v)", r.Seq, r.Card, st, key, r.AWitness, r.Routes)
		}
		if r.Control == nil || r.Control.Status != Equivalent {
			t.Errorf("seq %d %q: control %+v", r.Seq, r.Card, r.Control)
		}
		for _, rr := range r.Routes {
			if rr.Route == RouteFloat && rr.Resolved != string(Equivalent) {
				t.Errorf("seq %d %q: float route resolved %q (%v)", r.Seq, r.Card, rr.Resolved, rr.Diffs)
			}
		}
	}
	if sacrificing == 0 {
		t.Fatalf("no planned cast sacrificed a planned source (the check is vacuous): %d planned casts, %d turns", len(full), g.Turns)
	}
	t.Logf("MEASURED seed 1006: %d planned casts, %d with a sacrificed planned source", len(full), sacrificing)
}
