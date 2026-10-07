package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// The legal-walk S2 fast path (rules/legal_walk_flash.go): the walk answers
// castWithFlash's emptiness test from two precomputed bits -- a per-walk
// board bit and a per-face own-static bit -- instead of running
// activeStatics + withSelfStatics per card. These tests pin the two bits
// separately:
//
//   - a self-carried grant must survive with NO battlefield static (the face
//     bit; a board-only skip would drop it and withhold the cast), and
//   - a battlefield grant must survive (the board bit), and
//   - with neither bit set the cast is withheld (the skip path).
//
// walkSkipVerify is on in this test binary, so every skip also recomputes the
// original expression and panics on disagreement.

const s2SelfFlashSpell = "Name:Snap Sorcery\nManaCost:0\nTypes:Sorcery\n" +
	"S:Mode$ CastWithFlash | ValidCard$ Card.Self | ValidSA$ Spell | EffectZone$ All | Caster$ You | Description$ You may cast this as though it had flash.\n" +
	"A:SP$ Draw | Defined$ You\nOracle:x\n"

const s2BoardFlashSource = "Name:Orrery Stand-In\nTypes:Artifact\n" +
	"S:Mode$ CastWithFlash | ValidCard$ Card | ValidSA$ Spell | Caster$ You | Description$ You may cast spells as though they had flash.\n" +
	"Oracle:x\n"

const s2PlainSorcery = "Name:Slow Spell\nManaCost:0\nTypes:Sorcery\nA:SP$ Draw | Defined$ You\nOracle:x\n"

// s2ConfiguredEngine compiles c (so walkFaceFactsOf has an entry for its face)
// and puts it in seat 0's hand at instant-window timing: seat 0's Upkeep with
// an empty stack. Timing is then the only gate a flash grant can lift.
func s2ConfiguredEngine(t *testing.T, c *cards.Card) (*Engine, state.ObjID) {
	t.Helper()
	deck := append([]*cards.Card{c}, mountainDeck(t, 40)...)
	e := New(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}})
	e.G.SetZone(state.ZHand, 0, nil)
	o := e.G.AddObject(c, 0)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{o.ID})
	e.G.Step = state.StepUpkeep
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	if o.Face() == nil || o.Face().IsInstant() {
		t.Fatal("precondition: the probe card must be a non-instant, else timing would not gate it")
	}
	if e.G.Active != 0 || e.G.Step != state.StepUpkeep || len(e.G.Stack) != 0 {
		t.Fatalf("precondition: active=%d step=%v stack=%d, want seat 0 in Upkeep with an empty stack",
			e.G.Active, e.G.Step, e.G.Stack)
	}
	if mayFlashSacFace(o.Face()) || e.hasKeywordH(o.ID, kwhFlash) {
		t.Fatal("precondition: no printed Flash/MayFlashSac may be present, else timing would not gate")
	}
	return e, o.ID
}

// TestWalkFlashFaceFactMatchesScan proves the per-face bit is computed and
// current for a CONFIGURED face (the cached walkFaceFacts path the walk
// actually reads) and for one that carries no static.
func TestWalkFlashFaceFactMatchesScan(t *testing.T) {
	ownCard := card(t, s2SelfFlashSpell)
	e, _ := s2ConfiguredEngine(t, ownCard)
	own := e.G.Obj(e.G.Zone(state.ZHand, 0)[0]).Face()
	if !faceHasCastWithFlash(own) {
		t.Fatal("precondition: the self-flash card's face carries no CastWithFlash static")
	}
	ff := e.walkFaceFactsOf(own)
	if ff == nil || !ff.staticsCurrent(own) {
		t.Fatal("precondition: no fully current face facts for the configured self-flash face")
	}
	if !ff.flash {
		t.Error("walkFaceFacts.flash is false for a face carrying its own CastWithFlash static")
	}

	plainCard := card(t, s2PlainSorcery)
	pe, _ := s2ConfiguredEngine(t, plainCard)
	plain := pe.G.Obj(pe.G.Zone(state.ZHand, 0)[0]).Face()
	if faceHasCastWithFlash(plain) {
		t.Fatal("precondition: the plain sorcery's face unexpectedly carries a CastWithFlash static")
	}
	pf := pe.walkFaceFactsOf(plain)
	if pf == nil || !pf.staticsCurrent(plain) {
		t.Fatal("precondition: no fully current face facts for the configured plain face")
	}
	if pf.flash {
		t.Error("walkFaceFacts.flash is true for a face carrying no CastWithFlash static")
	}
}

