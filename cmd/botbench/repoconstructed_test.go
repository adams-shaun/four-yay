package main

// The repo-constructed SpellBench catalog: byte copies of the 14 supported
// 60-card constructed repo decks from internal/testutil/decks (mono-green-stompy
// included in the dir, out of the pool). These tests guard the copies against
// drift and prove botbench plays the catalog end to end. See
// docs/superpowers/reports/2026-09-30-repo-constructed-gauntlet.md.

import (
	"os"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestSpellbenchRepoConstructedCatalogCopiesAreByteIdentical guards the
// catalog copies against drift from internal/testutil/decks, their source of
// truth. Both sides are read from disk (the embeds are the same bytes at
// build time), and the test asserts the precondition — every copy exists and
// the two sides actually differ in pathname — so an empty or misplaced
// directory fails loudly.
func TestSpellbenchRepoConstructedCatalogCopiesAreByteIdentical(t *testing.T) {
	ids := append([]string(nil), spellbench.RepoPool...)
	ids = append(ids, "mono-green-stompy")
	slices.Sort(ids)
	for _, id := range ids {
		copyB, err := os.ReadFile("../../internal/spellbench/decks/repo-constructed/" + id + ".json")
		if err != nil {
			t.Fatalf("catalog copy %s: %v", id, err)
		}
		srcB, err := os.ReadFile("../../internal/testutil/decks/" + id + ".json")
		if err != nil {
			t.Fatalf("repo deck %s: %v", id, err)
		}
		if string(copyB) == string(srcB) {
			continue
		}
		line := 1
		for i := 0; i < len(copyB) && i < len(srcB); i++ {
			if copyB[i] != srcB[i] {
				t.Errorf("%s: catalog copy has drifted from internal/testutil/decks at byte %d (line %d); re-copy the file", id, i+1, line)
				break
			}
			if copyB[i] == '\n' {
				line++
			}
		}
		if len(copyB) != len(srcB) {
			t.Errorf("%s: copy is %d bytes, source %d", id, len(copyB), len(srcB))
		}
	}
	// The mirror image of the guard: every constructed repo deck is in the
	// catalog, so a deck added to internal/testutil/decks without a copy is
	// caught too.
	for _, name := range testutil.RepoDeckNames() {
		f, err := testutil.LoadRepoDeckFile(name)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range f.Cards {
			n += e.Count
		}
		if n != 60 || f.Format != "custom" {
			continue // commander and non-60-card decks are out of the catalog's scope
		}
		if !slices.Contains(ids, name) {
			t.Errorf("constructed repo deck %q is not copied into the repo-constructed catalog", name)
		}
	}
}

// TestSpellbenchRepoConstructedCatalogMirrors plays two repo-constructed
// catalog mirrors (sb-uniform against sb-heuristic) and checks each finishes
// without an engine halt and records the repo-constructed ledger format —
// the same shape TestSpellbenchFDNCatalogMirrors proves for fdn-limited.
func TestSpellbenchRepoConstructedCatalogMirrors(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cat, err := spellbench.CatalogByID("repo-constructed")
	if err != nil {
		t.Fatal(err)
	}
	pool := cat.Pool[:2] // death-n-taxes, dimir-tempo
	for _, g := range sbSchedule([]string{"sb-uniform", "sb-heuristic"}, pool, 1, 20260930) {
		if g.game != 0 {
			continue // one game per deck
		}
		deck, err := spellbench.Deck(reg, cat.Dir, g.deck)
		if err != nil {
			t.Fatal(err)
		}
		r := sbPlay(g, deck, reg, 60, 20000)
		if r.err != nil {
			t.Fatalf("%s %s: %v", g.id, g.deck, r.err)
		}
		row := sbLedgerRow(g, r, map[string]string{}, cat.Format)
		if row["format"] != "repo-constructed-bo1" {
			t.Fatalf("format %v", row["format"])
		}
		t.Logf("%s %s: %s turns=%d intents=%d", g.id, g.deck, sbResultLabel(r), r.outcome.Turns, r.outcome.Intents)
	}
}
