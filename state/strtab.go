package state

import (
	"slices"
	"strings"
)

// StrEntry is one row of a StrTable.
type StrEntry[T any] struct {
	Key string
	Val T
}

// StrTable is an immutable string-keyed lookup table built once at package
// init: a sorted dense slice answered by binary search, with no map and no
// allocation on a read. It is the compiled form of a `switch s { case "a",
// "b": return X ... }` whose clauses only return constants (spec W4).
type StrTable[T any] struct {
	ents []StrEntry[T]
}

// NewStrTable sorts the entries by key. A duplicate key keeps its first row,
// as the switch it replaces would have matched the first clause.
func NewStrTable[T any](entries ...StrEntry[T]) StrTable[T] {
	e := slices.Clone(entries)
	slices.SortStableFunc(e, func(a, b StrEntry[T]) int {
		switch {
		case a.Key < b.Key:
			return -1
		case a.Key > b.Key:
			return 1
		}
		return 0
	})
	e = slices.CompactFunc(e, func(a, b StrEntry[T]) bool { return a.Key == b.Key })
	return StrTable[T]{ents: e}
}

// Get returns the value stored for key.
func (t StrTable[T]) Get(key string) (T, bool) {
	lo, hi := 0, len(t.ents)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		switch c := strings.Compare(t.ents[m].Key, key); {
		case c == 0:
			return t.ents[m].Val, true
		case c < 0:
			lo = m + 1
		default:
			hi = m
		}
	}
	var z T
	return z, false
}

// NameSet is an immutable set of strings with the same representation.
type NameSet struct {
	names []string
}

// NewNameSet builds a set of names.
func NewNameSet(names ...string) NameSet {
	n := slices.Clone(names)
	slices.Sort(n)
	return NameSet{names: slices.Compact(n)}
}

// Has reports membership by binary search.
func (s NameSet) Has(name string) bool {
	_, ok := slices.BinarySearch(s.names, name)
	return ok
}

// StrCodes maps a closed string vocabulary to dense uint16 codes (1-based; 0
// is "not in the vocabulary"), built once at package init. A string switch
// whose arms do real work dispatches on the code with an integer switch, so
// the string compare happens once, in one binary search, and the vocabulary
// is named in one table.
type StrCodes struct {
	t StrTable[uint16]
}

// NewStrCodes builds the vocabulary from key -> code rows.
func NewStrCodes(entries ...StrEntry[uint16]) StrCodes {
	return StrCodes{t: NewStrTable(entries...)}
}

// Code returns key's code, or 0 when key is outside the vocabulary.
func (c StrCodes) Code(key string) uint16 {
	v, _ := c.t.Get(key)
	return v
}
