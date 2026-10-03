package effects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOnlyTheSharedGateReadsUnlessPay guards the class behind the cardfuzz
// Flash livelock: Resolve's unlessProceed gate consumes and CLEARS
// Ctx.UnlessPay before any effect body runs, so an effect body that reads it
// sees "" on every answered re-entry and re-poses its own ask forever. Only
// the gate (unless.go) and effWard (misc.go; the gate exempts API Ward) may
// read the answer.
func TestOnlyTheSharedGateReadsUnlessPay(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"unless.go": true, "misc.go": true}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || allowed[f] {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "c.UnlessPay") {
			t.Errorf("%s reads Ctx.UnlessPay in an effect body; the shared unless gate owns the answer", f)
		}
	}
}
