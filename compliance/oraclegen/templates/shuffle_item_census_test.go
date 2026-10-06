package templates

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// Count emitted items, not shuffle-capable cards: one face can serve several
// requirements, and a template gap emits no item at all.
var wantShuffleItemCensus = map[string]int{
	"BIG": 0, "BLB": 0, "DFT": 0, "DSK": 3, "ECL": 0,
	"EOE": 1, "FDN": 3, "FIN": 1, "FRA": 2, "HOB": 2,
	"LCI": 0, "MKM": 0, "MSH": 0, "OTJ": 0, "SOS": 0,
	"SPM": 0, "TDM": 0, "TLA": 0, "TMT": 0, "WOE": 0,
}

func TestShuffleEmittedItemCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..", "..", "compliance")
	declared, err := compliance.LoadDeclared(filepath.Join(root, "declared.json"))
	if err != nil {
		t.Fatal(err)
	}
	sets := make([]string, 0, len(declared))
	for set := range declared {
		sets = append(sets, set)
	}
	sort.Strings(sets)
	folded := compliance.FoldedNames(reg)
	has := func(name string) bool { _, ok := reg.Lookup(name); return ok }
	got := make(map[string]int, len(sets))
	foundFblthp := false
	for _, set := range sets {
		printed, err := compliance.LoadPrinted(filepath.Join(root, "printed"), set)
		if err != nil {
			t.Fatal(err)
		}
		got[set] = 0
		seen := map[string]bool{}
		for _, name := range printed.Cards {
			name, ok := compliance.CorpusNameFold(has, folded, name)
			if !ok {
				t.Fatalf("%s: %q absent from corpus", set, name)
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			card, _ := reg.Lookup(name)
			for _, req := range levelb.Requirements(card) {
				if req.Gap != "" || !oraclegen.CanShuffleLibrary(card.Faces[req.Face]) {
					continue
				}
				item, skip := GenerateB(reg, name, req)
				if skip != nil {
					continue
				}
				if item.ID == "" {
					t.Fatalf("%s/%s: served an empty item", name, req.Key)
				}
				marks := 0
				for _, option := range item.Compare {
					if option == oraclegen.CompareNoLibraryOrder {
						marks++
					}
				}
				if marks != 1 {
					t.Fatalf("%s: got %d library-order marks, want exactly one", item.ID, marks)
				}
				got[set]++
				t.Logf("%s %s", set, item.ID)
				if set == "FRA" && item.ID == "Fblthp, Impossibly Lost/trigger#0.0/v1" {
					foundFblthp = true
				}
			}
		}
	}
	if !foundFblthp {
		t.Fatal("precondition: census must include the emitted FRA Fblthp trigger")
	}
	if !reflect.DeepEqual(got, wantShuffleItemCensus) {
		t.Fatalf("shuffle emitted-item census changed: got %#v, want %#v", got, wantShuffleItemCensus)
	}
}
