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
	abMask     uint32
}

// abHot reports whether the class admits an ability-loop offer in zone z.
func (c walkObjClass) abHot(z state.Zone) bool {
	return c.abAlways || zoneBit(c.abMask, z)
}

// computeWalkObjClass classifies o (see walkObjClass).
func (e *Engine) computeWalkObjClass(o *state.Object) walkObjClass {
	c := walkObjClass{set: true}
	if o == nil || len(o.MergedCards) != 0 {
		c.manaHot, c.abAlways, c.mayPlayHot = true, true, true
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
		if kwName, ok := cards.CounterKeyword(ct.Kind); ok && ct.N > 0 && grantedKWHeadMatch(kwName) {
			c.abAlways = true
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
	if !c.mayPlayHot {
		for _, st := range f.Statics {
			if st.Mode == "Continuous" {
				c.mayPlayHot = true
			}
		}
		if f.HasKeyword("Paradigm") {
			c.mayPlayHot = true
		}
	}
	ff := e.walkFaceFactsOf(f)
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

// walkClassOf returns id's class, computing and caching it on first use.
// The caller has brought the catch-up up to date (walkClassesCatchUp).
func (e *Engine) walkClassOf(id state.ObjID) walkObjClass {
	if i := uint(id) - 1; i < uint(len(e.walkObjCls)) && !walkSkipVerify {
		if c := e.walkObjCls[i]; c.set {
			return c
		}
	}
	return e.walkClassOfSlow(id)
}

func (e *Engine) walkClassOfSlow(id state.ObjID) walkObjClass {
	i := int(id) - 1
	if i < 0 || i >= len(e.G.Objs) {
		return walkObjClass{set: true, manaHot: true, abAlways: true}
	}
	if n := len(e.G.Objs); n > len(e.walkObjCls) {
		// Capacity past the length was never written (the cache only grows),
		// so a re-slice exposes zero (unset) classes.
		if n > cap(e.walkObjCls) {
			grown := make([]walkObjClass, n, n+n/2+8)
			copy(grown, e.walkObjCls)
			e.walkObjCls = grown
		} else {
			e.walkObjCls = e.walkObjCls[:n]
		}
	}
	c := e.walkObjCls[i]
	if !c.set {
		c = e.computeWalkObjClass(&e.G.Objs[i])
		e.walkObjCls[i] = c
	} else if walkSkipVerify {
		if fresh := e.computeWalkObjClass(&e.G.Objs[i]); fresh != c {
			panic(fmt.Sprintf("rules: walk class of obj %d is stale (%+v, recomputed %+v)", id, c, fresh))
		}
	}
	return c
}

// walkClassesCatchUp brings every cached class up to date with the log.
func (e *Engine) walkClassesCatchUp() {
	e.staticZonesCatchUp()
}

// walkClassTouch drops id's cached class (staticZoneTouch's per-object
// half).
func (e *Engine) walkClassTouch(id state.ObjID) {
	if i := int(id) - 1; i >= 0 && i < len(e.walkObjCls) {
		e.walkObjCls[i].set = false
	}
}

// walkClassDropAll drops every cached class.
func (e *Engine) walkClassDropAll() {
	clear(e.walkObjCls)
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
