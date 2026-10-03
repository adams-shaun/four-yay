// gift_of_doom_test.go — CR 704.5m's Aura classification against a face-down
// permanent (CR 708.5). Gift of Doom is the one corpus card that prints both
// `Types:Enchantment Aura` and `K:Morph:` (measured: 1263 corpus cards print
// an Aura type, exactly 1 of them a morph-family keyword), so it is the card
// the printed-face subtype read killed: at an SBA checkpoint the face-down
// 2/2 was swept to the graveyard as "Aura attached to nothing" before its
// controller could ever pay the Morph turn-up cost. The tests prove, end to
// end, on the real compiled corpus card:
//
//   - a face-down Gift of Doom survives SBA checkpoints indefinitely — its
//     CURRENT characteristics are exactly [Creature], and the attachment SBA
//     classifies with them, never the hidden printed face;
//   - with another creature seated, the turn-face-up special action pays its
//     printed Morph cost — sacrificing another creature — the TurnFaceUp
//     event folds exactly once, and the printed turn-up replacement offers
//     the optional attach; choosing the remaining bear attaches the now
//     face-up Aura to it (deathtouch and indestructible go live on the
//     bearer);
//   - the face-up control: a face-up Gift of Doom attached to nothing STILL
//     dies at the SBA (CR 704.5m unchanged for ordinary Auras — the fix keys
//     on the face-down state, not on the card); and
//   - when the sacrifice cost consumed the only other creature, the turn-up
//     replacement refuses the attach with its Note and the now face-up,
//     unattached Aura goes to the graveyard at the next checkpoint (the
//     brief's declined branch, CR 704.5m again).
//
// No Forge script text is committed: every card comes from the compiled
// corpus through the search/manifest helpers. The helpers come from
// morph_test.go, morph_turnup_test.go, morph_turnup_cost_test.go,
// cast_test.go and search_library_test.go.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestGiftOfDoomFaceUpUnattachedAuraStillDies is the negative control: the
// fix keys on the FACE-DOWN state, not on the card. A face-up Gift of Doom
// attached to nothing is an ordinary unattached Aura (CR 704.5m) and still
// goes to its owner's graveyard at the checkpoint.
func TestGiftOfDoomFaceUpUnattachedAuraStillDies(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Gift of Doom")
	id := searchMoveByName(t, e, "Gift of Doom", state.ZHand)
	// A hand-built unattached face-up Aura (CR 303.4g keeps a real entry
	// with nothing to enchant out of play altogether).
	stageRawEntry(t, e, id)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.FaceDown {
		t.Fatalf("precondition: face-up battlefield entry missing: %+v", o)
	}
	if !hasTypeWord(e.G.Obj(id).Face().Types, "Aura") {
		t.Fatalf("precondition: printed types %v carry no Aura", e.G.Obj(id).Face().Types)
	}
	e.checkStateBased()
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the face-up unattached Gift of Doom is in %v, want the graveyard (CR 704.5m)", o)
	}
	replayCheck(t, e, cfg)
}

// TestGiftOfDoomTurnUpWithNoBearerGoesToGraveyard proves the declined branch:
// the printed Morph cost (Sac<1/Creature.Other>) consumes the only other
// creature, so the turn-up replacement finds no legal attach bearer, refuses
// with its Note, and the now face-up unattached Aura dies at the next SBA
// checkpoint (CR 704.5m again — the turn-up itself is never blocked).
func TestGiftOfDoomTurnUpWithNoBearerGoesToGraveyard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := manifestEngine(t, reg, "Gift of Doom", "Grizzly Bears")
	// The bear must precede the cast: the printed Aura cast needs a legal
	// bearer, and this bear is also the sacrifice-cost creature.
	bear := putCorpusPermanent(t, e, "Grizzly Bears")
	id := morphDownCast(t, e, "Gift of Doom", "morphed", "CCCCCB", 3)
	if o := e.G.Obj(id); !o.FaceDown {
		t.Fatalf("precondition: Gift of Doom is not face down")
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: bear %d not on the battlefield", bear)
	}
	mark := len(e.L.Events)
	idx := turnFaceUpIndex(t, e, id)
	submitChoices(t, e, idx)
	// The singleton Creature.Other candidate is the forced sacrifice cost —
	// no ask, settled inside the action. The turn-up replacement then finds
	// no legal bearer (the source itself is never offered) and refuses.
	passUntilStackEmpty(t, e, 20)
	note := false
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Note && ev.Obj == id && ev.Text == "cannot attach: no legal target" {
			note = true
		}
	}
	if !note {
		t.Fatalf("no attach-refusal Note for the bearerless turn-up")
	}
	assertTurnUpEventOnce(t, e, id, mark)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the face-up unattached Gift of Doom is in %v, want the graveyard", o)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the sacrifice-cost bear %d is in %v, want the graveyard", bear, o)
	}
	replayCheck(t, e, cfg)
}
