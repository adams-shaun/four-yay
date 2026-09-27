package effects

// The effects test binary runs the per-spec fronts in verify mode: every hit
// is recomputed and a difference panics.
func init() { VerifySpecCaches = true }
