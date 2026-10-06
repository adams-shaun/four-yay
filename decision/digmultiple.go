package decision

// DistinctTypesFit tests whether each selected option can occupy a different
// type slot. A greedy first-match fails for a Creature Land followed by a
// Creature: the first card can instead occupy Land. Enumerate assignments in
// offered token order; the number of slots is bounded by the ability's types.
// This is the sole legality rule for validation, policy and clamp.
// MaxDistinctTypeChoices returns a maximum-cardinality assignment in option
// order. A mandatory DigMultiple requires that many cards (not an impossible
// demand for one of every listed type when the library lacks a type).
func (d *Decision) MaxDistinctTypeChoices() []int {
	var best []int
	var walk func(int, []int)
	walk = func(at int, picks []int) {
		if len(picks) > len(best) {
			best = append([]int(nil), picks...)
		}
		if len(best) >= d.Max || at >= len(d.Options) || len(picks)+len(d.Options)-at <= len(best) {
			return
		}
		candidate := append(append([]int(nil), picks...), at)
		if d.DistinctTypesFit(candidate) {
			walk(at+1, candidate)
		}
		walk(at+1, picks)
	}
	walk(0, nil)
	return best
}

func (d *Decision) DistinctTypesFit(choices []int) bool {
	if !d.DistinctTypePicks {
		return true
	}
	used := make(map[string]bool)
	var assign func(int) bool
	assign = func(at int) bool {
		if at == len(choices) {
			return true
		}
		if choices[at] < 0 || choices[at] >= len(d.Options) {
			return false
		}
		for _, typ := range d.Options[choices[at]].SetProps {
			if used[typ] {
				continue
			}
			used[typ] = true
			if assign(at + 1) {
				return true
			}
			delete(used, typ)
		}
		return false
	}
	return assign(0)
}
