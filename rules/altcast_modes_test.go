package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// altCastModeLiteralResidue is every string literal outside
// rules/altcast_modes.go that spells an alternative-cost keyword row's mode
// word, measured when the descriptor landed. Both are the madness ResumeKind /
// resumePoint kind -- a different vocabulary (the resume dispatch) that
// happens to share the word -- not a cast-mode read. SHRINK-ONLY: a new
// keyword of the family is one altCastModes row, never a new literal.
var altCastModeLiteralResidue = []string{
	"rules/altcast.go: madness",
	"rules/altcast.go: madness",
}

// TestAltCastModeLiteralsLiveInTheDescriptor freezes the string literals
// naming an alt-cast mode outside the descriptor file, so the family cannot
// grow back a per-site spelling (the Impending ticket added its word to four
// tables and cloned bestow's offer block twice before the descriptor).
func TestAltCastModeLiteralsLiveInTheDescriptor(t *testing.T) {
	words := map[string]bool{}
	for i := range altCastModes {
		words[altCastModes[i].mode] = true
	}
	var got []string
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && strings.HasPrefix(d.Name(), "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || path == "altcast_modes.go" {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil && words[s] {
					got = append(got, "rules/"+filepath.ToSlash(path)+": "+s)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := append([]string(nil), altCastModeLiteralResidue...)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("alt-cast mode literals outside rules/altcast_modes.go:\n%s\nwant (shrink-only residue):\n%s\nadd the keyword as an altCastModes row and read it through altMode/altCastFor",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestAltCastModesTablesAgree pins the derived tables: every row is a
// castModeCodes alternative-cost entry, modeFlags records each row's flag,
// and the descriptor's rows are distinct.
func TestAltCastModesTablesAgree(t *testing.T) {
	seen := map[string]bool{}
	for i := range altCastModes {
		m := &altCastModes[i]
		if m.mode == "" || m.head == "" || seen[m.mode] {
			t.Fatalf("row %d: empty or duplicate mode %q / head %q", i, m.mode, m.head)
		}
		seen[m.mode] = true
		if castModeCodes.Code(m.mode) != castModeAltCostKeyword {
			t.Errorf("%s: castModeCodes does not route it to the alternative-cost arm", m.mode)
		}
		if altCastFor(m.mode) != m {
			t.Errorf("%s: altCastFor does not find its row", m.mode)
		}
		if got := modeFlags(m.mode); (m.flag == 0) != (got == "") {
			t.Errorf("%s: modeFlags = %q with row flag %d", m.mode, got, m.flag)
		}
	}
}
