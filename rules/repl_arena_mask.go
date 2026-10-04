package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
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
	// ask is the same kind of superset for the ask-free predicate's board
	// scans (objectReplAsk's classes): an arena with no object carrying a
	// class lets tapeAnyReplBodyMayAsk / tapeLandReplMayAsk skip the walk.
	ask uint32
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
			a.classify(&objs[i])
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
		a.classify(&objs[i])
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
	a.classify(e.G.Obj(ev.Obj))
	for _, id := range ev.IDs {
		a.classify(e.G.Obj(id))
	}
	for _, pr := range ev.Pairs {
		a.classify(e.G.Obj(pr[0]))
		a.classify(e.G.Obj(pr[1]))
	}
}

// classify ORs o's event bits and ask classes into the union. The ask
// classes are read even for an object whose lines carry no event bit (an
// out-of-vocabulary R:Event$): a walk still visits it in a zone another
// object makes hot.
func (a *replArenaMask) classify(o *state.Object) {
	a.mask |= objectReplMask(o)
	a.ask |= objectReplAsk(o)
}

// The ask classes of an R: line (replLineAsk), unioned over an object's
// faces by objectReplAsk: the per-line facts the ask-free predicate's board
// scans count, so an arena carrying none of a class answers without a walk.
const (
	// replAskBody: a non-Moved line tapeAnyReplBodyMayAsk can count -- it
	// elects, its ReplaceWith$ body may ask, or the body opens a board gate.
	replAskBody uint32 = 1 << iota
	// replAskMovedOther: a Moved line that can apply to ANOTHER object's
	// battlefield entry (movedLineRejects admits it), which
	// tapeLandReplMayAsk counts for every entering object.
	replAskMovedOther
)

// replLineAsk is r's ask classes, read with its own face's SVars.
func replLineAsk(r *cards.Repl, f *cards.Face) uint32 {
	if r.Event == "Moved" {
		if !movedLineRejects(r, 1, events.Event{Obj: 2, To: state.ZBattlefield}) {
			return replAskMovedOther
		}
		return 0
	}
	if cards.ReplMayElect(r) || (r.With != nil && (cards.SAChainMayAsk(r.With, f.SVars, nil, false) || cards.SAChainBoardGates(r.With, f.SVars) != 0)) {
		return replAskBody
	}
	return 0
}

// objectReplAsk is the union of replLineAsk over every face objectReplMask
// reads (CopyFace and each of the card's faces).
func objectReplAsk(o *state.Object) uint32 {
	if o == nil {
		return 0
	}
	var m uint32
	if f := o.CopyFace; f != nil {
		for i := range f.Repls {
			m |= replLineAsk(&f.Repls[i], f)
		}
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if f == nil {
				continue
			}
			for i := range f.Repls {
				m |= replLineAsk(&f.Repls[i], f)
			}
		}
	}
	return m
}

// replArenaAskFor brings the union up to date and reports whether some
// arena object may carry an R: line of ask class c.
func replArenaAskFor(e *Engine, c uint32) bool {
	e.replArenaMaskFor(0)
	return e.replArena.ask&c != 0
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
