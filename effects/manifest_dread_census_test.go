package effects

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// manifestDreadParamCarriers pins every corpus card whose ManifestDread SA
// carries each implemented parameter family. A new carrier fails loudly so
// the mechanism cannot silently regress behind a fresh script.
var manifestDreadRememberedCarriers = []string{
	"Conductive Machete",
	"Cursed Windbreaker",
	"Dissection Tools",
	"Experimental Lab",
	"Killer's Mask",
	"Slimy Aquarium",
	"Valgavoth's Onslaught",
	"Weight Room",
}

var manifestDreadAmountCarriers = []string{
	"Experimental Lab",
	"Slimy Aquarium",
	"They Came from the Pipes",
	"Valgavoth's Onslaught",
	"Weight Room",
}

var manifestDreadDefinedPlayerCarriers = []string{
	"Fear of Impostors",
	"Unidentified Hovership",
	"Unwanted Remake",
}

// TestManifestDreadParamCensus walks every corpus ManifestDread SA and pins
// the carriers of each parameter family exactly.
func TestManifestDreadParamCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	remembered := map[string]bool{}
	amount := map[string]bool{}
	definedPlayer := map[string]bool{}
	saw := 0
	visit := func(name string, sa *cards.SA) {
		if sa == nil || sa.API != "ManifestDread" {
			return
		}
		saw++
		if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberManifested)), "True") {
			remembered[name] = true
		}
		if _, present := sa.Param(cards.PKAmount); present {
			amount[name] = true
		}
		if definedPlayerRef(sa).Set() {
			definedPlayer[name] = true
		}
	}
	for _, card := range reg.AllCards() {
		for _, f := range card.Faces {
			for _, sa := range f.Abilities {
				visit(f.Name, sa)
			}
			names := make([]string, 0, len(f.SVars))
			for n := range f.SVars {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				visit(f.Name, cards.ResolveSVar(f.SVars, n))
			}
		}
	}
	if saw < 30 {
		t.Fatalf("census saw only %d ManifestDread SAs: the scan is not reading the corpus", saw)
	}
	check := func(label string, got map[string]bool, want []string) {
		wantSet := map[string]bool{}
		for _, n := range want {
			wantSet[n] = true
		}
		var added, removed []string
		for n := range got {
			if !wantSet[n] {
				added = append(added, n)
			}
		}
		for n := range wantSet {
			if !got[n] {
				removed = append(removed, n)
			}
		}
		sort.Strings(added)
		sort.Strings(removed)
		if len(added) > 0 {
			t.Errorf("new ManifestDread %s carriers: %v", label, added)
		}
		if len(removed) > 0 {
			t.Errorf("pinned ManifestDread %s carriers gone: %v", label, removed)
		}
	}
	check("RememberManifested$", remembered, manifestDreadRememberedCarriers)
	check("Amount$", amount, manifestDreadAmountCarriers)
	check("DefinedPlayer$", definedPlayer, manifestDreadDefinedPlayerCarriers)
	t.Logf("%d ManifestDread SAs: %d RememberManifested, %d Amount, %d DefinedPlayer",
		saw, len(remembered), len(amount), len(definedPlayer))
}
