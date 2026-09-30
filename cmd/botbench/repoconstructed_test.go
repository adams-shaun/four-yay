package main

// The repo-constructed SpellBench catalog: byte copies of the 14 supported
// 60-card constructed repo decks from internal/testutil/decks (mono-green-stompy
// included in the dir, out of the pool). These tests guard the copies against
// drift and prove botbench plays the catalog end to end. See
// docs/superpowers/reports/2026-09-30-repo-constructed-gauntlet.md.

import (
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// deckPayload is the load-bearing deck payload for the drift guard.
type deckPayload struct {
	Format     string
	Archetype  string
	Commander  string
	Commanders []string
	Cards      []deck.Entry
	Sideboard  []deck.Entry
}

// payloadOf extracts the load-bearing deck payload for the drift guard.
func payloadOf(f deck.File) deckPayload {
	return deckPayload{f.Format, f.Archetype, f.Commander, f.Commanders, f.Cards, f.Sideboard}
}

// TestSpellbenchRepoConstructedCatalogCopiesMatchSource guards the catalog
// copies against drift from internal/testutil/decks, their source of truth.
// The copies are byte-identical to their sources EXCEPT the top-level
// "name" field, which each copy replaces with its file stem: CatalogIDs
// (internal/spellbench/decks.go) returns the deck's name as its id and
// Deck resolves ids as lowercased file stems, so a display name like
// "Death & Taxes" would produce an id no Deck call can resolve (found by
// builtins' TestReanimateValueTerminates, which iterates every catalog).
// The load-bearing payload — format, archetype, commanders, main deck and
// sideboard — must match the source exactly. Both sides are read from disk,
// and the test asserts the precondition — every copy exists — so an empty
// or misplaced directory fails loudly.
func TestSpellbenchRepoConstructedCatalogCopiesMatchSource(t *testing.T) {
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
		cf, err := deck.Parse(copyB)
		if err != nil {
			t.Fatalf("catalog copy %s: %v", id, err)
		}
		sf, err := deck.Parse(srcB)
		if err != nil {
			t.Fatalf("repo deck %s: %v", id, err)
		}
		// The copies' name is the stem, so every id CatalogIDs returns
		// round-trips through Deck.
		if cf.Name != id {
			t.Errorf("catalog copy %s: name %q; want the file stem (CatalogIDs returns the name as the id)", id, cf.Name)
		}
		cp, sp := payloadOf(cf), payloadOf(sf)
		if !reflect.DeepEqual(cp, sp) {
			msg := fmt.Sprintf(" (format %q vs %q, archetype %q vs %q, commander %q vs %q)", cp.Format, sp.Format, cp.Archetype, sp.Archetype, cp.Commander, sp.Commander)
			for _, e := range []struct {
				zone      string
				got, want []deck.Entry
			}{
				{"main", cp.Cards, sp.Cards}, {"sideboard", cp.Sideboard, sp.Sideboard}} {
				for i := 0; i < len(e.got) && i < len(e.want); i++ {
					if e.got[i] != e.want[i] {
						msg += fmt.Sprintf(" (%s entry %d: %q copy %d, source %d)", e.zone, i, e.want[i].Name, e.got[i].Count, e.want[i].Count)
						break
					}
				}
			}
			t.Errorf("%s: catalog copy has drifted from internal/testutil/decks%s; re-copy the file", id, msg)
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

// The source's name differs from the stem only in that one field; assert it
// so the narrowing stays justified — if the source name ever BECOMES the
// stem, the copies can go back to byte-identical and this test's name-field
// carve-out is stale.
func TestSpellbenchRepoConstructedSourceNamesAreDisplayNames(t *testing.T) {
	for _, id := range append(append([]string(nil), spellbench.RepoPool...), "mono-green-stompy") {
		srcB, err := os.ReadFile("../../internal/testutil/decks/" + id + ".json")
		if err != nil {
			t.Fatalf("repo deck %s: %v", id, err)
		}
		sf, err := deck.Parse(srcB)
		if err != nil {
			t.Fatalf("repo deck %s: %v", id, err)
		}
		if sf.Name == id {
			t.Errorf("repo deck %s: source name %q is now the stem; the catalog copies can be byte-identical again", id, sf.Name)
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
