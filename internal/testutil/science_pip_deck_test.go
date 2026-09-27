package testutil

import (
	"testing"
)

// TestSciencePipDeckImport pins the Science! (pip) Commander precon import
// (internal/testutil/decks/science-pip.json, task
// agent-20260922T231317Z-842a3f1e). It is the deck that surfaced the
// kw:Fortify gap: C.A.M.P. is one of the keyword's two corpus carriers, and
// the deck had to be imported before the deck-wide coverage and parameter
// ratchets could measure its full card list.
//
// The test asserts the import's own contract, not the engine's: the fixture
// declares Dr. Madison Li as a legal 100-card Commander deck, resolves every
// one of its cards against the corpus, and still carries C.A.M.P. -- so a
// future edit that silently drops the card, renames the commander or
// truncates the list fails here rather than only moving the ratchet totals.
func TestSciencePipDeckImport(t *testing.T) {
	reg := CorpusRegistry(t)

	f := RepoDeckFile(t, "science-pip")
	if f.Commander != "Dr. Madison Li" {
		t.Fatalf("science-pip commander = %q, want %q", f.Commander, "Dr. Madison Li")
	}

	// Precondition the ratchets depend on: the fixture is a legal Commander
	// deck (CR 903.4: exactly 100 cards including the commander). A fixture
	// that stopped being 100 cards would still resolve, but every number the
	// import contributes to the ratchet would be measuring something else.
	if err := f.ValidateCommander(reg); err != nil {
		t.Fatalf("science-pip is not a legal Commander deck: %v", err)
	}

	cs, err := LoadRepoDeck(reg, "science-pip")
	if err != nil {
		t.Fatalf("science-pip does not resolve: %v", err)
	}
	if len(cs) != 100 {
		t.Fatalf("science-pip resolved %d cards, want 100", len(cs))
	}

	// The deck's reason for existing in the ratchet: C.A.M.P. must be one of
	// the 100 resolved cards, or the import no longer measures the card whose
	// kw:Fortify gap motivated it.
	var hasCamp bool
	for _, c := range cs {
		if c.Faces[0].Name == "C.A.M.P." {
			hasCamp = true
			break
		}
	}
	if !hasCamp {
		t.Fatal("science-pip no longer contains C.A.M.P.; the deck no longer measures the card it was imported for")
	}
}
