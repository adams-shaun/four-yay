// morph_land_test.go — the face-down Morph/Disguise cast of a printed LAND
// (CR 702.37a/702.168a/702.169a). A land carrying K:Morph or K:Disguise
// (Zoetic Cavern, K:Morph:2; Branch of Vitu-Ghazi, K:Disguise:3) is castable
// face down for {3} exactly like a creature carrier, and that cast is a
// CREATURE SPELL, never a land play (CR 305.1: playing a land is a special
// action; casting the spell goes on the stack). So the two rights are
// independent:
//
//   - the face-down cast does NOT increment LandsPlayed and does NOT
//     consume the turn's land drop — the seat can still play a different
//     land afterwards, which proves the quota was untouched (not merely
//     that no play_land option was present);
//   - a printed land offers ONLY play_land plus the face-down cast, never
//     the ordinary printed cast the nonland walk would offer;
//   - playing the land itself consumes a land drop and never puts the card
//     on the stack.
//
// The land's front face is a land, so the reachable ordinary-cast guard
// (castSuppressed/castRestricted/spellTimingOK/offerCastable) is re-applied
// inside the land branch; these tests exercise the affordability and timing
// ends of it.
//
// Decks are compiled corpus cards only (no Forge script text is committed
// here). A fresh worktree without `.cards` skips these tests via
// CorpusRegistry, exactly like the sibling morph tests.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// landCastOptions lists the pending priority decision's cast/play_land
// options for one object: every "cast" option for it, plus whether a
// "play_land" option is offered. It is the shape assertion the fix turns
// on — a printed land must have play_land and the face-down cast, and NO
// plain cast.
func landCastOptions(t *testing.T, e *Engine, id state.ObjID) (casts []decision.Option, playLand bool) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == id {
			playLand = true
		}
		if o.Kind == "cast" && o.Obj == id {
			casts = append(casts, o)
		}
	}
	return casts, playLand
}

// morphLandFaceDownCast drives the named printed-land card's face-down cast
// from seat 0's hand at Main1. It asserts the precondition (the object really
// is a land in the hand whose printed face carries the wanted family), the
// option SHAPE (play_land is offered, the face-down cast is offered, and NO
// plain cast is offered — the addition-not-replacement precondition this
// ticket is about), then casts and resolves. symbols funds exactly the {3};
// poolAfter is the exact total that must remain (0 when the pool was {3}).
func morphLandFaceDownCast(t *testing.T, e *Engine, name, mode, symbols string, poolAfter int) state.ObjID {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZHand)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand || o.Face() == nil || !o.Face().IsLand() {
		t.Fatalf("precondition: %s not a printed land in hand: %+v", name, o)
	}
	if got := morphDownFamily(o.Face()); got != mode {
		t.Fatalf("precondition: %s morphDownFamily = %q, want %q", name, got, mode)
	}
	addMana(t, e, 0, symbols)
	casts, playLand := landCastOptions(t, e, id)
	if !playLand {
		t.Fatalf("%s: no play_land option — a printed land must still be playable normally: %+v", name, e.Pending().Options)
	}
	var down *decision.Option
	for i := range casts {
		if casts[i].Mode == mode {
			down = &casts[i]
		}
		if casts[i].Mode == "" {
			t.Fatalf("%s: offered the ordinary printed cast — a printed land must not acquire it: %+v", name, casts[i])
		}
	}
	if down == nil {
		t.Fatalf("%s: no (%s) face-down cast option: %+v", name, mode, e.Pending().Options)
	}
	submitChoices(t, e, down.Index)
	// CR 708.4: a face-down spell has no targets to announce.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("face-down land cast posed a target ask: %+v", d)
	}
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Players[0].Pool.Total(); got != int32(poolAfter) {
		t.Fatalf("pool after face-down land cast = %d, want %d (exactly the {3} spent)", got, poolAfter)
	}
	return id
}

