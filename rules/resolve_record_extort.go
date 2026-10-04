package rules

// resolve_record_extort.go holds Extort's answer record (W3 step 2;
// rules/resolve_record.go's tapeAnswerRecord): the event-emitting half of
// an answered "extort" ask, written by the resolution kernel's Record.

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/pay"
)

// extortAnswerRecord charges Extort's {W/B} pip on a "pay" answer (option 0)
// and reports whether it was paid: a pool lacking both colours declines
// (the drain never runs without the mana genuinely paid). effExtort reads
// the charge off the pool.
func extortAnswerRecord(e *Engine, chosen []decision.Option) bool {
	return len(chosen) > 0 && chosen[0].Index == 0 && pay.PayExtortPip(asPayer(e), chosen[0].Player)
}
