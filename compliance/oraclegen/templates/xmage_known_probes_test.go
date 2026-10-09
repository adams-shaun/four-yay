package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestHandListedProbesAreKnownToXMage: XMage's addCard throws "Couldn't find a
// card" on a name its database lacks, so every probe a template lists by hand
// must be a card the committed set manifests hold. This is the census behind
// oraclegen.XMageKnown: the corpus-walking pickers filter on it at run time, and
// a hand list has no run-time filter worth trusting (Disguise Agent, an MKC
// card, sat in etbCastProbes and failed two MKM rows).
func TestHandListedProbesAreKnownToXMage(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "manifests", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no committed manifests found (glob err %v, %d paths)", err, len(paths))
	}
	sort.Strings(paths)
	var names []string
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var m compliance.Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		for _, c := range m.Cards {
			names = append(names, c.Name)
		}
	}
	if len(names) < 10000 {
		t.Fatalf("only %d manifest names loaded: the census would pass vacuously", len(names))
	}
	oraclegen.SetXMageKnown(names)
	t.Cleanup(func() { oraclegen.SetXMageKnown(nil) })

	lists := map[string][]string{
		"etbLandProbes":  etbLandProbes,
		"etbCastProbes":  etbCastProbes,
		"scryProbes":     scryProbes,
		"surveilProbes":  surveilProbes,
		"discardProbes":  discardProbes,
		"loyaltyProbes":  loyaltyProbes,
		"destroyProbes":  destroyProbes,
		"lifegainProbes": lifegainProbes,
		"drawProbes":     drawProbes,
		"singles":        {powerProbe, menaceProbe},
	}
	for _, tb := range typedBoardProbes {
		lists["typedBoardProbes/"+tb.word] = tb.probes
	}
	for _, n := range attackerTypeProbes {
		lists["attackerTypeProbes/"+n.word] = []string{n.probe}
	}
	for list, probes := range lists {
		if len(probes) == 0 {
			t.Errorf("%s is empty: the census names nothing", list)
		}
		for _, n := range probes {
			if !oraclegen.XMageKnown(n) {
				t.Errorf("%s lists %q, which no committed XMage set manifest holds", list, n)
			}
		}
	}
	if oraclegen.XMageKnown("Disguise Agent") {
		t.Errorf("Disguise Agent is known: the installed set is not the manifests' (precondition)")
	}
}
