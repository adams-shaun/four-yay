package azmcts

import "github.com/adams-shaun/gorge/internal/searchprobe"

// Every redeal probe a hidden-blind board lets Deal skip is still run and
// checked in this test binary (searchprobe/redeal_blind.go).
func init() { searchprobe.SetRedealProbeVerify(true) }
