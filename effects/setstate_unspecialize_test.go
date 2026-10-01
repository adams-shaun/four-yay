package effects

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task api:SetState Unspecialize: the SetState choke point treated
// Mode$ Unspecialize like an unrecognised face mode and ran the generic
// next-face walk -- from a later Specialize face that lands on a DIFFERENT
// specialization (or wraps), never the card's front face. The mode now
// selects face 0 through the same FlipFace fold every other face change
// uses, so the emitted event stream reconstructs the restoration in replay
// (the fakeHost double applies every Emit through events.Apply, so these
// pins exercise the event fold directly).
//
// The SA under test is the REAL compiled corpus ability (Lukamina's
// TrigUnspecialize SVar, a real `DB$ SetState | Defined$ TriggeredCard |
// Mode$ Unspecialize` body), never a synthetic re-typing of it: the corpus
// compiles the whole specialize file into one merged face, so the ability is
// found by scanning the registry for a SetState SVar body that carries the
// mode.

// corpusSetStateUnspecializeSA returns the REAL compiled Mode$ Unspecialize
// SetState sub-ability of the corpus (Lukamina, Moon Druid's file), asserting
// it really carries the shape under test -- a caller can never be handed an
// SA that only looks like it. The resolution scans the compiled registry's
// SVar tables; the compile merges the file's specialize sections into one
// face whose SVars carry the (last-wins) TrigUnspecialize body.
func corpusSetStateUnspecializeSA(t *testing.T) *cards.SA {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			for _, name := range slices.Sorted(maps.Keys(f.SVars)) {
				body := f.SVars[name]
				if !strings.Contains(body, "SetState") || !strings.Contains(body, "Unspecialize") {
					continue
				}
				sa := cards.ResolveSVar(f.SVars, name)
				if sa == nil || sa.API != "SetState" {
					continue
				}
				if !strings.EqualFold(strings.TrimSpace(sa.Params["Mode"]), "Unspecialize") {
					continue
				}
				if strings.EqualFold(strings.TrimSpace(sa.Params["Optional"]), "True") {
					continue
				}
				return sa
			}
		}
	}
	t.Fatal("corpus has no compiled Mode$ Unspecialize SetState SA")
	return nil
}

// unspecializeOn builds a copy of the real corpus SA addressed at the object
// under test. The corpus body's Defined$ TriggeredCard is a trigger-role
// referent this unit harness has no triggering event for; re-pointing ONLY
// that param at Defined$ Self keeps every other parameter of the compiled
// body byte-identical while the face-mutating branch itself is untouched.
func unspecializeOn(sa *cards.SA) *cards.SA {
	params := make(map[string]string, len(sa.Params))
	for k, v := range sa.Params {
		params[k] = v
	}
	params["Defined"] = "Self"
	copy := *sa
	copy.Params = params
	return &copy
}

// fourFacedCard is a four-face fixture: parse.go starts a new Face at every
// ALTERNATE marker. Four faces (not two) is what makes the generic
// next-face walk observably WRONG for Unspecialize: from face 2 it lands on
// face 3, not the front face the mode must restore.
func fourFacedCard(t *testing.T) *cards.Card {
	t.Helper()
	return mkCard(t, "Name:Front\nTypes:Creature\nPT:1/1\nOracle:x\n\n"+
		"ALTERNATE\n\nName:Second\nTypes:Creature\nPT:2/2\nOracle:x\n\n"+
		"ALTERNATE\n\nName:Third\nTypes:Creature\nPT:3/3\nOracle:x\n\n"+
		"ALTERNATE\n\nName:Fourth\nTypes:Creature\nPT:4/4\nOracle:x\n")
}

// specializedObject adds the card to seat owner's battlefield and parks it
// on face idx, the stand-in for a specialized permanent.
func specializedObject(t *testing.T, h *fakeHost, owner state.PlayerID, idx int) state.ObjID {
	t.Helper()
	o := h.g.AddObject(fourFacedCard(t), owner)
	o.Zone = state.ZBattlefield
	o.SetFaceIdx(uint8(idx))
	h.g.SetZone(state.ZBattlefield, owner, append(h.g.Zone(state.ZBattlefield, owner), o.ID))
	return o.ID
}

// flipFaceEvents returns the FlipFace events the host recorded.
func flipFaceEvents(h *fakeHost) []events.Event {
	var out []events.Event
	for _, ev := range h.log {
		if ev.Kind == events.FlipFace {
			out = append(out, ev)
		}
	}
	return out
}

