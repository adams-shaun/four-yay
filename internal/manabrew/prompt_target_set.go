package manabrew

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The set-level target constraints -- Decision.TargetsWithSameController,
// Decision.SetPropMode (the TargetsWithSameCardType$/DifferentCMC$ families)
// and Option.Group exclusivity -- are invisible per candidate on the ManaBrew
// wire (spec gap G-4): TargetRef carries no controller and no property
// tokens, so a rules-ignorant client cannot evaluate the constraint itself.
// The remedy the scope spec sanctions for G-4 -- a human-readable constraint
// in presentation.description, Decision.Validate as the fence, rejections
// counted in the census -- is made ANSWERABLE here by ordering:
// orderTargetOptions reorders the candidate list so the first Decision.Min
// candidates always form one legal set, and targetSetSentences renders the
// constraint in the exact wording mbtest's MockClient parses (mock.go's
// setConstraintBound). A client that honours the sentence can therefore
// always answer a constrained ask legally by taking the offered prefix; one
// that ignores it is fenced by Validate and counted in the census. An
// unconstrained ask builds no sentence and keeps its offered order, so every
// existing prompt serialises byte-identically.
//
// The group constraint is the per-Group CAP, not bare exclusivity:
// orderTargetOptions walks Decision.GroupAdmits (Decision.GroupCapFor's cap), so
// at a raised cap (Decision.GroupLimit / Decision.GroupLimits) several options of
// one group are legal together -- and targetSetSentences names that cap in its
// wording, derived from GroupCapFor over the distinct groups present, so the
// description can never claim exclusivity a raised cap permits.
//
// The greedy prefix walks the SAME incremental rules Decision.Validate
// walks -- decision.SetPropAdmits/SetPropMerge for the set-property
// constraints and Decision.GroupAdmits (the per-Group cap) for the group
// marker -- so a prefix this file builds is a set the validator accepts --
// one home for the legal-answer rule, not a parallel re-implementation.

// targetSetSentences renders d's set-level target constraints as the
// presentation description of a target ask. Empty when the decision carries
// no such constraint.
func targetSetSentences(d *decision.Decision) string {
	if d == nil {
		return ""
	}
	var parts []string
	if d.TargetsWithSameController {
		parts = append(parts, "All chosen targets must share one controller.")
	}
	switch d.SetPropMode {
	case decision.SetPropShared:
		parts = append(parts, "All chosen targets must share a property.")
	case decision.SetPropDistinct:
		parts = append(parts, "No two chosen targets may share a property.")
	}
	if optionsHaveGroups(d.Options) {
		parts = append(parts, groupCapSentence(d))
	}
	return strings.Join(parts, " ")
}

// groupCapSentence renders the per-Group selection rule from the effective caps
// Decision.GroupCapFor reports for the distinct non-empty groups present among
// the options (cap values sorted ascending -- deterministic; the group ids
// themselves are opaque tokens meaningless to a player and are never rendered).
// Every present group at the default cap of 1 -- the historical universe -- keeps
// the historical mutual-exclusion sentence byte-identically; one uniform raised
// cap N names the count; mixed cap values name the spread ascending.
func groupCapSentence(d *decision.Decision) string {
	caps := distinctGroupCaps(d)
	if len(caps) == 1 && caps[0] == 1 {
		return "Options that share a group are mutually exclusive."
	}
	if len(caps) == 1 {
		return fmt.Sprintf("At most %d options of each group may be chosen together.", caps[0])
	}
	var b strings.Builder
	b.WriteString("Options of a group may be chosen together up to the group's cap: ")
	for i, n := range caps {
		if i == 0 {
			b.WriteString(fmt.Sprintf("at most %d for some groups", n))
		} else {
			b.WriteString(fmt.Sprintf(", at most %d for others", n))
		}
	}
	b.WriteString(".")
	return b.String()
}

// distinctGroupCaps returns the sorted distinct effective per-Group caps
// Decision.GroupCapFor reports over the non-empty groups present among the
// options. The cap comes from GroupCapFor -- the one home -- never from a
// parallel count.
func distinctGroupCaps(d *decision.Decision) []int {
	seen := map[int]bool{}
	for _, o := range d.Options {
		if o.Group != "" {
			seen[d.GroupCapFor(o.Group)] = true
		}
	}
	caps := make([]int, 0, len(seen))
	for n := range seen {
		caps = append(caps, n)
	}
	sort.Ints(caps)
	return caps
}

