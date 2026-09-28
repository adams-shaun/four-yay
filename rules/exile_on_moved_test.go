package rules

// ExileOnMoved$ (task inbox-paramcensus-exile-on-moved): the whole-effect
// end of a created Effect — a card the effect remembers leaves the named
// zone and the ENTIRE continuous effect ends ("the effect is exiled"),
// unlike ForgetOnMoved$, which only drops the card from the remembered set
// and lets the effect live on for whatever else it still holds.
//
// The sweep is rules/layers.go's effectMoveSweep (wired from Engine.emit for
// every MoveZone and PutOnStack); effEffect (effects/misc.go) carries the
// param onto the registered ContinuousEffect. The census label
// param:api:Effect.ExileOnMoved was already deleted from
// knownUnsupportedParams (commit 06769b73, the same commit that landed the
// sweep with the Rakdos deck import), so — as for ForgetOnMoved$ — these
// tests are the per-card proof the removal stands on, on real corpus cards:
//
//   - Abbot of Keral Keep (EXILE departure): the ETB dig exiles the top
//     card and the may-play grant remembers it; once the card leaves exile
//     the whole grant is gone from the registry, not merely forgotten.
//   - Vines of Vastwood (BATTLEFIELD departure): the CantTarget restriction
//     stops blocking the blinked creature, and the differential is real —
//     while the creature stays put the opponent cannot target it, after the
//     blink-return they can again (CR 400.7a's new object was never
//     remembered by the ended effect).
//
// Escape Tunnel and Whirler Rogue share the label in the census but their
// Effect SAs carry StaticAbilities$ Unblockable (Mode$ CantBlockBy), which
// effEffect does not register (reported separately) — the sweep is
// param-complete for everything it can be handed.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// mayPlayGrantOn returns the Effect-created may-play grant remembering id,
// or nil.
func mayPlayGrantOn(e *Engine, id state.ObjID) *state.ContinuousEffect {
	for i := range e.continuous {
		if ce := &e.continuous[i]; ce.MayPlay && objIDIn(ce.Remembered, id) {
			return ce
		}
	}
	return nil
}

// TestAbbotOfKeralKeepEffectEndsWhenExileDeparts pins the may-play grant's
// ExileOnMoved$ Exile on the real corpus Abbot of Keral Keep: the ETB dig
// exiles the top card, the grant remembers it, and once the card leaves
// exile the whole Effect ends — the grant is removed from the registry
// outright (ExileOnMoved$), not merely forgotten (ForgetOnMoved$'s weaker
// outcome).
func TestAbbotOfKeralKeepEffectEndsWhenExileDeparts(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	abbot := choiceCorpusCard(t, "Abbot of Keral Keep")
	e := corpusEngine(t, reg, []*cards.Card{abbot}, nil)
	abbotID := findCardObj(t, e, 0, "Abbot of Keral Keep", state.ZHand)

	addMana(t, e, 0, "1R")
	// Abbot has no target ask: cast, then drain the resolving ETB trigger.
	cast := optionOf(t, e, "cast", abbotID)
	if cast < 0 {
		t.Fatalf("cast option not offered for %d: %+v", abbotID, e.Pending().Options)
	}
	submitChoices(t, e, cast)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(abbotID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Abbot zone = %+v, want battlefield", o)
	}
	// The dig exiled the top card (a Mountain from the padded deck). The
	// exile zone also parks the resolved trigger's own ability object (CR
	// 608.2m), so the dug CARD is identified through Card != nil.
	var exiledID state.ObjID
	for _, id := range e.G.Zone(state.ZExile, 0) {
		if o := e.G.Obj(id); o != nil && o.Card != nil && o.Zone == state.ZExile {
			if exiledID != 0 {
				t.Fatalf("two cards in exile after Abbot's dig: %v", e.G.Zone(state.ZExile, 0))
			}
			exiledID = id
		}
	}
	if exiledID == 0 {
		t.Fatalf("no card in exile after Abbot's dig: %v", e.G.Zone(state.ZExile, 0))
	}
	grant := mayPlayGrantOn(e, exiledID)
	if grant == nil {
		t.Fatalf("may-play grant does not remember the exiled card %d: %+v", exiledID, e.continuous)
	}
	if grant.ExileOnMoved != "Exile" {
		t.Fatalf("grant ExileOnMoved = %q, want Exile", grant.ExileOnMoved)
	}

	// The departure: the card leaves exile and the WHOLE effect must end.
	e.pending = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: exiledID, From: state.ZExile, To: state.ZGraveyard})
	if grant := mayPlayGrantOn(e, exiledID); grant != nil {
		t.Fatalf("may-play grant still remembers the card that left exile: %+v", grant.Remembered)
	}
	for i := range e.continuous {
		if ce := &e.continuous[i]; ce.MayPlay && ce.ExileOnMoved == "Exile" {
			t.Fatalf("the ExileOnMoved effect itself survived its card's departure: %+v", ce)
		}
	}
}

// vinesTargeter returns the Effect-created CantTarget restriction
// remembering id, or nil.
func vinesTargeter(e *Engine, id state.ObjID) *state.ContinuousEffect {
	for i := range e.continuous {
		ce := &e.continuous[i]
		if ce.Restriction == "CantTarget" && objIDIn(ce.Remembered, id) {
			return ce
		}
	}
	return nil
}

// TestVinesOfVastwoodEffectEndsWhenBattlefieldDeparts pins the CantTarget
// restriction's ExileOnMoved$ Battlefield on the real corpus Vines of
// Vastwood. While the remembered creature stays put the opponent cannot
// target it; once it is blinked out the whole effect ENDS (the sweep removes
// the restriction outright), and after it blinks back — a new object under
// CR 400.7a — the opponent can target it again.
func TestVinesOfVastwoodEffectEndsWhenBattlefieldDeparts(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	vines := choiceCorpusCard(t, "Vines of Vastwood")
	e := corpusEngine(t, reg, []*cards.Card{vines}, nil)
	vinesID := findCardObj(t, e, 0, "Vines of Vastwood", state.ZHand)
	bear := onBoard(t, e, 1, forgetRegenerator)

	addMana(t, e, 0, "G")
	castAt(t, e, vinesID, bear)
	if o := e.G.Obj(bear); o.Zone != state.ZBattlefield {
		t.Fatalf("bear zone = %v, want battlefield", o.Zone)
	}
	ce := vinesTargeter(e, bear)
	if ce == nil {
		t.Fatalf("CantTarget effect does not remember the targeted bear: %+v", e.continuous)
	}
	if ce.ExileOnMoved != "Battlefield" {
		t.Fatalf("restriction ExileOnMoved = %q, want Battlefield", ce.ExileOnMoved)
	}
	if !e.restrictionBlocksTarget(bear, 1) {
		t.Fatal("the opponent can target the creature the Vines effect protects")
	}

	// The blink-out: the whole restriction ends, not just the membership.
	e.pending = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZExile})
	if ce := vinesTargeter(e, bear); ce != nil {
		t.Fatalf("CantTarget effect survived the creature's battlefield departure: %+v", ce.Remembered)
	}
	// ...and back in: a new object, and nothing protects it any more.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZExile, To: state.ZBattlefield})
	if e.restrictionBlocksTarget(bear, 1) {
		t.Fatal("the returned creature is still shielded from the opponent's targeting")
	}
}
