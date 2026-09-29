package rules

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetLifeCorpusCensus names every corpus card whose scripts carry a
// `SetLife` API line. `api:SetLife` was among the largest single unimplemented
// APIs in this set (task eoe-the-endstone-set-life); the census pins the
// carrier list so a future change to the primitive's grammar has the full
// population to re-measure against, and so the registration cannot silently
// lose a shape (a reverted REGISTER fails here as well as at the Endstone
// test).
//
// The 45 files are every card with a `DB$`/`SP$`/`AB$`/`AP$`/`A$ SetLife`
// line. The brief that dispatched this ticket reported 35 using the
// `(DB|AP|A)$ SetLife` spelling, which MISSES the `SP$` (spell) and `AB$`
// (activated) prefixes entirely; the measured population is 45, and `SP$`
// and `AB$` abilities reach the same registry entry, so all five prefixes
// are censused. Plain `SetLife` also occurs inside SVar names (45 files by
// substring); those are not the API line this primitive implements.
func TestSetLifeCorpusCensus(t *testing.T) {
	dir := filepath.Join("..", ".cards", "cardsfolder")
	var names []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hasSetLifeAPILine(string(b)) {
			names = append(names, strings.TrimSuffix(d.Name(), ".txt"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("corpus census: %v", err)
	}
	// Precondition: the corpus is present. Without it the walk finds no
	// files and the assertion below would fail on an empty list rather than
	// silently pass -- state it plainly so a missing corpus reads as such.
	if len(names) == 0 {
		t.Fatalf("corpus census found no cards under %s (is .cards linked?)", dir)
	}
	want := []string{
		"angel_of_grace", "arbiter_of_knollridge", "beacon_of_immortality",
		"biorhythm", "blessed_wind", "captive_audience", "celestial_mantle",
		"elderscale_wurm", "enduring_angel_angelic_enforcer",
		"eternity_vessel", "exquisite_archangel", "form_of_the_dinosaur",
		"form_of_the_dragon", "invincible_hymn", "lichs_mirror", "long_rest",
		"loxodon_lifechanter", "magister_sphinx", "master_of_cruelties",
		"mortal_flesh_is_weak", "nira_hellkite_duelist", "oketras_last_mercy",
		"once_more_with_feeling", "providence", "puresteel_angel", "rebirth",
		"repay_in_kind", "resolute_archangel", "reverse_the_sands",
		"revival_revenge", "sanctum_of_serra", "shaman_of_forgotten_ways",
		"sorin_markov", "stunning_reversal", "sway_of_the_stars",
		"the_doctors_tomb", "the_endstone", "the_golden_throne",
		"torgaar_famine_incarnate", "touch_of_the_eternal",
		"urza_academy_headmaster", "vraska_relic_seeker",
		"welcome_the_darkness", "worldfire",
		"you_live_only_because_i_will_it",
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("SetLife corpus files = %v, want %v", names, want)
	}
}

// hasSetLifeAPILine reports whether a card script text carries an ability
// whose API is SetLife, which is the line api:SetLife resolves.
// The five prefixes are the ability types Forge writes: DB$ (a sub-ability),
// SP$ (a spell ability), AB$ (an activated ability), AP$ (an activated
// ability in the newer spelling) and A$ (an activated ability's chained
// body). Each reaches the same registry entry, so the census must see all of
// them -- the `(DB|AP|A)$` spelling the dispatch brief used sees neither SP$
// nor AB$.
func hasSetLifeAPILine(script string) bool {
	for _, marker := range []string{
		"DB$ SetLife", "SP$ SetLife", "AB$ SetLife", "AP$ SetLife", "A$ SetLife",
	} {
		if strings.Contains(script, marker) {
			return true
		}
	}
	return false
}
