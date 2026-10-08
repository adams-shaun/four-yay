package rules

import (
	"fmt"
	"maps"
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// Walk-scoped caches beside the Derived memo (rules/derivedmemo.go).
//
// The Derived memo's argument is that a legal-actions walk (and a
// BeginDerivedReads scope) is a pure read: it emits no event and writes no
// state field, so every input of a board-only question is fixed for the
// walk's duration. The caches here reuse exactly that scope and exactly that
// key -- the memo generation (bumped on every outermost scope entry, so no
// entry outlives its walk) plus (len(e.L.Events), continuousVersion,
// len(e.G.Objs)), with the priority-tail alias the memo serves -- for other
// per-walk invariants whose answer depends on the board only:
//
//   - boardStatics: ONE whole-board pile-static walk that fills the three
//     membership snapshots a walk needs -- the cost-modifier statics
//     (collectCostStatics), the cast/activation restriction and Continuous
//     statics (collectActionStatics) and the ManaConvert sources below. The
//     three collectors walked the same seats, zones, objects and piles in the
//     same order, each keeping its own gates, so one walk dispatching on
//     Mode$ yields each list in its collector's exact order.
//   - manaConvSources: the printed S:Mode$ ManaConvert statics on every
//     static-source zone. manaConversionParts ran that whole-board pile scan
//     on EVERY payability check (paymentConv sits under castable,
//     manaAbilityPayable and the mana-feasibility walks), although the list
//     of sources is a function of the board alone; only the per-payment
//     filter (ValidPlayer$/ValidCard$/AffectedZone$/ValidSA$) reads the payer
//     and subject, and that part still runs per call.
//   - activeStatics(mode): the battlefield S:Mode$ <mode> statics, one list
//     per mode. Every cost/restriction/grant reader re-walked the battlefield
//     piles for it (PayLifeInsteadOfB alone does so on every
//     payability check). Returned CLIPPED, so a caller that appends to it
//     (castRestrictionSources) reallocates instead of writing the cache.
//   - mayPlaysThisTurn(p): the per-turn may-play count, a backward log scan
//     to the turn's start that every may-play gate re-ran per candidate card.
//
// Outside a scope nothing is cached (the scan runs exactly as before). In the
// rules test binary walkCacheVerify recomputes every hit and panics on any
// difference, the empirical half of the purity argument (the same check the
// Derived memo runs).

// walkCacheVerify: see derivedMemoVerify. Set by the rules test binary.
var walkCacheVerify = derivedMemoVerifyFlag != ""

// walkKey is one cache entry's validity key.
type walkKey struct {
	gen  uint64
	ep   int
	ver  int
	objs int
}

// walkKeyNow returns the current scope's key, or ok=false outside a memo
// scope. The caches here hold board-only answers (no layer-walk runtime field
// is an input), so unlike derivedMemoUsable no derivation guard applies.
func (e *Engine) walkKeyNow() (walkKey, bool) {
	if e.derivedMemoDepth == 0 || e.derivedMemoGen == 0 {
		return walkKey{}, false
	}
	return walkKey{gen: e.derivedMemoGen, ep: len(e.L.Events), ver: e.continuousVersion, objs: len(e.G.Objs)}, true
}

// walkKeyHit reports whether an entry keyed k is valid at now.
func (e *Engine) walkKeyHit(k, now walkKey) bool {
	if k.gen != now.gen || k.ver != now.ver || k.objs != now.objs {
		return false
	}
	return k.ep == now.ep || (k.ep == e.derivedMemoAliasFrom && now.ep == e.derivedMemoAliasTo)
}

// manaConvSource is one printed ManaConvert static found by the board scan.
type manaConvSource struct {
	sv staticView
}

// boardStatics is one walk's fused static membership (see above).
type boardStatics struct {
	cost     costStaticViews
	action   actionStaticViews
	manaConv []manaConvSource
}

type boardStaticsCache struct {
	key walkKey
	// seq is activeBuildSeq when v was scanned (0: not eligible for
	// cross-walk reuse); see walkCrossHit.
	seq uint64
	v   boardStatics
	// scan records the lists v's printed part was scanned over and
	// printed[i] the printed prefix length of v.cost's raise, reduce, set
	// and optional lists (the Effect-delivered statics follow it):
	// static_scan_reuse.go.
	scan    staticScanRec
	printed [4]int
}

// walkCrossHit reports whether an entry an EARLIER scope built at key k and
// activeBuildSeq seq is still exact now -- the Derived memo's cross-walk
// argument (derivedmemo.go) applied to a board-only walk cache. active()
// rebuilds (moving activeBuildSeq) on every event that is not layer-inert
// (DecisionAsk, DecisionMade and Priority write nothing but the priority
// bookkeeping), on a continuousVersion move, on an object-count move and on
// the explicit no-event invalidations (retireCrossWalkMemo: the face probe's
// flip, the cost-composition exclusion), so an unchanged count after bringing
// active() up to date means every object's zone, controller, face, face-down
// status and pile, and the continuous registry, are what they were at the
// build. Only a top-level read qualifies (never inside an active() build),
// and verify mode recomputes every such hit.
func (e *Engine) walkCrossHit(k walkKey, seq uint64, now walkKey) bool {
	if k.gen == 0 || seq == 0 || k.gen == now.gen || k.ver != now.ver || k.objs != now.objs || e.activeDepth != 0 {
		return false
	}
	e.active()
	return seq == e.activeBuildSeq
}

// walkBuildSeq brings active() up to date and returns the activeBuildSeq a
// cache entry built now should record, or 0 inside an active() build (such an
// entry is never reused across scopes).
func (e *Engine) walkBuildSeq() uint64 {
	if e.activeDepth != 0 {
		return 0
	}
	e.active()
	return e.activeBuildSeq
}

// boardStaticsWalk returns the walk's fused static membership, or ok=false
// outside a walk. Every returned slice is clipped, so a caller that appends
// (castRestrictionSources) reallocates instead of writing the cache.
func (e *Engine) boardStaticsWalk() (boardStatics, bool) {
	now, ok := e.walkKeyNow()
	if !ok {
		return boardStatics{}, false
	}
	c := &e.boardStaticsCache
	switch {
	case c.key.gen != 0 && e.walkKeyHit(c.key, now):
		if walkCacheVerify {
			e.verifyBoardStatics(c.v)
		}
	case e.walkCrossHit(c.key, c.seq, now):
		// An earlier walk's scan at an unchanged board (walkCrossHit):
		// re-stamp it into this scope.
		if walkCacheVerify {
			e.verifyBoardStatics(c.v)
		}
		c.key = now
	default:
		// A rebuild inside the scope that built the entry takes fresh
		// backing, so no slice a caller of this scope may still be ranging
		// is ever rewritten. An entry from an EARLIER outermost scope is
		// dead -- the memo generation moves only on an outermost entry, so
		// that scope has returned, and every holder of these views (the
		// walk's costStaticSource/actionStaticSource, the readers it called)
		// lived inside it -- so its arrays are refilled instead of regrown.
		seq := e.walkBuildSeq()
		earlier := c.key.gen != 0 && c.key.gen != now.gen
		lists := e.gatherBoardScan()
		if earlier && e.boardScanMatches(&c.scan, lists) {
			// The printed scan of an unchanged static board
			// (static_scan_reuse.go): keep it, and recompute only the
			// Effect-delivered cost statics after it.
			c.v.cost = e.reappendEffectCostStatics(c.v.cost, c.printed)
			if walkCacheVerify {
				e.verifyBoardStatics(c.v)
			}
		} else {
			var into boardStatics
			if earlier {
				into = boardStaticsArrays(c.v)
			}
			c.v = e.scanBoardStaticsPrintedLists(into, lists)
			c.printed = [4]int{len(c.v.cost.raise), len(c.v.cost.reduce), len(c.v.cost.set), len(c.v.cost.optional)}
			e.recordBoardScan(&c.scan, lists)
			e.appendEffectCostStatics(&c.v.cost)
			markCostValidTarget(&c.v.cost)
		}
		e.putBoardScan(lists)
		if e.activeBuildSeq != seq {
			seq = 0 // active() rebuilt mid-scan: never reuse across walks
		}
		c.key, c.seq = now, seq
	}
	return clipBoardStatics(c.v), true
}

func clipBoardStatics(v boardStatics) boardStatics {
	return boardStatics{
		cost: costStaticViews{raise: slices.Clip(v.cost.raise), reduce: slices.Clip(v.cost.reduce),
			set: slices.Clip(v.cost.set), optional: slices.Clip(v.cost.optional), validTarget: v.cost.validTarget},
		action: actionStaticViews{cantCast: slices.Clip(v.action.cantCast),
			cantActivate: slices.Clip(v.action.cantActivate), continuous: slices.Clip(v.action.continuous)},
		manaConv: slices.Clip(v.manaConv),
	}
}

// boardStaticsArrays is v's slices emptied (cleared, so the dead views pin
// nothing) for scanBoardStaticsInto to refill; every other field is zero.
func boardStaticsArrays(v boardStatics) boardStatics {
	empty := func(s []staticView) []staticView { clear(s); return s[:0] }
	clear(v.manaConv)
	return boardStatics{
		cost: costStaticViews{raise: empty(v.cost.raise), reduce: empty(v.cost.reduce),
			set: empty(v.cost.set), optional: empty(v.cost.optional)},
		action: actionStaticViews{cantCast: empty(v.action.cantCast),
			cantActivate: empty(v.action.cantActivate), continuous: empty(v.action.continuous)},
		manaConv: v.manaConv[:0],
	}
}

// reappendEffectCostStatics truncates cost's four lists to their printed
// prefixes (clearing the dropped views) and appends the Effect-delivered
// cost statics and the ValidTarget mark afresh -- scanBoardStaticsInto's
// tail over a kept printed scan.
func (e *Engine) reappendEffectCostStatics(cost costStaticViews, printed [4]int) costStaticViews {
	cut := func(s []staticView, n int) []staticView { clear(s[n:]); return s[:n] }
	out := costStaticViews{raise: cut(cost.raise, printed[0]), reduce: cut(cost.reduce, printed[1]),
		set: cut(cost.set, printed[2]), optional: cut(cost.optional, printed[3])}
	e.appendEffectCostStatics(&out)
	markCostValidTarget(&out)
	return out
}

// scanBoardStaticsPrintedLists is the printed walk over lists, the zone
// lists gatherBoardScan collected in the walk's order.
func (e *Engine) scanBoardStaticsPrintedLists(out boardStatics, lists []boardScanList) boardStatics {
	for _, l := range lists {
		z := l.z
		{
			for _, id := range l.ids {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil || offBattlefieldStaticsInert(z, o) {
					continue
				}
				// CR 702.25b/d: a phased-out permanent is treated as though it
				// does not exist, so its statics do not function. Every arm
				// shares this object-level gate, which is the same one
				// scanActiveStatics runs -- so the pass-scoped snapshot and the
				// fresh activeStatics walk cannot disagree on a grantor.
				if z == state.ZBattlefield && o.PhasedOut {
					continue
				}
				// CR 708.8: a face-down battlefield permanent's printed
				// statics do not exist -- every arm, the scanActionStatics /
				// scanCostStatics / scanManaConvSources gate alike.
				if z == state.ZBattlefield && e.printedAbilitiesGone(o) {
					continue
				}
				for si, sn := 0, o.PileStaticCount(); si < sn; si++ {
					pst, ok := o.PileStaticAt(si)
					if !ok {
						continue
					}
					st := pst.Static
					var dst *[]staticView
					zoneGated := true
					switch st.ModeKind() {
					case cards.StaticCantBeCast:
						if z != state.ZBattlefield {
							continue
						}
						dst, zoneGated = &out.action.cantCast, false
					case cards.StaticCantBeActivated:
						if z != state.ZBattlefield {
							continue
						}
						dst, zoneGated = &out.action.cantActivate, false
					case cards.StaticContinuous:
						dst = &out.action.continuous
					case cards.StaticRaiseCost:
						dst = &out.cost.raise
					case cards.StaticReduceCost:
						dst = &out.cost.reduce
					case cards.StaticSetCost:
						dst = &out.cost.set
					case cards.StaticOptionalCost:
						dst = &out.cost.optional
					case cards.StaticManaConvert:
						if !staticEffectZoneOK(st, o.Zone) {
							continue
						}
						out.manaConv = append(out.manaConv, manaConvSource{sv: staticView{Source: id,
							Controller: o.Controller, Params: st.Params, PS: st.ParamSetOf(), SVars: pst.Face.SVars}})
						continue
					default:
						continue
					}
					if zoneGated && !staticEffectZoneOK(st, o.Zone) {
						continue
					}
					*dst = append(*dst, staticView{Source: id, Controller: o.Controller, Params: st.Params, PS: st.ParamSetOf(), SVars: pst.Face.SVars})
				}
			}
		}
	}
	return out
}

func (e *Engine) verifyBoardStatics(got boardStatics) {
	// The three scans below are one pure read: each zone summary's own
	// verification runs once for the call, not once per scan
	// (staticZoneSkipVerifyOnce).
	if !e.staticZoneVerifyScope {
		e.staticZoneVerifyScope = true
		clear(e.staticZoneVerified)
		defer func() { e.staticZoneVerifyScope = false }()
	}
	cost, action, mc := e.scanCostStatics(), e.scanActionStatics(), e.scanManaConvSources(nil)
	same := got.cost.validTarget == cost.validTarget && staticViewsSame(got.cost.raise, cost.raise) && staticViewsSame(got.cost.reduce, cost.reduce) &&
		staticViewsSame(got.cost.set, cost.set) && staticViewsSame(got.cost.optional, cost.optional) &&
		staticViewsSame(got.action.cantCast, action.cantCast) &&
		staticViewsSame(got.action.cantActivate, action.cantActivate) &&
		staticViewsSame(got.action.continuous, action.continuous) && len(got.manaConv) == len(mc)
	for i := 0; same && i < len(mc); i++ {
		same = staticViewSame(got.manaConv[i].sv, mc[i].sv)
	}
	if !same {
		panic("rules: walk cache stale (or fused scan divergent) for the board statics")
	}
}

func staticViewsSame(a, b []staticView) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !staticViewSame(a[i], b[i]) {
			return false
		}
	}
	return true
}