// orderTargetOptions reorders d's options so the first d.Min options are one
// legal set under every set-level constraint the decision carries. It
// returns nil when the decision has no set-level constraint, when Min is 0
// (an empty answer is always legal) or when no legal prefix of size Min
// could be built (the engine does not pose such an ask; if it ever did, the
// candidates keep their offered order and Validate stays the fence). The
// reordering is deterministic: groups hoist by size then first-offered
// order, tokens by count then key, and options within a group keep their
// offered order. The mapping back from a chosen candidate to its option is
// by kind+id (parseBoardTargets), never by list position, so the reordering
// cannot corrupt the response parse.
func orderTargetOptions(d *decision.Decision) []decision.Option {
	if d == nil || d.Min <= 0 ||
		(d.SetPropMode == decision.SetPropNone && !d.TargetsWithSameController && !optionsHaveGroups(d.Options)) {
		return nil
	}
	ordered := append([]decision.Option(nil), d.Options...)
	if d.SetPropMode == decision.SetPropShared {
		ordered = hoistBySharedToken(ordered)
	}
	if d.TargetsWithSameController {
		ordered = hoistByController(ordered)
	}
	chosen := greedyPrefix(ordered, d)
	if chosen == nil {
		// The hoists above pick ONE anchor (the largest controller group, the
		// largest shared-token group); when two set-level constraints interact,
		// that anchor can be infeasible while another anchor -- or a subset
		// that no single hoist fronts -- still fills Min. The engine poses an
		// ask only when some legal set of size Min exists
		// (sameControllerTargetBounds / SetPropCapacity), so search for one
		// rather than returning the offered order. When targets must share a
		// controller the legal set lies inside ONE controller group, so search
		// each controller group (largest first, the deterministic order
		// hoistByController uses); otherwise search the whole candidate list.
		// searchLegalPrefix walks the same incremental rule Validate walks, so
		// a set it returns is one Validate accepts -- still one home for the
		// rule. If no legal set reaches Min the ask is not one the engine
		// poses; keep the offered order and let Validate stay the fence.
		if d.TargetsWithSameController {
			chosen = searchControllerPrefix(ordered, d)
		} else {
			chosen = searchLegalPrefix(ordered, d)
		}
	}
	if chosen == nil {
		return nil
	}
	seen := make(map[int]bool, len(chosen))
	for _, o := range chosen {
		seen[o.Index] = true
	}
	out := chosen
	for _, o := range d.Options {
		if !seen[o.Index] {
			out = append(out, o)
		}
	}
	return out
}

// greedyPrefix walks ordered once, taking the first d.Min options that
// satisfy every set-level constraint d carries (one controller, shared /
// distinct property, at most d.GroupCapFor(group) per group). It returns nil
// when it cannot reach d.Min. The rules it walks are the same incremental
// rules Decision.Validate walks (decision.SetPropAdmits/SetPropMerge and
// Decision.GroupAdmits).
func greedyPrefix(ordered []decision.Option, d *decision.Decision) []decision.Option {
	chosen := make([]decision.Option, 0, d.Min)
	var acc []string
	ctlSet := false
	var ctl state.PlayerID
	groups := map[string]int{}
	for _, o := range ordered {
		if len(chosen) == d.Min {
			break
		}
		if d.TargetsWithSameController {
			if !ctlSet {
				ctl, ctlSet = o.Controller, true
			} else if o.Controller != ctl {
				continue
			}
		}
		if d.SetPropMode != decision.SetPropNone && !decision.SetPropAdmits(d.SetPropMode, acc, o.SetProps) {
			continue
		}
		if !d.GroupAdmits(groups, o.Group) {
			continue
		}
		chosen = append(chosen, o)
		if d.SetPropMode != decision.SetPropNone {
			acc = decision.SetPropMerge(d.SetPropMode, acc, o.SetProps)
		}
		if o.Group != "" {
			groups[o.Group]++
		}
	}
	if len(chosen) < d.Min {
		return nil
	}
	return chosen
}

// searchControllerPrefix tries searchLegalPrefix with each distinct
// controller as the fixed base, largest controller group first (ties broken
// by first offered), so a controller whose options are not consumed by group
// exclusivity or a competing property constraint can still fill Min. Any
// set satisfying TargetsWithSameController lies inside ONE controller group,
// so one of these bases contains it. Deterministic: the controller list is
// sorted, never a map range.
func searchControllerPrefix(ordered []decision.Option, d *decision.Decision) []decision.Option {
	counts := map[state.PlayerID]int{}
	first := map[state.PlayerID]int{}
	for i, o := range ordered {
		counts[o.Controller]++
		if _, ok := first[o.Controller]; !ok {
			first[o.Controller] = i
		}
	}
	keys := make([]state.PlayerID, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if counts[keys[a]] != counts[keys[b]] {
			return counts[keys[a]] > counts[keys[b]]
		}
		return first[keys[a]] < first[keys[b]]
	})
	for _, k := range keys {
		base := make([]decision.Option, 0, len(ordered))
		for _, o := range ordered {
			if o.Controller == k {
				base = append(base, o)
			}
		}
		if chosen := searchLegalPrefix(base, d); chosen != nil {
			return chosen
		}
	}
	return nil
}

