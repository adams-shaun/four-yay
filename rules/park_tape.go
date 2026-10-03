package rules

// park_tape.go migrates the park-and-continue asks onto the W3 resolution
// kernel (lasagna spec §7.2). On the legacy path such an ask parks the event
// it interrupts ("never emitted, never applied") and the chain that emitted
// it keeps running; the answer applies the parked event after everything the
// chain did meanwhile. Under the kernel the answer is in hand at the point of
// the event, so the answer's handler -- exactly the one a Submit dispatches
// to (Engine.handle) -- runs in place and the parked event applies where it
// would have happened, which is what CR 616.1 and CR 614.12 describe. This is
// a deliberate behaviour change, not a refactor: the kernel's event order
// differs from the legacy park's wherever the chain did something after the
// park.

import "github.com/adams-shaun/gorge/decision"

// parkAsk poses the park-and-continue decision d (its flow marker already in
// e.choosing). Inside a tape run, or with a synchronous answerer, it is
// answered from the tape and handled in place, and parkAsk reports true;
// otherwise it is asked on the legacy path (the chain runs on past the park)
// and parkAsk reports false. A tape run whose tape is exhausted unwinds here
// with d posed.
func parkAsk(e *Engine, d *decision.Decision) bool {
	if in, ok := parkTapeAnswer(e, d); ok {
		e.handle(d, in)
		return true
	}
	e.ask(d)
	return false
}

// parkTapeAnswer is TapeAnswer for a park-and-continue ask: it skips the
// ReplaceWith$-body refusal (the in-place answer is the point of the
// migration), and the asks are served only from a tape run's resolution or a
// synchronous answerer.
func parkTapeAnswer(e *Engine, d *decision.Decision) (decision.Intent, bool) {
	if tapeForceLegacy != nil && tapeForceLegacy(d) {
		return decision.Intent{}, false
	}
	return e.tape.Answer(asResolve(e), d)
}
