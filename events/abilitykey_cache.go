package events

import (
	"strconv"
	"strings"
	"sync/atomic"
	"unsafe"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// The ability-key content cache. ResolvedAbilityKey's content half (the
// writeSAKey walk: Kind, API, sorted Params, then the Sub chain) is a pure
// function of the SA chain's contents, and every resolution of an ability
// rebuilt it -- a key-sort, a builder and a string per resolution. The cache
// is direct-mapped by the root SA's address and answers only for the exact
// chain it was built from: every level's SA pointer, Kind, API and Params map
// identity and size must match (the same immutability contract
// cards.ParamSet relies on: a printed node's Params map is never written in
// place; a rewritten node gets a fresh map). Entries are immutable and
// published atomically, so engines on other goroutines share them safely.

const saKeyCacheBits = 11

type saKeyLevel struct {
	sa     *cards.SA
	kind   string
	api    string
	params unsafe.Pointer
	n      int
}

type saKeyEntry struct {
	levels  []saKeyLevel
	content string
}

var saKeyCache [1 << saKeyCacheBits]atomic.Pointer[saKeyEntry]

func saKeySlot(sa *cards.SA) *atomic.Pointer[saKeyEntry] {
	h := uint64(uintptr(unsafe.Pointer(sa))>>3) * 0x9e3779b97f4a7c15
	return &saKeyCache[h>>(64-saKeyCacheBits)]
}

func mapPtr(m map[string]string) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&m))
}

// matches reports whether en was built from exactly sa's current chain.
func (en *saKeyEntry) matches(sa *cards.SA) bool {
	cur := sa
	for i := range en.levels {
		l := &en.levels[i]
		if cur != l.sa || cur.Kind != l.kind || cur.API != l.api || mapPtr(cur.Params) != l.params || len(cur.Params) != l.n {
			return false
		}
		cur = cur.Sub
	}
	// writeSAKey stops at depth 32: a chain longer than the recorded levels
	// is a match only when the recorded walk was cut off there too.
	return cur == nil || len(en.levels) == 33
}

// saKeyContent is writeSAKey(sa, 0)'s text, through the cache.
func saKeyContent(sa *cards.SA) string {
	if sa == nil {
		return ""
	}
	slot := saKeySlot(sa)
	if en := slot.Load(); en != nil && en.matches(sa) {
		return en.content
	}
	var b strings.Builder
	writeSAKey(&b, sa, 0)
	en := &saKeyEntry{content: b.String()}
	for cur, d := sa, 0; cur != nil && d <= 32; cur, d = cur.Sub, d+1 {
		en.levels = append(en.levels, saKeyLevel{sa: cur, kind: cur.Kind, api: cur.API, params: mapPtr(cur.Params), n: len(cur.Params)})
	}
	slot.Store(en)
	return en.content
}

// appendResolvedAbilityKey appends ResolvedAbilityKey(source, sa)'s bytes.
func appendResolvedAbilityKey(dst []byte, source state.ObjID, sa *cards.SA) []byte {
	dst = strconv.AppendUint(dst, uint64(source), 10)
	dst = append(dst, '|')
	return append(dst, saKeyContent(sa)...)
}

// ResolvedThisTurnOf is g.ResolvedThisTurn[ResolvedAbilityKey(source, sa)]
// without building the key string.
func ResolvedThisTurnOf(g *state.Game, source state.ObjID, sa *cards.SA) int32 {
	if len(g.ResolvedThisTurn) == 0 {
		return 0
	}
	var buf [256]byte
	k := appendResolvedAbilityKey(buf[:0], source, sa)
	return g.ResolvedThisTurn[string(k)]
}
