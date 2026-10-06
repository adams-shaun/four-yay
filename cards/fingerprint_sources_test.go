package cards

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// TestCompilerSourcesListEveryNonTestFile holds fingerprint_sources.go's
// explicit embed list equal to the package's non-test .go files. A file
// missing from the list would leave its source out of CompilerFingerprint, so
// a parser change there could be served a stale IR cache; a _test.go file in
// the list would make test-only edits rebuild every importer of cards.
func TestCompilerSourcesListEveryNonTestFile(t *testing.T) {
	onDisk, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, e := range onDisk {
		n := e.Name()
		if !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			want = append(want, n)
		}
	}
	embedded, err := compilerSources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, e := range embedded {
		have[e.Name()] = true
		if strings.HasSuffix(e.Name(), "_test.go") {
			t.Errorf("fingerprint_sources.go embeds test file %s: remove its //go:embed line", e.Name())
		}
	}
	sort.Strings(want)
	for _, n := range want {
		if !have[n] {
			t.Errorf("cards/%s is not embedded: add `//go:embed %s` to fingerprint_sources.go (sorted)", n, n)
		}
		delete(have, n)
	}
	for n := range have {
		if !strings.HasSuffix(n, "_test.go") {
			t.Errorf("fingerprint_sources.go embeds %s, which is not a source file here: remove its line", n)
		}
	}
}
