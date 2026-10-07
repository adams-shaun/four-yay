package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// CounterAddedSub is the level-B sub-family of the "whenever one or more
// counters are put on <recipient>" trigger family (Forge Mode$ CounterAdded,
// CounterAddedOnce, CounterAddedAll, CounterPlayerAddedAll and
// CounterTypeAddedAll). Its cause is a counter-placing probe -- Battlegrowth
// for a +1/+1 counter, or a proliferate spell one counter short of a PLAN
// threshold -- so the recipe in compliance/oraclegen/templates is separate.
// Exported so the recipe and this classifier share one spelling.
const CounterAddedSub = "trigger.counter-added"

// classifyCounterTrigger names the counter-added trigger family, whose cause
// a p0 probe can produce on turn 1. It serves the counter kinds a
// counter-placing probe can supply: the whole P1P1 (or kind-less, any-counter)
// family on any creature recipient the trigger's own filter accepts, and the
// CounterAdded PLAN threshold on the card itself (setup holds n-1 plan
// counters and a proliferate adds the last one). The LOYALTY-by-you shape
// stays with the loyalty-ability recipe in classifyEventTrigger's switch; any
// other counter kind falls back to the mode gap there, so running this first
// moves a requirement only between sub-families.
func classifyCounterTrigger(t *cards.Trigger) (sub string, ok bool) {
	switch t.ModeKind() {
	case cards.TriggerCounterAdded, cards.TriggerCounterAddedOnce,
		cards.TriggerCounterAddedAll, cards.TriggerCounterPlayerAddedAll,
		cards.TriggerCounterTypeAddedAll:
	default:
		return "", false
	}
	// A CounterAddedOnce loyalty trigger is a planeswalker's activation, not a
	// counter-placing probe: the switch's own case names its sub-family.
	if t.ModeKind() == cards.TriggerCounterAddedOnce &&
		strings.EqualFold(t.ParamStr(cards.PKCounterType), "LOYALTY") {
		return "", false
	}
	switch kind := strings.TrimSpace(t.ParamStr(cards.PKCounterType)); {
	case kind == "" || strings.EqualFold(kind, "P1P1"):
		return CounterAddedSub, true
	case strings.EqualFold(kind, "PLAN") && namesSelf(t.ParamStr(cards.PKValidCard)):
		return CounterAddedSub, true
	}
	return "", false
}
