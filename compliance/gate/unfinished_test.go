package gate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestGateHonoursXMageUnfinished: SOS's set class removes five cards after
// listing them, so XMage's card database does not hold them. They belong in
// the no-XMage bucket -- a hand-authored oracle scenario, never a generated
// XMage one. Each now carries such a scenario under
// rules/testdata/oracle/paradigm, which is what closes the card: the gate
// must report NO level-A problem for it. This pins both halves of that
// contract -- the manifest still marks the card unfinished (so no generated
// scenario can be expected) and a hand scenario exists -- so deleting either
// the scenario or the manifest row reintroduces the "XMage does not
// implement it" problem and fails here.
func TestGateHonoursXMageUnfinished(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..")
	m, err := compliance.LoadManifest(filepath.Join(root, "compliance", "manifests"), "SOS")
	if err != nil {
		t.Fatal(err)
	}
	unfinished := map[string]bool{}
	for _, u := range m.Unfinished {
		unfinished[compliance.FoldName(u)] = true
	}
	hand, err := HandScenarios(filepath.Join(root, "rules", "testdata", "oracle"))
	if err != nil {
		t.Fatal(err)
	}
	probs, err := Check(reg, root, "SOS", "A")
	if err != nil {
		t.Fatal(err)
	}
	byCard := map[string]string{}
	for _, p := range probs {
		byCard[p.Card] = p.Reason
	}
	for _, card := range []string{
		"Decorum Dissertation", "Echocasting Symposium", "Germination Practicum",
		"Improvisation Capstone", "Restoration Seminar",
	} {
		if !unfinished[compliance.FoldName(card)] {
			t.Errorf("%s: not marked unfinished in the SOS manifest", card)
		}
		if !hand[card] {
			t.Errorf("%s: no hand-authored oracle scenario covers it", card)
		}
		if reason, ok := byCard[card]; ok && strings.Contains(reason, "XMage does not implement it") {
			t.Errorf("%s: still reported as needing a hand scenario: %q", card, reason)
		}
	}
}

// TestGateFoldsPrintedNames: the gate keys inXMage by the manifest's spelling
// and looks it up by the printed spelling, so a diacritic or punctuation
// difference (Forge "Dáin Ironfoot", XMage "Dain Ironfoot") must not read as
// "XMage does not implement it". Every one of the 13 fold carriers is
// XMage-implemented, so none may carry that reason.
func TestGateFoldsPrintedNames(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..")
	carriers := map[string][]string{
		"HOB": {"Dáin Ironfoot", "Dáin's Company", "Dáin, Lord of the Iron Hills", "Fíli the Pathfinder",
			"Glóin the Mighty", "Kíli the Resourceful", "Thrór's Map", "Óin the Brave"},
		"MSH": {"Mjölnir, Hammer of Thor"},
		"SPM": {"Araña, Heart of the Spider", "With Great Power . . ."},
		"TMT": {"Bespoke Bō"},
		"LCI": {"Bartolomé del Presidio"},
	}
	for set, cards := range carriers {
		probs, err := Check(reg, root, set, "A")
		if err != nil {
			t.Fatal(err)
		}
		reason := map[string]string{}
		for _, p := range probs {
			reason[p.Card] = p.Reason
		}
		for _, card := range cards {
			if r, ok := reason[card]; ok && strings.Contains(r, "XMage does not implement it") {
				t.Errorf("%s %s: %q, but XMage implements it (folded name lookup missed)", set, card, r)
			}
		}
	}
}
