package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The asks an XMage waterbend-cost activation poses after the tap-helpers
// ask. The routing (oraclegen.answerRouting's waterbendHelperDeclined) drops
// the declined pair the generic mapping scripts, because XMage pays the
// waterbend generic from the prefilled mana pool and never poses the tap ask
// (WaterbendCost/WaterbendXCost pay through playMana; the pool covers the
// amount, so no helper dialog opens). What remains is whatever the ability
// itself asks at activation or resolution, which this file re-scripts:
//
//   - an optional target slot the fixture omitted is an "up to N" ask
//     XMage poses at activation and closes with a queued skip (Aang, Swift
//     Savior's "Return up to one other target creature or spell");
//   - a Waterbend<X> part announces X through XMage's getAmount, which reads
//     an "X=<n>" choice (Katara, Water Tribe's Hope, XMin 1);
//   - a "you may reveal" dig asks its looked-at pick as an optional target
//     (Water Tribe Rallier).
//
// The gorge-side scenario is untouched: these are XMage-answer edits only,
// so the raw replay that feeds the comparator does not move.
func addWaterbendFollowups(answers [][]oraclegen.XAnswer, step int, cost, abilityText string,
	targets []string, omitted []int, slots []oraclegen.Slot) {
	if step < 0 || step >= len(answers) || !costHasWaterbend(cost) {
		return
	}
	// An optional slot the fixture omitted, or one gorge's replay declined
	// (the step carries fewer targets than the ability has slots): XMage
	// still poses that "up to N" ask and closes it with a queued skip.
	optionalUnfilled := len(omitted) > 0 || len(targets) < len(slots)
	if optionalUnfilled {
		// XMage asks targets before it pays, so the skip leads the step.
		answers[step] = prependAnswer(answers[step], oraclegen.XAnswer{Seat: 0, Kind: "target", Value: "[target_skip]"})
	}
	if n, ok := waterbendX(cost); ok && !hasChoicePrefix(answers[step], "X=") {
		answers[step] = insertAfterSkips(answers[step], oraclegen.XAnswer{Seat: 0, Kind: "choice", Value: "X=" + strconv.Itoa(n)})
	}
	if strings.Contains(strings.ToLower(abilityText), "you may reveal") && !hasTargetAnswer(answers[step]) {
		answers[step] = append(answers[step], oraclegen.XAnswer{Seat: 0, Kind: "target", Value: "[target_skip]"})
	}
}

// costHasWaterbend reports a cost whose tokens carry a Waterbend part; that
// part is the payment ask the routing dropped.
func costHasWaterbend(cost string) bool {
	for _, tok := range costTokens(cost) {
		if tokenHead(tok) == "Waterbend" {
			return true
		}
	}
	return false
}

// waterbendX reads the announced X of a Waterbend<X> cost.
func waterbendX(cost string) (int, bool) {
	for _, tok := range costTokens(cost) {
		if tokenHead(tok) != "Waterbend" {
			continue
		}
		if strings.Contains(tok, "<X>") {
			if n := activationX(cost); n > 0 {
				return n, true
			}
			return 0, false
		}
	}
	return 0, false
}

// tokenHead is a cost token's word before its bracket payload (activate.go's
// costHead folds the payload away, which the switch tables want; these
// checks need the payload to tell Waterbend<X> from Waterbend<N>).
func tokenHead(tok string) string {
	if i := strings.IndexByte(tok, '<'); i >= 0 {
		return tok[:i]
	}
	return tok
}

func prependAnswer(as []oraclegen.XAnswer, a oraclegen.XAnswer) []oraclegen.XAnswer {
	return append([]oraclegen.XAnswer{a}, as...)
}

// hasChoicePrefix reports a choice answer whose value starts with prefix.
func hasChoicePrefix(as []oraclegen.XAnswer, prefix string) bool {
	for _, a := range as {
		if a.Kind == "choice" && strings.HasPrefix(a.Value, prefix) {
			return true
		}
	}
	return false
}

func hasTargetAnswer(as []oraclegen.XAnswer) bool {
	for _, a := range as {
		if a.Kind == "target" {
			return true
		}
	}
	return false
}

// insertAfterSkips places a after the leading "[target_skip]" answers (the
// activation's target asks are answered first) and before every choice.
func insertAfterSkips(as []oraclegen.XAnswer, a oraclegen.XAnswer) []oraclegen.XAnswer {
	at := 0
	for at < len(as) && as[at].Kind == "target" && as[at].Value == "[target_skip]" {
		at++
	}
	out := make([]oraclegen.XAnswer, 0, len(as)+1)
	out = append(out, as[:at]...)
	out = append(out, a)
	return append(out, as[at:]...)
}
