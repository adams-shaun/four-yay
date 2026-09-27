package effects

// The effects test binary runs the name-choice memo's resolving-context path
// in verify mode: every memoised answer served to a resolving context is
// recomputed with that context and a difference panics.
func init() {
	nameChoicesMemoVerify = true
}
