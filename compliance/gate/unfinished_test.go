package gate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestGateHonoursXMageUnfinished: SOS's set class removes five cards after
// listing them, so XMage's card database does not hold them. They belong in
// the no-XMage bucket (a hand-authored oracle scenario), never the harness
// bucket.
func TestGateHonoursXMageUnfinished(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..")
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
		reason, ok := byCard[card]
		if !ok {
			t.Errorf("%s: not reported at all; want the no-XMage reason", card)
			continue
		}
		if !strings.Contains(reason, "XMage does not implement it") {
			t.Errorf("%s: %q, want the no-XMage reason", card, reason)
		}
	}
}

// TestGateFoldsPrintedNames: the gate keys inXMage by the manifest's spelling
// and looks it up by the printed spelling, so a diacritic or punctuation
// difference (Forge "Dáin Ironfoot", XMage "Dain Ironfoot") must not read as
// "XMage does not implement it". Every one of the 13 fold carriers is
// XMage-implemented, so none may carry that reason.
func TestGateFoldsPrintedNames(t *testing.T) {
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
