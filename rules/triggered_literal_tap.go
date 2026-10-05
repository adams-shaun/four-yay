package rules

import (
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// literalTapOptions offers a complete allocation, not just independently
// payable parts. When later parts compete for the same permanent, expose only
// the current part's reserved objects: any legal answer must leave the rest
// payable. A single part retains the full candidate choice.
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
	// Deterministic bipartite matching: each slot receives a distinct object.
	// Augmenting paths find a full allocation even when a greedy reservation
	// of an early broad filter would starve a later self-only part.
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