// manaConvPrintedSources returns the printed ManaConvert statics on the
// board, in the scan order manaConversionParts has always applied them.
// The returned slice is owned by the cache; callers only range it.
func (e *Engine) manaConvPrintedSources() []manaConvSource {
	if v, ok := e.boardStaticsWalk(); ok {
		return v.manaConv
	}
	return e.scanManaConvSources(nil)
}

func (e *Engine) scanManaConvSources(out []manaConvSource) []manaConvSource {
	for pi, p := range e.G.AliveFrom(0) {
		for _, z := range staticSourceZones {
			if z == state.ZStack && pi > 0 {
				continue
			}
			for _, oid := range e.staticSourceIDs(p, z) {
				o := e.G.Obj(oid)
				if o == nil || o.Face() == nil || (z == state.ZBattlefield && e.printedAbilitiesGone(o)) ||
					offBattlefieldStaticsInert(z, o) {
					continue
				}
				// CR 702.25b/d: a phased-out permanent is treated as though it
				// does not exist, so its ManaConvert static does not function --
				// the same object-level gate scanActiveStatics and the fused
				// scanBoardStatics run.
				if z == state.ZBattlefield && o.PhasedOut {
					continue
				}
				for si, sn := 0, o.PileStaticCount(); si < sn; si++ {
					pst, ok := o.PileStaticAt(si)
					if !ok || pst.Static.Mode != "ManaConvert" || !staticEffectZoneOK(pst.Static, o.Zone) {
						continue
					}
					out = append(out, manaConvSource{sv: staticView{Source: oid, Controller: o.Controller,
						Params: pst.Static.Params, SVars: pst.Face.SVars}})
				}
			}
		}
	}
	return out
}

