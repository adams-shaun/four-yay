package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/events"
)

// countVoteReveals counts the per-voter "votes for" reveal Notes so far.
func countVoteReveals(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.HasPrefix(ev.Text, "votes for ") {
			n++
		}
	}
	return n
}
