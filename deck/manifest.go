package deck

import (
	"sort"

	"github.com/adams-shaun/gorge/cards"
)

// This file owns the Manifest family — the immutable genesis list assigned
// to one seat and the canonicalization that builds it. It is split out of
// deck.go along the File/Manifest seam: deck.go holds deck-file parsing and
// resolution (File, Entry, Resolve, Load, ValidateCommander), while this
// file holds the seat's own-deck list and its derived observation data.
// Two live branches contended on deck.go — one adding a File method, one
// adding a field to ManifestRow — and each touched exactly one side of this
// seam, so the split keeps future tickets on separate files.

// ManifestRow is one canonical card-name/count pair in an own-deck manifest.
type ManifestRow struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Manifest is the immutable, unordered genesis list assigned to one seat.
// Call Clone before publishing or retaining it outside its owner.
type Manifest struct {
	Name string `json:"name"`
	// Archetype is the deck's authoring archetype (File.Archetype), carried so
	// the seat's own prior survives into the projection the bot reads as
	// view.View.OwnDeck. Empty for a deck file that declares none, which
	// marshals away (omitempty) and preserves the pre-field wire shape.
	Archetype  string        `json:"archetype,omitempty"`
	Main       []ManifestRow `json:"main"`
	Sideboard  []ManifestRow `json:"sideboard,omitempty"`
	Commanders []string      `json:"commanders,omitempty"`
	// Curve is the derived mana curve over Main (CurveOf): one row per
	// front-face CMC in ascending order, each carrying the copies at that
	// cost. Derived observation data like the rows themselves, not game state.
	Curve []CurveRow `json:"curve,omitempty"`
}

// NewManifest canonicalizes the configured card lists without retaining their
// slices. Commander identities preserve the declared index order.
// archetype is the deck file's authoring archetype ("" when it declares
// none).
func NewManifest(name, archetype string, main, sideboard []*cards.Card, commanderIndices []int) Manifest {
	m := Manifest{Name: name, Archetype: archetype, Main: manifestRows(main), Curve: CurveOf(main)}
	if len(sideboard) > 0 {
		m.Sideboard = manifestRows(sideboard)
	}
	for _, index := range commanderIndices {
		if index >= 0 && index < len(main) && main[index] != nil && len(main[index].Faces) > 0 {
			m.Commanders = append(m.Commanders, main[index].Faces[0].Name)
		}
	}
	return m
}

// Clone returns a manifest with independently-owned slices.
func (m Manifest) Clone() Manifest {
	m.Main = append([]ManifestRow{}, m.Main...)
	if len(m.Sideboard) > 0 {
		m.Sideboard = append([]ManifestRow{}, m.Sideboard...)
	} else {
		m.Sideboard = nil
	}
	if len(m.Commanders) > 0 {
		m.Commanders = append([]string{}, m.Commanders...)
	} else {
		m.Commanders = nil
	}
	if len(m.Curve) > 0 {
		m.Curve = append([]CurveRow{}, m.Curve...)
	} else {
		m.Curve = nil
	}
	return m
}

func manifestRows(cardsIn []*cards.Card) []ManifestRow {
	counts := make(map[string]int)
	for _, c := range cardsIn {
		if c != nil && len(c.Faces) > 0 && c.Faces[0].Name != "" {
			counts[c.Faces[0].Name]++
		}
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make([]ManifestRow, 0, len(names))
	for _, name := range names {
		rows = append(rows, ManifestRow{Name: name, Count: counts[name]})
	}
	return rows
}
