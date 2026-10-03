package archtest

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// sizeOnlyFileName matches a basename that names a file by its SIZE rather
// than its concern: the second half of something (`_rest`, `_more`,
// `_extra2`, `_part3`, `_cont`, `_2`), or a grab-bag (`_helpers`, `_misc`,
// `_util`). The old steward metric rewarded splitting a long file at a line
// count, which scattered one concern over several files without adding a
// boundary (rules/resolution_answer_rest.go is the second half of one switch
// moved verbatim; rules/stack_helpers.go hosts 39 effects.Host methods).
var sizeOnlyFileName = regexp.MustCompile(
	`(^|_)(rest|more|extra|part|cont|continued|tail|remainder|overflow|helpers?|misc|utils?)\d*\.go$|_\d+\.go$`)

// sizeOnlyFileNamesAllowed are the size-only names that existed when the
// lint landed (main at 49bd9af8d). They are grandfathered, not endorsed:
// splitting one by concern behind a named seam deletes its entry here.
// NEVER add an entry.
var sizeOnlyFileNamesAllowed = map[string]bool{
	"effects/misc.go":                 true,
	"rules/mana_cost_extra.go":        true,
	"rules/raise_cost_extra.go":       true,
	"rules/resolution_answer_rest.go": true,
	"rules/stack_helpers.go":          true,
	"rules/token_rest.go":             true,
	"rules/trigmatch_misc.go":         true,
}

// TestNoNewSizeOnlyFileNames is the rules-engine refactor spec's W0 file-name
// lint: a new non-test file in rules/ or effects/ (recursively) may not be
// named for its size. It checks names only; it does not measure file length.
func TestNoNewSizeOnlyFileNames(t *testing.T) {
	root := filepath.Join("..", "..")
	var seen []string
	for _, dir := range []string{"rules", "effects"} {
		for _, file := range goSourcesUnder(t, filepath.Join(root, dir)) {
			rel, err := filepath.Rel(root, file)
			if err != nil {
				t.Fatalf("rel %s: %v", file, err)
			}
			rel = filepath.ToSlash(rel)
			if !sizeOnlyFileName.MatchString(filepath.Base(rel)) {
				continue
			}
			seen = append(seen, rel)
			if !sizeOnlyFileNamesAllowed[rel] {
				t.Errorf("%s is named for its size, not its concern (matches %s).\n"+
					"Split by CONCERN behind a named seam, not by size: name the file for "+
					"the one thing it owns (e.g. rules/ward.go, rules/replacement_life.go), "+
					"and move a cohesive concern -- its types, its entry point and its "+
					"helpers -- rather than the second half of a long function or file. "+
					"Do not add the name to sizeOnlyFileNamesAllowed.",
					rel, sizeOnlyFileName)
			}
		}
	}
	// A grandfathered file that was split away or renamed must leave the list,
	// so the allow-list only shrinks.
	present := map[string]bool{}
	for _, rel := range seen {
		present[rel] = true
	}
	var stale []string
	for rel := range sizeOnlyFileNamesAllowed {
		if !present[rel] {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)
	for _, rel := range stale {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			t.Errorf("allow-list entry %s no longer matches the size-only pattern; delete it from sizeOnlyFileNamesAllowed", rel)
		} else {
			t.Errorf("allow-list entry %s no longer exists; delete it from sizeOnlyFileNamesAllowed (thank you for splitting it)", rel)
		}
	}
}

func TestSizeOnlyFileNamePattern(t *testing.T) {
	for name, want := range map[string]bool{
		"resolution_answer_rest.go": true,
		"stack_helpers.go":          true,
		"stack_helper.go":           true,
		"trigmatch_misc.go":         true,
		"cast_more.go":              true,
		"cast_extra2.go":            true,
		"cast_part3.go":             true,
		"cast_2.go":                 true,
		"misc.go":                   true,
		"helpers.go":                true,
		"util.go":                   true,
		"ward.go":                   false,
		"replacement_life.go":       false,
		"count_cov3.go":             false,
		"restriction.go":            false,
		"extra_turn.go":             false,
		"helpers_test.go":           false, // a test file never reaches the lint anyway
		"stack.go":                  false,
	} {
		got := sizeOnlyFileName.MatchString(name) && !strings.HasSuffix(name, "_test.go")
		if got != want {
			t.Errorf("sizeOnlyFileName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestGoSourcesUnderIsRecursive(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"a.go", "a_test.go", "sub/b.go", "sub/deeper/c.go", "testdata/d.go", ".hidden/e.go"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, f := range goSourcesUnder(t, dir) {
		rel, _ := filepath.Rel(dir, f)
		got = append(got, filepath.ToSlash(rel))
	}
	want := []string{"a.go", "sub/b.go", "sub/deeper/c.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("goSourcesUnder = %v, want %v", got, want)
	}
}
