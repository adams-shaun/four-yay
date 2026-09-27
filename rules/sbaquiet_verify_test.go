package rules

// The rules test binary runs the SBA quiet skip (sbaquiet.go) and the
// cast-provenance early-out (cast_provenance.go) in verify mode: every
// would-be skip runs the full pass loop anyway and panics if it emits, and
// every gated spec re-runs each provenance stage's own guard.
func init() {
	sbaQuietVerify = true
	provenanceGateVerify = true
	specDerivedVerify = true
}
