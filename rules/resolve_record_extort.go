package rules

// resolve_record_extort.go holds Extort's answer record (W3 step 2;
// rules/resolve_record.go's tapeAnswerRecord): the event-emitting half of
// the "extort" resume arm, shared by that arm and the resolution kernel's
// Record so both paths charge the pip from one home.

import "github.com/adams-shaun/gorge/decision"

// extortAnswerRecord charges Extort's {W/B} pip on a "pay" answer (option 0)
// and reports whether it was paid: a pool lacking both colours declines
// (the drain never runs without the mana genuinely paid). The "extort"
// resume arm binds the result into Ctx.Extort; on the kernel's path effExtort
// reads the charge off the pool.
func extortAnswerRecord(e *Engine, chosen []decision.Option) bool {
	return len(chosen) > 0 && chosen[0].Index == 0 && e.payExtortPip(chosen[0].Player)
}
