package rules

// The offer walk's skip verdicts (legal_walk_skip.go, walk_face_facts.go)
// run the skipped work anyway in the rules test binary and panic on any
// option it would have produced.
func init() { walkSkipVerify = true }
