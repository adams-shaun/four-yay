package levelb

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// StateSelfCountersSub is the level-B sub-family of a state trigger (CR
// 603.8, Forge Mode$ Always) on the card's own counter count: "When there are
// four or more page counters on Mazemind Tome, ..." (IsPresent$
// Card.Self+counters_GE4_PAGE). The recipe in compliance/oraclegen/templates
// seeds the source with one counter short and adds the last one with the
// card's own activation, so both engines see the state become true. Exported
// so the recipe and this classifier share one spelling.
const StateSelfCountersSub = "trigger.state-self-counters"

// classifyStateTrigger names the sub-family of a Mode$ Always trigger whose
// only condition is the source's own counter count on the battlefield. ok is
// false for every other state trigger, which stays a mode gap.
func classifyStateTrigger(t *cards.Trigger) (sub string, ok bool) {
	if t.ModeKind() != cards.TriggerAlways {
		return "", false
	}
	if zones := t.ParamStr(cards.PKTriggerZones); zones != "" && !strings.EqualFold(zones, "Battlefield") {
		return "", false
	}
	if _, _, ok := SelfCounterGate(t.ParamStr(cards.PKIsPresent)); !ok {
		return "", false
	}
	return StateSelfCountersSub, true
}

// SelfCounterGate reads an IsPresent$ filter that is the source itself with
// at least n counters of one kind (Card.Self+counters_GE<n>_<KIND>, n a
// literal). ok is false for any other filter: an alternative list, a
// variable count, a controller or type qualifier beyond the card itself.
func SelfCounterGate(spec string) (kind string, n int, ok bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.Contains(spec, ",") {
		return "", 0, false
	}
	self := false
	for _, tok := range strings.FieldsFunc(spec, func(r rune) bool { return r == '.' || r == '+' }) {
		tok = strings.TrimSpace(tok)
		switch {
		case strings.EqualFold(tok, "Card"), strings.EqualFold(tok, "Permanent"):
		case strings.EqualFold(tok, "Self"):
			self = true
		case strings.HasPrefix(tok, "counters_GE"):
			if kind != "" {
				return "", 0, false
			}
			num, k, found := strings.Cut(strings.TrimPrefix(tok, "counters_GE"), "_")
			v, err := strconv.Atoi(num)
			if !found || err != nil || v < 1 || k == "" {
				return "", 0, false
			}
			kind, n = k, v
		default:
			return "", 0, false
		}
	}
	if !self || kind == "" {
		return "", 0, false
	}
	return kind, n, true
}
