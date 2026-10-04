package effects

import (
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
)

// The compiled player-spec form. MatchesPlayerSpecCtx used to re-split a spec
// on ',' and '+', trim it, cut each clause at '.', and classify the base and
// qualifier through string-code tables on EVERY seat it was asked about.
// compiledPlayerSpecFor parses each distinct spec string once into an
// immutable playerSpecCompiled carrying that split with every word already
// classified; the evaluators (matchesPlayerCompound, matchesPlayerClause,
// matchesPlayerSingleSpec) walk it and perform exactly the reads they did
// before, in the same order. Values are shared by every game in the process
// and hold no game state (compiledSpecFor's contract).

// playerSpecCompiled is MatchesPlayerSpecCtx's ',' split: each non-blank
// alternative's '+' conjunction.
type playerSpecCompiled struct {
	alts [][]playerClause
}

// playerClause is one '+' clause of an alternative.
type playerClause struct {
	// blank is an empty clause: the conjunction fails.
	blank bool
	// neg is a leading '!'; text is the clause after it, trimmed.
	neg  bool
	text string
	// bare is isBarePlayerProperty(text); bareCode its bare-property code.
	bare     bool
	bareCode matchesPlayerClauseCtxCode
	// dealt is the bare `wasDealtDamageThisGameBy <ref>` clause.
	dealt    bool
	dealtRef string
	// single is matchesPlayerSingleSpec's compiled form of text.
	single []playerAlt
}

// playerAlt is one ',' alternative of a single (no '+') player spec, cut at
// its first '.'.
type playerAlt struct {
	base, qualifier string
	qualified       bool
	baseCode        playerSpecBaseCode
	qualCode        playerSpecQualifierCode
	// negInner is a qualifier "!<inner>"; innerKnown reports that the
	// evaluator reads inner (matchesPlayerSingleSpecKeys). baseOnly and
	// positive are the compiled base and base.inner specs the negation
	// evaluates.
	negInner   bool
	inner      string
	innerKnown bool
	baseOnly   []playerAlt
	positive   []playerAlt
}

func compilePlayerSpec(spec string) *playerSpecCompiled {
	ps := &playerSpecCompiled{}
	for alt := range strings.SplitSeq(spec, ",") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		var clauses []playerClause
		for clause := range strings.SplitSeq(alt, "+") {
			clause = strings.TrimSpace(clause)
			if clause == "" {
				clauses = append(clauses, playerClause{blank: true})
				break
			}
			c := playerClause{}
			if strings.HasPrefix(clause, "!") {
				c.neg = true
				clause = strings.TrimSpace(clause[1:])
			}
			c.text = clause
			c.bare = isBarePlayerProperty(clause)
			c.bareCode = matchesPlayerClauseCtxCodes.Code(clause)
			if ref, is := strings.CutPrefix(clause, "wasDealtDamageThisGameBy "); is {
				c.dealt, c.dealtRef = true, ref
			} else if !c.bare {
				c.single = compilePlayerSingle(clause)
			}
			clauses = append(clauses, c)
		}
		ps.alts = append(ps.alts, clauses)
	}
	return ps
}

func compilePlayerSingle(spec string) []playerAlt {
	var out []playerAlt
	for alt := range strings.SplitSeq(spec, ",") {
		a := playerAlt{}
		a.base, a.qualifier, a.qualified = strings.Cut(strings.TrimSpace(alt), ".")
		a.baseCode = playerSpecBaseCodes.Code(a.base)
		a.qualCode = playerSpecQualifierCodes.Code(a.qualifier)
		if inner, negated := strings.CutPrefix(a.qualifier, "!"); a.qualified && negated {
			a.negInner, a.inner = true, inner
			a.innerKnown = matchesPlayerSingleSpecKeys.Has(inner)
			if a.innerKnown {
				a.baseOnly = compilePlayerSingle(a.base)
				a.positive = compilePlayerSingle(a.base + "." + inner)
			}
		}
		out = append(out, a)
	}
	return out
}

// playerSpecCacheMax bounds the process-wide cache (compiledSpecCacheMax's
// reasoning: specs come from card scripts).
const playerSpecCacheMax = 1 << 14

// playerSpecs is a copy-on-write map (specCache's shape): readers load an
// immutable snapshot with one atomic read; a miss compiles under the mutex
// and republishes once enough misses have accumulated.
var playerSpecs struct {
	ro     atomic.Pointer[map[string]*playerSpecCompiled]
	mu     sync.Mutex
	dirty  map[string]*playerSpecCompiled
	misses int
}

type playerSpecFrontEntry struct {
	spec string
	ps   *playerSpecCompiled
}

// playerSpecFront is a direct-mapped front indexed by the spec's data
// pointer and length (specFront's shape): a spec read from the immutable IR
// passes the same backing array every time, so a hit is one pointer-equal
// string compare.
var playerSpecFront [1 << 11]atomic.Pointer[playerSpecFrontEntry]

func compiledPlayerSpecFor(spec string) *playerSpecCompiled {
	p := uintptr(unsafe.Pointer(unsafe.StringData(spec)))
	h := uint64(p>>3) ^ uint64(len(spec))*0x9e3779b97f4a7c15
	h ^= h >> 29
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 32
	slot := &playerSpecFront[uint(h)&(1<<11-1)]
	if e := slot.Load(); e != nil && e.spec == spec {
		return e.ps
	}
	var ps *playerSpecCompiled
	if m := playerSpecs.ro.Load(); m != nil {
		ps = (*m)[spec]
	}
	if ps == nil {
		ps = playerSpecSlow(spec)
	}
	slot.Store(&playerSpecFrontEntry{spec: spec, ps: ps})
	return ps
}

func playerSpecSlow(spec string) *playerSpecCompiled {
	c := &playerSpecs
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirty == nil {
		c.dirty = map[string]*playerSpecCompiled{}
	}
	ps, ok := c.dirty[spec]
	if !ok {
		ps = compilePlayerSpec(spec)
		if len(c.dirty) >= playerSpecCacheMax {
			return ps
		}
		c.dirty[spec] = ps
	}
	c.misses++
	ro := c.ro.Load()
	if ro == nil || c.misses >= len(*ro) {
		snap := make(map[string]*playerSpecCompiled, len(c.dirty))
		for k, v := range c.dirty {
			snap[k] = v
		}
		c.ro.Store(&snap)
		c.misses = 0
	}
	return ps
}
