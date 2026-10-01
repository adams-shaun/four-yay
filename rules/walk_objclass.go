package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// Offer-walk object classes: the battlefield section's two whole-board loops
// (legal_walk_battlefield.go) pass over a cold object without visiting it.
//
// Both loops visit every object of every seat's battlefield, graveyard, hand
// and exile (the mana loop: every battlefield, and p's hand and graveyard),
// and for almost all of them offer nothing: a creature prints no mana
// ability, a hand or graveyard card prints no ability its zone admits. Each
// such object already takes a per-object skip (manaWalkEmpty,
// pileAbilitiesEmpty plus the granted-keyword precheck), but the visit itself
// -- the object, its face, its face facts, its controller -- was the cost.
//
// An object's class records, over every face o.Face() could resolve to
// (every entry of o.Card.Faces and o.CopyFace, so a face flip or
// faceprobe's temporary FaceIdx swap changes nothing):
//
//   - manaHot: it is a merged pile, face down, or some face has no
//     configured facts or prints a mana ability (walkFaceFacts.mana). A
//     manaHot-false object is skipped by manaWalkEmpty on every board where
//     that skip can answer (ready, no AddAbility$ carrier, no grant) outside
//     the granted land-type intrinsic block -- and the mana loop uses the
//     class only on such a board, never for a battlefield while that block
//     can apply (hasLType with land-type words), and never for the seat's own
//     battlefield in a recording walk (which records every position).
//   - abAlways / abMask: abAlways is set for a merged pile, a cloaked,
//     suspected or suspend-granted object, one whose intrinsic keywords or
//     keyword counters carry a granted-expansion head, and one with a face
//     lacking configured facts, current keyword facts, or carrying such a
//     head (kwGranted); abMask is the union of every face's abZones. An
//     object with abAlways false and abMask's zone bit clear is skipped by
//     pileAbilitiesEmpty (whatever its controller: abZonesActivator is a
//     subset of abZones) and objectGrantedKWMaybe answers false, so on a
//     board with no granted-head AddKeywords (kwOK && !kwMaybe) its block in
//     the ability loop is empty, reads no pricing pool and is never recorded
//     (walk_block_reuse.go) -- the loop uses the class only on such a board.
//
// Every input of a class is the object's own Card, CopyFace, MergedCards,
// FaceDown, Cloaked, Suspected, SuspendGranted, IntrinsicKeywords and
// Counters, plus the immutable face facts. Each of those fields is written
// only by events.Apply for an object the event names (Obj, IDs or Pairs), so
// the static summaries' catch-up (static_zoneskip.go: staticZonesCatchUp,
// whose per-object touch drops the object's class) keeps every cached class
// exact; a log shorter than the last catch-up drops them all.
//
// walkSkipVerify (the rules test binary) visits every object anyway,
// recomputes each class it reads, and panics when a cold object's skip
// would not hold.
type walkObjClass struct {
	set      bool
	manaHot  bool
	abAlways bool
	// mayPlayHot: some face carries a Continuous static or the Paradigm
	// keyword -- the two permissions mayPlaySpellIds reads for a card while
	// no board-side grant is open (faceHasContinuousStatic,
	// paradigmMayPlay), so a card without one yields no offer there.
	mayPlayHot bool
	// staticOn / staticOff are objectStaticHotOn / objectStaticHotOff
	// (static_zoneskip.go): some face carries any static / a static an
	// off-battlefield collector could admit, or the object is a merged pile.
	staticOn, staticOff bool
	// ctrDep: some counter kind on the object is a keyword counter, so a
	// counter amount change can move the class (walkClassTouch recomputes).
	ctrDep bool
	abMask uint32
	// fp is the object's fields the class and the static scans read, as of
	// the classification (walkObjFPOf).
	fp walkObjFP
}

