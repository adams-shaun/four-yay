package rules

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The corpus census includes three ExchangeLifeVariant carriers. The other
// eight use ExchangeLife itself, including both one- and two-target forms.
func TestExchangeLifeCorpusCensus(t *testing.T) {
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
		if strings.Contains(string(b), "ExchangeLife") {
			names = append(names, strings.TrimSuffix(d.Name(), ".txt"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("corpus census: %v", err)
	}
	want := []string{"axis_of_mortality", "cliffside_market", "evra_halcyon_witness", "magus_of_the_mirror", "mirror_universe", "mister_negative", "profane_transfusion", "psychic_transfer", "soul_conduit", "tree_of_perdition", "tree_of_redemption"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("ExchangeLife corpus files = %v, want %v", names, want)
	}
}