type activeStaticsEntry struct {
	mode string
	key  walkKey
	sv   []staticView
	// shared: sv was handed to a reader outside any memo scope
	// (activeStaticsOutOfScope), whom no scope boundary bounds, so the next
	// fused scan gives the entry fresh backing instead of refilling it.
	shared bool
}

func (e *Engine) activeStaticsCached(mode string) []staticView {
	now, ok := e.walkKeyNow()
	if !ok {
		if sv, hit := e.activeStaticsOutOfScope(mode); hit {
			return sv
		}
		return e.scanActiveStatics(mode, nil)
	}
	var m *activeStaticsEntry
	for i := range e.activeStaticsCache {
		if e.activeStaticsCache[i].mode == mode {
			m = &e.activeStaticsCache[i]
			break
		}
	}
	if m == nil {
		e.activeStaticsCache = append(e.activeStaticsCache, activeStaticsEntry{mode: mode})
		m = &e.activeStaticsCache[len(e.activeStaticsCache)-1]
	}
	if m.key.gen != 0 && e.walkKeyHit(m.key, now) {
		if walkCacheVerify {
			e.verifyActiveStatics(mode, m.sv)
		}
		return slices.Clip(m.sv)
	}
	// A miss refreshes EVERY mode this engine has been asked for in one
	// battlefield pass (scanActiveStaticsFused), so the walk's other modes
	// are hits instead of one full battlefield walk each -- or, when every
	// entry was scanned in an earlier scope over an unchanged static board
	// (static_scan_reuse.go), re-stamps them all.
	if e.activeStaticsReusable(now) {
		for i := range e.activeStaticsCache {
			e.activeStaticsCache[i].key = now
		}
		if walkCacheVerify {
			for i := range e.activeStaticsCache {
				e.verifyActiveStatics(e.activeStaticsCache[i].mode, e.activeStaticsCache[i].sv)
			}
		}
		return slices.Clip(m.sv)
	}
	e.scanActiveStaticsFused(now)
	e.recordActiveScan(&e.activeStaticsScan)
	return slices.Clip(m.sv)
}

