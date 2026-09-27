package effects

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task RememberChanged$ on DB$ SetState: effSetState emitted the face change
// but never joined the resolution's Remembered, so every chained consumer of
// the change read an empty list -- Megatron, Tyrant's DBMana produced nothing,
// Enduring Angel's lose-game gate saw the empty Remembered and INVERTED the
// printed card, Soul Seizer's DB$ Attach never fired, Lukamina's DBReturn
// never returned the unspecialized body. The fix appends each object the loop
// actually emitted a change for to Ctx.Remembered (the Dig precedent,
// digRemember), so the real compiled corpus bodies pin it here.
//
// Every SA under test is the REAL compiled corpus ability (never a synthetic
// re-typing), pulled out of the registry the same way the sibling
// setstate_unspecialize_test.go finds Lukamina's body.

// corpusMegatronTransformSA returns the REAL compiled Mode$ Transform
// RememberChanged$ True SetState sub-ability of the corpus (Megatron, Tyrant's
// TrigConvert), asserting it really carries the shape under test. Megatron's
// back face carries a second Mode$ Transform body (DBConvert) WITHOUT
// RememberChanged; the filter below demands the parameter, so it can never be
// handed that one.
func corpusMegatronTransformSA(t *testing.T) *cards.SA {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	for _, c := range reg.Cards {
		if len(c.Faces) == 0 || !strings.HasPrefix(c.Faces[0].Name, "Megatron, Tyrant") {
			continue
		}
		for _, f := range c.Faces {
			for _, name := range slices.Sorted(maps.Keys(f.SVars)) {
				sa := cards.ResolveSVar(f.SVars, name)
				if sa == nil || sa.API != "SetState" {
					continue
				}
				if !strings.EqualFold(strings.TrimSpace(sa.Params["Mode"]), "Transform") {
					continue
				}
				if !strings.EqualFold(strings.TrimSpace(sa.Params["RememberChanged"]), "True") {
					continue
				}
				if strings.EqualFold(strings.TrimSpace(sa.Params["Optional"]), "True") {
					continue
				}
				return sa
			}
		}
	}
	t.Fatal("corpus has no compiled Megatron, Tyrant Mode$ Transform RememberChanged$ True SetState SA")
	return nil
}

// withoutSub strips the compiled body's SubAbility$ chain link. The compiled
// chain (Megatron: DBMana -> DBCleanup ClearRemembered$ True) empties the Ctx
// list at its own end, so the pin wants the MID-CHAIN list this body produced;
// driving DBMana's mana production is out of scope for this ticket (its
// Amount$ X SVar machinery is not what the param read serves). Every other
// byte of the compiled body is untouched.
func withoutSub(sa *cards.SA) *cards.SA {
	out := *sa
	out.Sub = nil
	return &out
}

// withOptionalAdded copies the compiled body with an Optional$ True election
// added, for the decline pin: answered "no" (Ctx.SetStateOpt), the body must
// change no face and remember nothing.
func withOptionalAdded(sa *cards.SA) *cards.SA {
	params := make(map[string]string, len(sa.Params)+1)
	for k, v := range sa.Params {
		params[k] = v
	}
	params["Optional"] = "True"
	out := *sa
	out.Params = params
	return &out
}

// withoutRememberChanged copies the compiled body with the RememberChanged
// parameter removed -- the corpus default shape -- for the absent-param pin.
func withoutRememberChanged(sa *cards.SA) *cards.SA {
	params := make(map[string]string, len(sa.Params))
	for k, v := range sa.Params {
		params[k] = v
	}
	delete(params, "RememberChanged")
	out := *sa
	out.Params = params
	return &out
}

// twoFacedMegatronShape is the two-face fixture: Megatron, Tyrant compiles
// AlternateMode: DoubleFaced, so a Mode$ Transform walk really flips it.
func twoFacedMegatronShape(t *testing.T) *cards.Card {
	t.Helper()
	return mkCard(t, "Name:Megatron Tyrant\nTypes:Artifact Creature\nPT:7/5\nOracle:x\n\n"+
		"ALTERNATE\n\nName:Megatron Destructive\nTypes:Artifact Vehicle\nPT:4/5\nOracle:x\n")
}