// walkObjFP is the part of an object every input of its class -- and every
// per-object read of the printed static scans (static_scan_reuse.go) --
// comes from, short of the immutable faces themselves: an object whose
// fingerprint is unchanged (and that is no merged pile, and carries no
// keyword counter) has the same class and contributes the same views.
type walkObjFP struct {
	card     *cards.Card
	copyFace *cards.Face
	faceIdx  uint8
	flags    uint8
	zone     state.Zone
	ctl      state.PlayerID
	merged   int32
	keywords int32
	counters int32
}

const (
	fpFaceDown = 1 << iota
	fpPhasedOut
	fpCloaked
	fpSuspected
	fpSuspendGranted
)

func walkObjFPOf(o *state.Object) walkObjFP {
	fp := walkObjFP{card: o.Card, copyFace: o.CopyFace, faceIdx: o.FaceIdx, zone: o.Zone, ctl: o.Controller,
		merged: int32(len(o.MergedCards)), keywords: int32(len(o.IntrinsicKeywords)), counters: int32(len(o.Counters))}
	if o.FaceDown {
		fp.flags |= fpFaceDown
	}
	if o.PhasedOut {
		fp.flags |= fpPhasedOut
	}
	if o.Cloaked {
		fp.flags |= fpCloaked
	}
	if o.Suspected {
		fp.flags |= fpSuspected
	}
	if o.SuspendGranted {
		fp.flags |= fpSuspendGranted
	}
	return fp
}

// staticHot is objectStaticHot(o, z) from the class.
func (c *walkObjClass) staticHot(z state.Zone) bool {
	if z == state.ZBattlefield {
		return c.staticOn
	}
	return c.staticOff
}

// abHot reports whether the class admits an ability-loop offer in zone z.
func (c *walkObjClass) abHot(z state.Zone) bool {
	return c.abAlways || zoneBit(c.abMask, z)
}

// computeWalkObjClass classifies o (see walkObjClass).
func (e *Engine) computeWalkObjClass(o *state.Object) walkObjClass {
	c := walkObjClass{set: true}
	if o == nil {
		c.manaHot, c.abAlways, c.mayPlayHot = true, true, true
		return c
	}
	c.fp = walkObjFPOf(o)
	if len(o.MergedCards) != 0 {
		c.manaHot, c.abAlways, c.mayPlayHot, c.staticOn, c.staticOff = true, true, true, true, true
		return c
	}
	c.manaHot = o.FaceDown
	c.abAlways = o.Cloaked || o.Suspected || o.SuspendGranted
	for _, k := range o.IntrinsicKeywords {
		if grantedKWHeadMatch(k) {
			c.abAlways = true
		}
	}
	for _, ct := range o.Counters {
		if kwName, ok := cards.CounterKeyword(ct.Kind); ok {
			c.ctrDep = true
			if ct.N > 0 && grantedKWHeadMatch(kwName) {
				c.abAlways = true
			}
		}
	}
	if o.CopyFace != nil {
		e.classifyWalkFace(&c, o.CopyFace)
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if f != nil {
				e.classifyWalkFace(&c, f)
			}
		}
	}
	return c
}

func (e *Engine) classifyWalkFace(c *walkObjClass, f *cards.Face) {
	ff := e.walkFaceFactsOf(f)
	if ff != nil && ff.fullyCurrent(f) {
		// The face's own class bits, computed with its facts.
		c.staticOn = c.staticOn || ff.staticOn
		c.staticOff = c.staticOff || ff.staticOff
		c.mayPlayHot = c.mayPlayHot || ff.mayPlay
	} else {
		c.staticOn = c.staticOn || faceStaticHotOn(f)
		c.staticOff = c.staticOff || faceStaticHotOff(f)
		c.mayPlayHot = c.mayPlayHot || faceMayPlayHot(f)
	}
	if ff == nil {
		c.manaHot, c.abAlways = true, true
		return
	}
	if ff.mana {
		c.manaHot = true
	}
	c.abMask |= ff.abZones
	if !ff.keywordsCurrent(f) || ff.kwGranted {
		c.abAlways = true
	}
}

