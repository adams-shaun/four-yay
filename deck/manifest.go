package deck

import (
	"sort"

	"github.com/adams-shaun/gorge/cards"
)

// ManifestRow is one canonical card-name/count pair in an own-deck manifest.
// Land is whether the card's FRONT face is a land: the face every card in a
// library presents (CR 711.2/712.2 -- a double-faced card has only its front
// face's characteristics outside the stack and battlefield), so it is the
// land/nonland split of whatever copies remain in the library
// (LibraryComposition). A printed card fact of the seat's own list, not
// game state; omitted from the wire when false.
type ManifestRow struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Land  bool   `json:"land,omitempty"`
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
	land := make(map[string]bool)
	for _, c := range cardsIn {
		if c != nil && len(c.Faces) > 0 && c.Faces[0].Name != "" {
			counts[c.Faces[0].Name]++
			if c.Faces[0].IsLand() {
				land[c.Faces[0].Name] = true
			}
		}
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make([]ManifestRow, 0, len(names))
	for _, name := range names {
		rows = append(rows, ManifestRow{Name: name, Count: counts[name], Land: land[name]})
	}
	return rows
}
