package paymirror

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestCloneFidelityAtPlannedCastsSeed1068(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d, err := LoadDecks(reg)
	if err != nil {
		t.Fatal(err)
	}
	g := PlayGame(d, GameSpec{Seed: 1068, Decks: []string{"tron", "death-n-taxes"}, Policy: "bot"},
		DriverOptions{Control: true})
	checked := 0
	for _, r := range g.Reports {
		if r.Control == nil {
			continue
		}
		checked++
		if r.Control.Status != Equivalent {
			t.Errorf("seq %d %q: live engine vs clone after the same intents: %s %v", r.Seq, r.Card, r.Control.Signature, r.Control.Diffs)
		}
	}
	if checked == 0 {
		t.Fatal("no planned cast was checked")
	}
}