// activeStaticsOutOfScope serves mode's list outside a memo scope from the
// last fused scan when that scan is exactly the scan of the current static
// board (static_scan_reuse.go) -- a scan made in a scope, so never one built
// across a face or cast probe, which runs scope-free and moves
// crossWalkRetires on both edges. The entry is marked shared so no later
// scan refills the array the reader may still range.
func (e *Engine) activeStaticsOutOfScope(mode string) ([]staticView, bool) {
	for i := range e.activeStaticsCache {
		m := &e.activeStaticsCache[i]
		if m.mode != mode {
			continue
		}
		if m.key.gen == 0 || !e.activeScanUnchanged(&e.activeStaticsScan) {
			return nil, false
		}
		if walkCacheVerify {
			e.verifyActiveStatics(mode, m.sv)
		}
		m.shared = true
		return slices.Clip(m.sv), true
	}
	return nil, false
}

// activeStaticsReusable reports whether every activeStaticsCache entry holds
// the fused scan of an earlier scope over the current static board
// (static_scan_reuse.go), so re-stamping them is exact.
func (e *Engine) activeStaticsReusable(now walkKey) bool {
	for i := range e.activeStaticsCache {
		if k := e.activeStaticsCache[i].key; k.gen == 0 || k.gen == now.gen {
			return false
		}
	}
	return e.activeScanUnchanged(&e.activeStaticsScan)
}

