package cards

import (
	"sync/atomic"
	"unsafe"
)

// cardProbes is the memoised answer of the per-card pool probes the rules
// engine asks at every genesis over the match's decks and its whole token
// table (MayCarryControlStatic, ChangesTypes). Each is a pure function of
// the card's text, which is immutable once the card is linked, but a scan
// walks every SVar body with strings.Contains: a search that builds an
// engine per sample attempt paid the 800-token scan thousands of times.
type cardProbes struct {
	card          *Card // the card these answers were computed for
	controlStatic bool
	changesTypes  bool
}

// probe returns c's probe answers, computing and publishing them on first
// use. A by-value Card copy shares its original's pointer, so an entry that
// names another card is treated as absent and recomputed (unpublished);
// concurrent first stores race benignly, the first wins.
func (c *Card) probe() *cardProbes {
	if c == nil {
		return &cardProbes{}
	}
	if p := (*cardProbes)(atomic.LoadPointer(&c.probes)); p != nil && p.card == c {
		return p
	}
	p := &cardProbes{card: c, controlStatic: c.mayCarryControlStaticScan(), changesTypes: c.changesTypesScan()}
	atomic.CompareAndSwapPointer(&c.probes, nil, unsafe.Pointer(p))
	return p
}
