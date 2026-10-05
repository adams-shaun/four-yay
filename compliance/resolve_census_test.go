package compliance

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// foldedCarriers is the census of every printed card name that is not an
// exact manifest name but folds onto one, at the committed XMAGE_REF and
// FORGE_REF. These are the cards the gate's `inXMage` lookup used to miss:
// Forge prints a diacritic or punctuation XMage's set class writes
// differently. The list is a ratchet: a new carrier (a Forge spelling XMage
// writes differently) fails loudly, and the fix is to confirm the fold
// resolves it, then add the line.
var foldedCarriers = []string{
	"HOB/Dáin Ironfoot",
	"HOB/Dáin's Company",
	"HOB/Dáin, Lord of the Iron Hills",
	"HOB/Fíli the Pathfinder",
	"HOB/Glóin the Mighty",
	"HOB/Kíli the Resourceful",
	"HOB/Thrór's Map",
	"HOB/Óin the Brave",
	"LCI/Bartolomé del Presidio",
	"MSH/Mjölnir, Hammer of Thor",
	"SPM/Araña, Heart of the Spider",
	"SPM/With Great Power . . .",
	"TMT/Bespoke Bō",
}

// TestPrintedManifestNamesFold is the census for the cross-source name
// lookup the gate and gen share: for every committed printed set, every
// printed name that is not an exact manifest name must still be found by
// FoldName, and the carriers are pinned. It reads only committed data
// (printed lists and manifests), so it runs without the corpus.
func TestPrintedManifestNamesFold(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("printed", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 20 {
		t.Fatalf("only %d printed lists; the glob is not reaching compliance/printed", len(paths))
	}
	var got []string
	for _, p := range paths {
		code := strings.TrimSuffix(filepath.Base(p), ".json")
		pr, err := LoadPrinted(filepath.Dir(p), code)
		if err != nil {
			t.Fatal(err)
		}
		m, err := LoadManifest("manifests", code)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		exact := map[string]bool{}
		folded := map[string]bool{}
		for _, mc := range m.Cards {
			exact[mc.Name] = true
			folded[FoldName(mc.Name)] = true
		}
		for _, pc := range pr.Cards {
			if exact[pc] {
				continue
			}
			if folded[FoldName(pc)] {
				got = append(got, code+"/"+pc)
			}
		}
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, foldedCarriers) {
		t.Errorf("folded printed/manifest carriers changed:\n got  %v\n want %v", got, foldedCarriers)
	}
}

// TestFoldDoesNotCollapseDistinctManifestNames guards the other direction:
// the fold is only safe while it does not merge two cards a single manifest
// lists separately. If a set ever does, the folded lookup would resolve one
// card's printed name to the other's corpus name.
func TestFoldDoesNotCollapseDistinctManifestNames(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("manifests", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		code := strings.TrimSuffix(filepath.Base(p), ".json")
		m, err := LoadManifest("manifests", code)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]string{}
		for _, mc := range m.Cards {
			k := FoldName(mc.Name)
			if prev, ok := seen[k]; ok && prev != mc.Name {
				t.Errorf("%s: %q and %q fold to the same name %q", code, prev, mc.Name, k)
			}
			seen[k] = mc.Name
		}
	}
}