func TestMorphLandFaceDownCastDoesNotSpendLandPlay(t *testing.T) {
	reg := searchTestRegistry(t)
	// Seat 0's hand: the morph land under test plus a Forest, so the
	// land-drop assertion can be made behaviourally — after the face-down
	// cast the Forest must still be playable.
	e, cfg := manifestEngine(t, reg, "Zoetic Cavern", "Forest")
	id := morphLandFaceDownCast(t, e, "Zoetic Cavern", "morphed", "CCC", 0)
	o := e.G.Obj(id)
	if o.CastFlags&state.FlagMorphed == 0 || o.CastFlags&(state.FlagMegamorphed|state.FlagDisguised) != 0 {
		t.Fatalf("Zoetic Cavern CastFlags = %v, want exactly the morph family flag", o.CastFlags)
	}
	// Precondition for the land-drop assertion: the cast is a spell, so the
	// drop must be exactly as it was before the cast.
	if got := e.G.Players[0].LandsPlayed; got != 0 {
		t.Fatalf("land play consumed by the face-down cast: LandsPlayed = %d, want 0", got)
	}
	assertFaceDownTwoTwo(t, e, id, "Zoetic Cavern", nil)
	assertFaceDownMarkers(t, e, id, events.FaceDownEntryCounter)
	// The untouched land drop is REAL: the Forest is still playable and,
	// when played, consumes the drop and enters without touching the stack.
	forest := searchMoveByName(t, e, "Forest", state.ZHand)
	casts, playLand := landCastOptions(t, e, forest)
	if !playLand {
		t.Fatalf("Forest not playable after the face-down cast — the land drop WAS spent: %+v", e.Pending().Options)
	}
	if len(casts) != 0 {
		t.Fatalf("Forest wrongly offered cast options: %+v", casts)
	}
	stackBefore := len(e.G.Stack)
	playIndex := -1
	for _, opt := range e.Pending().Options {
		if opt.Kind == "play_land" && opt.Obj == forest {
			playIndex = opt.Index
		}
	}
	submitChoices(t, e, playIndex)
	if len(e.G.Stack) != stackBefore {
		t.Fatalf("playing a land put it on the stack: depth %d -> %d", stackBefore, len(e.G.Stack))
	}
	if got := e.G.Players[0].LandsPlayed; got != 1 {
		t.Fatalf("LandsPlayed after playing the Forest = %d, want 1", got)
	}
	if got := e.G.Obj(forest).Zone; got != state.ZBattlefield {
		t.Fatalf("Forest zone = %s, want battlefield", got)
	}
	// Playing a SECOND land is now forbidden — the quota was consumed by the
	// land play, not by the face-down cast.
	if id2 := findInHandByName(t, e, "Forest"); id2 != 0 {
		if _, playLand := landCastOptions(t, e, id2); playLand {
			t.Fatalf("a second land was playable after the quota was used")
		}
	}
	replayCheck(t, e, cfg)
}

func TestDisguiseLandFaceDownCastDoesNotSpendLandPlay(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Branch of Vitu-Ghazi", "Forest")
	id := morphLandFaceDownCast(t, e, "Branch of Vitu-Ghazi", "disguised", "CCC", 0)
	o := e.G.Obj(id)
	if o.CastFlags&state.FlagDisguised == 0 || o.CastFlags&(state.FlagMorphed|state.FlagMegamorphed) != 0 {
		t.Fatalf("Branch of Vitu-Ghazi CastFlags = %v, want exactly the disguise family flag", o.CastFlags)
	}
	if !o.Cloaked {
		t.Fatalf("disguised land Cloaked=false, want the ward {2} state bit")
	}
	if got := e.G.Players[0].LandsPlayed; got != 0 {
		t.Fatalf("land play consumed by the face-down cast: LandsPlayed = %d, want 0", got)
	}
	assertFaceDownTwoTwo(t, e, id, "Branch of Vitu-Ghazi", []string{"Ward:2"})
	assertFaceDownMarkers(t, e, id, events.CloakEntryCounter)
	forest := searchMoveByName(t, e, "Forest", state.ZHand)
	if _, playLand := landCastOptions(t, e, forest); !playLand {
		t.Fatalf("Forest not playable after the face-down cast — the land drop WAS spent: %+v", e.Pending().Options)
	}
	replayCheck(t, e, cfg)
}