// megatronObject adds the two-faced card to seat 0's battlefield parked on
// face idx and asserts the preconditions the pins below depend on.
func megatronObject(t *testing.T, h *fakeHost, idx int) state.ObjID {
	t.Helper()
	o := h.g.AddObject(twoFacedMegatronShape(t), 0)
	o.Zone = state.ZBattlefield
	o.FaceIdx = uint8(idx)
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{o.ID})
	if o.Zone != state.ZBattlefield {
		t.Fatal("object not on the battlefield")
	}
	if n := len(o.Card.Faces); n != 2 {
		t.Fatalf("fixture has %d faces, want 2", n)
	}
	return o.ID
}

// rememberedExactlyOne asserts the Ctx list grew to exactly the one object.
func rememberedExactlyOne(t *testing.T, ctx *Ctx, id state.ObjID) {
	t.Helper()
	if n := len(ctx.Remembered); n != 1 {
		t.Fatalf("Remembered holds %d entries, want the one changed object (%+v)", n, ctx.Remembered)
	}
	if ctx.Remembered[0].Obj != id || ctx.Remembered[0].IsPlayer {
		t.Fatalf("Remembered[0] = %+v, want the changed object %d", ctx.Remembered[0], id)
	}
}

// TestSetStateRememberChangedTransformRemembersTheChangedFace pins the
// Megatron, Tyrant carrier with the REAL compiled TrigConvert body: the
// transformed object must sit in the resolution's Remembered, where the
// chained DBMana gate (ConditionDefined$ Remembered | ConditionPresent$ Card)
// reads it.
func TestSetStateRememberChangedTransformRemembersTheChangedFace(t *testing.T) {
	h := newHost(t, 2)
	id := megatronObject(t, h, 0)
	if o := h.g.Obj(id); o.FaceIdx != 0 || o.Face().Name != "Megatron Tyrant" {
		t.Fatalf("precondition void: object at face %d %q, want the front face", o.FaceIdx, o.Face().Name)
	}
	ctx := &Ctx{Controller: 0, Source: id}
	if n := len(ctx.Remembered); n != 0 {
		t.Fatalf("precondition void: Remembered already holds %d entries", n)
	}

	Resolve(h, ctx, withoutSub(corpusMegatronTransformSA(t)))

	if o := h.g.Obj(id); o.FaceIdx != 1 || o.Face().Name != "Megatron Destructive" {
		t.Fatalf("face = %d %q, want 1 Megatron Destructive", o.FaceIdx, o.Face().Name)
	}
	rememberedExactlyOne(t, ctx, id)
	handlerRan(t, h)
}

// TestSetStateRememberChangedUnspecializeRemembersTheRestoredFace pins the
// Lukamina carrier: the real compiled TrigUnspecialize body (re-pointed
// Defined$ TriggeredCard -> Self, the sibling file's technique) restoring a
// specialized object to its front face must remember that object for the
// chained DBReturn (Defined$ Remembered). DBReturn's own Origin$ Graveyard
// gate is a no-op on a battlefield object, so the pin reads only the list the
// SetState body joined.
func TestSetStateRememberChangedUnspecializeRemembersTheRestoredFace(t *testing.T) {
	h := newHost(t, 2)
	id := specializedObject(t, h, 0, 2)
	if o := h.g.Obj(id); o.FaceIdx != 2 {
		t.Fatalf("precondition void: object at face %d, want 2", o.FaceIdx)
	}
	ctx := &Ctx{Controller: 0, Source: id}
	if n := len(ctx.Remembered); n != 0 {
		t.Fatalf("precondition void: Remembered already holds %d entries", n)
	}

	Resolve(h, ctx, unspecializeOn(corpusSetStateUnspecializeSA(t)))

	if o := h.g.Obj(id); o.FaceIdx != 0 {
		t.Fatalf("FaceIdx = %d, want 0 (the front face)", o.FaceIdx)
	}
	rememberedExactlyOne(t, ctx, id)
	handlerRan(t, h)
}

