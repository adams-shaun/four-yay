package effects

import (
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The name-card universe is ~24k compiled cards, shared read-only by every
// game an embedder starts from one registry (botbench runs thousands of
// games in parallel from one reg.Cards). Rebuilding its sorted name list at
// every genesis, re-filtering it at every NameCard ask and re-wrapping it in
// a fresh decision.Option slice per ask was the bulk of a constructed game's
// allocation, so the three derived lists are memoised here, process-wide,
// keyed by the IDENTITY of the slices they derive from.
//
// Identity keying is sound because every input is immutable by contract:
// state.Game.NameUniverse is "the immutable compiled card-name universe", a
// persisted NameUniverseNames is a pinned snapshot nothing writes into, and
// every list this cache hands out is itself shared and READ-ONLY -- a caller
// that needs to modify one copies it first. A key holds its backing array
// alive, so an address can never be reused by a different slice while its
// entry exists (no ABA). The cache is bounded: on overflow it is dropped
// wholesale, which only costs a recomputation.
//
// Determinism: every memoised value is a pure function of its key (the
// filter memo admits only specs pureNameSpec proves read nothing but the
// printed face), so a hit returns exactly what a recomputation would.

// sliceKey identifies a slice by its first element's address and length.
type sliceKey struct {
	first any
	n     int
}

func cardsKey(u *cards.Universe) sliceKey {
	if u.Len() == 0 {
		return sliceKey{}
	}
	return sliceKey{first: u, n: u.Len()}
}

func namesKey(s []string) sliceKey {
	if len(s) == 0 {
		return sliceKey{}
	}
	return sliceKey{first: &s[0], n: len(s)}
}

type nameFilterKey struct {
	universe, snapshot   sliceKey
	spec, chooseFromList string
	strict               bool
}

type nameOptionsKey struct {
	names  sliceKey
	player state.PlayerID
}

// nameCacheLimit bounds each memo. Real embedders hold one or two
// universes; tests build many tiny ones.
const nameCacheLimit = 256

var nameCache struct {
	mu       sync.Mutex
	filtered map[nameFilterKey][]string
	// shared marks every names slice this cache handed out, so the options
	// memo only ever keys on a list whose identity is stable and immutable.
	shared  map[sliceKey]bool
	options map[nameOptionsKey][]decision.Option
}

// resetNameCacheLocked drops every memo. The caller holds nameCache.mu.
func resetNameCacheLocked() {
	nameCache.filtered = nil
	nameCache.shared = nil
	nameCache.options = nil
}

func rememberSharedLocked(names []string) {
	if len(names) == 0 {
		return
	}
	if nameCache.shared == nil {
		nameCache.shared = make(map[sliceKey]bool)
	}
	nameCache.shared[namesKey(names)] = true
}

// NameUniverseNames returns the sorted, distinct primary-face-name list a
// live match snapshots at genesis (cards.Universe.Names, memoised on the
// universe). The result is SHARED: callers must treat it as read-only.
func NameUniverseNames(universe *cards.Universe) []string {
	out := universe.Names()
	nameCache.mu.Lock()
	rememberSharedLocked(out)
	nameCache.mu.Unlock()
	return out
}

// cachedNameChoices returns the memoised filter result for key, computing it
// with build on a miss. The stored list is capacity-clipped so an append by
// a careless caller reallocates rather than writing into the shared array.
func cachedNameChoices(key nameFilterKey, build func() []string) []string {
	nameCache.mu.Lock()
	if out, ok := nameCache.filtered[key]; ok {
		nameCache.mu.Unlock()
		return out
	}
	nameCache.mu.Unlock()
	out := build()
	out = out[:len(out):len(out)]
	nameCache.mu.Lock()
	defer nameCache.mu.Unlock()
	if prev, ok := nameCache.filtered[key]; ok {
		return prev
	}
	if len(nameCache.filtered) >= nameCacheLimit {
		resetNameCacheLocked()
	}
	if nameCache.filtered == nil {
		nameCache.filtered = make(map[nameFilterKey][]string)
	}
	nameCache.filtered[key] = out
	rememberSharedLocked(out)
	return out
}

// NameOptions wraps a NameCard ask's names in its decision options: Index i,
// Kind "name", Label names[i], Player player -- the one shape both the
// mid-resolution ask (effNameCard) and the as-enters ask (rules.etbOptions)
// post. When names is a list this cache handed out, the option slice is
// memoised per (list, player) and SHARED: no consumer writes into a name
// ask's options (the in-place Option writers are the combat and mutate
// decisions), and the slice is capacity-clipped so an append reallocates.
func NameOptions(names []string, player state.PlayerID) []decision.Option {
	if len(names) == 0 {
		return nil
	}
	key := nameOptionsKey{names: namesKey(names), player: player}
	nameCache.mu.Lock()
	shared := nameCache.shared[key.names]
	if shared {
		if out, ok := nameCache.options[key]; ok {
			nameCache.mu.Unlock()
			return out
		}
	}
	nameCache.mu.Unlock()
	out := make([]decision.Option, len(names))
	for i, name := range names {
		out[i] = decision.Option{Index: i, Kind: "name", Label: name, Player: player}
	}
	if !shared {
		return out
	}
	nameCache.mu.Lock()
	defer nameCache.mu.Unlock()
	if !nameCache.shared[key.names] {
		return out // the cache was reset meanwhile; do not key on a stale list.
	}
	if prev, ok := nameCache.options[key]; ok {
		return prev
	}
	if len(nameCache.options) >= nameCacheLimit {
		nameCache.options = nil
	}
	if nameCache.options == nil {
		nameCache.options = make(map[nameOptionsKey][]decision.Option)
	}
	nameCache.options[key] = out
	return out
}

// pureNameBases and pureNameWords are the spec vocabulary pureNameSpec
// admits: printed card types and supertypes, answered from the printed face
// alone by the filter's type predicates.
var pureNameBases = map[string]bool{
	"card": true, "creature": true, "land": true, "artifact": true,
	"enchantment": true, "planeswalker": true, "instant": true,
	"sorcery": true, "battle": true,
}

var pureNameWords = map[string]bool{
	"land": true, "creature": true, "artifact": true, "enchantment": true,
	"planeswalker": true, "instant": true, "sorcery": true, "battle": true,
	"basic": true, "legendary": true, "snow": true,
}

// pureNameSpec reports whether a NameCard ValidCards$ spec is a pure
// printed-type filter -- `Base[.w1+w2...]` with each w a type/supertype word
// or its non- negation -- whose verdict for a bare universe card cannot
// depend on game state, so its filtered list may be memoised across games.
// Anything else (cmcEQX, ManaCost=Imprinted, a comma alternative, a
// !-negation) is recomputed per ask.
func pureNameSpec(spec string) bool {
	if spec == "" {
		return true
	}
	base, rest, hasRest := strings.Cut(spec, ".")
	if !pureNameBases[strings.ToLower(base)] {
		return false
	}
	if !hasRest {
		return true
	}
	for w := range strings.SplitSeq(rest, "+") {
		lw := strings.ToLower(w)
		lw = strings.TrimPrefix(lw, "non")
		if !pureNameWords[lw] {
			return false
		}
	}
	return true
}
