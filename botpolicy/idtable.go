package botpolicy

import (
	"iter"
	"slices"

	"github.com/adams-shaun/gorge/state"
)

// TableKey is an IDTable's key: a dense small-integer engine id.
type TableKey interface {
	~uint32 | ~uint8
}

// IDTable is the Board's map-free keyed table: values stored densely in
// insertion order, with a position index addressed directly by the id. It
// replaces the map[id]T fields the Board carried so a refill costs no
// hashing and a reused Board's refill allocates nothing once its slices
// have grown to the game's size.
//
// It reads like the map it replaced: Get is the map index (the zero T for
// an absent id), Lookup the comma-ok form, Len the map's len, and All
// iterates every entry -- in INSERTION order, which is deterministic. The
// map it replaced iterated in random order, so no reader may depend on the
// order (every reader was already order-independent; the fill order is the
// adapter's own zone-walk order).
//
// The zero IDTable is an empty, usable table. A Board copy shares its
// tables' storage with the original, exactly as a copied map shared its
// buckets: like the maps before them, a table is filled only by the board
// adapters and is valid only until the next refill (BoardFromGameInto's
// ownership contract).
type IDTable[K TableKey, V any] struct {
	keys []K
	vals []V
	// pos[k] is 1 + k's index in keys/vals, 0 when k is absent; it is
	// only ever as long as the largest id Set has seen.
	pos []int32
}

// TableOf builds a table holding m's entries, inserted in ascending key
// order (a test or a caller that only has a map gets a deterministic
// table).
func TableOf[K TableKey, V any](m map[K]V) IDTable[K, V] {
	var t IDTable[K, V]
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		t.Set(k, m[k])
	}
	return t
}

// makeTable is an empty table with room for n entries.
func makeTable[K TableKey, V any](n int) IDTable[K, V] {
	return IDTable[K, V]{keys: make([]K, 0, n), vals: make([]V, 0, n)}
}

// Len is the number of entries.
func (t IDTable[K, V]) Len() int { return len(t.keys) }

// index is k's entry index, or -1. pos is a sparse-set index: an entry is
// present only when its slot points at an entry that names k back, so a
// stale slot (Reset never clears pos) or one written through another copy
// of the Board sharing this storage reads as absent, never as a wrong entry.
func (t IDTable[K, V]) index(k K) int {
	if uint64(k) < uint64(len(t.pos)) {
		if i := int(t.pos[k]) - 1; uint(i) < uint(len(t.keys)) && t.keys[i] == k {
			return i
		}
	}
	return -1
}

// Get is k's value, the zero V when k is absent (a map index).
func (t IDTable[K, V]) Get(k K) V {
	if i := t.index(k); i >= 0 {
		return t.vals[i]
	}
	var zero V
	return zero
}

// Lookup is k's value and whether k is present (a comma-ok map index).
func (t IDTable[K, V]) Lookup(k K) (V, bool) {
	if i := t.index(k); i >= 0 {
		return t.vals[i], true
	}
	var zero V
	return zero, false
}

// Ref is a pointer to k's stored value, nil when k is absent. The pointer
// is valid until the table is next written.
func (t IDTable[K, V]) Ref(k K) *V {
	if i := t.index(k); i >= 0 {
		return &t.vals[i]
	}
	return nil
}

// Has reports whether k is present.
func (t IDTable[K, V]) Has(k K) bool { return t.index(k) >= 0 }

// Set stores v under k, replacing k's value in place when it is present
// and appending a new entry otherwise.
func (t *IDTable[K, V]) Set(k K, v V) { *t.slot(k) = v }

// slot is the storage for k's value -- the existing entry's, or a new
// entry's appended for k -- for a caller that overwrites it whole (Set
// without the by-value copy of a large V). The pointer is valid until the
// table is next written.
func (t *IDTable[K, V]) slot(k K) *V {
	if i := t.index(k); i >= 0 {
		return &t.vals[i]
	}
	if n := int(k) + 1; n > len(t.pos) {
		if n > cap(t.pos) {
			np := make([]int32, n, max(n, 2*cap(t.pos), 64))
			copy(np, t.pos)
			t.pos = np
		} else {
			t.pos = t.pos[:n]
		}
	}
	t.keys = append(t.keys, k)
	var zero V
	t.vals = append(t.vals, zero)
	t.pos[k] = int32(len(t.keys))
	return &t.vals[len(t.vals)-1]
}

// Delete removes k, keeping the remaining entries' relative order.
func (t *IDTable[K, V]) Delete(k K) {
	i := t.index(k)
	if i < 0 {
		return
	}
	t.keys = slices.Delete(t.keys, i, i+1)
	t.vals = slices.Delete(t.vals, i, i+1)
	for j := i; j < len(t.keys); j++ {
		t.pos[t.keys[j]] = int32(j + 1)
	}
}

// Reset empties the table, keeping its storage for the next fill. It is
// O(1): pos keeps its stale slots, which index rejects.
func (t *IDTable[K, V]) Reset() {
	t.keys = t.keys[:0]
	t.vals = t.vals[:0]
}

// Keys is the entries' keys in insertion order. The slice is the table's
// own storage: read it, never write or retain it.
func (t IDTable[K, V]) Keys() []K { return t.keys }

// Values is the entries' values, parallel to Keys. The slice is the
// table's own storage: read it, never write or retain it.
func (t IDTable[K, V]) Values() []V { return t.vals }

// All iterates every entry in insertion order.
func (t IDTable[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for i, k := range t.keys {
			if !yield(k, t.vals[i]) {
				return
			}
		}
	}
}

// Map is the table's entries as a fresh map (tests and diagnostics; it
// allocates).
func (t IDTable[K, V]) Map() map[K]V {
	m := make(map[K]V, len(t.keys))
	for i, k := range t.keys {
		m[k] = t.vals[i]
	}
	return m
}

// Equal reports whether t and u hold the same entries (order-insensitive,
// like map equality).
func (t IDTable[K, V]) Equal(u IDTable[K, V], eq func(a, b V) bool) bool {
	if len(t.keys) != len(u.keys) {
		return false
	}
	for i, k := range t.keys {
		j := u.index(k)
		if j < 0 || !eq(t.vals[i], u.vals[j]) {
			return false
		}
	}
	return true
}

// The Board's four tables.
type (
	CreatureTable  = IDTable[state.ObjID, Creature]
	CardTable      = IDTable[state.ObjID, Card]
	LifeTable      = IDTable[state.PlayerID, int32]
	CommanderTable = IDTable[state.ObjID, Commander]
)