// TestSetStateRememberChangedOptionalDeclineRemembersNothing pins the decline
// path: an Optional$ True body answered "no" returns before the emitting
// loop, so it must neither flip nor remember -- Forge remembers the objects
// whose state CHANGED, not the Defined set.
func TestSetStateRememberChangedOptionalDeclineRemembersNothing(t *testing.T) {
	h := newHost(t, 2)
	id := megatronObject(t, h, 0)
	ctx := &Ctx{Controller: 0, Source: id, SetStateOpt: "no"}
	if n := len(ctx.Remembered); n != 0 {
		t.Fatalf("precondition void: Remembered already holds %d entries", n)
	}

	Resolve(h, ctx, withoutSub(withOptionalAdded(corpusMegatronTransformSA(t))))

	if o := h.g.Obj(id); o.FaceIdx != 0 {
		t.Fatalf("FaceIdx = %d, want it unchanged at 0 on a decline", o.FaceIdx)
	}
	if n := len(ctx.Remembered); n != 0 {
		t.Fatalf("decline remembered %d entries, want none (%+v)", n, ctx.Remembered)
	}
	handlerRan(t, h)
}

// TestSetStateRememberChangedAlreadyOnTheFrontFaceRemembersNothing pins the
// Unspecialize no-op: an object already on its front face emits nothing, so
// it must be remembered by nothing.
func TestSetStateRememberChangedAlreadyOnTheFrontFaceRemembersNothing(t *testing.T) {
	h := newHost(t, 2)
	id := specializedObject(t, h, 0, 0)
	if o := h.g.Obj(id); o.FaceIdx != 0 || o.Face().Name != "Front" {
		t.Fatalf("precondition void: object at face %d %q, want the front face", o.FaceIdx, o.Face().Name)
	}
	ctx := &Ctx{Controller: 0, Source: id}
	if n := len(ctx.Remembered); n != 0 {
		t.Fatalf("precondition void: Remembered already holds %d entries", n)
	}

	Resolve(h, ctx, unspecializeOn(corpusSetStateUnspecializeSA(t)))

	if o := h.g.Obj(id); o.FaceIdx != 0 {
		t.Fatalf("FaceIdx = %d, want it unchanged at 0", o.FaceIdx)
	}
	if n := len(ctx.Remembered); n != 0 {
		t.Fatalf("no-op remembered %d entries, want none (%+v)", n, ctx.Remembered)
	}
	handlerRan(t, h)
}

// TestSetStateRememberChangedSingleFacedCardRemembersNothing pins the
// single-faced guard: a Mode$ Transform walk on a one-face card continues
// before any emit, so it must remember nothing.
func TestSetStateRememberChangedSingleFacedCardRemembersNothing(t *testing.T) {
	h := newHost(t, 2)
	o := h.g.AddObject(mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	o.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{o.ID})
	if len(o.Card.Faces) != 1 {
		t.Fatalf("fixture has %d faces, want 1", len(o.Card.Faces))
	}
	ctx := &Ctx{Controller: 0, Source: o.ID}
	if n := len(ctx.Remembered); n != 0 {
		t.Fatalf("precondition void: Remembered already holds %d entries", n)
	}

	Resolve(h, ctx, withoutSub(corpusMegatronTransformSA(t)))

	if o.FaceIdx != 0 {
		t.Fatalf("FaceIdx = %d, want it unchanged at 0", o.FaceIdx)
	}
	if n := len(ctx.Remembered); n != 0 {
		t.Fatalf("single-faced walk remembered %d entries, want none (%+v)", n, ctx.Remembered)
	}
	handlerRan(t, h)
}

// TestSetStateRememberChangedAbsentParameterRemembersNothing pins the corpus
// default: the same compiled body with the parameter deleted still flips the
// face (proving the emitting branch really ran) but joins nothing to the Ctx
// list -- absent RememberChanged$ the walk adds nothing, so every
// pre-existing game replays byte-identically.
func TestSetStateRememberChangedAbsentParameterRemembersNothing(t *testing.T) {
	h := newHost(t, 2)
	id := megatronObject(t, h, 0)
	ctx := &Ctx{Controller: 0, Source: id}
	if n := len(ctx.Remembered); n != 0 {
		t.Fatalf("precondition void: Remembered already holds %d entries", n)
	}

	Resolve(h, ctx, withoutSub(withoutRememberChanged(corpusMegatronTransformSA(t))))

	if o := h.g.Obj(id); o.FaceIdx != 1 || o.Face().Name != "Megatron Destructive" {
		t.Fatalf("face = %d %q, want 1 Megatron Destructive (the emit must still run)", o.FaceIdx, o.Face().Name)
	}
	if n := len(ctx.Remembered); n != 0 {
		t.Fatalf("absent-param walk remembered %d entries, want none (%+v)", n, ctx.Remembered)
	}
	handlerRan(t, h)
}
