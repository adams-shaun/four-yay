package cards

import (
	"os"
	"sync"
	"testing"
)

// corpusDir is the fetched corpus. Tests that need it skip when it is absent
// so a clean checkout still passes `go test ./...` without a network fetch.
func corpusDir(t *testing.T) string {
	t.Helper()
	dir := "../.cards/cardsfolder"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("corpus not fetched; run `make fetch-cards`")
	}
	return dir
}

// compiledCorpus is the corpus registry every test that needs the *whole*
// corpus consumes, compiled once per package run. Compiling the 33k scripts
// is the single largest allocation in the cards package; before this helper
// existed each test that touched the corpus (the M0 compile gate, the
// primitive-surface gate, and now the colour-identity corpus tests) compiled
// a fresh registry, and each full compile added a large, identical amount to
// the package's total allocation. Sharing one build keeps the allocation
// budget real while the coverage is identical.
var compiled struct {
	once  sync.Once
	reg   *Registry
	diags []Diag
	err   error
}

func compiledCorpus(t *testing.T) *Registry {
	t.Helper()
	dir := corpusDir(t) // Skips when there is no corpus
	compiled.once.Do(func() {
		compiled.reg, compiled.diags, compiled.err = CompileDir(dir)
	})
	if compiled.err != nil {
		t.Fatalf("CompileDir: %v", compiled.err)
	}
	return compiled.reg
}

// TestWholeCorpusCompiles is the M0 acceptance gate. The diagnostic budget is
// deliberately tight: a jump means either a parser regression or an upstream
// data change, and both are worth a human look.
func TestWholeCorpusCompiles(t *testing.T) {
	r := compiledCorpus(t)
	diags := compiled.diags
	if r.Len() < 30000 {
		t.Fatalf("compiled %d cards, expected >30000", r.Len())
	}
	const budget = 20
	if len(diags) > budget {
		for i, d := range diags {
			if i == 25 {
				t.Logf("... and %d more", len(diags)-25)
				break
			}
			t.Logf("%s: %s", d.Path, d.Msg)
		}
		t.Fatalf("%d diagnostics, budget is %d", len(diags), budget)
	}
	t.Logf("compiled %d cards with %d diagnostics", r.Len(), len(diags))
}

func TestCorpusPrimitiveSurface(t *testing.T) {
	r := compiledCorpus(t)
	all := map[string]bool{}
	for _, c := range r.AllCards() {
		for _, p := range c.Primitives() {
			all[p] = true
		}
	}
	// The spec measured 694 primitives plus api:Mana from intrinsics. Upstream
	// adds a few a year; the bound catches an accidental explosion, not growth.
	if len(all) < 600 || len(all) > 900 {
		t.Fatalf("primitive surface = %d, expected 600..900", len(all))
	}
	t.Logf("primitive surface: %d", len(all))
}
