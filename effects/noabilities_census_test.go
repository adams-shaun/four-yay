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

// noAbilitiesCarriers is the pinned set of corpus files carrying the
// NoAbilities filter token at FORGE_REF 95f04e8a. A new carrier changes this
// set and fails the census loudly, exactly as the other corpus ratchets do.
var noAbilitiesCarriers = []string{
	"f/fang_druid_summoner.txt",
	"j/jasmine_boreal_of_the_seven.txt",
	"m/muraganda_petroglyphs.txt",
	"r/rise_from_the_wreck.txt",
	"r/ruxa_patient_professor.txt",
}

// noAbilitiesWordRe extracts each whitespace-delimited filter word carrying
// the NoAbilities token from a carrier line (e.g. `Creature.NoAbilities+YouOwn`,
// `Creature.!NoAbilities`, `Spell.Creature+NoAbilities`).
var noAbilitiesWordRe = regexp.MustCompile(`\S*NoAbilities\S*`)

// TestNoAbilitiesCensus is the corpus-wide ratchet for the NoAbilities
// predicate. It pins the exact carrier set (5 files) and asserts every filter
// word carrying the token in each carrier is recognised -- no
// effects.UnknownPredicates -- so the token can never again sit in the
// intersection of "recognised by the census" and "matched by nothing". It
// skips when the corpus is absent, like the other corpus-backed tests.
func TestNoAbilitiesCensus(t *testing.T) {
	root := filepath.Join("..", ".cards", "cardsfolder")
	if _, err := os.Stat(root); err != nil {
		t.Skip("effects: no .cards/ corpus present -- run `make fetch-cards compile-cards`")
	}
	var carriers []string
	unknownReported := map[string][]string{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return err
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		s := string(b)
		if !strings.Contains(s, "NoAbilities") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		carriers = append(carriers, rel)
		for _, word := range noAbilitiesWordRe.FindAllString(s, -1) {
			if got := UnknownPredicates(word); len(got) > 0 {
				unknownReported[rel] = append(unknownReported[rel], word+" -> "+strings.Join(got, ","))
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk corpus: %v", walkErr)
	}
	sort.Strings(carriers)
	if !equalStringSlices(carriers, noAbilitiesCarriers) {
		t.Fatalf("NoAbilities carrier set changed:\n  got  %v\n  want %v", carriers, noAbilitiesCarriers)
	}
	if len(unknownReported) > 0 {
		var lines []string
		for _, rel := range carriers {
			for _, u := range unknownReported[rel] {
				lines = append(lines, rel+": "+u)
			}
		}
		t.Fatalf("NoAbilities carrier filter(s) still report unknown predicates:\n  %s", strings.Join(lines, "\n  "))
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
