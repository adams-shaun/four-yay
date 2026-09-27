package rules

import (
	"fmt"
	"maps"
	"slices"

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
//     piles for it (payerGrantsPayLifeInsteadOfB alone does so on every
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
	v   boardStatics
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
	if c.key.gen == 0 || !e.walkKeyHit(c.key, now) {
		// Fresh backing on every rebuild, so no slice a caller may still be
		// ranging is ever rewritten.
		c.v = e.scanBoardStatics()
		c.key = now
	} else if walkCacheVerify {
		e.verifyBoardStatics(c.v)
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

// scanBoardStatics is scanCostStatics + scanActionStatics +
// scanManaConvSources in one walk. Each arm keeps its collector's own gate:
// CantBeCast/CantBeActivated battlefield-only; Continuous and the cost modes
// through effectZoneOK; ManaConvert through effectZoneOK; and every arm skips
// a face-down battlefield permanent (CR 708.8). The Effect-delivered cost statics are
// appended after the printed walk, exactly as scanCostStatics does.
func (e *Engine) scanBoardStatics() boardStatics {
	var out boardStatics
	for pi, p := range e.G.AliveFrom(0) {
		for _, z := range staticSourceZones {
			if z == state.ZStack && pi > 0 {
				continue
			}
			for _, id := range e.staticSourceIDs(p, z) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil || offBattlefieldStaticsInert(z, o) {
					continue
				}
				// CR 708.8: a face-down battlefield permanent's printed
				// statics do not exist -- every arm, the scanActionStatics /
				// scanCostStatics / scanManaConvSources gate alike.
				if z == state.ZBattlefield && e.faceDownPrintedHides(o) {
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
					switch st.Mode {
					case "CantBeCast":
						if z != state.ZBattlefield {
							continue
						}
						dst, zoneGated = &out.action.cantCast, false
					case "CantBeActivated":
						if z != state.ZBattlefield {
							continue
						}
						dst, zoneGated = &out.action.cantActivate, false
					case "Continuous":
						dst = &out.action.continuous
					case "RaiseCost":
						dst = &out.cost.raise
					case "ReduceCost":
						dst = &out.cost.reduce
					case "SetCost":
						dst = &out.cost.set
					case "OptionalCost":
						dst = &out.cost.optional
					case "ManaConvert":
						if !effectZoneOK(st.Params["EffectZone"], o.Zone) {
							continue
						}
						out.manaConv = append(out.manaConv, manaConvSource{sv: staticView{Source: id,
							Controller: o.Controller, Params: st.Params, SVars: pst.Face.SVars}})
						continue
					default:
						continue
					}
					if zoneGated && !effectZoneOK(st.Params["EffectZone"], o.Zone) {
						continue
					}
					*dst = append(*dst, staticView{Source: id, Controller: o.Controller, Params: st.Params, SVars: pst.Face.SVars})
				}
			}
		}
	}
	e.appendEffectCostStatics(&out.cost)
	markCostValidTarget(&out.cost)
	return out
}

func (e *Engine) verifyBoardStatics(got boardStatics) {
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
				if o == nil || o.Face() == nil || (z == state.ZBattlefield && e.faceDownPrintedHides(o)) ||
					offBattlefieldStaticsInert(z, o) {
					continue
				}
				for si, sn := 0, o.PileStaticCount(); si < sn; si++ {
					pst, ok := o.PileStaticAt(si)
					if !ok || pst.Static.Mode != "ManaConvert" || !effectZoneOK(pst.Static.Params["EffectZone"], o.Zone) {
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
}

func (e *Engine) activeStaticsCached(mode string) []staticView {
	now, ok := e.walkKeyNow()
	if !ok {
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
	if m.key.gen == now.gen {
		m.sv = nil // same walk, moved key: never rewrite a slice a caller may range
	}
	m.sv = e.scanActiveStatics(mode, m.sv[:0])
	m.key = now
	return slices.Clip(m.sv)
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
		g.chosenNumberBound == w.chosenNumberBound && maps.Equal(g.Params, w.Params) && maps.Equal(g.SVars, w.SVars)
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
			if pst, ok := o.PileStaticAt(si); ok && effectZoneOK(pst.Static.Params["EffectZone"], o.Zone) {
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
