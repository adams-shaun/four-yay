package deck

import (
	"sort"

	"github.com/adams-shaun/gorge/cards"
)

// CurveRow is one bucket of a deck's mana curve: Count copies whose
// front-face converted mana cost is CMC.
type CurveRow struct {
	CMC   int32 `json:"cmc"`
	Count int   `json:"count"`
}

// CurveOf derives the mana curve over a main decklist: every copy counted at
// its front face's Cmc() — the same face manifestRows takes each row's name
// from — bucketed in ascending cost order. Zero-cost cards (lands, free
// spells) bucket at 0. A nil or empty list yields a nil curve, so with
// omitempty the manifest member is absent rather than an empty lie. The
// buckets are emitted in ascending CMC order regardless of the input's
// order, so the derivation is deterministic.
func CurveOf(main []*cards.Card) []CurveRow {
	counts := make(map[int32]int)
	for _, c := range main {
		if c != nil && len(c.Faces) > 0 && c.Faces[0].Name != "" {
			counts[c.Faces[0].Cmc()]++
		}
	}
	cmcs := make([]int32, 0, len(counts))
	for cmc := range counts {
		cmcs = append(cmcs, cmc)
	}
	sort.Slice(cmcs, func(i, j int) bool { return cmcs[i] < cmcs[j] })
	if len(cmcs) == 0 {
		return nil
	}
	out := make([]CurveRow, 0, len(cmcs))
	for _, cmc := range cmcs {
		out = append(out, CurveRow{CMC: cmc, Count: counts[cmc]})
	}
	return out
}
