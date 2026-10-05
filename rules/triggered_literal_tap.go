package rules

import (
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// literalTapOptions offers choices for the current part that can still be
// completed by the later parts. For a one-object election, retain every legal
// first choice with a feasible completion; a single matching would hide other
// valid choices from the player.
func literalTapOptions(p pay.Engine, player state.PlayerID, source state.ObjID, parts []CostPart, start int) []state.ObjID {
	if start >= len(parts) {
		return nil
	}
	candidates := make([][]state.ObjID, len(parts)-start)
	var slots []int
	for i, part := range parts[start:] {
		candidates[i] = pay.TapCostCandidates(p, player, source, part)
		if part.N <= 0 || int64(part.N) > int64(len(candidates[i])) {
			return nil
		}
		for n := int32(0); n < part.N; n++ {
			slots = append(slots, i)
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	if parts[start].N == 1 {
		var offered []state.ObjID
		for _, id := range candidates[0] {
			if literalTapMatching(candidates, slots[1:], map[state.ObjID]bool{id: true}) {
				offered = append(offered, id)
			}
		}
		return offered
	}
	// Multi-object decisions accept an arbitrary subset of their options. A
	// deterministic full matching is therefore the safe offer when a part
	// needs multiple objects: every offered subset is exactly that matching.
	owner := make(map[state.ObjID]int, len(slots))
	var augment func(int, map[state.ObjID]bool) bool
	augment = func(slot int, seen map[state.ObjID]bool) bool {
		for _, id := range candidates[slots[slot]] {
			if seen[id] {
				continue
			}
			seen[id] = true
			prev, used := owner[id]
			if !used || augment(prev, seen) {
				owner[id] = slot
				return true
			}
		}
		return false
	}
	for slot := range slots {
		if !augment(slot, make(map[state.ObjID]bool)) {
			return nil
		}
	}
	var offered []state.ObjID
	for _, id := range candidates[0] {
		if slot, ok := owner[id]; ok && slots[slot] == 0 {
			offered = append(offered, id)
		}
	}
	return offered
}

// literalTapMatching reports whether the requested slots can be assigned
// distinct candidates, excluding objects already selected by the current ask.
func literalTapMatching(candidates [][]state.ObjID, slots []int, excluded map[state.ObjID]bool) bool {
	owner := make(map[state.ObjID]int, len(slots))
	var augment func(int, map[state.ObjID]bool) bool
	augment = func(slot int, seen map[state.ObjID]bool) bool {
		for _, id := range candidates[slots[slot]] {
			if excluded[id] || seen[id] {
				continue
			}
			seen[id] = true
			prev, used := owner[id]
			if !used || augment(prev, seen) {
				owner[id] = slot
				return true
			}
		}
		return false
	}
	for slot := range slots {
		if !augment(slot, make(map[state.ObjID]bool)) {
			return false
		}
	}
	return true
}
