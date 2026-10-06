package rules

// Every rules test run checks each skipped rename refresh against a full
// recompute (setname_gate.go).
func init() { renameGateVerify = true }