// searchLegalPrefix finds any d.Min-sized subset of opts satisfying every
// set-level constraint d carries (one controller, shared / distinct
// property, at most d.GroupCapFor(group) per group) for which the incremental
// rules hold -- SetPropAdmits/SetPropMerge and Decision.GroupAdmits, the
// exact rules Decision.Validate applies. greedyPrefix
// is attempted first as the cheap, order-preserving path; the backtracking
// walk is the fallback for the cases a single greedy pass cannot see (a first
// pick that blocks a later legal set). Deterministic: options are tried in
// offered order and the first legal set in that order is returned. A node
// budget bounds the worst case; a set bigger than it finds returns nil and
// leaves Validate as the fence, exactly as an unposed ask would.
func searchLegalPrefix(opts []decision.Option, d *decision.Decision) []decision.Option {
	if chosen := greedyPrefix(opts, d); chosen != nil {
		return chosen
	}
	budget := searchLegalPrefixBudget
	chosen := make([]decision.Option, 0, d.Min)
	var walk func(start int, ctlSet bool, ctl state.PlayerID, acc []string, groups map[string]int) bool
	walk = func(start int, ctlSet bool, ctl state.PlayerID, acc []string, groups map[string]int) bool {
		if len(chosen) == d.Min {
			return true
		}
		if budget <= 0 || len(chosen)+(len(opts)-start) < d.Min {
			return false
		}
		budget--
		for i := start; i < len(opts); i++ {
			o := opts[i]
			if d.TargetsWithSameController && ctlSet && o.Controller != ctl {
				continue
			}
			if d.SetPropMode != decision.SetPropNone && !decision.SetPropAdmits(d.SetPropMode, acc, o.SetProps) {
				continue
			}
			if !d.GroupAdmits(groups, o.Group) {
				continue
			}
			nctl, nctlSet := ctl, ctlSet
			if d.TargetsWithSameController && !ctlSet {
				nctl, nctlSet = o.Controller, true
			}
			nacc := acc
			if d.SetPropMode != decision.SetPropNone {
				nacc = decision.SetPropMerge(d.SetPropMode, acc, o.SetProps)
			}
			ngroups := groups
			if o.Group != "" {
				ngroups = make(map[string]int, len(groups)+1)
				for k, v := range groups {
					ngroups[k] = v
				}
				ngroups[o.Group]++
			}
			chosen = append(chosen, o)
			if walk(i+1, nctlSet, nctl, nacc, ngroups) {
				return true
			}
			chosen = chosen[:len(chosen)-1]
		}
		return false
	}
	if walk(0, false, 0, nil, map[string]int{}) {
		return chosen
	}
	return nil
}

// searchLegalPrefixBudget caps the backtracking walk so a pathological ask
// (a large Min over many options) cannot stall the mock. The greedy first
// pass resolves every ordinary ask, so the budget is only reached by an ask
// the engine does not pose.
const searchLegalPrefixBudget = 200000

// hoistByController moves the options of the LARGEST controller group (ties:
// the group whose first option is offered earliest) to the front, keeping
// each group's internal offered order. It is the hoist the
// TargetsWithSameController greedy walk wants: its capacity is the largest
// controller group (rules/stack.go sameControllerTargetBounds), so the
// largest group always fills Min.
func hoistByController(opts []decision.Option) []decision.Option {
	counts := map[state.PlayerID]int{}
	first := map[state.PlayerID]int{}
	for i, o := range opts {
		counts[o.Controller]++
		if _, ok := first[o.Controller]; !ok {
			first[o.Controller] = i
		}
	}
	keys := make([]state.PlayerID, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if counts[keys[a]] != counts[keys[b]] {
			return counts[keys[a]] > counts[keys[b]]
		}
		return first[keys[a]] < first[keys[b]]
	})
	out := make([]decision.Option, 0, len(opts))
	for _, k := range keys {
		for _, o := range opts {
			if o.Controller == k {
				out = append(out, o)
			}
		}
	}
	return out
}

// hoistBySharedToken moves the options of the largest shared-token group
// (ties: the alphabetically first token) to the front, keeping each
// option's offered order inside its pass. It is the hoist the SetPropShared
// greedy walk wants: once every hoisted option shares the one token, the
// running intersection never empties, so the walk fills Min out of the
// group whenever the group is large enough (decision.SetPropCapacity's
// largest-token count, which the engine checks before posing).
func hoistBySharedToken(opts []decision.Option) []decision.Option {
	counts := map[string]int{}
	for _, o := range opts {
		seen := map[string]bool{}
		for _, t := range o.SetProps {
			if !seen[t] {
				seen[t] = true
				counts[t]++
			}
		}
	}
	tokens := make([]string, 0, len(counts))
	for t := range counts {
		tokens = append(tokens, t)
	}
	sort.Strings(tokens)
	best, bestN := "", 0
	for _, t := range tokens {
		if counts[t] > bestN {
			best, bestN = t, counts[t]
		}
	}
	if best == "" {
		return opts
	}
	out := make([]decision.Option, 0, len(opts))
	for _, o := range opts {
		if containsToken(o.SetProps, best) {
			out = append(out, o)
		}
	}
	for _, o := range opts {
		if !containsToken(o.SetProps, best) {
			out = append(out, o)
		}
	}
	return out
}

func containsToken(set []string, t string) bool {
	for _, s := range set {
		if s == t {
			return true
		}
	}
	return false
}

// optionsHaveGroups reports whether any option carries a Group marker --
// the wire-visible half of the generic group contract, whose set-level (at
// most d.GroupCapFor(group) per group) reading the greedy walk honours.
func optionsHaveGroups(opts []decision.Option) bool {
	for _, o := range opts {
		if o.Group != "" {
			return true
		}
	}
	return false
}
