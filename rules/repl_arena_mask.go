package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Whole-arena replacement event mask.
//
// forEachReplacementSourceFor skips a summarized zone whose hot objects
// carry no R: line for the caller's event bit, but it still validates every
// zone summary of every seat (and catches up the touch log) on every
// replaceable event -- and most replaceable events (a step change, a mana
// add, an untap, damage) have no carrier anywhere on the board. replArena
// keeps a SUPERSET of every arena object's objectReplMask: each object is
// classified once when the arena grows past it, and an object whose
// classified faces may have changed in place is re-classified and OR-ed in.
// objectReplMask reads only an object's Card (every face) and CopyFace, and
// events.Apply never reassigns an existing object's Card; the one fold that
// can install a new CopyFace on an existing object is ClonePermanent's
// (CloneStatic only appends statics to it; the token and card-copy folds
// mint new objects, which the arena growth classifies). So the catch-up
// re-classifies only the referents of the replArenaTouchKinds events. Bits
// are never cleared, so a stale bit costs
// only an ordinary walk. When the caller's bit is absent from the union no
// zone summary of the current lists can carry it either (a valid summary's
// mask is an OR over objects of this arena), so the walk would visit no
// object outside the command zone, which is visited unconditionally as
// before. The skipped summary updates are pure caches validated on their
// next use. replZoneSkipVerify runs the full walk anyway and panics if it
// would have visited an object.
type replArenaMask struct {
	mask uint32
	// objs is how many arena objects the mask has classified; ep the log
	// length its touch catch-up has reached. ok is false until the first
	// full classification (a fresh or recycled engine).
	objs, ep int
	ok       bool
}

// replArenaMaskFor brings the union up to date and reports whether bit may
// be carried by some arena object.
func (e *Engine) replArenaMaskFor(bit uint32) bool {
	a := &e.replArena
	objs, n := e.G.Objs, len(e.L.Events)
	if !a.ok || a.objs > len(objs) || a.ep > n {
		*a = replArenaMask{ok: true}
		for i := range objs {
			a.mask |= objectReplMask(&objs[i])
		}
		a.objs, a.ep = len(objs), n
		return a.mask&bit != 0
	}
	for i := a.ep; i < n; i++ {
		if ev := &e.L.Events[i]; replArenaTouchKinds.has(ev.Kind) {
			e.replArenaTouchEvent(ev)
		}
	}
	a.ep = n
	for i := a.objs; i < len(objs); i++ {
		a.mask |= objectReplMask(&objs[i])
	}
	a.objs = len(objs)
	return a.mask&bit != 0
}

// replArenaTouchKinds are the kinds whose fold can change an existing
// object's objectReplMask (see the file comment).
var replArenaTouchKinds = newKindSet(events.ClonePermanent, events.CloneStatic)

// replArenaTouchEvent ORs in the current class of every object ev's fold
// may have rewritten in place.
func (e *Engine) replArenaTouchEvent(ev *events.Event) {
	if !replArenaTouchKinds.has(ev.Kind) {
		return
	}
	a := &e.replArena
	a.mask |= objectReplMask(e.G.Obj(ev.Obj))
	for _, id := range ev.IDs {
		a.mask |= objectReplMask(e.G.Obj(id))
	}
	for _, pr := range ev.Pairs {
		a.mask |= objectReplMask(e.G.Obj(pr[0])) | objectReplMask(e.G.Obj(pr[1]))
	}
}

// replArenaNoteApplied is the touch for a fold applied without a log entry
// (entryPreview's private Apply): its referents are re-classified now.
func (e *Engine) replArenaNoteApplied(ev events.Event) {
	if e.replArena.ok {
		e.replArenaTouchEvent(&ev)
	}
}

// verifyReplArenaSkip is the skip's check: the full walk must visit nothing
// outside the command zone for bit.
func (e *Engine) verifyReplArenaSkip(bit uint32) {
	for si, p := range e.G.AliveFrom(0) {
		for z := state.ZLibrary; z <= state.ZStack; z++ {
			if z == state.ZStack && si != 0 {
				continue
			}
			for _, id := range e.G.Zone(z, p) {
				if objectReplMask(e.G.Obj(id))&bit != 0 {
					panic(fmt.Sprintf("rules: replacement arena mask %#x skipped obj %d carrying event bit %#x", e.replArena.mask, id, bit))
				}
			}
		}
	}
}
