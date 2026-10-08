package rules

// The rules test binary checks every referent-placed trigger hot merge
// (trigger_hotmerge.go) against the full zone-list scan.
func init() {
	trigHotMergeVerify = true
}
