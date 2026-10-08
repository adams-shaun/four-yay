package azmcts

import (
	"math"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/state"
)

// playerTarget is a single-target decision over the first n players: it
// names no object, so a collector needs no engine to observe it.
func playerTarget(seq uint64, n int) *decision.Decision {
	d := &decision.Decision{Seq: seq, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1}
	labels := []string{"you", "opponent"}
	for i := 0; i < n; i++ {
		d.Options = append(d.Options, decision.Option{Index: i, Kind: "target", Player: state.PlayerID(i), Label: labels[i]})
	}
	return d
}

// passOrAbility is a priority decision whose options name no object.
func passOrAbility(seq uint64) *decision.Decision {
	return &decision.Decision{Seq: seq, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "pass", Label: "Pass"},
		{Index: 1, Kind: "ability", Label: "Draw a card"},
	}}
}

func choicesOf(cands []cand) [][]int {
	out := make([][]int, len(cands))
	for i, c := range cands {
		out[i] = c.in.Choices
	}
	return out
}

func TestEnumerateTargetBotFirstWithStableKeys(t *testing.T) {
	d := playerTarget(4, 2)
	bot := decision.Intent{Seq: 4, Player: 0, Choices: []int{1}}
	cands, kind, ok := enumerate(searchprobe.NewCollector(0), nil, d, bot, AllKinds(), 6)
	if !ok || kind != "target" {
		t.Fatalf("enumerate = %v, %q", ok, kind)
	}
	if got := choicesOf(cands); !reflect.DeepEqual(got, [][]int{{1}, {0}}) {
		t.Fatalf("candidates %v, want the bot's pick first", got)
	}
	if cands[0].key == cands[1].key {
		t.Fatal("two different targets share a key")
	}
	again, _, _ := enumerate(searchprobe.NewCollector(0), nil, d, bot, AllKinds(), 6)
	if again[0].key != cands[0].key || again[1].key != cands[1].key {
		t.Fatal("keys differ between two fresh collectors")
	}
	if _, kind, ok := enumerate(searchprobe.NewCollector(0), nil, d, bot, Kinds{Priority: true}, 6); ok || kind != "" {
		t.Fatalf("an unsearched kind enumerated: %v, %q", ok, kind)
	}
}

func TestEnumeratePriorityBotFirstThenPass(t *testing.T) {
	d := passOrAbility(5)
	for _, tc := range []struct {
		bot  int
		want [][]int
	}{{0, [][]int{{0}, {1}}}, {1, [][]int{{1}, {0}}}} {
		bot := decision.Intent{Seq: 5, Player: 0, Choices: []int{tc.bot}}
		cands, kind, ok := enumerate(searchprobe.NewCollector(0), nil, d, bot, AllKinds(), 6)
		if !ok || kind != "priority" {
			t.Fatalf("bot %d: enumerate = %v, %q", tc.bot, ok, kind)
		}
		if got := choicesOf(cands); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("bot %d: candidates %v, want %v", tc.bot, got, tc.want)
		}
	}
}

// Review Focus 4: a searched kind with fewer than two candidates is never a
// node.
func TestEnumerateOneOrZeroCandidates(t *testing.T) {
	one := playerTarget(6, 1)
	if _, kind, ok := enumerate(searchprobe.NewCollector(0), nil, one, decision.Intent{Seq: 6, Player: 0, Choices: []int{0}}, AllKinds(), 6); ok || kind != "" {
		t.Fatalf("a one-option target enumerated: %v, %q", ok, kind)
	}
	passOnly := &decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "pass", Label: "Pass"}}}
	if _, kind, ok := enumerate(searchprobe.NewCollector(0), nil, passOnly, decision.Intent{Seq: 7, Player: 0, Choices: []int{0}}, AllKinds(), 6); ok || kind != "priority" {
		t.Fatalf("a pass-only priority enumerated: %v, %q", ok, kind)
	}
	noAttack := &decision.Decision{Seq: 8, Player: 0, Kind: decision.KAttackers}
	if _, kind, ok := enumerate(searchprobe.NewCollector(0), nil, noAttack, decision.Intent{Seq: 8, Player: 0}, AllKinds(), 6); ok || kind != "attackers" {
		t.Fatalf("an attackers decision with no attacker enumerated: %v, %q", ok, kind)
	}
}

