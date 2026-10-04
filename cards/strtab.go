package cards

import (
	"slices"
	"strings"
)

// StrEntry is one row of a StrTable.
type StrEntry[T any] struct {
	Key string
	Val T
}

// strIndex is an open-addressed hash index over a fixed key list, built once
// at package init: a dense power-of-two slot slice (no map, deterministic,
// no allocation on a read). The hash samples the key's length and four of
// its bytes, so a probe costs a handful of loads plus one string equality;
// the multiplier is chosen at build time to minimise the probe count over
// the table's own keys.
type strIndex struct {
	slots []uint16 // 0 = empty, else 1 + key position
	mask  uint32
	mul   uint32
}

func strHash(s string, mul uint32) uint32 {
	n := len(s)
	h := uint32(n) * 0x9e3779b1
	if n > 0 {
		h = (h ^ uint32(s[0])) * mul
		h = (h ^ uint32(s[n-1])) * mul
		h = (h ^ uint32(s[n>>1])) * mul
		h = (h ^ uint32(s[n>>2])) * mul
	}
	return h ^ h>>16
}

// strIndexMuls are the candidate multipliers (odd, well mixed); the first
// that gives the fewest total probes wins, so the choice is deterministic.
var strIndexMuls = [...]uint32{0x01000193, 0x85ebca6b, 0xc2b2ae35, 0x27d4eb2f, 0x165667b1, 0x9e3779b1, 0x7feb352d, 0x846ca68b}

func buildStrIndex(n int, key func(int) string) strIndex {
	size := 8
	for size < 4*n {
		size <<= 1
	}
	var best strIndex
	bestCost := -1
	for _, mul := range strIndexMuls {
		ix := strIndex{slots: make([]uint16, size), mask: uint32(size - 1), mul: mul}
		cost := 0
		for i := 0; i < n; i++ {
			h := strHash(key(i), mul) & ix.mask
			for ix.slots[h] != 0 {
				h = (h + 1) & ix.mask
				cost++
			}
			ix.slots[h] = uint16(i + 1)
		}
		if bestCost < 0 || cost < bestCost {
			best, bestCost = ix, cost
		}
		if cost == 0 {
			break
		}
	}
	return best
}

// StrTable is an immutable string-keyed lookup table built once at package
// init: a dense entry slice answered through a hashed slot index, with no
// map and no allocation on a read. It is the compiled form of a `switch s {
// case "a", "b": return X ... }` whose clauses only return constants (spec
// W4).
type StrTable[T any] struct {
	ents []StrEntry[T]
	ix   strIndex
}

// NewStrTable sorts the entries by key. A duplicate key keeps its first row,
// as the switch it replaces would have matched the first clause.
func NewStrTable[T any](entries ...StrEntry[T]) StrTable[T] {
	e := slices.Clone(entries)
	slices.SortStableFunc(e, func(a, b StrEntry[T]) int { return strings.Compare(a.Key, b.Key) })
	e = slices.CompactFunc(e, func(a, b StrEntry[T]) bool { return a.Key == b.Key })
	return StrTable[T]{ents: e, ix: buildStrIndex(len(e), func(i int) string { return e[i].Key })}
}

// Get returns the value stored for key.
func (t *StrTable[T]) Get(key string) (T, bool) {
	if len(t.ix.slots) != 0 {
		h := strHash(key, t.ix.mul) & t.ix.mask
		for {
			s := t.ix.slots[h]
			if s == 0 {
				break
			}
			if e := &t.ents[s-1]; e.Key == key {
				return e.Val, true
			}
			h = (h + 1) & t.ix.mask
		}
	}
	var z T
	return z, false
}

// NameSet is an immutable set of strings with the same representation.
type NameSet struct {
	names []string
	ix    strIndex
}

// NewNameSet builds a set of names.
func NewNameSet(names ...string) NameSet {
	n := slices.Clone(names)
	slices.Sort(n)
	n = slices.Compact(n)
	return NameSet{names: n, ix: buildStrIndex(len(n), func(i int) string { return n[i] })}
}

// Has reports membership.
func (s *NameSet) Has(name string) bool {
	if len(s.ix.slots) == 0 {
		return false
	}
	h := strHash(name, s.ix.mul) & s.ix.mask
	for {
		k := s.ix.slots[h]
		if k == 0 {
			return false
		}
		if s.names[k-1] == name {
			return true
		}
		h = (h + 1) & s.ix.mask
	}
}

// StrCodes maps a closed string vocabulary to dense codes of an integer enum
// type (1-based; 0 is "not in the vocabulary"), built once at package init.
// A string switch whose arms do real work dispatches on the code with an
// integer switch, so the string compare happens once and the vocabulary is
// named in one table. Where the string is fixed when an IR node is compiled,
// the code is resolved there and stored on the node instead.
type StrCodes[T ~uint16] struct {
	t StrTable[T]
}

// NewStrCodes builds the vocabulary from key -> code rows.
func NewStrCodes[T ~uint16](entries ...StrEntry[T]) StrCodes[T] {
	return StrCodes[T]{t: NewStrTable(entries...)}
}

// Code returns key's code, or 0 when key is outside the vocabulary.
func (c *StrCodes[T]) Code(key string) T {
	v, _ := c.t.Get(key)
	return v
}