// walkFacesEdited reports whether some face of o was edited in place after
// its facts were computed (a face with facts that are not fullyCurrent).
func (e *Engine) walkFacesEdited(o *state.Object) bool {
	edited := func(f *cards.Face) bool {
		ff := e.walkFaceFactsOf(f)
		return ff == nil || !ff.fullyCurrent(f)
	}
	if o.CopyFace != nil && edited(o.CopyFace) {
		return true
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if f != nil && edited(f) {
				return true
			}
		}
	}
	return false
}

// withoutFP is c with its fingerprint zeroed.
func (c walkObjClass) withoutFP() walkObjClass {
	c.fp = walkObjFP{}
	return c
}

// faceMayPlayHot reports whether f carries a Continuous static or the
// Paradigm keyword (walkObjClass.mayPlayHot's per-face half).
func faceMayPlayHot(f *cards.Face) bool {
	for _, st := range f.Statics {
		if st.Mode == "Continuous" {
			return true
		}
	}
	return f.HasKeyword("Paradigm")
}

// walkClassOf returns id's class, computing and caching it on first use.
// The caller has brought the catch-up up to date (walkClassesCatchUp).
// The result points into the cache: read it before anything can grow the
// cache again.
func (e *Engine) walkClassOf(id state.ObjID) *walkObjClass {
	// No owner test here: every caller has run the catch-up, which gives a
	// by-value Engine copy its own cache first (ownWalkClasses).
	if i := uint(id) - 1; i < uint(len(e.walkObjCls)) && e.walkObjCls[i].set && !walkSkipVerify {
		return &e.walkObjCls[i]
	}
	return e.walkClassOfSlow(id)
}

// walkClassInvalid is the class of an id that names no object: hot for the
// two walk loops, cold for the static summaries and the may-play scan (each
// of which skips a nil object itself).
var walkClassInvalid = walkObjClass{set: true, manaHot: true, abAlways: true}

func (e *Engine) walkClassOfSlow(id state.ObjID) *walkObjClass {
	e.ownWalkClasses()
	i := int(id) - 1
	if i < 0 || i >= len(e.G.Objs) {
		c := walkClassInvalid
		return &c
	}
	if n := len(e.G.Objs); n > len(e.walkObjCls) {
		if n > cap(e.walkObjCls) {
			grown := make([]walkObjClass, n, n+n/2+8)
			copy(grown, e.walkObjCls)
			e.walkObjCls = grown
		} else {
			// The capacity past the length may hold a recycled array's old
			// classes (Spare.walkCls): expose it unset.
			old := len(e.walkObjCls)
			e.walkObjCls = e.walkObjCls[:n]
			clear(e.walkObjCls[old:])
		}
	}
	c := &e.walkObjCls[i]
	if !c.set {
		if e.offerProbeDepth > 0 {
			// Inside a face or cast probe: answer from the probed state but
			// cache nothing (its fingerprint names a no-event write the
			// catch-up never undoes).
			fresh := e.computeWalkObjClass(&e.G.Objs[i])
			return &fresh
		}
		*c = e.computeWalkObjClass(&e.G.Objs[i])
	} else if walkSkipVerify {
		// The fingerprint is compared by the catch-up's touches, not here:
		// a field it records may move without changing the class.
		if fresh := e.computeWalkObjClass(&e.G.Objs[i]); fresh.withoutFP() != c.withoutFP() {
			panic(fmt.Sprintf("rules: walk class of obj %d is stale (%+v, recomputed %+v)", id, *c, fresh))
		}
	}
	return c
}

// walkClassesCatchUp brings every cached class up to date with the log.
func (e *Engine) walkClassesCatchUp() {
	e.staticZonesCatchUp()
}