// Review Focus 1: an auto-pay answer cannot be expressed as semantic actions
// (Intent.Payment is exclusive with Choices), so the decision is skipped.
func TestEnumerateRefusesAPaymentIntent(t *testing.T) {
	d := passOrAbility(9)
	bot := decision.Intent{Seq: 9, Player: 0, Payment: &decision.PaymentSelection{}}
	if _, kind, ok := enumerate(searchprobe.NewCollector(0), nil, d, bot, AllKinds(), 6); ok || kind != "priority" {
		t.Fatalf("a payment intent enumerated: %v, %q", ok, kind)
	}
}

func TestEnumerateRefusesAnotherSeatsCollector(t *testing.T) {
	d := passOrAbility(10)
	bot := decision.Intent{Seq: 10, Player: 0, Choices: []int{1}}
	if _, _, ok := enumerate(searchprobe.NewCollector(1), nil, d, bot, AllKinds(), 6); ok {
		t.Fatal("seat 1's collector enumerated seat 0's decision")
	}
}

func TestActionsKeyIsCanonical(t *testing.T) {
	a := []searchprobe.Action{{Decision: decision.KPriority, Kind: "pass", Value: "Pass"}}
	b := []searchprobe.Action{{Decision: decision.KPriority, Kind: "pass", Value: "Pass"}}
	c := []searchprobe.Action{{Decision: decision.KPriority, Kind: "pass", Value: "Pass!"}}
	if actionsKey(a) != actionsKey(b) || actionsKey(a) == actionsKey(c) {
		t.Fatal("keys do not follow action equality")
	}
	// An empty declaration (a no-attack answer) is its own key: equal to
	// every other empty one, distinct from any non-empty list and from nil,
	// exactly as the JSON "[]" it replaced was.
	if actionsKey([]searchprobe.Action{}) != actionsKey(make([]searchprobe.Action, 0, 4)) || actionsKey([]searchprobe.Action{}) == actionsKey(nil) || actionsKey([]searchprobe.Action{}) == actionsKey(a) {
		t.Fatal("the empty declaration's key is not its own")
	}
}

// TestPayActionsKeyMatchesConcatenation pins the POC cut that builds a
// payment candidate's key in one buffer (payActionsKey) instead of
// concatenating actionsKey's result onto the prefix: the two must name the
// same key for every action list, including the empty and nil declarations.
func TestPayActionsKeyMatchesConcatenation(t *testing.T) {
	for _, acts := range [][]searchprobe.Action{
		nil,
		{},
		{{Decision: decision.KPriority, Kind: "pass", Value: "Pass"}},
		{{Decision: decision.KPriority, Kind: "cast", Obj: 5, Value: "cast"}},
		{{Decision: decision.KPriority, Kind: "cast", Obj: 5}, {Decision: decision.KPriority, Kind: "activate", Obj: 9}},
	} {
		if got, want := payActionsKey(acts), Key(payKeyPrefix+string(actionsKey(acts))); got != want {
			t.Fatalf("payActionsKey(%v) = %q, want %q", acts, got, want)
		}
	}
}

