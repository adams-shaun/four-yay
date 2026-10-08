package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// SetSkipPass turns on the simulation-only auto-pass of quiet priority
// windows (azmcts Options.SkipPass). It must only be set on a hypothetical
// search engine: it makes the engine answer some priority windows itself
// instead of posing them, so the posed-decision stream and the intent log
// differ from a real game's. Off (the zero value) every window is posed.
//
// Level 1 skips windows outside a main phase with an empty stack. Level 2
// also skips the non-active seat's empty-stack main-phase window (it can only
// answer there with instant-speed plays). 0 is off.
func (e *Engine) SetSkipPass(level int) { e.skipPass = uint8(level) }

// skipPassDecision and skipPassIntent are the one-option stand-in a skipped
// window hands handlePriority: its pass case reads only the chosen option's
// Kind, so nothing else of the decision is needed. Read-only.
var (
	skipPassDecision = &decision.Decision{Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: optPass}}}
	skipPassIntent = decision.Intent{Choices: passZero[:]}
	passZero       = [1]int{0}
)

// skippableWindow reports whether the priority window about to be posed is
// auto-passed under SetSkipPass: with an empty stack, outside a main phase (level 2:
// or holder not the active seat), and
// no cast, choice or resolution in flight. The window is passed without
// running the legal-action walk or the payment build.
func (e *Engine) skippableWindow(p state.PlayerID) bool {
	if e.skipPass == 0 || len(e.G.Stack) != 0 {
		return false
	}
	if e.G.Step.IsMain() && (e.skipPass < 2 || p == e.G.Active) {
		return false
	}
	return e.cast == nil && e.choosing == chooseNone && !e.Suspended() && e.pending == nil
}

// skipPriority answers the window with the engine's own pass: the same
// handlePriority pass a posed "pass" would run, minus the DecisionAsk and
// DecisionMade events and the intent-log entry. The engine is left idle
// (pending nil), so Advance's loop grants the next window.
func (e *Engine) skipPriority() {
	e.priorityWalk = priorityWalkTail{}
	e.handlePriority(skipPassDecision, skipPassIntent)
}
