package rules

import "github.com/adams-shaun/gorge/state"

// Cross-walk reuse of the whole-board printed static scans.
//
// boardStaticsWalk's fused scan (the cost, action and ManaConvert
// memberships) and scanActiveStaticsFused (the battlefield statics by Mode$)
// are rebuilt for every walk whose memo key moved, and the cross-walk hit
// (walkCrossHit) needs active() unrebuilt -- which every non-inert event
// defeats: a land tapped for mana, a creature dealt damage, a step change.
// Yet each scan's printed part reads only:
//
//   - the zone lists it walks: each alive seat's static-source zones through
//     staticSourceIDs (the static-hot subsequence of a summarized zone, the
//     whole stack and command zone), or each alive seat's battlefield;
//   - per visited object, its face and pile statics, zone, controller,
//     phased-out and face-down status -- and an object contributes only when
//     it is static-hot (walk_objclass.go: staticOn on the battlefield,
//     staticOff elsewhere; a cold object is skipped by every arm of both
//     scans, see static_zoneskip.go).
//
// Each of those object fields is written only by events.Apply for an object
// the event names, and the shared catch-up (staticZonesCatchUp) bumps
// staticTouchGen for every named object that is, or was, static-hot. So a
// printed scan recorded with its visited lists and the generation is exactly
// the scan of a later state whose lists and generation are equal. The
// Effect-delivered cost statics boardStaticsWalk appends (active()'s
// registry, appendEffectCostStatics) are recomputed on every reuse.
//
// Each scan also skips a battlefield object whose printed abilities a "loses
// all abilities" effect removes (printedAbilitiesGone), and that verdict is
// NOT an input of the object alone: the remover's applicability reads the
// board (an Aura re-attached by an Attach event that touches no static-hot
// fingerprint, a type change). So a record also keeps the visited objects
// the verdict skipped (lost), read only while a remover is live
// (abilityLossMemo.boardLive), and a reuse re-derives the verdict for every visited object and
// requires the same set -- abilityLoss's per-build memo (abilityloss_memo.go)
// answers those reads again for the walk that follows.
//
// Reuse happens only on the path that already rebuilds an entry from an
// earlier outermost scope (whose arrays nobody ranges any more), never
// inside a face or cast probe (those run with no memo scope, so the caches
// are bypassed), and verify mode (walkCacheVerify) recomputes every reuse.

// staticScanSeg is one visited zone list of a recorded scan.
type staticScanSeg struct {
	p state.PlayerID
	z state.Zone
	n int32
}

// staticScanRec is a printed scan's visited lists and touch generation.
type staticScanRec struct {
	// owner is the engine that recorded the scan: a by-value Engine copy
	// neither matches nor appends into the original's record.
	owner *Engine
	ok    bool
	gen   uint64
	// retires is crossWalkRetires at the scan: a face or cast probe and the
	// cost-composition exclusion move it at both edges, so no scan made
	// across one is served on its other side.
	retires uint64
	segs    []staticScanSeg
	ids     []state.ObjID
	// lost is the visited ids whose printed abilities were removed at the
	// scan, in visit order.
	lost []state.ObjID
}

func (r *staticScanRec) begin(e *Engine, gen, retires uint64) {
	if r.owner != e {
		r.owner, r.segs, r.ids, r.lost = e, nil, nil, nil
	}
	r.ok, r.gen, r.retires, r.segs, r.ids = true, gen, retires, r.segs[:0], r.ids[:0]
	r.lost = r.lost[:0]
}

// recordLoss records, after r's lists, which visited objects' printed
// abilities are removed now. While no remover is live (boardLive) no
// object's are, and nothing is read.
func (r *staticScanRec) recordLoss(e *Engine) {
	if _, live := e.lossMemo.boardLive(e); !live {
		return
	}
	for _, id := range r.ids {
		if o := e.G.Obj(id); scanLossMatters(o) && e.printedAbilitiesLost(o) {
			r.lost = append(r.lost, id)
		}
	}
}

// lossUnchanged reports whether the visited objects whose printed abilities
// are removed now are exactly r's recorded set. It runs only after the lists
// and touch generation matched.
func (r *staticScanRec) lossUnchanged(e *Engine) bool {
	if _, live := e.lossMemo.boardLive(e); !live {
		// No object loses its abilities now.
		return len(r.lost) == 0
	}
	k := 0
	for _, id := range r.ids {
		if o := e.G.Obj(id); !scanLossMatters(o) || !e.printedAbilitiesLost(o) {
			continue
		}
		if k >= len(r.lost) || r.lost[k] != id {
			return false
		}
		k++
	}
	return k == len(r.lost)
}

