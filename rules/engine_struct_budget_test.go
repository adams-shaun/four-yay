package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The steward metric scripts/reward.py tracked until 2026-10-03
// (`oversized_files`, since replaced by cmd/codeshape's funcs_over_300)
// flagged every .go file at or over 1500 lines.
// Until now nothing in the suite enforced that threshold, so the Engine
// struct's field-contract file could grow past it unnoticed. This test is
// the enforcement: the file holding `type Engine struct` stays under the
// steward's oversized threshold.
func TestEngineStructStaysUnderTheOversizeBudget(t *testing.T) {
	// `go test` runs the binary in the package directory, so "." is this
	// package's source. runtime.Caller is not used: under -trimpath (which
	// lets worktrees share build and test cache entries) it returns a
	// module-relative path that does not exist on disk.
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var holder string
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(src), "type Engine struct {") {
			if holder != "" {
				t.Fatalf("type Engine struct is declared in both %s and %s", holder, name)
			}
			holder = name
		}
	}
	if holder == "" {
		t.Fatal("no non-test file in the package declares `type Engine struct {`")
	}
	src, err := os.ReadFile(filepath.Join(dir, holder))
	if err != nil {
		t.Fatalf("read %s: %v", holder, err)
	}
	lines := strings.Count(string(src), "\n")
	const budget = 1500
	if lines >= budget {
		t.Fatalf("%s holds type Engine struct and is %d lines, at or over the %d-line oversized_files threshold the steward metric tracks", holder, lines, budget)
	}
	t.Logf("%s holds type Engine struct at %d lines (budget %d)", holder, lines, budget)
}
