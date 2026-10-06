package templates

import "github.com/adams-shaun/gorge/compliance/oraclegen"

// withDrawCheckpoint gives a draw cause its private fire probe. A draw spell
// that triggers several abilities at once (a card with two draw triggers, or
// one that triggers per card drawn) leaves them pending behind a trigger-order
// decision once the spell resolves, so neither the bare cause nor one pass by
// each player shows them on the stack. pass_to the next priority decision
// answers that order ask and stops before the abilities resolve (and is a
// no-op when nothing is asked). Only probeSteps change: the emitted steps are
// the same as before.
func withDrawCheckpoint(c triggerCause) triggerCause {
	c.probeSteps = append(append([]oraclegen.Step(nil), c.steps...),
		oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1},
		oraclegen.Step{Op: "pass_to", Decision: "priority"})
	return c
}