// TestBotFirstCapsAndKeepsFirst pins the POC cut that pre-sizes botFirst's
// output: all[botAt] stays at the front, the result is capped at limit and
// repeats no candidate.
func TestBotFirstCapsAndKeepsFirst(t *testing.T) {
	all := make([]cand, 6)
	for i := range all {
		all[i] = cand{key: Key(string(rune('a' + i)))}
	}
	for _, tc := range []struct {
		botAt, limit, want int
	}{
		{3, 4, 4}, {3, 10, 6}, {0, 3, 3}, {0, 1, 1}, {5, 6, 6},
	} {
		got := botFirst(all, tc.botAt, tc.limit)
		if len(got) != tc.want {
			t.Fatalf("botFirst(botAt %d, limit %d) len %d, want %d", tc.botAt, tc.limit, len(got), tc.want)
		}
		if got[0].key != all[tc.botAt].key {
			t.Fatalf("botFirst(botAt %d, limit %d) first = %q, want %q", tc.botAt, tc.limit, got[0].key, all[tc.botAt].key)
		}
		seen := map[Key]bool{}
		for _, c := range got {
			if seen[c.key] {
				t.Fatalf("botFirst(botAt %d, limit %d) repeated %q", tc.botAt, tc.limit, c.key)
			}
			seen[c.key] = true
		}
	}
}

func TestSoftmax(t *testing.T) {
	p, ok := softmax([]float64{0, math.Log(3)})
	if !ok || !near(p[0], 0.25) || !near(p[1], 0.75) {
		t.Fatalf("softmax = %v, %v", p, ok)
	}
	if p, ok := softmax([]float64{math.Inf(-1), 0}); !ok || p[0] != 0 || !near(p[1], 1) {
		t.Fatalf("softmax with -Inf = %v, %v", p, ok)
	}
	for _, bad := range [][]float64{{math.Inf(-1), math.Inf(-1)}, {math.NaN(), 0}, {math.Inf(1), 0}} {
		if _, ok := softmax(bad); ok {
			t.Errorf("softmax(%v) accepted", bad)
		}
	}
}

func TestPriorsUniformWithoutANetwork(t *testing.T) {
	d := playerTarget(11, 2)
	bot := decision.Intent{Seq: 11, Player: 0, Choices: []int{0}}
	cands, kind, ok := enumerate(searchprobe.NewCollector(0), nil, d, bot, AllKinds(), 6)
	if !ok {
		t.Fatal("enumerate failed")
	}
	p, fell := priors(nil, nil, d, bot, kind, cands, nil)
	if fell || !reflect.DeepEqual(p, []float64{0.5, 0.5}) {
		t.Fatalf("priors = %v (fell back %v), want uniform", p, fell)
	}
}

// The priority arm never offers an ability the bot's own guards decline
// (botpolicy A1/A5): here a free Equip with no creature to attach to (the
// zero Board a nil engine reads) and a keyword grant the source already has.
// With only the pass left there is nothing to search. A worth-taking ability
// beside them is still a candidate.
func TestEnumerateDropsAbilitiesTheBotDeclines(t *testing.T) {
	d := &decision.Decision{Seq: 12, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "pass", Label: "Pass"},
		{Index: 1, Kind: "ability", Label: "Equip 0", Attach: true},
		{Index: 2, Kind: "ability", Label: "Gain flying", Grant: &decision.Grant{Already: true}},
	}}
	bot := decision.Intent{Seq: 12, Player: 0, Choices: []int{0}}
	if _, kind, why, ok := enumerateWhy(searchprobe.NewCollector(0), nil, d, bot, AllKinds(), 6); ok || kind != "priority" || why != SkipFewCandidates {
		t.Fatalf("declined abilities searched: ok %v kind %q why %v", ok, kind, why)
	}
	d.Options = append(d.Options, decision.Option{Index: 3, Kind: "ability", Label: "Draw a card"})
	cands, _, ok := enumerate(searchprobe.NewCollector(0), nil, d, bot, AllKinds(), 6)
	if !ok {
		t.Fatal("a worth-taking ability beside the declined ones was not searched")
	}
	if got := choicesOf(cands); !reflect.DeepEqual(got, [][]int{{0}, {3}}) {
		t.Fatalf("candidates %v, want pass then the draw only", got)
	}
}