// scanActiveStaticsFused refreshes every activeStaticsCache entry at key now
// from ONE battlefield pile-static walk: each entry's list is the walk's
// statics of its mode, in walk order -- exactly scanActiveStatics(mode) for
// each, since that scan is this same walk filtered by Mode$. An entry built
// earlier in this same scope gets fresh backing (a caller may still be
// ranging its old slice); an entry from an earlier scope reuses its array.
func (e *Engine) scanActiveStaticsFused(now walkKey) {
	c := e.activeStaticsCache
	for i := range c {
		if c[i].key.gen == now.gen || c[i].shared {
			c[i].sv = nil
		} else {
			c[i].sv = c[i].sv[:0]
		}
		c[i].key, c[i].shared = now, false
	}
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			f := o.Face()
			if f == nil || e.printedAbilitiesGone(o) || o.PhasedOut {
				// The scan's face-down (CR 708.8) and phased-out (CR
				// 702.25b/d) object gates.
				continue
			}
			for si, sn := 0, o.PileStaticCount(); si < sn; si++ {
				pst, ok := o.PileStaticAt(si)
				if !ok {
					continue
				}
				st := pst.Static
				// The same Room-door gate scanActiveStatics runs: a printed
				// Room static is live only while its own door is unlocked. A
				// non-cast Room designates no door (CR 709.5d), so its Face()
				// statics are inert and both scans must exclude them.
				if isRoom(o) && pst.Face == o.Face() && !o.DoorUnlocked(int(o.FaceIdx)) {
					continue
				}
				for i := range c {
					if c[i].mode != st.Mode {
						continue
					}
					if staticEffectZoneOK(st, o.Zone) {
						c[i].sv = append(c[i].sv, staticView{Source: id, Controller: o.Controller, Params: st.Params, PS: st.ParamSetOf(), SVars: pst.Face.SVars})
					}
					break
				}
			}
		}
	}
}

