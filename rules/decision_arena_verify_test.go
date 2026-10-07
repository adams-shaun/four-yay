package rules

// The rules test binary runs the decision arena's poison verification on: a
// retained decision is a test failure, not a silent slot reuse
// (decision_arena_live.go).
func init() { decisionArenaVerify = true }
