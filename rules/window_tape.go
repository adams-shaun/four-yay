package rules

// window_tape.go puts the engine-posed resolution-time payment windows on
// the W3 resolution kernel (lasagna spec §7.7, the suspension holders): the
// triggered-effect Cost$ window (e.triggerCost), its mandatory component
// walk, cumulative upkeep (e.cumulative) and echo (e.echo). Every one of
// them asks through windowAsk. Inside a tape run the decision is posed and
// answered from the tape, and the flow's own answer handler -- exactly the
// one the legacy Submit dispatches to (turn.go's chooseFor arms) -- runs in
// line; its re-asks come back here, and its completion (the resumed body,
// or finishResumption and the CR 117.3b grant) runs in line too. The
// holder stays set while the window is open, exactly as on the legacy path,
// so every gate that reads it (Suspended, the off-stack mana frame's base
// bits) sees the same state; only the kernel's Busy test looks past it while
// the window itself is asking (tapeWindowAsking).

import "github.com/adams-shaun/gorge/decision"

// tapeWindowFlow reports whether flow's window is served from the tape.
func tapeWindowFlow(flow chooseFor) bool {
	switch flow {
	case chooseTriggeredCost, chooseTriggeredMandatory, chooseCumulative:
		return true
	}
	// chooseEcho stays legacy: e.echo is not a Suspended() holder, so the
	// legacy resolution logs its CR 117.3b grant while the election is still
	// outstanding (DecisionAsk, Priority, DecisionMade) -- an order an in-line
	// answer cannot reproduce byte for byte.
	return false
}

// windowAsk poses the window decision d for flow: from the tape when a tape
// run (or an inline answerer) serves it, else on the legacy path.
func (e *Engine) windowAsk(d *decision.Decision, flow chooseFor) {
	e.choosing = flow
	if tapeWindowFlow(flow) {
		e.tapeWindowAsking = true
		in, ok := e.TapeAnswer(d)
		e.tapeWindowAsking = false
		if ok {
			e.windowAnswer(flow, d.Chosen(in))
			if !e.Suspended() {
				// The handler's continuation completed the resolution and
				// logged its priority grant (finishResumption's tail or the
				// resumed body's): handlePriority must not log a second.
				e.tapeGranted = true
			}
			return
		}
	}
	e.ask(d)
}

// windowAnswer is the legacy Submit's chooseFor dispatch for the window
// flows (turn.go).
func (e *Engine) windowAnswer(flow chooseFor, chosen []decision.Option) {
	switch flow {
	case chooseTriggeredCost:
		e.triggeredCostAnswer(chosen)
	case chooseTriggeredMandatory:
		e.triggeredMandatoryAnswer(chosen)
	case chooseCumulative:
		e.cumulativeAnswer(chosen)
	case chooseEcho:
		e.echoAnswer(chosen)
	default:
		panic("rules: windowAnswer for a non-window flow")
	}
}
