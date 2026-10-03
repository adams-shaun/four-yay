package rules

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// sourceFilesUnder returns the non-test .go files under dir, RECURSIVELY
// (testdata and dot/underscore directories excluded), whose basename keep
// accepts (nil keeps all), sorted. The source-parsing census tests use it
// instead of filepath.Glob(dir/*.go) so a later move of code into a rules
// subpackage (the rules-engine refactor spec's W5) cannot silently drop files
// from a census and leave it running vacuously green.
func sourceFilesUnder(t *testing.T, dir string, keep func(base string) bool) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != dir && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") && (keep == nil || keep(name)) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(files)
	return files
}
