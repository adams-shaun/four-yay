package effects

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestConditionCorpusCensus is the corpus-wide ratchet over Condition* shapes.
// It walks every Forge card script in ../.cards/cardsfolder and asserts that
// every Condition* key and every ConditionDefined$/Condition$ value is either
// evaluated (in conditionEvaluatedKeys / conditionSupportedDefined /
// conditionSupportedBare) or explicitly listed as unmodelled
// (conditionUnmodelledKeys / conditionUnmodelledDefined /
// conditionUnmodelledBare). A new carrier of an unclassified shape fails this
// test until it is classified, so the fail-open/fail-closed boundary can never
// silently drift.
//
// It skips when the corpus is absent (a clean clone), exactly as the other
// corpus-backed tests do.
func TestConditionCorpusCensus(t *testing.T) {
	dir := filepath.Join("..", ".cards", "cardsfolder")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("effects: no .cards/ corpus present -- run `make fetch-cards compile-cards`")
	}
	var (
		keyRe     = regexp.MustCompile(`(?:^|[^A-Za-z])(Condition[A-Za-z0-9]+)\$`)
		definedRe = regexp.MustCompile(`ConditionDefined\$ ([^ |\n\r]*)`)
		bareRe    = regexp.MustCompile(`(?:^|[^A-Za-z])Condition\$ ([^ |\n\r]*)`)
	)
	keys := map[string]int{}
	defined := map[string]int{}
	bare := map[string]int{}
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		s := string(b)
		for _, m := range keyRe.FindAllStringSubmatch(s, -1) {
			keys[m[1]]++
		}
		for _, m := range definedRe.FindAllStringSubmatch(s, -1) {
			defined[m[1]]++
		}
		for _, m := range bareRe.FindAllStringSubmatch(s, -1) {
			bare[m[1]]++
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk corpus: %v", walkErr)
	}

	var unknown []string
	for k := range keys {
		if k == "ConditionDescription" {
			continue
		}
		if conditionEvaluatedKeys.Has(k) || conditionUnmodelledKeys.Has(k) {
			continue
		}
		unknown = append(unknown, "key "+k)
	}
	for v := range defined {
		if conditionSupportedDefined.Has(v) || conditionUnmodelledDefined.Has(v) {
			continue
		}
		unknown = append(unknown, "ConditionDefined$ "+v)
	}
	for v := range bare {
		if conditionSupportedBare.Has(v) || conditionUnmodelledBare.Has(v) {
			continue
		}
		unknown = append(unknown, "Condition$ "+v)
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		t.Fatalf("unclassified Condition shape(s) in the corpus -- add each to condition_census.go "+
			"(evaluated or unmodelled):\n  %s", strings.Join(unknown, "\n  "))
	}
}
