package rules

import (
	"fmt"
	"slices"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Derived memo for one legal-actions walk.
//
// legalActionsPriced (the priority offer walk and its potential-action twin)
// asks Derived for the same objects over and over: every cost/restriction
// static's ValidCard$ match goes through matchesSpec, which reads the
// candidate's derived keywords, and HasKeyword, derivedKeywordParam and the
// granted-cycling/cumulative/mana-ability collectors each derive the same
// object again. Measured on botbench -pairs all -games 4 -seed 13 (27 decks),
// 39.3M of the 68.9M derivedWith calls ran inside a legal-actions walk and
// 29.7M of those (75.5%) repeated an object already derived earlier in the
// SAME walk; outside the walk the repeat rate is ~11%, so the memo is scoped
// to the walk and nowhere else.
//
// Why the walk scope and not a global per-epoch cache: Derived's inputs are
// the board (e.G, moved only by events.Apply, so len(e.L.Events) keys it),
// e.continuous (keyed by continuousVersion, exactly active()'s cache key), and
// a set of NON-event engine runtime fields that the layer walk reads
// transitively -- the rename table and its renameBuilding guard, the layer-4
// type table and typesBuilding, the effectMatchOverride observer binding,
// goadProbe, derivingColors*, activeDepth/derivedDepth, and whatever an
// EvalCount host method reads for a layer-7 amount. A global cache would have
// to prove every one of those constant across an epoch (and tests mutate
// e.G directly with no event at all). legalActionsPriced is documented and
// written as a pure read -- it emits no event and writes no state field -- so
// inside one walk every one of those inputs is fixed, and the memo only has to
// be invalidated by the walk's own boundaries:
//
//   - derivedMemoGen is bumped on every OUTERMOST scope entry, so an entry
//     built by an earlier walk is never served (whatever changed between the
//     two walks, event-backed or not, is irrelevant);
//   - each entry also records (len(e.L.Events), continuousVersion,
//     len(e.G.Objs)) and must match all three, so even an emit or a
//     continuous-registry write inside a walk (none exists today) would miss
//     rather than serve a stale result;
//   - derivedMemoUsable bypasses the memo (neither read nor written) whenever
//     one of the runtime guards above is live, i.e. for any Derived that is
//     nested inside another derivation, an active() build, a rename/type
//     table build, a goad probe or an Effect observer match. The atStack
//     zone-override path (convoke's AffectedZone$ Stack read) is memoized
//     in its own table (derivedMemoStack), keyed and guarded identically.
//
// Ownership: a cached result's Keywords/Types are copied out of the shared
// derivedKW/derivedTypes scratch into the entry's own backing arrays, so a
// later Derived of ANOTHER object (which rewrites the scratch) can never
// mutate a slice a caller is still ranging. An entry's arrays are rewritten
// only when the same ObjID misses again, which within one walk cannot happen
// (the key cannot move), and callers never retain a Derived result past the
// walk (Derived's documented contract). The arrays are reused across walks,
// so the steady state stays allocation-free.
//
// The copy keeps the computed slice's nil-ness (memoOwned). An EMPTY derived
// keyword list is an answer -- "no keywords after layer 6" -- while a nil one
// is effects.SpecContext's "unbound" (matchesSpec binds d.Keywords as
// ExtraKeywords, and a nil ExtraKeywords falls back to the printed face). A
// plain append into an entry that never held a keyword collapsed a computed
// empty list to nil, so Surge Engine, its printed Defender removed by its own
// Animate, matched Card.Self+!withDefender only in an engine whose memo entry
// had been filled while it still had Defender: a replay or a Clone offered a
// different priority option list than the live game (round-8 fuzz).
//
// derivedMemoVerify (set by the rules test binary, or at link time through
// derivedMemoVerifyFlag for a botbench run) recomputes every hit and panics
// on any difference -- the empirical half of the argument above.

// Carrying the priority walk across the decision boundary.
//
// A bot seat answers a priority decision by building its board
// (botpolicy.BoardFromGameInto), which derives every battlefield creature and
// every card in the seat's own zones -- the objects askPriority's offer walk
// derived a moment earlier, at the same board. BeginDerivedReads /
// EndDerivedReads open a memo scope for such an external pure read, and when
// the engine is provably still at the state that walk read, the scope RESUMES
// the walk's generation instead of starting a new one, so those Derived
// results are served rather than recomputed. The resume condition
// (derivedMemoTailLive) is:
//
//   - askPriority recorded the tail right after its walk and before its ask,
//     and the decision it posed is still e.pending -- pointer identity, so no
//     Submit has run since (Submit clears pending before anything else) and
//     no other ask replaced it;
//   - derivedMemoGen is still the walk's, so no other scope opened since;
//   - continuousVersion and len(e.G.Objs) are unchanged;
//   - every event logged since the walk is a DecisionAsk marker (ask's own
//     emit), whose Apply writes nothing.
//
// Between that walk and the ask, askPriority runs no code, and ask's emit of
// the marker is the only engine work; the non-event runtime inputs the walk
// contract lists (rename/type tables, observer bindings, goad probes, depth
// counters) are all at rest outside a derivation, as they were when the walk
// read them. The walk's entries were keyed at the walk's log length, which
// the marker has since grown, so the resumed scope aliases exactly that pair:
// an entry keyed at the walk's length is valid while the log is at the
// scope's opening length (derivedMemoAliasFrom/To), and any further emit
// still misses. Anything else -- a non-priority decision, a Submit, a
// direct test write to e.G without an event -- opens a fresh generation
// exactly as a walk does (a direct write between an ask and a board build is
// the one input the argument cannot see; verify mode below, which recomputes
// every hit, is what checks the suite never does that).
//
// Submit refuses to run inside a BeginDerivedReads scope: the scope promises
// a pure read.

type derivedMemoTail struct {
	gen     uint64
	ep      int
	ver     int
	objs    int
	pending *decision.Decision
}

func (e *Engine) recordDerivedMemoTail(d *decision.Decision) {
	e.derivedMemoTail = derivedMemoTail{gen: e.derivedMemoGen, ep: len(e.L.Events),
		ver: e.continuousVersion, objs: len(e.G.Objs), pending: d}
}

func (e *Engine) derivedMemoTailLive() bool {
	t := &e.derivedMemoTail
	if t.pending == nil || t.pending != e.pending || t.gen != e.derivedMemoGen || t.gen == 0 ||
		t.ver != e.continuousVersion || t.objs != len(e.G.Objs) || t.ep > len(e.L.Events) {
		return false
	}
	for _, ev := range e.L.Events[t.ep:] {
		if ev.Kind != events.DecisionAsk {
			return false
		}
	}
	return true
}

// BeginDerivedReads opens a Derived memo scope for a caller's pure read of
// the board (botpolicy's board build); EndDerivedReads closes it. Between
// the two the caller must not Submit or otherwise change engine state, and
// must not retain a Derived/Characteristics slice past the next query (the
// ordinary Derived contract). At an outermost scope where the priority offer
// walk's memo is still exact (see above) the walk's entries are served.
func (e *Engine) BeginDerivedReads() {
	if e.derivedMemoDepth == 0 && e.derivedMemoTailLive() {
		e.derivedMemoDepth++
		e.derivedMemoAliasFrom, e.derivedMemoAliasTo = e.derivedMemoTail.ep, len(e.L.Events)
		return
	}
	e.beginDerivedMemo()
}

// EndDerivedReads closes a BeginDerivedReads scope.
func (e *Engine) EndDerivedReads() {
	e.endDerivedMemo()
	if e.derivedMemoDepth == 0 {
		e.derivedMemoAliasFrom, e.derivedMemoAliasTo = 0, 0
	}
}

// derivedMemoVerifyFlag turns verify mode on in a non-test binary:
// go build -ldflags "-X github.com/adams-shaun/gorge/rules.derivedMemoVerifyFlag=1".
var derivedMemoVerifyFlag string

var derivedMemoVerify = derivedMemoVerifyFlag != ""

type derivedMemoEntry struct {
	gen  uint64
	ep   int
	ver  int
	objs int
	// seq is activeBuildSeq when the entry was computed (0: not eligible for
	// cross-walk reuse); see derivedMemoizedAt.
	seq uint64
	d   Derived
	kw  []string
	ty  []string
}

// beginDerivedMemo opens a memo scope; endDerivedMemo closes it. Only the
// outermost entry starts a new generation, so a nested walk shares the outer
// walk's entries.
func (e *Engine) beginDerivedMemo() {
	if e.derivedMemoDepth == 0 {
		e.derivedMemoGen++
	}
	e.derivedMemoDepth++
}

func (e *Engine) endDerivedMemo() { e.derivedMemoDepth-- }

// derivedMemoUsable reports whether this Derived call is a top-level read
// whose every non-event input is the walk's fixed state (see above).
func (e *Engine) derivedMemoUsable() bool {
	return e.derivedDepth == 0 && e.activeDepth == 0 && !e.renameBuilding && !e.typesBuilding &&
		!e.effectMatchOverride && e.goadProbe == 0 && !e.derivingColorsSet
}

func (e *Engine) derivedMemoized(id state.ObjID) Derived {
	return e.derivedMemoizedAt(id, 0)
}

// derivedMemoizedAt serves both the live-zone derivation (atStack 0) and the
// stack zone override (atStack ZStack, the convoke/improvise/conspire/
// offspring cast-keyword reads, which the offer walk makes for every hand
// card). Each has its own table: the override is a different question about
// the same object, with the same walk-fixed inputs.
func (e *Engine) derivedMemoizedAt(id state.ObjID, atStack state.Zone) Derived {
	if id == 0 || int(id) > len(e.G.Objs) {
		return e.derivedCompute(id, atStack)
	}
	table := &e.derivedMemo
	if atStack != 0 {
		table = &e.derivedMemoStack
	}
	if n := len(e.G.Objs) + 1; len(*table) < n {
		if cap(*table) < n {
			// Size the table to the arena's own capacity (newWithRNG reserves
			// headroom for minted objects), so it is allocated once per game
			// rather than regrown -- with a temporary -- on each new object.
			grown := make([]derivedMemoEntry, len(*table), max(n, cap(e.G.Objs)+1, 2*cap(*table)))
			copy(grown, *table)
			*table = grown
		}
		// The table only ever grows, so [len, cap) was never written: the
		// entries exposed here are zero, exactly what the append added.
		*table = (*table)[:n]
	}
	m := &(*table)[id]
	ep, ver, objs := len(e.L.Events), e.continuousVersion, len(e.G.Objs)
	epOK := m.ep == ep || (m.ep == e.derivedMemoAliasFrom && ep == e.derivedMemoAliasTo)
	if m.gen == e.derivedMemoGen && epOK && m.ver == ver && m.objs == objs {
		if derivedMemoVerify {
			e.verifyDerivedMemo(id, atStack, m.d)
		}
		return m.d
	}
	// Cross-walk reuse: an entry an EARLIER scope computed is still exact when
	// active() has not been rebuilt since (activeBuildSeq unchanged after
	// bringing active() up to date). active() rebuilds on every event that is
	// not layer-inert (layercache.go: DecisionAsk, DecisionMade and Priority
	// write nothing the layer path reads), on a continuousVersion move, on an
	// object-count move and on the explicit scratch invalidations
	// (cascade.go, trigger_queue.go), so an unchanged count means every
	// event-backed input of this object's derivation -- its own fields, the
	// board the effects read, the registry -- is what it was when the entry
	// was built. The non-event runtime inputs (rename/type tables, observer
	// bindings, goad probe, depth guards) are at rest at both reads:
	// derivedMemoUsable gates every memo read and write. The entry is then
	// re-stamped into this scope (gen/ep), so a same-scope key move still
	// takes the fresh-arrays path below. Measured: the priority walk and the
	// bot's board build re-derived the same objects across every priority
	// pass of an unchanged board. Verify mode recomputes every such hit.
	if m.seq != 0 && m.gen != e.derivedMemoGen && m.ver == ver && m.objs == objs {
		e.active()
		if m.seq == e.activeBuildSeq {
			if derivedMemoVerify {
				e.verifyDerivedMemo(id, atStack, m.d)
			}
			m.gen, m.ep = e.derivedMemoGen, ep
			return m.d
		}
	}
	if m.gen == e.derivedMemoGen {
		// Same walk, moved key (unreachable today: the walk emits nothing).
		// A caller of this walk may still be ranging the old arrays, so
		// the rebuild gets fresh ones instead of rewriting them in place.
		m.kw, m.ty = nil, nil
	}
	e.active()
	seq := e.activeBuildSeq
	d := e.derivedCompute(id, atStack)
	if e.activeBuildSeq != seq {
		seq = 0 // active() rebuilt mid-derivation: never reuse across walks
	}
	d.Keywords = memoOwned(&m.kw, d.Keywords)
	d.Types = memoOwned(&m.ty, d.Types)
	m.d = d
	m.gen, m.ep, m.ver, m.objs, m.seq = e.derivedMemoGen, ep, ver, objs, seq
	return d
}

// memoOwned copies src into the entry's reusable backing array *buf and
// returns the copy with src's nil-ness: nil only for a nil src, a non-nil
// (possibly empty) slice otherwise. See "The copy keeps the computed slice's
// nil-ness" above.
func memoOwned(buf *[]string, src []string) []string {
	*buf = append((*buf)[:0], src...)
	if src == nil {
		return nil
	}
	if *buf == nil {
		return []string{}
	}
	return *buf
}

func (e *Engine) verifyDerivedMemo(id state.ObjID, atStack state.Zone, got Derived) {
	want := e.derivedCompute(id, atStack)
	if (got.Keywords == nil) != (want.Keywords == nil) || (got.Types == nil) != (want.Types == nil) ||
		got.Power != want.Power || got.Toughness != want.Toughness ||
		got.BasePower != want.BasePower || got.BaseToughness != want.BaseToughness ||
		got.Name != want.Name ||
		got.Colors != want.Colors || !slices.Equal(got.Keywords, want.Keywords) || !slices.Equal(got.Types, want.Types) {
		panic(fmt.Sprintf("rules: derived memo stale for obj %d: cached %+v, fresh %+v", id, got, want))
	}
}

// retireCrossWalkMemo makes every existing memo entry ineligible for
// cross-walk reuse, and every entry built from here until the next call
// ineligible after it, by moving activeBuildSeq without rebuilding active().
// It is for the engine's few no-event runtime inputs to Derived (faceprobe.go's
// face flip, statics.go's cost-composition exclusion): an event reaches
// active() on its own, these do not.
func (e *Engine) retireCrossWalkMemo() { e.activeBuildSeq++ }
