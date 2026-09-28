package builtins

import (
	"context"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// TestSplitMix64MatchesSpellBench pins the port to values the Python
// original (spellbench.arena.bots.uniform.SplitMix64) produces.
func TestSplitMix64MatchesSpellBench(t *testing.T) {
	s := NewSplitMix64(0)
	want := []uint64{16294208416658607535, 7960286522194355700, 487617019471545679}
	for i, w := range want {
		if got := s.Next(); got != w {
			t.Fatalf("draw %d = %d, want %d", i, got, w)
		}
	}
	s = NewSplitMix64(11 ^ 12345)
	wantIdx := []int{2, 3, 5, 3, 0, 2, 2, 2, 5, 3} // [s.next() % 7 for _ in range(10)]
	for i, w := range wantIdx {
		if got := s.Index(7); got != w {
			t.Fatalf("index %d = %d, want %d", i, got, w)
		}
	}
}

func opt(i int, kind string, obj state.ObjID) decision.Option {
	return decision.Option{Index: i, Kind: kind, Obj: obj, Label: kind}
}

func prio(opts ...decision.Option) decision.Decision {
	return decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
}

func me(v *view.View) *view.PlayerView {
	if len(v.Players) == 0 {
		v.Players = []view.PlayerView{{ID: 0}, {ID: 1}}
	}
	return &v.Players[0]
}

func decide(t *testing.T, s *Seat, v view.View, d decision.Decision) decision.Intent {
	t.Helper()
	in, err := s.Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	if in.Seq != d.Seq || in.Player != d.Player {
		t.Fatalf("intent %+v does not answer seq %d player %d", in, d.Seq, d.Player)
	}
	return in
}

// A priority window with every family the heuristic ranks.
func richPriority() decision.Decision {
	return prio(
		opt(0, "cast", 10),
		opt(1, "activate", 20),
		opt(2, "play_land", 30),
		opt(3, "ability", 40),
		opt(4, "concede", 0),
		opt(5, "pass", 0),
	)
}

func TestHeuristicPriorityOrder(t *testing.T) {
	v := view.View{}
	me(&v)
	for _, tc := range []struct {
		name string
		drop map[string]bool
		mana ManaMode
		want string
	}{
		{"land first", nil, AutoPay, "play_land"},
		{"then cast", map[string]bool{"play_land": true}, AutoPay, "cast"},
		{"then ability", map[string]bool{"play_land": true, "cast": true}, AutoPay, "ability"},
		{"mana hidden under autopay", map[string]bool{"play_land": true, "cast": true, "ability": true}, AutoPay, "pass"},
		{"manual taps mana first", map[string]bool{"play_land": true, "cast": true}, Manual, "activate"},
		{"else pass", map[string]bool{"play_land": true, "cast": true, "ability": true, "activate": true}, Manual, "pass"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			full := richPriority()
			var opts []decision.Option
			for _, o := range full.Options {
				if !tc.drop[o.Kind] {
					o.Index = len(opts)
					opts = append(opts, o)
				}
			}
			d := prio(opts...)
			in := decide(t, New(Heuristic, tc.mana, 1), v, d)
			if len(in.Choices) != 1 || d.Options[in.Choices[0]].Kind != tc.want {
				t.Fatalf("heuristic chose %+v, want %s", in, tc.want)
			}
		})
	}
}

func TestFirstAlwaysPasses(t *testing.T) {
	v := view.View{}
	me(&v)
	d := richPriority()
	in := decide(t, New(First, AutoPay, 1), v, d)
	if d.Options[in.Choices[0]].Kind != "pass" {
		t.Fatalf("first chose %+v, want pass", in)
	}
}

func TestUniformNeverConcedesOrTapsUnderAutoPay(t *testing.T) {
	v := view.View{}
	me(&v)
	s := New(Uniform, AutoPay, 3)
	seen := map[string]int{}
	for i := 0; i < 400; i++ {
		d := richPriority()
		in := decide(t, s, v, d)
		seen[d.Options[in.Choices[0]].Kind]++
	}
	if seen["concede"] > 0 || seen["activate"] > 0 {
		t.Fatalf("uniform picked a hidden candidate: %v", seen)
	}
	for _, k := range []string{"pass", "cast", "play_land", "ability"} {
		if seen[k] < 60 { // expected 100 of 400
			t.Fatalf("uniform is not uniform over the 4 candidates: %v", seen)
		}
	}
}

