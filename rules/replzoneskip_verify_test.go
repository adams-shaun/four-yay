package rules

// The rules test binary runs the replacement-source walk skip in verify mode
// (repl_zoneskip.go): every zone summary is recomputed from scratch and a
// skipped object with R: lines panics.
func init() {
	replZoneSkipVerify = true
}
