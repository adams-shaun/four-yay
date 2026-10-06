package oraclegen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

var shuffleCensusSets = []string{
	"BIG", "BLB", "DFT", "DSK", "ECL", "EOE", "FDN", "FIN", "FRA", "HOB",
	"LCI", "MKM", "MSH", "OTJ", "SOS", "SPM", "TDM", "TLA", "TMT", "WOE",
}

var shuffleCensusWant = map[string]int{
	"BIG": 2, "BLB": 4, "DFT": 13, "DSK": 15, "ECL": 12,
	"EOE": 27, "FDN": 23, "FIN": 21, "FRA": 16, "HOB": 11,
	"LCI": 14, "MKM": 14, "MSH": 9, "OTJ": 11, "SOS": 10,
	"SPM": 6, "TDM": 30, "TLA": 19, "TMT": 13, "WOE": 8,
}

func TestShuffleCompareCensus(t *testing.T) {
	reg, err := cards.SharedCorpus(filepath.Join("..", "..", ".cards"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, set := range shuffleCensusSets {
		raw, err := os.ReadFile(filepath.Join("..", "..", "compliance", "manifests", set+".json"))
		if err != nil {
			t.Fatalf("manifest %s: %v", set, err)
		}
		var manifest struct {
			Cards []struct {
				Name string `json:"name"`
			} `json:"cards"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatalf("manifest %s: %v", set, err)
		}
		members := map[string]bool{}
		for _, c := range manifest.Cards {
			members[strings.ToLower(strings.TrimSpace(c.Name))] = true
		}
		for i := range reg.Cards {
			card := reg.Cards[i]
			if len(card.Faces) == 0 || !members[strings.ToLower(strings.TrimSpace(card.Faces[0].Name))] {
				continue
			}
			for fi := range card.Faces {
				if CanShuffleLibrary(card.Faces[fi]) {
					got[set] = append(got[set], card.Faces[fi].Name)
					if card.Faces[fi].Name == "Fblthp, Impossibly Lost" {
						item := NewItem(card.Faces[fi], card.Faces[fi].Name, "trigger#0.0", 1, Scenario{})
						if len(item.Compare) != 1 || item.Compare[0] != CompareNoLibraryOrder {
							t.Errorf("generated Fblthp item missing comparison mark: %v", item.Compare)
						}
					}
					break
				}
			}
		}
		sort.Strings(got[set])
	}
	if len(shuffleCensusWant) == 0 {
		counts := map[string]int{}
		for set, names := range got {
			counts[set] = len(names)
		}
		t.Fatalf("shuffle census not pinned; measured: %#v", counts)
	}
	for _, set := range shuffleCensusSets {
		if count := len(got[set]); count != shuffleCensusWant[set] {
			t.Errorf("%s shuffle-marked items: got %d (%v), want %d", set, count, got[set], shuffleCensusWant[set])
		}
	}
	declaredRaw, err := os.ReadFile(filepath.Join("..", "..", "compliance", "declared.json"))
	if err != nil {
		t.Fatal(err)
	}
	var declared map[string]string
	if err := json.Unmarshal(declaredRaw, &declared); err != nil {
		t.Fatal(err)
	}
	if len(declared) != len(shuffleCensusWant) {
		t.Fatalf("shuffle census covers %d sets, but %d are declared", len(shuffleCensusWant), len(declared))
	}
	for set := range declared {
		if _, ok := shuffleCensusWant[set]; !ok {
			t.Errorf("declared set %s missing from shuffle census", set)
		}
	}
	if !containsShuffleName(got["FRA"], "Fblthp, Impossibly Lost") {
		t.Errorf("FRA census omitted Fblthp, Impossibly Lost: %v", got["FRA"])
	}
}

func containsShuffleName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
