package effects

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestTargetedCardSelfCensus(t *testing.T) {
	dir := "../.cards/cardsfolder"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("corpus not fetched")
	}
	got := map[string]bool{}
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "TargetedCard.Self") {
			got[filepath.Base(path)] = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"banishment.txt", "declaration_in_stone.txt", "deputy_of_detention.txt", "echoing_calm.txt", "echoing_return.txt", "echoing_ruin.txt", "echoing_truth.txt", "enchanters_bane.txt", "essence_reliquary.txt", "hubris.txt", "legions_end.txt", "legions_to_ashes.txt", "maelstrom_pulse.txt", "mists_of_lorien.txt", "portal_of_sanctuary.txt", "rat_king_verminister.txt", "sever_the_bloodline.txt", "sewers_of_estark.txt", "wake_of_destruction.txt"}
	var names []string
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("TargetedCard.Self carriers = %v, want %v", names, want)
	}
}