func (e *Engine) verifyActiveStatics(mode string, got []staticView) {
	want := e.scanActiveStatics(mode, nil)
	same := len(got) == len(want)
	for i := 0; same && i < len(got); i++ {
		same = staticViewSame(got[i], want[i])
	}
	if !same {
		panic(fmt.Sprintf("rules: walk cache stale for activeStatics(%q): cached %d views, fresh %d", mode, len(got), len(want)))
	}
}

func staticViewSame(g, w staticView) bool {
	return g.Source == w.Source && g.Controller == w.Controller && g.ChosenNumber == w.ChosenNumber &&
		g.chosenNumberBound == w.chosenNumberBound && g.selfOnly == w.selfOnly && maps.Equal(g.Params, w.Params) && maps.Equal(g.SVars, w.SVars)
}

// offBattlefieldStaticsInert reports whether every static on o, found in
// zone z by a whole-board static collector, is certainly refused by that
// collector: off the battlefield a static is admitted only through
// effectZoneOK, which needs a non-empty EffectZone$ (or, for the
// CantBeCast/CantBeActivated modes, the battlefield itself). A pile (merged
// cards) is never skipped. The face probe is conservative (a stale or
// unbound probe answers "may"), and in verify mode the skipped statics are
// checked anyway.
func offBattlefieldStaticsInert(z state.Zone, o *state.Object) bool {
	if z == state.ZBattlefield || o.Zone == state.ZBattlefield || len(o.MergedCards) != 0 ||
		o.Face().StaticsMayNameEffectZone() {
		return false
	}
	if walkCacheVerify {
		for si, sn := 0, o.PileStaticCount(); si < sn; si++ {
			if pst, ok := o.PileStaticAt(si); ok && staticEffectZoneOK(pst.Static, o.Zone) {
				panic(fmt.Sprintf("rules: off-battlefield static skip hid a live %s static on obj %d", pst.Static.Mode, o.ID))
			}
		}
	}
	return true
}

type mayPlaysEntry struct {
	key walkKey
	p   state.PlayerID
	n   int
}

func (e *Engine) mayPlaysThisTurnCached(p state.PlayerID) int {
	now, ok := e.walkKeyNow()
	if !ok {
		return e.scanMayPlaysThisTurn(p)
	}
	for i := range e.mayPlaysCache {
		m := &e.mayPlaysCache[i]
		if m.p == p && m.key.gen != 0 && e.walkKeyHit(m.key, now) {
			if walkCacheVerify {
				if want := e.scanMayPlaysThisTurn(p); want != m.n {
					panic(fmt.Sprintf("rules: walk cache stale for mayPlaysThisTurn(%d): cached %d, fresh %d", p, m.n, want))
				}
			}
			return m.n
		}
	}
	n := e.scanMayPlaysThisTurn(p)
	for i := range e.mayPlaysCache {
		if e.mayPlaysCache[i].p == p {
			e.mayPlaysCache[i] = mayPlaysEntry{key: now, p: p, n: n}
			return n
		}
	}
	e.mayPlaysCache = append(e.mayPlaysCache, mayPlaysEntry{key: now, p: p, n: n})
	return n
}
