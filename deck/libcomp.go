package deck

// LibraryComposition is a seat's HONEST picture of what is left in its own
// library: the multiset of card names (never their order) derived from the
// seat's own genesis list minus every one of its own cards it can currently
// see outside the library. It is a function of what the seat may know -- its
// Manifest, the public zones, its own hand and stack, and the public library
// size -- and never of the engine's hidden library contents.
//
// The fill is a three-call fold, shared by both producers so they cannot
// drift (botpolicy.BoardFromGameInto walks state.Game, view.OwnLibrary walks
// a projected View; seat/ownlibrary_test.go pins them equal):
//
//	lc.Begin(manifest)
//	lc.See(printedName)  // each own card seen outside the library
//	lc.SeeHidden()       // each own card seen outside the library face down
//	lc.Finish(librarySize)
//
// A name the manifest's Main does not list is ignored by See: it never came
// out of this library (a token, a dungeon, a card wished in from the
// sideboard or conjured). Only Main is counted -- the sideboard is not in the
// library.
//
// The composition is UNKNOWN (Known false, Unknown naming why, Counts empty)
// whenever the seat cannot account for its library from what it sees:
//
//   - UnknownNoManifest: no genesis list (a spectator, a host with none).
//   - UnknownHiddenOwnCard: one of its own cards sits outside the library
//     face down with a face the seat may not look at (a card an opponent
//     exiled face down with WithMayLook$, a manifest an opponent controls).
//   - UnknownOverdrawn: more copies of a name are visible than the list
//     holds (a sideboard or conjured copy of a main-deck card).
//   - UnknownSizeMismatch: the remaining total disagrees with the public
//     library size -- a card entered or left the library unseen (an unseen
//     card shuffled in, a card moved out to a zone the seat cannot see, a
//     phased-out permanent the projection does not show).
//
// Residual limit (a stated approximation, not a claim of soundness beyond
// it): two unseen anomalies that cancel exactly in count -- one own card
// gone from sight and one foreign card of a listed name come into the
// library -- pass every check. Both halves are rules-breaking or require two
// independent hidden moves.
type LibraryComposition struct {
	Known   bool
	Unknown LibraryUnknown
	// Size is the public library size Finish was given.
	Size int32
	// Counts parallels the manifest's Main rows: Counts[i] copies of
	// Main[i].Name remain in the library. Empty when !Known.
	Counts []int32
	// Lands and Nonlands split Size by the rows' front-face Land flag.
	// Zero when !Known.
	Lands, Nonlands int32

	main   []ManifestRow
	sorted bool
}

// LibraryUnknown names why a composition is not derivable; a bit set.
type LibraryUnknown uint8

const (
	UnknownNoManifest LibraryUnknown = 1 << iota
	UnknownHiddenOwnCard
	UnknownOverdrawn
	UnknownSizeMismatch
)

// Begin resets the fold to m's Main (nil m: unknown). It reuses Counts'
// backing array, so a composition refilled per decision allocates only when
// the list grows.
func (lc *LibraryComposition) Begin(m *Manifest) {
	lc.Known, lc.Unknown, lc.Size, lc.Lands, lc.Nonlands = false, 0, 0, 0, 0
	lc.Counts = lc.Counts[:0]
	lc.main, lc.sorted = nil, true
	if m == nil {
		lc.Unknown = UnknownNoManifest
		return
	}
	lc.main = m.Main
	for i, r := range m.Main {
		lc.Counts = append(lc.Counts, int32(r.Count))
		if i > 0 && m.Main[i-1].Name >= r.Name {
			lc.sorted = false
		}
	}
}

// row is the Main index named name, or -1. NewManifest's rows are sorted by
// name, so the lookup is a binary search; a hand-built unsorted list falls
// back to a scan.
func (lc *LibraryComposition) row(name string) int {
	if !lc.sorted {
		for i := range lc.main {
			if lc.main[i].Name == name {
				return i
			}
		}
		return -1
	}
	lo, hi := 0, len(lc.main)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if lc.main[mid].Name < name {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(lc.main) && lc.main[lo].Name == name {
		return lo
	}
	return -1
}

// See records one of the seat's own cards, named by its printed front-face
// name, seen outside its library.
func (lc *LibraryComposition) See(name string) {
	if lc.Unknown != 0 {
		return
	}
	i := lc.row(name)
	if i < 0 {
		return
	}
	lc.Counts[i]--
	if lc.Counts[i] < 0 {
		lc.Unknown |= UnknownOverdrawn
	}
}

// SeeHidden records one of the seat's own cards seen outside its library
// with a face the seat may not look at.
func (lc *LibraryComposition) SeeHidden() { lc.Unknown |= UnknownHiddenOwnCard }

// Finish closes the fold against the public library size.
func (lc *LibraryComposition) Finish(librarySize int) {
	lc.Size = int32(librarySize)
	if lc.Unknown == 0 {
		var sum, lands int32
		for i, c := range lc.Counts {
			sum += c
			if lc.main[i].Land {
				lands += c
			}
		}
		if sum != lc.Size {
			lc.Unknown |= UnknownSizeMismatch
		} else {
			lc.Known, lc.Lands, lc.Nonlands = true, lands, sum-lands
			return
		}
	}
	lc.Counts = lc.Counts[:0]
}

// Row is the manifest row Counts[i] counts.
func (lc *LibraryComposition) Row(i int) ManifestRow { return lc.main[i] }

// Equal compares the derived facts (Known, Unknown, Size, Counts, Lands,
// Nonlands); nil and empty Counts are equal.
func (lc LibraryComposition) Equal(o LibraryComposition) bool {
	if lc.Known != o.Known || lc.Unknown != o.Unknown || lc.Size != o.Size ||
		lc.Lands != o.Lands || lc.Nonlands != o.Nonlands || len(lc.Counts) != len(o.Counts) {
		return false
	}
	for i := range lc.Counts {
		if lc.Counts[i] != o.Counts[i] {
			return false
		}
	}
	return true
}

// String names the unknown reasons ("" when none).
func (u LibraryUnknown) String() string {
	var s string
	for _, x := range [...]struct {
		bit  LibraryUnknown
		name string
	}{{UnknownNoManifest, "no-manifest"}, {UnknownHiddenOwnCard, "hidden-own-card"}, {UnknownOverdrawn, "overdrawn"}, {UnknownSizeMismatch, "size-mismatch"}} {
		if u&x.bit != 0 {
			if s != "" {
				s += ","
			}
			s += x.name
		}
	}
	return s
}