// scanLossMatters reports whether o's loss of abilities can change a scan's
// output: only its pile statics contribute. The count is a function of the
// object's faces and pile, which a reuse has already held unchanged (the
// touch generation), so record and check filter the same objects.
func scanLossMatters(o *state.Object) bool {
	return o != nil && o.PileStaticCount() > 0
}

func (r *staticScanRec) add(p state.PlayerID, z state.Zone, list []state.ObjID) {
	r.segs = append(r.segs, staticScanSeg{p: p, z: z, n: int32(len(list))})
	r.ids = append(r.ids, list...)
}

// staticScanCheck matches a walk of the zone lists against a record.
type staticScanCheck struct {
	r        *staticScanRec
	seg, off int
	ok       bool
}

func (c *staticScanCheck) next(p state.PlayerID, z state.Zone, list []state.ObjID) {
	if !c.ok {
		return
	}
	if c.seg >= len(c.r.segs) {
		c.ok = false
		return
	}
	s := c.r.segs[c.seg]
	if s.p != p || s.z != z || int(s.n) != len(list) {
		c.ok = false
		return
	}
	for i, id := range list {
		if c.r.ids[c.off+i] != id {
			c.ok = false
			return
		}
	}
	c.seg++
	c.off += len(list)
}

func (c *staticScanCheck) done() bool { return c.ok && c.seg == len(c.r.segs) }

// boardScanList is one zone list the printed board scan walks.
type boardScanList struct {
	p   state.PlayerID
	z   state.Zone
	ids []state.ObjID
}

// gatherBoardScan collects the zone lists scanBoardStaticsInto walks, in
// its order: each alive seat's static-source zones through staticSourceIDs
// (the stack for the first seat only). The lists are read-only (a summary's
// hot list is never rewritten; a live zone list does not move inside the
// pure read that uses them). The returned buffer is taken from e and handed
// back with putBoardScan, so a nested gather allocates its own.
func (e *Engine) gatherBoardScan() []boardScanList {
	// The touch generation a caller compares is current only once the
	// catch-up has run (staticSourceIDs skips it for an empty or
	// unsummarized zone).
	e.staticZonesCatchUp()
	out := e.boardScanBuf[:0]
	e.boardScanBuf = nil
	for pi, p := range e.G.AliveFrom(0) {
		for _, z := range staticSourceZones {
			if z == state.ZStack && pi > 0 {
				continue
			}
			out = append(out, boardScanList{p: p, z: z, ids: e.staticSourceIDs(p, z)})
		}
	}
	return out
}

// putBoardScan hands a gathered buffer back to e.
func (e *Engine) putBoardScan(lists []boardScanList) {
	clear(lists)
	e.boardScanBuf = lists[:0]
}

// boardScanMatches reports whether r's printed board scan, recorded at the
// current touch generation, walked exactly lists.
func (e *Engine) boardScanMatches(r *staticScanRec, lists []boardScanList) bool {
	if !r.ok || r.owner != e || r.gen != e.staticTouchGen || r.retires != e.crossWalkRetires {
		return false
	}
	c := staticScanCheck{r: r, ok: true}
	for _, l := range lists {
		if c.next(l.p, l.z, l.ids); !c.ok {
			return false
		}
	}
	return c.done() && r.lossUnchanged(e)
}

// recordBoardScan records lists, the zone lists a printed board scan just
// walked.
func (e *Engine) recordBoardScan(r *staticScanRec, lists []boardScanList) {
	r.begin(e, e.staticTouchGen, e.crossWalkRetires)
	for _, l := range lists {
		r.add(l.p, l.z, l.ids)
	}
	r.recordLoss(e)
}

// activeScanUnchanged reports whether r's fused battlefield scan is exactly
// the scan of the current state (scanActiveStaticsFused walks every alive
// seat's whole battlefield list).
func (e *Engine) activeScanUnchanged(r *staticScanRec) bool {
	e.staticZonesCatchUp()
	if !r.ok || r.owner != e || r.gen != e.staticTouchGen || r.retires != e.crossWalkRetires {
		return false
	}
	c := staticScanCheck{r: r, ok: true}
	for _, p := range e.G.AliveFrom(0) {
		c.next(p, state.ZBattlefield, e.G.Zone(state.ZBattlefield, p))
		if !c.ok {
			return false
		}
	}
	return c.done() && r.lossUnchanged(e)
}

// recordActiveScan records the battlefield lists the fused scan walked.
func (e *Engine) recordActiveScan(r *staticScanRec) {
	e.staticZonesCatchUp()
	r.begin(e, e.staticTouchGen, e.crossWalkRetires)
	for _, p := range e.G.AliveFrom(0) {
		r.add(p, state.ZBattlefield, e.G.Zone(state.ZBattlefield, p))
	}
	r.recordLoss(e)
}