// handlerRan asserts the SetState registration actually dispatched (an
// unregistered API would emit an "unimplemented API" Note and the no-op
// pins below would otherwise pass with the whole feature reverted).
func handlerRan(t *testing.T, h *fakeHost) {
	t.Helper()
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API") {
			t.Fatalf("SetState never dispatched: %q", ev.Text)
		}
	}
}

// TestSetStateUnspecializeRestoresTheFrontFace pins the fix end to end with
// the REAL compiled corpus ability on an object parked on face 2 of four:
// the mode must land on the FRONT face, not the next one. Every
// precondition is asserted, including that the generic walk would have gone
// somewhere else -- without that, this test could not fail on a regression
// to the generic path.
func TestSetStateUnspecializeRestoresTheFrontFace(t *testing.T) {
	h := newHost(t, 2)
	id := specializedObject(t, h, 0, 2)
	o := h.g.Obj(id)
	if o == nil {
		t.Fatal("object not on the game")
	}
	if o.Zone != state.ZBattlefield {
		t.Fatalf("zone = %v, want Battlefield", o.Zone)
	}
	if n := len(o.Card.Faces); n != 4 {
		t.Fatalf("fixture has %d faces, want 4", n)
	}
	if o.FaceIdx != 2 {
		t.Fatalf("FaceIdx = %d, want 2", o.FaceIdx)
	}
	if generic := (int(o.FaceIdx) + 1) % len(o.Card.Faces); generic == 0 {
		t.Fatalf("precondition void: generic next-face walk from %d lands on 0 too", o.FaceIdx)
	}

	sa := unspecializeOn(corpusSetStateUnspecializeSA(t))
	Resolve(h, &Ctx{Controller: 0, Source: id}, sa)

	if o.FaceIdx != 0 {
		t.Fatalf("FaceIdx = %d, want 0 (the front face)", o.FaceIdx)
	}
	if name := o.Face().Name; name != "Front" {
		t.Fatalf("face = %q, want the front face", name)
	}
	flips := flipFaceEvents(h)
	if len(flips) != 1 {
		t.Fatalf("FlipFace events = %d, want exactly 1 (%+v)", len(flips), h.log)
	}
	if flips[0].Obj != id || flips[0].Amount != 0 {
		t.Fatalf("FlipFace = {obj %d amount %d}, want {obj %d amount 0}", flips[0].Obj, flips[0].Amount, id)
	}
	handlerRan(t, h)
}

// TestSetStateUnspecializeAlreadyOnTheFrontFaceIsANoOp pins the idempotent
// case: unspecializing an unspecialized permanent changes nothing.
func TestSetStateUnspecializeAlreadyOnTheFrontFaceIsANoOp(t *testing.T) {
	h := newHost(t, 2)
	id := specializedObject(t, h, 0, 0)
	o := h.g.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("object not on the battlefield")
	}
	if o.FaceIdx != 0 || o.Face().Name != "Front" {
		t.Fatalf("precondition void: already on face %d %q", o.FaceIdx, o.Face().Name)
	}

	Resolve(h, &Ctx{Controller: 0, Source: id}, unspecializeOn(corpusSetStateUnspecializeSA(t)))

	if o.FaceIdx != 0 {
		t.Fatalf("FaceIdx = %d, want it unchanged at 0", o.FaceIdx)
	}
	if n := len(flipFaceEvents(h)); n != 0 {
		t.Fatalf("FlipFace events = %d, want none for an already-front object", n)
	}
	handlerRan(t, h)
}

// TestSetStateUnspecializeSingleFacedCardIsANoOp pins the single-face guard:
// a card with one face is already its own front face, so the mode is a
// no-op (and never the generic path's single-face continue, which it shares).
func TestSetStateUnspecializeSingleFacedCardIsANoOp(t *testing.T) {
	h := newHost(t, 2)
	o := h.g.AddObject(mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	o.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{o.ID})
	if len(o.Card.Faces) != 1 {
		t.Fatalf("fixture has %d faces, want 1", len(o.Card.Faces))
	}

	Resolve(h, &Ctx{Controller: 0, Source: o.ID}, unspecializeOn(corpusSetStateUnspecializeSA(t)))

	if o.FaceIdx != 0 {
		t.Fatalf("FaceIdx = %d, want 0", o.FaceIdx)
	}
	if n := len(flipFaceEvents(h)); n != 0 {
		t.Fatalf("FlipFace events = %d, want none for a single-faced card", n)
	}
	handlerRan(t, h)
}
