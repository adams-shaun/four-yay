package manabrew

import (
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
// The greedy prefix walks the SAME incremental rule Decision.Validate walks
// (decision.SetPropAdmits/SetPropMerge), so a prefix this file builds is a
// set the validator accepts -- one home for the legal-answer rule, not a
// parallel re-implementation.

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
		parts = append(parts, "Options that share a group are mutually exclusive.")
	}
	return strings.Join(parts, " ")
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
	chosen := make([]decision.Option, 0, d.Min)
	var acc []string
	ctlSet := false
	var ctl state.PlayerID
	groups := map[string]bool{}
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
		if o.Group != "" && groups[o.Group] {
			continue
		}
		chosen = append(chosen, o)
		if d.SetPropMode != decision.SetPropNone {
			acc = decision.SetPropMerge(d.SetPropMode, acc, o.SetProps)
		}
		if o.Group != "" {
			groups[o.Group] = true
		}
	}
	if len(chosen) < d.Min {
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

// optionsHaveGroups reports whether any option carries a Group exclusivity
// marker -- the wire-visible half of the generic group contract, whose
// set-level (at most one per group) reading the greedy walk honours.
func optionsHaveGroups(opts []decision.Option) bool {
	for _, o := range opts {
		if o.Group != "" {
			return true
		}
	}
	return false
}
