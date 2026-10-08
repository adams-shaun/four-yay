package view

import "github.com/adams-shaun/gorge/rules"

// The view test binary runs the decision arena's poison verification on: a
// projection that retains a decision past the next Submit must fail loudly
// (decision_arena_live.go).
func init() { rules.SetDecisionArenaVerify(true) }
