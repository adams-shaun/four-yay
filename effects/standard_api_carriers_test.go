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

// Every corpus card carrying the two direct APIs; a new carrier requires a
// deliberate review of selectors, modes and the behaviour tests above.
func TestStandardUnattachChangeSpeedCarriers(t *testing.T) {
	testutil.CorpusRegistry(t) // do not pass on an absent corpus
	want := map[string][]string{
		"Unattach":    {"akiri_fearless_voyager", "carry_away", "fulgent_distraction", "ogre_geargrabber", "stolen_uniform", "tamiyos_compleation", "unexpected_request"},
		"ChangeSpeed": {"ghirapur_grand_prix", "spikeshell_harrier"},
	}
	for api, names := range want {
		var got []string
		err := filepath.WalkDir("../.cards/cardsfolder", func(path string, d fs.DirEntry, err error) error {
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
			if strings.Contains(string(b), "DB$ "+api) || strings.Contains(string(b), "AB$ "+api) {
				got = append(got, strings.TrimSuffix(filepath.Base(path), ".txt"))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, names) {
			t.Errorf("api:%s carriers = %q, want %q", api, got, names)
		}
	}
}