// TestWalkCastWithFlashBits is the direct unit test of the two bits over a
// CONFIGURED face (so the walkFaceFacts.flash path, not the fallback scan, is
// read): true for a self-carried grant, true for a battlefield grant, false
// with neither.
func TestWalkCastWithFlashBits(t *testing.T) {
	selfCard := card(t, s2SelfFlashSpell)
	e, spell := s2ConfiguredEngine(t, selfCard)
	if ff := e.walkFaceFactsOf(e.G.Obj(spell).Face()); ff == nil || !ff.flash {
		t.Fatal("precondition: the configured self-flash face must carry the cached flash bit")
	}
	w := &legalWalk{e: e, p: 0, actionStatics: actionStaticSource{e: e}}
	if w.flashBoard() {
		t.Fatal("precondition: no battlefield CastWithFlash static yet")
	}
	if !w.ownFlashFace(spell) {
		t.Fatal("precondition: the spell's live face must carry its own CastWithFlash static")
	}
	if !w.castWithFlash(0, spell) {
		t.Error("the walk's fast path dropped a SELF-CARRIED CastWithFlash grant with an empty board list")
	}

	// A battlefield grant only: remove the self-carried face and add the
	// board static. The face bit must go false and the board bit true.
	plainCard := card(t, s2PlainSorcery)
	pe, plainSpell := s2ConfiguredEngine(t, plainCard)
	src := pe.G.AddObject(card(t, s2BoardFlashSource), 0)
	src.Zone = state.ZBattlefield
	pe.G.SetZone(state.ZBattlefield, 0, []state.ObjID{src.ID})
	wb := &legalWalk{e: pe, p: 0, actionStatics: actionStaticSource{e: pe}}
	if wb.ownFlashFace(plainSpell) {
		t.Fatal("precondition: the plain spell's face must carry no CastWithFlash static")
	}
	if !wb.flashBoard() {
		t.Fatal("precondition: a battlefield CastWithFlash static must set flashBoard")
	}
	if !wb.castWithFlash(0, plainSpell) {
		t.Error("the walk's fast path dropped a BATTLEFIELD CastWithFlash grant")
	}
}

// TestWalkFlashSkipWithholdsWithoutGrant exercises the skip itself: off-turn,
// with no battlefield static and no self-carried one, the cast must be
// withheld -- and the same board must offer it once a battlefield grant
// exists, so the skip is not over-broad.
func TestWalkFlashSkipWithholdsWithoutGrant(t *testing.T) {
	e, spell := s2ConfiguredEngine(t, card(t, s2PlainSorcery))
	w := &legalWalk{e: e, p: 0, actionStatics: actionStaticSource{e: e}}
	if w.flashBoard() || w.ownFlashFace(spell) {
		t.Fatal("precondition: neither flash bit may be set for the skip to be exercised")
	}
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatal("the off-turn sorcery was offered although no CastWithFlash grant exists")
	}
	// The precondition did not change: the spell is still in hand and off-turn.
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: the spell moved to %v; the negative assertion is vacuous", e.G.Obj(spell).Zone)
	}
	src := e.G.AddObject(card(t, s2BoardFlashSource), 0)
	src.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{src.ID})
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatal("the same off-turn sorcery was withheld after a battlefield CastWithFlash grant was added")
	}
}
