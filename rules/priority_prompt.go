package rules

import (
	"strconv"
	"sync/atomic"

	"github.com/adams-shaun/gorge/state"
)

// The priority prompt table: a priority decision's Prompt is a pure function
// of (turn, step, the seat's facing name), and every priority ask built it
// afresh -- one string concatenation per ask, most of them in search
// simulations nobody reads the prompt of. The table memoizes the text per
// (turn, step, seat index) process-wide; an entry answers only for the exact
// name it was built with, so a seat renamed (or another table's seat at the
// same index) rebuilds and replaces it. Strings are immutable, so sharing
// one across engines and goroutines is safe, and the text is byte-identical
// to the uncached build.
const (
	promptTurns = 64
	promptSteps = 32
	promptSeats = 8
)

type priorityPromptEntry struct {
	name string
	s    string
}

var priorityPromptTable [promptTurns * promptSteps * promptSeats]atomic.Pointer[priorityPromptEntry]

// priorityPrompt is the priority decision's Prompt for p, byte-identical to
// fmt.Sprintf("turn %d, %s — %s has priority", turn, step, seatFacingName).
func (e *Engine) priorityPrompt(p state.PlayerID) string {
	turn, step := e.G.Turn, e.G.Step
	name := seatFacingName(e.G, p)
	if turn < 0 || turn >= promptTurns || int(step) >= promptSteps || int(p) >= promptSeats {
		return priorityPromptText(turn, step, name)
	}
	slot := &priorityPromptTable[(int(turn)*promptSteps+int(step))*promptSeats+int(p)]
	if en := slot.Load(); en != nil && en.name == name {
		return en.s
	}
	s := priorityPromptText(turn, step, name)
	slot.Store(&priorityPromptEntry{name: name, s: s})
	return s
}

func priorityPromptText(turn int32, step state.Step, name string) string {
	return "turn " + strconv.Itoa(int(turn)) + ", " + step.String() + " — " + name + " has priority"
}