// findInHandByName returns the id of the named card in seat 0's hand, or 0.
func findInHandByName(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	return 0
}

// TestMorphLandFaceDownCastStillOfferedAfterLandDrop is the temporal half of
// the independence claim: the face-down cast is a spell, so it must remain
// offered even after the turn's land play has been used (the play_land
// option itself is correctly gone). Without the fix the hand walk's land
// branch continues before the face-down offer, so this option never exists
// regardless of the drop.
func TestMorphLandFaceDownCastStillOfferedAfterLandDrop(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Zoetic Cavern")
	id := searchMoveByName(t, e, "Zoetic Cavern", state.ZHand)
	// Precondition: the land drop is spent (via the logged LandPlayed event,
	// the same one the play_land handler emits, so the log replays), and the
	// object is still in the hand (the offer reads the hand zone).
	e.emit(events.Event{Kind: events.LandPlayed, Player: 0})
	if got := e.G.Players[0].LandsPlayed; got != 1 {
		t.Fatalf("precondition: LandsPlayed = %d, want 1 after the logged play", got)
	}
	if got := e.G.Obj(id).Zone; got != state.ZHand {
		t.Fatalf("precondition: Zoetic Cavern zone = %s, want hand", got)
	}
	addMana(t, e, 0, "CCC")
	casts, playLand := landCastOptions(t, e, id)
	if playLand {
		t.Fatalf("play_land still offered after the drop was used: %+v", e.Pending().Options)
	}
	var down *decision.Option
	for i := range casts {
		if casts[i].Mode == "morphed" {
			down = &casts[i]
		}
	}
	if down == nil {
		t.Fatalf("the face-down cast is gone once the land drop is used: %+v", e.Pending().Options)
	}
	submitChoices(t, e, down.Index)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(id).Zone; got != state.ZBattlefield || !e.G.Obj(id).FaceDown {
		t.Fatalf("face-down cast after the drop did not resolve: zone %s faceDown %v", got, e.G.Obj(id).FaceDown)
	}
	if got := e.G.Players[0].LandsPlayed; got != 1 {
		t.Fatalf("LandsPlayed moved to %d during a face-down cast, want 1 (unchanged)", got)
	}
	replayCheck(t, e, cfg)
}

// TestMorphLandFaceDownCastWithheldWhenUnpayable is the affordability guard:
// a printed land with the keyword but no {3} available must offer neither the
// face-down cast nor, with the pool empty and the drop unspent, anything but
// play_land; adding exactly {3} must then surface the face-down cast. This is
// the same offerCastable gate the nonland face-down offer uses; without it a
// land would be offered a cast the payment then reverses (a livelock). The
// second half is what makes the negative half falsifiable: without the fix
// the affordable offer never appears, so the test fails rather than passing
// vacuously on an always-absent option.
func TestMorphLandFaceDownCastWithheldWhenUnpayable(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _ := manifestEngine(t, reg, "Zoetic Cavern")
	id := searchMoveByName(t, e, "Zoetic Cavern", state.ZHand)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("precondition: pool = %d, want 0 (nothing added)", got)
	}
	casts, playLand := landCastOptions(t, e, id)
	if len(casts) != 0 {
		t.Fatalf("unpayable face-down cast offered with an empty pool: %+v", casts)
	}
	if !playLand {
		t.Fatalf("the ordinary play_land offer vanished with the empty pool: %+v", e.Pending().Options)
	}
	// Now fund exactly the {3}: the face-down cast MUST appear. This is the
	// falsifiable end of the guard — without the land-branch offer it is
	// always absent.
	addMana(t, e, 0, "CCC")
	casts, _ = landCastOptions(t, e, id)
	var down bool
	for i := range casts {
		if casts[i].Mode == "morphed" {
			down = true
		}
	}
	if !down {
		t.Fatalf("face-down cast still absent after funding the {3}: %+v", e.Pending().Options)
	}
}