// TestDeterminism: a seat's answers are a pure function of its seed.
func TestDeterminism(t *testing.T) {
	v := view.View{}
	me(&v)
	run := func(seed uint64) []decision.Intent {
		s := New(Uniform, AutoPay, seed)
		var out []decision.Intent
		for i := 0; i < 50; i++ {
			out = append(out, decide(t, s, v, richPriority()))
			out = append(out, decide(t, s, v, attackersDecision()))
			out = append(out, decide(t, s, v, choose3()))
		}
		return out
	}
	a, b := run(42), run(42)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same seed, different answers")
	}
	if reflect.DeepEqual(a, run(43)) {
		t.Fatal("different seeds gave identical 150-answer streams")
	}
}

// attackersDecision: creature 1 may attack p1 or a planeswalker (two
// options), creature 2 must attack, creature 3 is free.
func attackersDecision() decision.Decision {
	return decision.Decision{Seq: 7, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 4, Options: []decision.Option{
		{Index: 0, Kind: "attacker", Obj: 1, Player: 1},
		{Index: 1, Kind: "attacker", Obj: 1, Player: 1, Battle: 99},
		{Index: 2, Kind: "attacker", Obj: 2, Player: 1, Required: true},
		{Index: 3, Kind: "attacker", Obj: 3, Player: 1},
	}}
}

func TestCombatMapping(t *testing.T) {
	v := view.View{}
	me(&v)
	d := attackersDecision()
	if in := decide(t, New(Heuristic, AutoPay, 1), v, d); !reflect.DeepEqual(in.Choices, []int{0, 2, 3}) {
		t.Fatalf("heuristic attackers %v, want every creature at its first defender [0 2 3]", in.Choices)
	}
	if in := decide(t, New(First, AutoPay, 1), v, d); !reflect.DeepEqual(in.Choices, []int{2}) {
		t.Fatalf("first attackers %v, want only the required creature [2]", in.Choices)
	}
	// uniform: each answer names each creature at most once and always
	// includes the required one; every option is reached.
	s := New(Uniform, AutoPay, 9)
	hit := map[int]int{}
	for i := 0; i < 300; i++ {
		in := decide(t, s, v, d)
		if err := d.Validate(in); err != nil {
			t.Fatal(err)
		}
		objs := map[state.ObjID]bool{}
		for _, c := range in.Choices {
			o := d.Options[c]
			if objs[o.Obj] {
				t.Fatalf("creature %d attacks twice: %v", o.Obj, in.Choices)
			}
			objs[o.Obj] = true
			hit[c]++
		}
		if !objs[2] {
			t.Fatalf("required attacker missing: %v", in.Choices)
		}
	}
	// creature 1: null/p1/pw each ~1/3; creature 3 ~1/2.
	if hit[0] < 60 || hit[1] < 60 || hit[3] < 110 {
		t.Fatalf("uniform per-creature draws look biased: %v", hit)
	}

	blk := decision.Decision{Seq: 7, Player: 0, Kind: decision.KBlockers, Min: 0, Max: 3, Options: []decision.Option{
		{Index: 0, Kind: "block", Obj: 5, Attacker: 1, Group: "blocker:5"},
		{Index: 1, Kind: "block", Obj: 5, Attacker: 2, Group: "blocker:5"},
		{Index: 2, Kind: "block", Obj: 6, Attacker: 1, Group: "blocker:6", Required: true},
	}}
	for _, p := range []Policy{Heuristic, First} {
		if in := decide(t, New(p, AutoPay, 1), v, blk); !reflect.DeepEqual(in.Choices, []int{2}) {
			t.Fatalf("%v blockers %v, want only the required block [2]", p, in.Choices)
		}
	}
}

func choose3() decision.Decision {
	return decision.Decision{Seq: 7, Player: 0, Kind: decision.KChoose, Min: 1, Max: 2, Options: []decision.Option{
		opt(0, "card", 1), opt(1, "card", 2), opt(2, "card", 3),
	}}
}

