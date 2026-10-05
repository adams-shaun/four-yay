package effects

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The AnimateAll sweep scopes to a player-kind ValidTgts$ through
// targetPlayerKindScope (effects/scoped_sweep.go).  This ratchet pins every
// corpus carrier that carries such a ValidTgts$, so a NEW carrier fails loudly
// and its author has to confirm the scope branch covers it (rather than the
// sweep silently failing to scope and animating every seat, the Curious
// Colossus bug).  It is a census, not a whitelist: the file names are the
// whole population, measured, and must be updated in the same commit that adds
// a carrier.
//
// The corpus is gitignored (GPL-3.0): the test Skips when it is absent so a
// clean clone still passes.
func TestAnimateAllPlayerKindValidTgtsCensus(t *testing.T) {
	dir := "../.cards/cardsfolder"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("corpus not fetched; run `make fetch-cards`")
	}
	got := map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(b), "\n") {
			if !isAnimateAllAPI(line) {
				continue
			}
			tg := animateAllValidTgts(line)
			if tg != "" && playerSpecBaseKnown(tg) {
				got[filepath.Base(path)] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk corpus: %v", err)
	}
	want := []string{
		"curious_colossus.txt",
		"jolrael_empress_of_beasts.txt",
		"mass_diminish.txt",
		"polymorphists_jest.txt",
		"quick_draw.txt",
		"sudden_spoiling.txt",
	}
	var names []string
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	sort.Strings(want)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("AnimateAll carriers with a player-kind ValidTgts$ = %v, want %v\n"+
			"a new carrier must be confirmed against targetPlayerKindScope and this list updated", names, want)
	}
}

// isAnimateAllAPI reports whether line's API token is exactly AnimateAll.
// A Forge line is `A:<prefix>$ <API> | ...` or `SVar:<name>:<prefix>$ <API> |
// ...`; the prefix is DB$/SP$/AB$ (and any future one) and the sub-ability
// NAME lives before the colon, so matching the token after the first "$"
// catches every prefix without a hand-maintained list.  Matching the whole
// `DB$ AnimateAll` substring instead missed the SP$/AB$ carriers (4 of the 6
// players-scoped ones), which is what this census exists to catch.
func isAnimateAllAPI(line string) bool {
	_, rest, ok := strings.Cut(line, "$")
	if !ok {
		return false
	}
	api, _, _ := strings.Cut(strings.TrimSpace(rest), " | ")
	return strings.TrimSpace(api) == "AnimateAll"
}

// animateAllValidTgts extracts the ValidTgts$ value from an AnimateAll line,
// or "" when the line has none.  It splits on the " | " parameter separator so
// a ValidTgts$ value cannot run past its own pipe.
func animateAllValidTgts(line string) string {
	for _, part := range strings.Split(line, "|") {
		part = strings.TrimSpace(part)
		if v, ok := strings.CutPrefix(part, "ValidTgts$"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
