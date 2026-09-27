package rules

// The rules test binary runs the livelock watcher's candidate filter in
// verify mode (livelock.go): every scan the filter skips is re-run over the
// candidate window and a match panics.
func init() {
	livelockCandVerify = true
}