func TestSequentialSelection(t *testing.T) {
	v := view.View{}
	me(&v)
	d := choose3()
	for _, p := range []Policy{Heuristic, First} {
		if in := decide(t, New(p, AutoPay, 1), v, d); !reflect.DeepEqual(in.Choices, []int{0}) {
			t.Fatalf("%v chose %v, want the Min lowest options [0]", p, in.Choices)
		}
	}
	s := New(Uniform, AutoPay, 5)
	sizes := map[int]int{}
	for i := 0; i < 300; i++ {
		in := decide(t, s, v, d)
		if err := d.Validate(in); err != nil {
			t.Fatal(err)
		}
		sizes[len(in.Choices)]++
	}
	// after the first pick, finish vs 2 options: finish ~1/3.
	if sizes[1] < 60 || sizes[2] < 140 {
		t.Fatalf("uniform subset sizes %v", sizes)
	}
	// A trigger order is a permutation for every policy.
	ord := decision.Decision{Seq: 7, Player: 0, Kind: decision.KTriggerOrder, Min: 3, Max: 3, Options: []decision.Option{
		opt(0, "trigger", 1), opt(1, "trigger", 2), opt(2, "trigger", 3),
	}}
	if in := decide(t, New(First, AutoPay, 1), v, ord); !reflect.DeepEqual(in.Choices, []int{0, 1, 2}) {
		t.Fatalf("first trigger order %v", in.Choices)
	}
	perms := map[[3]int]bool{}
	for i := 0; i < 200; i++ {
		in := decide(t, s, v, ord)
		if err := ord.Validate(in); err != nil {
			t.Fatal(err)
		}
		perms[[3]int{in.Choices[0], in.Choices[1], in.Choices[2]}] = true
	}
	if len(perms) != 6 {
		t.Fatalf("uniform reached %d of 6 trigger orders", len(perms))
	}
}

func TestAutoPayPlanAndPursuit(t *testing.T) {
	v := view.View{}
	p := me(&v)
	// A plan-only cast (object 10) and a potential ability (object 40).
	d := prio(opt(0, "activate", 20), opt(1, "pass", 0))
	d.PaymentActions = []decision.PaymentAction{{ID: "a1", Cast: decision.PlannedCast{Object: 10},
		Plans: []decision.PaymentPlan{{Version: decision.PaymentPlanV1}}}}
	p.PotentialActions = []decision.PotentialAction{
		{Kind: "cast", Obj: 10}, // the planned cast: not duplicated
		{Kind: "ability", Obj: 40, Ability: 2},
	}
	in := decide(t, New(Heuristic, AutoPay, 1), v, d)
	if in.Payment == nil || in.Payment.ActionID != "a1" {
		t.Fatalf("heuristic should cast through the plan, got %+v", in)
	}

	// Without a castable spell the heuristic pursues the potential ability:
	// it taps the offered source, then takes the ability once offered.
	d.PaymentActions = nil
	p.PotentialActions = p.PotentialActions[1:]
	s := New(Heuristic, AutoPay, 1)
	v.Turn, v.Step = 3, "main1"
	in = decide(t, s, v, d)
	if len(in.Choices) != 1 || d.Options[in.Choices[0]].Kind != "activate" {
		t.Fatalf("pursuit should tap the source first, got %+v", in)
	}
	offered := prio(decision.Option{Index: 0, Kind: "ability", Obj: 40, Ability: 2}, opt(1, "pass", 0))
	if in = decide(t, s, v, offered); !reflect.DeepEqual(in.Choices, []int{0}) {
		t.Fatalf("pursuit should take the now-offered ability, got %+v", in)
	}
	if s.Stats.Pursuits != 1 || s.Stats.PursuitTaps != 1 || s.Stats.PursuitFailures != 0 {
		t.Fatalf("stats %+v", s.Stats)
	}

	// No source to tap: the pursuit fails, the play is dropped for the
	// step and the seat passes instead.
	s = New(Heuristic, AutoPay, 1)
	bare := prio(opt(0, "pass", 0))
	if in = decide(t, s, v, bare); !reflect.DeepEqual(in.Choices, []int{0}) || s.Stats.PursuitFailures != 1 {
		t.Fatalf("failed pursuit should pass, got %+v stats %+v", in, s.Stats)
	}
	if in = decide(t, s, v, bare); s.Stats.Pursuits != 1 {
		t.Fatalf("a failed play must not be re-pursued in the same step: %+v", s.Stats)
	}
	v.Step = "main2"
	decide(t, s, v, bare)
	if s.Stats.Pursuits != 2 {
		t.Fatalf("a new step should allow the play again: %+v", s.Stats)
	}
}

func TestPursuitColourCoversCost(t *testing.T) {
	// Pool holds B; the remaining untapped dual makes B or R. The asked
	// source makes R or G: G is the colour nothing else left can make.
	v := view.View{}
	p := me(&v)
	p.Pool = map[string]int32{"B": 1}
	p.Battlefield = []view.CardView{{ID: 5, Produces: &cards.ManaProduction{Colour: [6]int32{0, 0, 1, 1, 0, 0}, Any: true}}}
	d := &decision.Decision{Kind: decision.KChoose, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "mana", ManaSymbol: "R"}, {Index: 1, Kind: "mana", ManaSymbol: "G"},
	}}
	if i, ok := pursuitColour(v, d); !ok || i != 1 {
		t.Fatalf("pursuitColour = %d, %v; want G (1)", i, ok)
	}
}
