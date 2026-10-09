// Mid-resolution per-player target asks recorded as engine chooses.
//
// The engine poses an effects-tier target ask as a KChoose carrying one
// option per candidate (effects/targets_ask.go's poseTargetsAsk). A
// TargetsForEachPlayer$ ask (Forge's OneEach family: Kaya, Spirits'
// Justice's "exile up to one target creature that player controls") is one
// such choose whose options each carry the engine's target-controller group,
// and XMage poses it as one real target per opponent. A declined ask must
// still close every opponent's slot on XMage's TARGET queue; the generic
// choose_n routing would emit a [choice_skip] on the choice queue instead and
// leave the XMage ask to the AI, whose answer is not gorge's.
package oraclegen

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/rules"
)

// targetGroupPrefix is the decision.Option.Group the engine's target asks
// attach to bind one option to its controller's selection slot. Only target
// asks set a group (rules' askTarget/cast.go targetAsk and effects'
// poseTargetsAsk), so a recorded group identifies a target ask the engine
// recorded under another kind.
const targetGroupPrefix = "target-controller-"

// perPlayerTargetAsk reports whether d is a per-controller target ask the
// engine recorded as a KChoose: every offered option carries a
// target-controller group. A generic effect choose never carries one.
func perPlayerTargetAsk(d rules.OracleDecision) bool {
	if d.Kind != "choose_n" || len(d.OptionGroups) == 0 {
		return false
	}
	for _, g := range d.OptionGroups {
		if !strings.HasPrefix(g, targetGroupPrefix) {
			return false
		}
	}
	return true
}

// perPlayerTargetSeats lists the distinct controller seats among d's offered
// options in ascending seat order -- the order XMage poses the per-player
// asks in.
func perPlayerTargetSeats(d rules.OracleDecision) []int {
	seen := map[int]bool{}
	for _, g := range d.OptionGroups {
		n, err := strconv.Atoi(strings.TrimPrefix(g, targetGroupPrefix))
		if err != nil || seen[n] {
			continue
		}
		seen[n] = true
	}
	seats := make([]int, 0, len(seen))
	for s := range seen {
		seats = append(seats, s)
	}
	sort.Ints(seats)
	return seats
}

// perPlayerTargetAnswers answers a declined per-player target ask: one target
// answer per offered controller seat, the seat's recorded pick when it has
// one and a target skip when gorge chose none. Picks are attributed by the
// seat their ref names, so a multi-opponent ask is answered in XMage's seat
// order.
func perControllerTargetAnswers(d rules.OracleDecision) []XAnswer {
	pick := map[int]string{}
	for _, ref := range d.PickRefs {
		s, ok := refSeat(ref)
		if !ok {
			continue
		}
		if _, dup := pick[s]; !dup {
			pick[s] = ref
		}
	}
	seats := perPlayerTargetSeats(d)
	as := make([]XAnswer, 0, len(seats))
	for _, s := range seats {
		if ref, ok := pick[s]; ok {
			as = append(as, XAnswer{d.Seat, "target", ref})
			continue
		}
		as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
	}
	return as
}
