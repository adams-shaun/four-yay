package cards

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// lazyCards backs an imaged registry (LoadRegistry, OpenCorpus,
// SharedCorpus): the corpus stays resident as the pointer-free Image, and a
// card's *Card tree is built only when something first touches it. The GC
// then marks the materialized cards (the decks in play) instead of the whole
// ~34k-card tree on every cycle -- the pointer-free corpus spec's §2.3.
//
// Materializing ordinal i is the image's raw (cache-shaped) card put through
// the per-card half of finishDecoded on a private one-card registry -- the
// subset loader's route, which TestImageCorpusOracle holds equal to the
// whole-registry route for every card. The result is published with
// CompareAndSwap, so concurrent callers all get ONE pointer per ordinal for
// the life of the registry (spec §2.5 contract 2): every pointer-keyed memo
// downstream depends on that.
type lazyCards struct {
	img  *Image
	root string // absolute corpus dir paths are re-rooted at; "" = as stored
	mat  []atomic.Pointer[Card]
	n    atomic.Int64

	allOnce sync.Once
	all     []*Card

	catOnce sync.Once
	cat     *CompiledCatalog
}

func newLazyCards(img *Image) *lazyCards {
	return &lazyCards{img: img, mat: make([]atomic.Pointer[Card], len(img.Cards))}
}

func (l *lazyCards) card(i int) *Card {
	if c := l.mat[i].Load(); c != nil {
		return c
	}
	raw := l.img.rawCard(i)
	if raw == nil {
		panic(fmt.Sprintf("cards: imaged registry: ordinal %d out of range", i))
	}
	mini, err := finishDecoded([]*Card{raw}, nil)
	if err != nil {
		panic(fmt.Sprintf("cards: imaged registry: compiling ordinal %d (%s): %v", i, raw.Path, err))
	}
	c := mini.cards[0]
	if l.root != "" {
		rerootCard(c, l.root)
	}
	if l.mat[i].CompareAndSwap(nil, c) {
		l.n.Add(1)
		return c
	}
	return l.mat[i].Load()
}

// allCards materializes every card, in ordinal order. Tools and tests only:
// a runtime path that reaches it re-grows the scan set this mode removes
// (the MaterializedCount ratchet catches that).
func (l *lazyCards) allCards() []*Card {
	l.allOnce.Do(func() {
		all := make([]*Card, len(l.mat))
		for i := range all {
			all[i] = l.card(i)
		}
		l.all = all
	})
	return l.all
}

// lookup answers Lookup from the image's tiered name index (nameIndexOf's
// semantics, recorded as sorted NameKeys with parallel ordinals).
func (l *lazyCards) lookup(key string) (*Card, bool) {
	im := l.img
	lo, hi := 0, len(im.NameKeys)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if string(im.strBytes(im.NameKeys[m])) < key {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo == len(im.NameKeys) || string(im.strBytes(im.NameKeys[lo])) != key {
		return nil, false
	}
	return l.card(int(im.NameOrds[lo])), true
}

// catalog is Registry.Catalog on an imaged registry: the whole-corpus
// catalog, built on demand over a private copy of every card and token so no
// materialized face is ever bound to it (spec §2.5 contract 3). Tests and
// tools only; nothing at runtime reads it.
func (l *lazyCards) catalog() *CompiledCatalog {
	l.catOnce.Do(func() {
		raw := make([]*Card, len(l.img.Cards))
		for i := range raw {
			raw[i] = l.img.rawCard(i)
		}
		toks := make(map[string]*Card, len(l.img.Tokens))
		for i, t := range l.img.Tokens {
			toks[l.img.str(t.Key)] = l.img.rawToken(i)
		}
		full, err := finishDecoded(raw, toks)
		if err != nil {
			panic(fmt.Sprintf("cards: imaged registry: whole-corpus catalog: %v", err))
		}
		l.cat = full.catalog
	})
	return l.cat
}

// strBytes is str without the copy, for comparisons.
func (im *Image) strBytes(id StringID) []byte {
	if id == 0 || int(id) > len(im.Strs) {
		return nil
	}
	r := im.Strs[id-1]
	return im.Blob[r.Offset : r.Offset+r.Length]
}

// imagedRegistry is LoadRegistry's imaged route: decoded holds the cache's
// cards exactly as gob decoded them (T0, before finishDecoded). The image
// keeps them; the trees themselves are dropped by the caller.
func imagedRegistry(decoded []*Card, tokens map[string]*Card) (*Registry, error) {
	img, err := buildImage(decoded, tokens)
	if err != nil {
		return nil, err
	}
	r, err := finishDecoded(nil, tokens)
	if err != nil {
		return nil, err
	}
	r.lazy = newLazyCards(img)
	return r, nil
}

// rerootCard points c's script Path at abs (see rerootPaths).
func rerootCard(c *Card, abs string) {
	if c == nil {
		return
	}
	for _, sub := range []string{"cardsfolder", "tokenscripts"} {
		seg := string(filepath.Separator) + sub + string(filepath.Separator)
		if i := strings.LastIndex(c.Path, seg); i >= 0 {
			c.Path = filepath.Join(abs, c.Path[i+1:])
			return
		}
	}
}
