package botpolicy

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestEvaluationCycleBudget(t *testing.T) {
	key := func(card int) dependencyKey {
		return dependencyKey{card: state.ObjID(card), ability: "reanimate", zone: "graveyard", context: "ETB"}
	}
	t.Run("self reanimation", func(t *testing.T) {
		nodes := []dependencyNode{{key: key(1), value: 7, deps: []int{0}}}
		if got := evaluateDependencies(nodes, 0); got.known || got.value != 0 {
			t.Fatalf("self-reanimation = %+v; want unknown, not an unbounded score", got)
		}
	})
	t.Run("mutual recursion returns unknown, never a score", func(t *testing.T) {
		nodes := []dependencyNode{
			{key: key(1), value: 10, deps: []int{1}},
			{key: key(2), value: 20, deps: []int{0}},
		}
		if got := evaluateDependencies(nodes, 0); got.known || got.value != 0 {
			t.Fatalf("mutual recursion = %+v; want unknown", got)
		}
	})
	t.Run("long acyclic chain", func(t *testing.T) {
		nodes := make([]dependencyNode, maxEvaluationDepth)
		for i := range nodes {
			nodes[i] = dependencyNode{key: key(i + 1), value: 1}
			if i+1 < len(nodes) {
				nodes[i].deps = []int{i + 1}
			}
		}
		if got := evaluateDependencies(nodes, 0); !got.known || got.value != maxEvaluationDepth {
			t.Fatalf("chain at depth limit = %+v", got)
		}
		nodes = append(nodes, dependencyNode{key: key(len(nodes) + 1), value: 1})
		nodes[len(nodes)-2].deps = []int{len(nodes) - 1}
		if got := evaluateDependencies(nodes, 0); got.known || got.value != 0 {
			t.Fatalf("chain beyond depth limit = %+v; want unknown", got)
		}
	})
	t.Run("completed visits separate from active path", func(t *testing.T) {
		// Both branches legally visit the same leaf. A permanent visiting bit
		// would reject the second branch; a completed-value cache can reuse it.
		nodes := []dependencyNode{
			{key: key(1), value: 1, deps: []int{1, 2}},
			{key: key(2), value: 2, deps: []int{3}},
			{key: key(3), value: 3, deps: []int{4}},
			{key: key(4), value: 5},
			{key: key(4), value: 5},
		}
		if got := evaluateDependencies(nodes, 0); !got.known || got.value != 16 {
			t.Fatalf("diamond = %+v; want 1+2+5+3+5", got)
		}
		// Different zone/target/context/ability keys must not be mistaken
		// for a self-cycle, even with the same card ID.
		for _, change := range []func(*dependencyKey){
			func(k *dependencyKey) { k.zone = "battlefield" },
			func(k *dependencyKey) { k.target = 42 },
			func(k *dependencyKey) { k.context = "cast" },
			func(k *dependencyKey) { k.ability = "rescue" },
		} {
			other := key(1)
			change(&other)
			pair := []dependencyNode{{key: key(1), value: 1, deps: []int{1}}, {key: other, value: 2}}
			if got := evaluateDependencies(pair, 0); !got.known || got.value != 3 {
				t.Fatalf("distinct context %+v rejected: %+v", other, got)
			}
		}
	})
	t.Run("node budget", func(t *testing.T) {
		// 4097 unique nodes are refused before traversal, including when
		// only a small prefix would have been visited. A caller may fall
		// back to an ordinary deterministic policy answer.
		nodes := make([]dependencyNode, maxEvaluationNodes+1)
		for i := range nodes {
			nodes[i].key = key(i + 1)
		}
		if got := evaluateDependencies(nodes, 0); got.known || got.value != 0 {
			t.Fatalf("over budget = %+v; want unknown", got)
		}
	})
	t.Run("production boundary: default policy answers a mutual-reanimation decision legally and deterministically", func(t *testing.T) {
		// The L1 acceptance at the ACTUAL boundary: today no botpolicy
		// evaluator follows spell/ETB dependencies (the premise the
		// structural test enforces), so the production answer to a decision
		// whose option set spells out a self-reanimation and a two-card
		// mutual recursion must be a legal, deterministic, terminating
		// answer — never a recursion, a crash or an infinite score. This
		// exercises the production entries, not the helper above.
		d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: []decision.Option{
			// Self-reanimation shape: one card whose only value is casting
			// itself again (zero board facts, C5). Mutual recursion shape:
			// two cards whose value each depends on the other.
			{Index: 0, Kind: "cast", Obj: 101, Label: "Nexus of Self-Reanimation"},
			{Index: 1, Kind: "cast", Obj: 102, Label: "Dread Return A"},
			{Index: 2, Kind: "cast", Obj: 103, Label: "Dread Return B"},
			{Index: 3, Kind: "pass"},
		}}
		if len(d.Options) != 4 || d.Min != 1 {
			t.Fatalf("bad precondition: %d options, Min %d", len(d.Options), d.Min)
		}
		first := Decide(Board{}, &d, rng(1))
		if err := d.Validate(first); err != nil {
			t.Fatalf("default policy rejected on the mutual-reanimation shape: %v", err)
		}
		again := Decide(Board{}, &d, rng(1))
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("default policy nondeterministic on the mutual-reanimation shape: %v versus %v", first, again)
		}
		// And through the one LIVE recursion in the package's call graph:
		// Clamp's constraint repair (allowlisted by the structural test as
		// bounded at three frames by its projections). Both repair entries
		// must return an answer Decision.Validate accepts, deterministically.
		same := decision.Decision{Seq: 9, Player: 0, Kind: decision.KTarget, Min: 2, Max: 2, TargetsWithSameController: true, Options: []decision.Option{
			{Index: 0, Kind: "creature", Obj: 11, Player: 0, Controller: 0}, // mine: cannot reach Min 2 alone
			{Index: 1, Kind: "creature", Obj: 12, Player: 1, Controller: 1},
			{Index: 2, Kind: "creature", Obj: 13, Player: 1, Controller: 1},
		}}
		if len(same.Options) != 3 {
			t.Fatalf("bad precondition: %d same-controller options", len(same.Options))
		}
		in := decision.Intent{Seq: 9, Player: 0, Choices: []int{0}}
		out := Clamp(&same, in)
		if err := same.Validate(out); err != nil {
			t.Fatalf("same-controller repair rejected: %v", err)
		}
		if !reflect.DeepEqual(out.Choices, []int{1, 2}) {
			t.Fatalf("same-controller repair = %v, want the two opposing picks", out.Choices)
		}
		if out2 := Clamp(&same, in); !reflect.DeepEqual(out, out2) {
			t.Fatalf("same-controller repair nondeterministic: %v versus %v", out, out2)
		}
		shared := decision.Decision{Seq: 11, Player: 0, Kind: decision.KTarget, Min: 2, Max: 2, SetPropMode: decision.SetPropShared, Options: []decision.Option{
			{Index: 0, Kind: "creature", Obj: 21, Player: 1, SetProps: []string{"cleric"}},
			{Index: 1, Kind: "creature", Obj: 22, Player: 1, SetProps: []string{"cleric"}},
			{Index: 2, Kind: "creature", Obj: 23, Player: 1, SetProps: []string{"wizard"}},
		}}
		if len(shared.Options) != 3 {
			t.Fatalf("bad precondition: %d shared-class options", len(shared.Options))
		}
		in = decision.Intent{Seq: 11, Player: 0, Choices: []int{2}}
		out = Clamp(&shared, in)
		if err := shared.Validate(out); err != nil {
			t.Fatalf("shared-class repair rejected: %v", err)
		}
		if !reflect.DeepEqual(out.Choices, []int{0, 1}) {
			t.Fatalf("shared-class repair = %v, want the two cleric picks", out.Choices)
		}
		if out2 := Clamp(&shared, in); !reflect.DeepEqual(out, out2) {
			t.Fatalf("shared-class repair nondeterministic: %v versus %v", out, out2)
		}
	})
}
