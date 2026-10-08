package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// The priority offer walk's CastWithFlash fast path.
//
// castWithFlash (statics_cast.go) answers "does an active CastWithFlash static
// give p permission to cast id at instant speed" by merging the battlefield
// collection (activeStatics) with the card's OWN face statics
// (withSelfStatics) and, only when the merged list is EMPTY, returning false
// without a target census. Both halves of that emptiness test are fixed for a
// walk -- the board's CastWithFlash list is one fact per walk and the face's
// own static is a per-face constant -- so the walk reads them as two bits and
// skips the per-card activeStatics/withSelfStatics pair. That is legal-walk
// step S2 (docs/superpowers/specs/2026-10-06-legal-walk-design.md, §6 S2).
//
// The two bits answer the ORIGINAL expression exactly: the merged list is
// empty iff the board list is empty AND the face carries none, because
// withSelfStatics only ever APPENDS the face's own statics to the board list.
// Every skip recomputes that original expression when walkSkipVerify is on
// (the rules test binary and enginebench-verify) and panics on disagreement.
// Non-walk callers keep Engine.castWithFlash unchanged.

// faceHasCastWithFlash reports whether f carries its own S:Mode$ CastWithFlash
// static. It is the face half of the fast path, compiled once per configured
// face into walkFaceFacts.flash and read from there while the facts are
// fullyCurrent; this scan is the fallback for a face the table does not hold
// (a runtime copy face, an unconfigured card, a by-value face copy).
func faceHasCastWithFlash(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for i := range f.Statics {
		if f.Statics[i].Mode == "CastWithFlash" {
			return true
		}
	}
	return false
}

// flashBoard is the board half of the fast path: len(activeStatics(
// "CastWithFlash")) != 0, read once per walk. It is a standalone lazy bit on
// the walk (not part of walkBoardFacts) because it must be readable from the
// hand section before the battlefield section takes the board facts, and it
// reads only the CastWithFlash list -- the fused activeStatics cache is
// populated by the walk's first static read, so this is a list lookup, not an
// extra board scan.
func (w *legalWalk) flashBoard() bool {
	if !w.flashBoardSet {
		w.flashBoardBit = len(w.e.activeStatics("CastWithFlash")) != 0
		w.flashBoardSet = true
	}
	return w.flashBoardBit
}

// ownFlashFace reports whether the LIVE face of id carries its own
// CastWithFlash static (the face Engine.castWithFlash's withSelfStatics reads,
// since that pair is priced off e.G.Obj(id).Face()).
func (w *legalWalk) ownFlashFace(id state.ObjID) bool {
	o := w.e.G.Obj(id)
	if o == nil {
		return false
	}
	return w.faceFlash(o.Face())
}

// faceFlash is ownFlashFace for an explicit face, served from the face facts
// when the table holds an entry current over f's static list (flash is a pure
// function of f.Statics, so the list-identity guard is the only one needed;
// reading under staticsCurrent is implied by the fullyCurrent guard
// verifyFresh uses to refresh the bit).
func (w *legalWalk) faceFlash(f *cards.Face) bool {
	if ff := w.e.walkFaceFactsOf(f); ff != nil && ff.staticsCurrent(f) {
		return ff.flash
	}
	return faceHasCastWithFlash(f)
}

// castWithFlash is the walk's bit-fast castWithFlash: when neither the board
// nor the priced object's live face can contribute a static, the merged list
// is provably empty and Engine.castWithFlash would return false without a
// target census, so the lookup is skipped. Otherwise the engine path runs
// unchanged, so a board with any flash static (or a self-carried one) behaves
// exactly as before.
func (w *legalWalk) castWithFlash(p state.PlayerID, id state.ObjID) bool {
	if !w.flashBoard() && !w.ownFlashFace(id) {
		if walkSkipVerify {
			if w.e.castWithFlash(p, id) {
				panic(fmt.Sprintf("rules: walk CastWithFlash skip for obj %d dropped a grant", id))
			}
		}
		return false
	}
	return w.e.castWithFlash(p, id)
}

// castWithFlashAsFace is castWithFlash for an alternate-face cast route: the
// skip test reads the face being priced (offerAsFace flips the object to it
// before the grant is re-read), exactly as castWithFlashAsFace's own
// withSelfStatics does under the probe.
func (w *legalWalk) castWithFlashAsFace(p state.PlayerID, id state.ObjID, f *cards.Face) bool {
	if !w.flashBoard() && !w.faceFlash(f) {
		if walkSkipVerify {
			if w.e.castWithFlashAsFace(p, id, f) {
				panic(fmt.Sprintf("rules: walk CastWithFlash skip for obj %d face %q dropped a grant", id, f.Name))
			}
		}
		return false
	}
	return w.e.castWithFlashAsFace(p, id, f)
}

// spellTimingOK is the walk's spellTimingOK: the same predicate
// (spellTimingPre is the shared home), with w.castWithFlash replacing the
// per-card activeStatics/withSelfStatics lookup.
func (w *legalWalk) spellTimingOK(p state.PlayerID, id state.ObjID, f *cards.Face, sorcery bool) bool {
	ok, needFlash := w.e.spellTimingPre(p, id, f, sorcery)
	if !ok {
		return false
	}
	return !needFlash || w.castWithFlash(p, id)
}
