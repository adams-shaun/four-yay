package effects

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestStandardDebuffCarriers(t *testing.T) {
	testutil.CorpusRegistry(t)
	if !Supported()["api:Debuff"] {
		t.Fatal(`Supported() is missing "api:Debuff"`)
	}
	want := []string{"adarkar_windform", "barbed_foliage", "burn_from_within", "burning_palm_efreet", "canopy_dragon", "cephalid_snitch", "downdraft", "elder_land_wurm", "emerald_charm", "fear_of_falling", "gargoyle_sentinel", "glittering_lion", "glittering_lynx", "goblin_skycutter", "gravity_well", "grozoth", "hammerheim", "jaded_analyst", "kinscaer_harpoonist", "leering_gargoyle", "loyal_gyrfalcon", "manor_gargoyle", "mist_dragon", "mu_yanling_sky_dancer", "phyrexian_splicer", "radjan_spirit", "rebel_salvo", "ribbon_snake", "scarwood_hag", "sentry_oak", "shelkin_brownie", "shoal_serpent", "smite_the_deathless", "soul_sear", "swooping_talon", "talruum_champion", "tidewater_minion", "tolaria", "torpid_moloch", "urborg", "vertigo", "vintara_elephant", "wishful_merfolk", "xathrid_slyblade", "zerapa_minotaur"}
	var got []string
	err := filepath.WalkDir("../.cards/cardsfolder", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "DB$ Debuff") || strings.Contains(string(b), "AB$ Debuff") {
			got = append(got, strings.TrimSuffix(filepath.Base(path), ".txt"))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("api:Debuff carriers = %q, want %q", got, want)
	}
}
