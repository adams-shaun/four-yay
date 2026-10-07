package host

import "github.com/adams-shaun/gorge/rules"

// The host test binary runs the decision arena's poison verification on: a
// host reader that retains a decision past the next Submit must fail loudly
// (decision_arena_live.go).
func init() { rules.SetDecisionArenaVerify(true) }
