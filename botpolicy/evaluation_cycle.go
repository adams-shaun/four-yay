package botpolicy

import (
	"sort"

	"github.com/adams-shaun/gorge/state"
)

// dependencyKey identifies an evaluation, not merely a card. Re-entering a
// card from a different zone, target, ability or evaluation context is a
// distinct visit. Callers must give equal keys the same dependencies/value.
// This is the shared guard for dependency-following evaluators; today's
// cast/ability/target scorers read only the current Board, not other scores.
type dependencyKey struct {
	card, target           state.ObjID
	ability, zone, context string
}

type dependencyNode struct {
	key   dependencyKey
	value int32
	deps  []int // indices into the caller's dense node slice, in evaluation order
}

type dependencyValue struct {
	value int32
	known bool // false means cyclic or budget-limited; never an infinite score
}

const (
	maxEvaluationDepth = 32
	maxEvaluationNodes = 4096
)

// evaluateDependencies walks an evaluation graph without recursive Go calls.
// Both visits and completed values are local to this invocation. The bitset
// holds ONLY the active path; successful completions are cached separately.
// Unknown (cycle or budget) is never cached: a node rejected on one path can
// be evaluated on a later, legal path. Work and depth are hard limits even if
// the caller supplies a maliciously large graph. No map is used on this path.
func evaluateDependencies(nodes []dependencyNode, root int) dependencyValue {
	unknown := dependencyValue{}
	if root < 0 || root >= len(nodes) || len(nodes) > maxEvaluationNodes {
		return unknown
	}
	// Canonicalise equal (card, ability, zone, target, context) keys to one
	// dense bit index. Sorting an index slice leaves the caller's ordering and
	// dependency indices untouched. No string concatenation or map allocation.
	order := make([]int, len(nodes))
	for i := range order {
		order[i] = i
	}
	less := func(a, b dependencyKey) bool {
		if a.card != b.card {
			return a.card < b.card
		}
		if a.ability != b.ability {
			return a.ability < b.ability
		}
		if a.zone != b.zone {
			return a.zone < b.zone
		}
		if a.target != b.target {
			return a.target < b.target
		}
		return a.context < b.context
	}
	sort.Slice(order, func(i, j int) bool { return less(nodes[order[i]].key, nodes[order[j]].key) })
	index := make([]int, len(nodes))
	unique := 0
	for pos, n := range order {
		if pos > 0 {
			prev := nodes[order[pos-1]].key
			cur := nodes[n].key
			if less(prev, cur) {
				unique++
			}
		}
		index[n] = unique
	}
	var visiting [maxEvaluationNodes / 64]uint64
	completed := make([]dependencyValue, unique+1)
	type frame struct {
		node, next int
		value      int32
		known      bool
	}
	stack := make([]frame, 0, maxEvaluationDepth)
	work := 0
	// push returns the result of a refused/cached child, if any. A fresh
	// child goes on the explicit stack and is completed on unwind.
	push := func(n int) (dependencyValue, bool) {
		if n < 0 || n >= len(nodes) {
			return unknown, true
		}
		k := index[n]
		if visiting[k/64]&(uint64(1)<<uint(k%64)) != 0 {
			return unknown, true
		}
		if completed[k].known {
			return completed[k], true
		}
		if len(stack) == maxEvaluationDepth || work == maxEvaluationNodes {
			return unknown, true
		}
		work++
		visiting[k/64] |= uint64(1) << uint(k%64)
		stack = append(stack, frame{node: n, value: nodes[n].value, known: true})
		return dependencyValue{}, false
	}
	if result, done := push(root); done {
		return result
	}
	for len(stack) > 0 {
		f := &stack[len(stack)-1]
		if f.next < len(nodes[f.node].deps) {
			child := nodes[f.node].deps[f.next]
			f.next++
			if result, done := push(child); done {
				if !result.known {
					f.known = false
				} else {
					f.value += result.value
				}
			}
			continue
		}
		result := dependencyValue{value: f.value, known: f.known}
		if !result.known {
			result.value = 0
		}
		k := index[f.node]
		visiting[k/64] &^= uint64(1) << uint(k%64)
		if result.known {
			completed[k] = result
		}
		stack = stack[:len(stack)-1]
		if len(stack) == 0 {
			return result
		}
		parent := &stack[len(stack)-1]
		if !result.known {
			parent.known = false
		} else {
			parent.value += result.value
		}
	}
	return unknown
}
