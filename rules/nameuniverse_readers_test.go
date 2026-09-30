package rules

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestNameUniverseReadersAreKnown pins the engine files that read the
// name-card universe (Config.NameUniverse / state.Game.NameUniverse) or the
// land-type vocabulary derived from it (Engine.landTypeWords).
// cards.NeedsFullNameUniverse decides from card text alone whether a subset
// registry's Cards may stand in for the whole corpus as a game's universe
// (cards.OpenCorpusFor; botbench and searchbench run on it), so it must know
// every script word that reaches one of these readers. A file newly on this
// list is a new reader: extend cards.nameUniverseMarkers (or
// NeedsFullNameUniverse) to the words that reach it, then add the file here.
func TestNameUniverseReadersAreKnown(t *testing.T) {
	want := []string{
		"effects/cardflow.go",       // effNameCard
		"effects/clone.go",          // CopyFromChosenName$
		"effects/namecard.go",       // NameChoices
		"effects/namecard_cache.go", // NameUniverseNames memo
		"events/apply_copy.go",      // chosen-name ClonePermanent fold
		"rules/cast_etbchoice.go",   // as-enters NameCard
		"rules/clone.go",            // copies landTypeWords into a clone
		"rules/engine.go",           // Config fields
		"rules/engine_struct.go",    // landTypeWords field
		"rules/genesis.go",          // derives landTypeWords and the name snapshot
		"rules/layers_types.go",     // AllNonBasicLandType
		"rules/mana_activation.go",  // CR 305.6 gate: len(landTypeWords) > 0
		"state/game.go",             // Game fields
	}
	re := regexp.MustCompile(`\bNameUniverse\b|\blandTypeWords\b|NameUniverseNames`)
	var got []string
	for _, pkg := range []string{"rules", "effects", "events", "state", "decision", "botpolicy", "view"} {
		paths, err := filepath.Glob(filepath.Join("..", pkg, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			if strings.HasSuffix(p, "_test.go") {
				continue
			}
			src, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if re.Match(src) {
				got = append(got, pkg+"/"+filepath.Base(p))
			}
		}
	}
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("NameUniverse readers changed:\n got %q\nwant %q\nextend cards.nameUniverseMarkers for a new reader, then update this list", got, want)
	}
}
