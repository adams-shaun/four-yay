// Package spellbench holds gorge's side of the SpellBench arena
// (github.com/jackmaiorino/spellbench): the benchmark deck catalogs gorge
// serves, and (later) the protocol adapters.
package spellbench

import (
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
)

//go:embed decks/pauper-kernel/*.json decks/fdn-limited/*.json decks/repo-constructed/*.json
var decksFS embed.FS

// PauperKernel is the pauper-kernel catalog directory.
const PauperKernel = "decks/pauper-kernel"

// BenchmarkPool is the eight decks the pauper-kernel benchmark rates on, in
// its benchmark.json order. The catalog also carries Terror, which g115 and
// a48 trained on but the benchmark does not play.
var BenchmarkPool = []string{"Wildfire", "Rally", "Affinity", "Elves", "Spy", "Burn", "CawGates", "Faeries"}

// RepoConstructed is the repo-constructed catalog directory: byte copies of
// the 14 supported 60-card constructed repo decks from
// internal/testutil/decks (format "custom", exactly 60 main-deck cards).
// The copies are deliberate: internal/spellbench must never import
// internal/testutil, and cmd/botbench's byte-equality guard pins the copies
// against drift.
const RepoConstructed = "decks/repo-constructed"

// RepoPool is the repo-constructed rotating deck pool: the 13 fully
// supported constructed decks (every one but mono-green-stompy, whose single
// Vines of Vastwood runs under the recorded stat:CantTarget approximation).
// mono-green-stompy stays IN the directory and OUT of the pool, exactly the
// pauper-kernel catalog's Terror precedent, so a -spellbench-decks run can
// still name it explicitly.
var RepoPool = []string{
	"death-n-taxes", "dimir-tempo", "eldrazi-stompy", "mono-black-aggro",
	"mono-blue-tempo", "mono-red-goblins", "mono-red-prowess",
	"mono-white-equipment", "the-epic-storm", "tron", "ur-delver",
	"uw-control", "uw-tempo",
}

// CatalogIDs lists every deck in dir by its catalog id (the file's "name").
func CatalogIDs(dir string) ([]string, error) {
	entries, err := decksFS.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		f, err := File(dir, strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		ids = append(ids, f.Name)
	}
	sort.Strings(ids)
	return ids, nil
}

// File parses catalog deck id (case-insensitive file stem) from dir.
func File(dir, id string) (deck.File, error) {
	raw, err := decksFS.ReadFile(path.Join(dir, strings.ToLower(id)+".json"))
	if err != nil {
		return deck.File{}, fmt.Errorf("spellbench: deck %q: %w", id, err)
	}
	return deck.Parse(raw)
}

// Deck resolves catalog deck id against r into rules.Config.Decks shape.
func Deck(r *cards.Registry, dir, id string) ([]*cards.Card, error) {
	f, err := File(dir, id)
	if err != nil {
		return nil, err
	}
	return f.Resolve(r)
}
