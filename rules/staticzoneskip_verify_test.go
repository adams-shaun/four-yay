package rules

// The rules test binary runs the off-battlefield static-source walk skip in
// verify mode (static_zoneskip.go): every zone summary is recomputed from
// scratch, a skipped object whose current face is off-battlefield-live
// panics, and staticEffects re-runs the unskipped walk and compares.
func init() {
	staticZoneSkipVerify = true
}
