package pay

import "github.com/adams-shaun/gorge/state"

// SacrificeRemainderFeasible reports whether the outstanding sacrifice
// units have a distinct assignment. Candidate and need slices are in cost
// order; the search order and result are deterministic.
func SacrificeRemainderFeasible(candidates [][]state.ObjID, needs []int) bool {
	used := make(map[state.ObjID]bool)
	var assign func(int, int) bool
	assign = func(part, unit int) bool {
		for part < len(needs) && unit >= needs[part] {
			part++
			unit = 0
		}
		if part == len(needs) {
			return true
		}
		for _, id := range candidates[part] {
			if used[id] {
				continue
			}
			used[id] = true
			if assign(part, unit+1) {
				return true
			}
			delete(used, id)
		}
		return false
	}
	return assign(0, 0)
}

// FeasibleSacrificeChoices keeps only current choices that leave a complete
// distinct assignment for the current part's remaining units and every later
// part. Pools and needs are in cost order; chosen objects were paid earlier.
func FeasibleSacrificeChoices(current []state.ObjID, pools [][]state.ObjID, needs []int, chosen []state.ObjID, part int) []state.ObjID {
	feasible := make([]state.ObjID, 0, len(current))
	for _, candidate := range current {
		var future [][]state.ObjID
		var outstanding []int
		for i := part; i < len(needs); i++ {
			need := needs[i]
			if i == part {
				need--
			}
			if need <= 0 {
				continue
			}
			pool := make([]state.ObjID, 0, len(pools[i]))
			for _, id := range pools[i] {
				reserved := id == candidate
				for _, prior := range chosen {
					if id == prior {
						reserved = true
						break
					}
				}
				if !reserved {
					pool = append(pool, id)
				}
			}
			future, outstanding = append(future, pool), append(outstanding, need)
		}
		if SacrificeRemainderFeasible(future, outstanding) {
			feasible = append(feasible, candidate)
		}
	}
	return feasible
}
