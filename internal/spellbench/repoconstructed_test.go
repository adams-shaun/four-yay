package spellbench

import (
	"slices"
	"strings"
	"testing"
)

// TestRepoConstructedCatalogDecks checks the repo-constructed catalog
// corpus-free: the directory is the 14 supported 60-card constructed repo
// decks, RepoPool is exactly the catalog minus mono-green-stompy (the Terror
// precedent: in the dir, out of the pool), every deck parses as a 60-card
// custom-format main deck, and CatalogByID serves the catalog. The
// byte-equality guard on the copies lives in cmd/botbench (which imports both
// internal/spellbench and internal/testutil).
func TestRepoConstructedCatalogDecks(t *testing.T) {
	entries, err := decksFS.ReadDir(RepoConstructed)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range entries {
		ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
	}
	slices.Sort(ids)
	if len(ids) == 0 {
		t.Fatal("repo-constructed catalog directory is empty")
	}
	// The catalog must carry the full 14 constructed decks, mono-green-stompy
	// included (it stays in the dir, out of the pool).
	if len(ids) != 14 {
		t.Fatalf("catalog has %d decks, want 14: %s", len(ids), strings.Join(ids, ","))
	}
	if !slices.Contains(ids, "mono-green-stompy") {
		t.Fatal("mono-green-stompy missing from the catalog directory")
	}
	want := append([]string(nil), RepoPool...)
	slices.Sort(want)
	var pool []string
	for _, id := range ids {
		if id != "mono-green-stompy" {
			pool = append(pool, id)
		}
	}
	slices.Sort(pool)
	if strings.Join(pool, ",") != strings.Join(want, ",") {
		t.Fatalf("RepoPool %v != catalog minus mono-green-stompy %v", want, pool)
	}
	// CatalogIDs returns each deck's name as its id, and Deck resolves ids
	// as lowercased file stems — so every copy's name MUST be its stem for
	// the returned ids to round-trip (a display name like "Death & Taxes"
	// would produce an id no Deck call can resolve). Pin both directions.
	catIDs, err := CatalogIDs(RepoConstructed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(catIDs, ",") != strings.Join(ids, ",") {
		t.Fatalf("CatalogIDs(repo-constructed) = %v; want the file stems %v", catIDs, ids)
	}
	for _, id := range ids {
		f, err := File(RepoConstructed, id)
		if err != nil {
			t.Fatal(err)
		}
		if f.Name != id {
			t.Errorf("%s: name %q; want the file stem so CatalogIDs round-trips through Deck", id, f.Name)
		}
	}
	for _, id := range ids {
		f, err := File(RepoConstructed, id)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range f.Cards {
			n += e.Count
		}
		if n != 60 || f.Format != "custom" {
			t.Errorf("%s: %d cards, format %q; want 60, custom", id, n, f.Format)
		}
	}
	c, err := CatalogByID("repo-constructed")
	if err != nil {
		t.Fatal(err)
	}
	if c.Dir != RepoConstructed || c.Format != "repo-constructed-bo1" || !slices.Equal(c.Pool, RepoPool) {
		t.Fatalf("CatalogByID(repo-constructed) = %+v", c)
	}
}
