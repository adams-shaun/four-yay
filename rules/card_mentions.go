package rules

import (
	"sync/atomic"
	"unsafe"

	"github.com/adams-shaun/gorge/cards"
)

// cardMentionMemo caches cardMentionsAscend / cardMentionsStoried per card.
// Both are pure functions of an immutable *cards.Card, and the arena scans
// that ask them (ascendPossible, storiedPossible) restart from object zero on
// every engine that has not scanned this game yet -- every search clone --
// so the same deck cards were re-walked, every SVar map ranged, once per
// simulation. A direct-mapped table of immutable entries, shared by every
// engine: an entry holds its card, so the address cannot be reused while it
// is cached, and a collision only costs a recomputation.
var cardMentionMemo [4096]atomic.Pointer[cardMentionEntry]

type cardMentionEntry struct {
	card    *cards.Card
	ascend  bool
	storied bool
}

func cardMentions(c *cards.Card) (ascend, storied bool) {
	if c == nil {
		return false, false
	}
	slot := &cardMentionMemo[(uintptr(unsafe.Pointer(c))>>4)%uintptr(len(cardMentionMemo))]
	if en := slot.Load(); en != nil && en.card == c {
		return en.ascend, en.storied
	}
	en := &cardMentionEntry{card: c, ascend: cardMentionsAscend(c), storied: cardMentionsStoried(c)}
	slot.Store(en)
	return en.ascend, en.storied
}
