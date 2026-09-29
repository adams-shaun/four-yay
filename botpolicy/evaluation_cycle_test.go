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
	t.Run("mutual recursion has a legal deterministic fallback", func(t *testing.T) {
		nodes := []dependencyNode{
			{key: key(1), value: 10, deps: []int{1}},
			{key: key(2), value: 20, deps: []int{0}},
		}
		if got := evaluateDependencies(nodes, 0); got.known || got.value != 0 {
			t.Fatalf("mutual recursion = %+v; want unknown", got)
		}
		// The existing default policy, which does not consume dependency
		// scores, must still submit a legal deterministic answer on this board.
		d := decision.Decision{Seq: 3, Kind: decision.KPriority, Min: 1, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "pass"}, {Index: 1, Kind: "concede"},
		}}
		first := Decide(Board{}, &d, rng(1))
		if err := d.Validate(first); err != nil {
			t.Fatalf("fallback rejected: %v", err)
		}
		if !reflect.DeepEqual(first.Choices, []int{0}) {
			t.Fatalf("fallback = %v, want pass", first.Choices)
		}
		if again := Decide(Board{}, &d, rng(1)); !reflect.DeepEqual(first, again) {
			t.Fatalf("fallback differs: %v versus %v", first, again)
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
}
