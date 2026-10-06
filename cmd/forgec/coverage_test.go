package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// readRepoFile reads a path relative to the repository root. Tests run in the
// package directory, which is two levels down from it.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func sampleCoverage() coverageData {
	return coverageData{
		Corpus:    "95f04e8a04c8925fa97cb226fc3341cabcc90a53",
		Cards:     100,
		Supported: 87,
		Tokens:    9,
		ByType: byCountThenKey([]cards.CoverageGroup{
			{Key: "Instant", Cards: 40, Supported: 39},
			{Key: "Creature", Cards: 60, Supported: 48},
		}),
		ByColour: byCountThenKey([]cards.CoverageGroup{{Key: "Blue", Cards: 100, Supported: 87}}),
		ByMana:   []cards.CoverageGroup{{Key: "2", Cards: 100, Supported: 87}},
		Missing:  []cards.MissingPrimitive{{Name: "kw:Crew", Cards: 12}},
	}
}

// TestRenderIsDeterministic pins the property the refresh job depends on: the
// same data renders byte-identically every time, with no clock or map order
// anywhere in the output.
func TestRenderIsDeterministic(t *testing.T) {
	d := sampleCoverage()
	doc, sum := renderCoverageDoc(d), renderSummary(d)
	for i := 0; i < 5; i++ {
		if got := renderCoverageDoc(d); got != doc {
			t.Fatal("renderCoverageDoc is not deterministic")
		}
		if got := renderSummary(d); got != sum {
			t.Fatal("renderSummary is not deterministic")
		}
	}
	for _, want := range []string{
		"- Cards in the corpus: **100**",
		"- Fully playable: **87 (87.0%)**",
		"- Corpus pin: `adams-shaun/forge@95f04e8a04c8925fa97cb226fc3341cabcc90a53`",
		"| Creature | 60 | 48 | 80.0% |",
		"| `kw:Crew` | 12 |",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("doc is missing %q", want)
		}
	}
	// Biggest bucket first on the unordered axes.
	if i, j := strings.Index(doc, "| Creature |"), strings.Index(doc, "| Instant |"); i > j {
		t.Error("by-type rows are not count-ordered")
	}
}

// TestByCountThenKeyDoesNotAliasInput guards the renderer against reordering
// the caller's slice in place, which would make the by-mana table (which must
// stay key-ordered) depend on whether a by-count table was rendered first.
func TestByCountThenKeyDoesNotAliasInput(t *testing.T) {
	in := []cards.CoverageGroup{{Key: "a", Cards: 1}, {Key: "b", Cards: 9}}
	out := byCountThenKey(in)
	if in[0].Key != "a" || in[1].Key != "b" {
		t.Errorf("input was reordered: %+v", in)
	}
	if out[0].Key != "b" {
		t.Errorf("output is not count-ordered: %+v", out)
	}
}

// TestSummaryLinksTheDocBesideIt pins the relative link: summary.md and
// coverage.md are written into the same directory, so the summary must not
// point at the old committed docs/coverage.md path.
func TestSummaryLinksTheDocBesideIt(t *testing.T) {
	sum := renderSummary(sampleCoverage())
	if !strings.Contains(sum, "[coverage.md](coverage.md)") {
		t.Errorf("summary does not link coverage.md by a relative path:\n%s", sum)
	}
	if strings.Contains(sum, "docs/coverage.md") {
		t.Errorf("summary still links the old committed path:\n%s", sum)
	}
}

// TestCoverageTablesAreNotCommitted holds the W2 decision: the coverage
// tables are generated into the gitignored .coverage/ and published by CI,
// never committed. A README coverage block or a docs/coverage.md in the tree
// is the old refresh-by-commit scheme coming back.
func TestCoverageTablesAreNotCommitted(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	for _, marker := range []string{"<!-- BEGIN COVERAGE -->", "<!-- END COVERAGE -->"} {
		if strings.Contains(readme, marker) {
			t.Errorf("README.md carries %s: the coverage summary is generated into .coverage/, not committed", marker)
		}
	}
	if _, err := os.Stat(filepath.Join("..", "..", "docs", "coverage.md")); err == nil {
		t.Error("docs/coverage.md exists: the full breakdown is generated into .coverage/coverage.md, not committed")
	}
	if !strings.Contains(readRepoFile(t, ".gitignore"), "\n.coverage/\n") {
		t.Error(".gitignore does not ignore .coverage/, where `make coverage` writes")
	}
}
