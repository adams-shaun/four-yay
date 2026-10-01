package cards

import (
	"sync/atomic"
	"unsafe"
)

// Slot is a write-once place on an IR node where a downstream package hangs
// its compiled form of one of the node's immutable texts, so a hot read
// follows a pointer instead of hashing the text into a side table. An entry
// is keyed by the exact text it was compiled from: a node copy whose text
// was rewritten (and that shares the original's slot) never reads the
// original's entry. The stored value must be immutable and a pure function
// of the text; concurrent first stores race benignly (the first wins and
// every reader then sees it).
type Slot struct {
	p atomic.Pointer[slotBox]
}

type slotBox struct {
	key string
	v   any
}

// Load returns the entry compiled from key, if any.
func (s *Slot) Load(key string) (any, bool) {
	if s == nil {
		return nil, false
	}
	if b := s.p.Load(); b != nil && b.key == key {
		return b.v, true
	}
	return nil, false
}

// Store publishes v as key's entry when the slot is empty and returns the
// entry readers will see for key: the published one, or v itself when the
// slot already holds another key's entry.
func (s *Slot) Store(key string, v any) any {
	if s == nil {
		return v
	}
	if s.p.CompareAndSwap(nil, &slotBox{key: key, v: v}) {
		return v
	}
	if b := s.p.Load(); b != nil && b.key == key {
		return b.v
	}
	return v
}

// ManaCostSlot is the face's slot for a compiled form of its ManaCost text
// (nil on a face never derived).
func (f *Face) ManaCostSlot() *Slot { return f.manaCostSlot }

// ExtSlot is a write-once pointer place on an IR node where a downstream
// package hangs its own compiled facts about that node, so a hot read
// follows a pointer instead of hashing the node into a side table. The value
// is opaque here. A by-value node copy shares its original's slot, so a
// reader must check that the stored facts are the node's own (they name the
// node they were computed for) and treat anything else as absent; concurrent
// first stores race benignly (the first wins).
type ExtSlot struct {
	p unsafe.Pointer
}

// Load returns the published value, or nil.
func (s *ExtSlot) Load() unsafe.Pointer {
	if s == nil {
		return nil
	}
	return atomic.LoadPointer(&s.p)
}

// Store publishes v when the slot is empty and reports whether it did.
func (s *ExtSlot) Store(v unsafe.Pointer) bool {
	if s == nil {
		return false
	}
	return atomic.CompareAndSwapPointer(&s.p, nil, v)
}

// ExtSlot is the face's downstream facts slot (nil on a face never derived).
func (f *Face) ExtSlot() *ExtSlot { return f.extSlot }

// ExtSlot is the ability's downstream facts slot (nil on an ability never
// bound by its face's derive).
func (sa *SA) ExtSlot() *ExtSlot { return sa.extSlot }
