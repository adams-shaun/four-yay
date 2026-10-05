package cards

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestClassCorpusCensus pins every Class carrier and ClassLevel$ reader at FORGE_REF.
// Paths include rebalanced variants; additions cannot silently escape this mechanic.
func TestClassCorpusCensus(t *testing.T) {
	root := filepath.Join("..", ".cards", "cardsfolder")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("corpus absent: %v", err)
	}
	var types, reads []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		text := string(raw)
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "Types:") {
				for _, word := range strings.Fields(strings.TrimPrefix(line, "Types:")) {
					if word == "Class" {
						types = append(types, rel)
						break
					}
				}
			}
		}
		if strings.Contains(text, "ClassLevel$") {
			reads = append(reads, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(types)
	sort.Strings(reads)
	wantTypes := []string{
		"a/advanced_floral_invocations.txt",
		"a/advanced_reconstruction.txt",
		"a/alchemists_talent.txt",
		"a/artificer_class.txt",
		"a/artists_talent.txt",
		"b/bandits_talent.txt",
		"b/barbarian_class.txt",
		"b/bard_class.txt",
		"b/blacksmiths_talent.txt",
		"b/builders_talent.txt",
		"c/caretakers_talent.txt",
		"c/cleric_class.txt",
		"c/cool_but_rude.txt",
		"d/does_machines.txt",
		"d/druid_class.txt",
		"f/fighter_class.txt",
		"f/fishers_talent.txt",
		"f/fortune_tellers_talent.txt",
		"g/gossips_talent.txt",
		"g/gourmands_talent.txt",
		"h/hunters_talent.txt",
		"i/innkeepers_talent.txt",
		"i/intermediate_chirography.txt",
		"l/leaders_talent.txt",
		"m/monk_class.txt",
		"n/ninja_teen.txt",
		"p/paladin_class.txt",
		"p/party_dude.txt",
		"r/ranger_class.txt",
		"r/rogue_class.txt",
		"rebalanced/a-druid_class.txt",
		"rebalanced/a-sorcerer_class.txt",
		"rebalanced/a-wizard_class.txt",
		"s/scavengers_talent.txt",
		"s/sorcerer_class.txt",
		"s/stormchasers_talent.txt",
		"w/warlock_class.txt",
		"w/wizard_class.txt",
	}
	if !reflect.DeepEqual(types, wantTypes) {
		t.Errorf("types = %v, want %v", types, wantTypes)
	}
	wantReads := []string{
		"a/artificer_class.txt",
		"b/builders_talent.txt",
		"c/caretakers_talent.txt",
		"c/cleric_class.txt",
		"c/cool_but_rude.txt",
		"d/does_machines.txt",
		"d/druid_class.txt",
		"m/monk_class.txt",
		"rebalanced/a-druid_class.txt",
		"rebalanced/a-wizard_class.txt",
		"s/stormchasers_talent.txt",
		"w/warlock_class.txt",
		"w/wizard_class.txt",
	}
	if !reflect.DeepEqual(reads, wantReads) {
		t.Errorf("reads = %v, want %v", reads, wantReads)
	}
}
