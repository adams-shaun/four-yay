package cards

import (
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Interned word ordinals for the two printed-face vocabularies the filter and
// keyword hot paths test membership in: type-line words (Face.Types) and
// keyword heads (KeywordHead of each Face.Keywords line).
//
// Both vocabularies are ASCII-case-folded, so an ordinal test answers exactly
// what the strings.EqualFold scans it replaces answer: a word is interned only
// when it is pure ASCII (ASCII lowercasing is then EqualFold's own relation),
// and a face holding any word that could not be interned never answers from
// its bitset. Ordinals are assigned in first-intern order under a mutex; they
// are process-local labels that never reach an event, a log or an output, so
// their assignment order cannot affect determinism.
//
// A face's sets are derived once at load (Face.derive) and are guarded by the
// identity of the slice they were computed over, like typeStaticsFirst: a
// face struct copy whose Types or Keywords slice was replaced or grown reads
// as unbound and falls back to the string scan.

// TypeWordID is a type-line word's interned ordinal; 0 is "not interned".
type TypeWordID uint16

// KeywordHeadID is a keyword head's interned ordinal; 0 is "not interned".
type KeywordHeadID uint16

const (
	typeWordCap    = 1024
	keywordHeadCap = 512
)

// TypeWordSet is a fixed-size bitset over TypeWordID ordinals.
type TypeWordSet [typeWordCap / 64]uint64

// KeywordHeadSet is a fixed-size bitset over KeywordHeadID ordinals.
type KeywordHeadSet [keywordHeadCap / 64]uint64

// Has reports whether id's bit is set. id 0 is never set.
func (s *TypeWordSet) Has(id TypeWordID) bool {
	return s[id>>6]&(1<<(id&63)) != 0
}

// Has reports whether id's bit is set. id 0 is never set.
func (s *KeywordHeadSet) Has(id KeywordHeadID) bool {
	return s[id>>6]&(1<<(id&63)) != 0
}

type wordInterner struct {
	mu  sync.Mutex
	ids map[string]uint16
	cap int
}

var (
	typeWordInterner    = wordInterner{ids: map[string]uint16{}, cap: typeWordCap}
	keywordHeadInterner = wordInterner{ids: map[string]uint16{}, cap: keywordHeadCap}
)

// foldASCII returns w lowercased when every byte is ASCII, else ok=false.
func foldASCII(w string) (string, bool) {
	upper := false
	for i := 0; i < len(w); i++ {
		c := w[i]
		if c >= 0x80 {
			return "", false
		}
		if 'A' <= c && c <= 'Z' {
			upper = true
		}
	}
	if !upper {
		return w, true
	}
	return strings.ToLower(w), true
}

// intern returns w's ordinal, assigning the next one on first sight. It
// answers 0 for a non-ASCII word or once the vocabulary is full, and callers
// treat 0 as "answer by string".
func (in *wordInterner) intern(w string) uint16 {
	key, ok := foldASCII(w)
	if !ok {
		return 0
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if id, ok := in.ids[key]; ok {
		return id
	}
	// Ordinal 0 is reserved, so the last usable ordinal is cap-1.
	if len(in.ids)+1 >= in.cap {
		return 0
	}
	id := uint16(len(in.ids) + 1)
	in.ids[strings.Clone(key)] = id
	return id
}

// InternTypeWord returns w's type-word ordinal (0 when w cannot be interned).
// It takes a lock: call it at compile or init time, never per match.
func InternTypeWord(w string) TypeWordID { return TypeWordID(typeWordInterner.intern(w)) }

// InternKeywordHead returns the keyword head's ordinal (0 when it cannot be
// interned). Like InternTypeWord it is a compile/init-time call.
func InternKeywordHead(head string) KeywordHeadID {
	return KeywordHeadID(keywordHeadInterner.intern(head))
}

// deriveWordSets computes the face's type-word and keyword-head bitsets.
func (f *Face) deriveWordSets() {
	f.typeWords, f.typeWordsFirst, f.typeWordsLen, f.typeWordsBound = TypeWordSet{}, nil, 0, false
	ok := true
	for _, t := range f.Types {
		id := InternTypeWord(t)
		if id == 0 {
			ok = false
			break
		}
		f.typeWords[id>>6] |= 1 << (id & 63)
	}
	if ok {
		if len(f.Types) > 0 {
			f.typeWordsFirst = &f.Types[0]
		}
		f.typeWordsLen, f.typeWordsBound = len(f.Types), true
	} else {
		f.typeWords = TypeWordSet{}
	}

	f.kwHeads, f.kwHeadsFirst, f.kwHeadsLen, f.kwHeadsBound = KeywordHeadSet{}, nil, 0, false
	ok = true
	for _, k := range f.Keywords {
		id := InternKeywordHead(KeywordHead(k))
		if id == 0 {
			ok = false
			break
		}
		f.kwHeads[id>>6] |= 1 << (id & 63)
	}
	if ok {
		if len(f.Keywords) > 0 {
			f.kwHeadsFirst = &f.Keywords[0]
		}
		f.kwHeadsLen, f.kwHeadsBound = len(f.Keywords), true
	} else {
		f.kwHeads = KeywordHeadSet{}
	}
}

func (f *Face) typeWordsValid() bool {
	if !f.typeWordsBound || f.typeWordsLen != len(f.Types) {
		return false
	}
	return len(f.Types) == 0 || f.typeWordsFirst == &f.Types[0]
}

func (f *Face) kwHeadsValid() bool {
	if !f.kwHeadsBound || f.kwHeadsLen != len(f.Keywords) {
		return false
	}
	return len(f.Keywords) == 0 || f.kwHeadsFirst == &f.Keywords[0]
}

// TypeLineHas reports whether some word of the printed type line equals t
// under strings.EqualFold. id is t's precompiled InternTypeWord ordinal (0
// when the caller has none): with a bound face and a nonzero id the answer is
// one bit test, otherwise it is the string scan.
func (f *Face) TypeLineHas(t string, id TypeWordID) bool {
	if id != 0 && f.typeWordsValid() {
		return f.typeWords.Has(id)
	}
	for _, x := range f.Types {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	return false
}

// KeywordLinesHaveHead reports whether some printed keyword line's head
// (KeywordHead) equals head under strings.EqualFold -- HasKeyword's line
// scan. id is head's precompiled InternKeywordHead ordinal (0 = none).
func (f *Face) KeywordLinesHaveHead(head string, id KeywordHeadID) bool {
	if id != 0 && f.kwHeadsValid() {
		return f.kwHeads.Has(id)
	}
	for _, x := range f.Keywords {
		if strings.EqualFold(KeywordHead(x), head) {
			return true
		}
	}
	return false
}

// wordFrontBits sizes the direct-mapped front caches TypeWordIDOf and
// KeywordHeadIDOf consult before the interner's lock.
const wordFrontBits = 10

type wordFrontEntry struct {
	s  string
	id uint16
}

var (
	typeWordFront    [1 << wordFrontBits]atomic.Pointer[wordFrontEntry]
	keywordHeadFront [1 << wordFrontBits]atomic.Pointer[wordFrontEntry]
)

// wordFrontSlot hashes a string by its data pointer and length: a caller
// passing the same literal or IR string hits its slot with one pointer-equal
// comparison. A different string with equal text lands elsewhere or evicts;
// either way the entry is re-checked by content, so a slot can only ever
// answer the ordinal of the exact text it holds.
func wordFrontSlot(s string) uint {
	p := uintptr(unsafe.Pointer(unsafe.StringData(s)))
	h := uint64(p>>3) ^ uint64(len(s))*0x9e3779b97f4a7c15
	h ^= h >> 29
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 32
	return uint(h) & (1<<wordFrontBits - 1)
}

func wordIDOf(front *[1 << wordFrontBits]atomic.Pointer[wordFrontEntry], in *wordInterner, s string) uint16 {
	slot := &front[wordFrontSlot(s)]
	if e := slot.Load(); e != nil && e.s == s {
		return e.id
	}
	id := in.intern(s)
	slot.Store(&wordFrontEntry{s: s, id: id})
	return id
}

// TypeWordIDOf is InternTypeWord behind a lock-free front cache keyed by the
// string's identity, for a hot caller holding a STABLE string (a literal or
// configured card text): a repeat call costs a pointer hash and compare. A
// caller building strings at run time should precompile the ordinal instead.
func TypeWordIDOf(s string) TypeWordID {
	return TypeWordID(wordIDOf(&typeWordFront, &typeWordInterner, s))
}

// KeywordHeadIDOf is InternKeywordHead behind the same front cache.
func KeywordHeadIDOf(s string) KeywordHeadID {
	return KeywordHeadID(wordIDOf(&keywordHeadFront, &keywordHeadInterner, s))
}