// walkClassTouch refreshes the cached class of o, an object an event named
// (staticZoneTouch's per-object half). It reports whether o's static-hot
// classification is provably unchanged -- the class was cached and its
// staticOn/staticOff bits are the fresh ones -- so no static summary holding
// o needs dropping (its recorded classification of o stands), and bumps
// staticTouchGen unless o is provably static-cold before and after (a
// static-cold object contributes nothing to any static scan, whatever its
// other fields).
func (e *Engine) walkClassTouch(o *state.Object) (staticSame bool) {
	e.ownWalkClasses()
	i := int(o.ID) - 1
	if i < 0 || i >= len(e.walkObjCls) || !e.walkObjCls[i].set {
		e.staticTouchGen++
		return false
	}
	if e.offerProbeDepth > 0 {
		// A catch-up run inside a face or cast probe reads probed fields:
		// drop the class instead of refreshing it, conservatively.
		e.walkObjCls[i].set = false
		e.staticTouchGen++
		return false
	}
	old := e.walkObjCls[i]
	if old.fp.merged == 0 && !old.ctrDep && old.fp == walkObjFPOf(o) {
		// Every input of the class and of the object's static views is
		// as classified: nothing to refresh, nothing a scan reads moved.
		if walkSkipVerify {
			if fresh := e.computeWalkObjClass(o); fresh != old {
				// Faces are immutable once configured; a test that edits
				// one in place (no event) is the only way a class moves
				// under an unchanged fingerprint. Refresh it then; any
				// other move is a missed input.
				if !e.walkFacesEdited(o) {
					panic(fmt.Sprintf("rules: walk class of obj %d moved under an unchanged fingerprint (%+v, recomputed %+v)", o.ID, old, fresh))
				}
				e.walkObjCls[i] = fresh
				e.staticTouchGen++
				return old.staticOn == fresh.staticOn && old.staticOff == fresh.staticOff
			}
		}
		return true
	}
	fresh := e.computeWalkObjClass(o)
	e.walkObjCls[i] = fresh
	if old.staticOn || old.staticOff || fresh.staticOn || fresh.staticOff {
		e.staticTouchGen++
	}
	return old.staticOn == fresh.staticOn && old.staticOff == fresh.staticOff
}

// walkClassDropAll drops every cached class.
func (e *Engine) walkClassDropAll() {
	e.ownWalkClasses()
	clear(e.walkObjCls)
	e.staticTouchGen++
}

// ownWalkClasses gives a by-value Engine copy (entryPreview's speculative
// engine) its own empty class cache, so it never writes the original's
// array: classes computed over the copy's state are the copy's alone.
func (e *Engine) ownWalkClasses() {
	if e.walkClsOwner != e {
		e.walkObjCls, e.walkClsOwner = nil, e
	}
}

// verifyManaCold panics unless a mana-cold object's visit in the mana loop
// is skipped by manaWalkEmpty.
func (w *legalWalk) verifyManaCold(board walkBoardFacts, o *state.Object, id state.ObjID, z state.Zone) {
	if o == nil || (z == state.ZBattlefield && !existsOnBattlefield(o)) {
		return
	}
	if f := o.Face(); f != nil && !w.manaWalkEmpty(board, o, id, f) {
		panic(fmt.Sprintf("rules: mana-cold obj %d in zone %v is not skipped by manaWalkEmpty", id, z))
	}
}

// verifyAbilityCold panics unless an ability-cold object's block in the
// ability loop is provably empty: its printed-ability skip holds, it has no
// granted keyword line, and a reusing walk has no recorded block for it.
func (w *legalWalk) verifyAbilityCold(board walkBoardFacts, o *state.Object, id state.ObjID, z state.Zone, zp state.PlayerID) {
	if o == nil {
		return
	}
	f := o.Face()
	if f == nil {
		return
	}
	skip, ff := w.pileAbilitiesEmpty(o, id, f, z)
	if !skip {
		panic(fmt.Sprintf("rules: ability-cold obj %d in zone %v has a printed ability the loop could offer", id, z))
	}
	if lines := w.grantedKeywordLines(board, o, id, f, ff); len(lines) != 0 {
		panic(fmt.Sprintf("rules: ability-cold obj %d in zone %v has granted keyword lines %q", id, z, lines))
	}
	if r := w.reuse; r != nil {
		for _, b := range r.blocks {
			if b.id == id && b.zone == z && b.zp == zp {
				panic(fmt.Sprintf("rules: ability-cold obj %d in zone %v has a recorded block", id, z))
			}
		}
	}
}
