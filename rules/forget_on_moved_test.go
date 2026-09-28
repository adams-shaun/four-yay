package rules

// ForgetOnMoved$ (task inbox-paramcensus-effect-forgetonmoved): the
// move-driven lifetime of a created Effect — a card the effect remembers
// leaves the named zone and the effect's remembered set must drop it, so the
// grant/restriction follows what the effect still holds.
//
// The sweep itself is rules/layers.go's effectMoveSweep, wired from
// Engine.emit for every MoveZone and PutOnStack; effEffect (effects/misc.go)
// carries the param onto the registered ContinuousEffect. The census label
// param:api:Effect.ForgetOnMoved had already been deleted from
// knownUnsupportedParams (commit 06769b73, with the Rakdos deck import's
// sweep) when this task started, so these tests are the per-card proof the
// removal stands on: the two genuinely different move triggers the census
// cards carry, on the real corpus cards.
//
//   - Incinerate forgets on a BATTLEFIELD departure (the CantRegenerate
//     restriction stops applying to a creature that left and returned —
//     CR 400.7a's new object), so a later regeneration works again.
//   - Valakut Exploration forgets on an EXILE departure that is not a cast
//     (the card's own end-step trigger returns the exiled card to the
//     graveyard), so the may-play grant stops remembering it. The cast
//     departure (Exile→Stack) is the sibling shape already pinned by
//     TestRakdosMuscleSacTriggerExilesAndMayPlaysWithAnyTypeMana.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// findHandCard is unused; keep the helper out of the build.
// castAt funds nothing, casts the spell in hand, answers its single target
// ask with target, and drains the stack.
func castAt(t *testing.T, e *Engine, spellID, target state.ObjID) {
	t.Helper()
	cast := optionOf(t, e, "cast", spellID)
	if cast < 0 {
		t.Fatalf("cast option not offered for %d: %+v", spellID, e.Pending().Options)
	}
	submitChoices(t, e, cast)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target ask missing after casting %d: %+v", spellID, d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == target {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("target %d not offered: %+v", target, d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
}

// incinerateRestrictionOn returns the Effect-created CantRegenerate
// continuous effect that still remembers id, or nil.
func incinerateRestrictionOn(e *Engine, id state.ObjID) *state.ContinuousEffect {
	for i := range e.continuous {
		ce := &e.continuous[i]
		if ce.Restriction == "CantRegenerate" && objIDIn(ce.Remembered, id) {
			return ce
		}
	}
	return nil
}

const forgetRegenerator = "Name:Regenerator\nManaCost:1 G\nTypes:Creature Bear\nPT:2/4\nOracle:x\n"

// TestIncinerateForgetOnMovedRestoresRegeneration pins the restriction-side
// ForgetOnMoved$ Battlefield on the real corpus Incinerate. Phase A: the
// restriction is live, so a shielded bear dies to a plain destroy after
// Incinerate damaged it (the rider blocks the shield). Phase B: a second
// bear is Incinerated and then blinked out and back — the battlefield
// departure must sweep it from the effect's remembered set (CR 400.7a: the
// returning object is a new object), and the same destroy then consumes a
// fresh shield and the bear survives.
func TestIncinerateForgetOnMovedRestoresRegeneration(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	inc := choiceCorpusCard(t, "Incinerate")
	smite := card(t, "Name:Smite\nTypes:Instant\nA:SP$ Destroy | ValidTgts$ Creature\nOracle:x\n")
	// Two copies of each spell: phase A spends one of each, phase B the other.
	e := corpusEngine(t, reg, []*cards.Card{inc, inc, smite, smite}, nil)
	incID := findCardObj(t, e, 0, "Incinerate", state.ZHand)
	smiteID := findCardObj(t, e, 0, "Smite", state.ZHand)

	// Phase A — restriction live while the damaged creature stays put.
	bear1 := onBoard(t, e, 1, forgetRegenerator)
	regenEffect(e, bear1, "Regenerate", nil)
	addMana(t, e, 0, "RC")
	castAt(t, e, incID, bear1)
	if o := e.G.Obj(bear1); o.Zone != state.ZBattlefield || o.Damage != 3 || o.Counter("Shield") != 1 {
		t.Fatalf("phase A bear: zone=%v damage=%d shields=%d, want battlefield/3/1", o.Zone, o.Damage, o.Counter("Shield"))
	}
	if incinerateRestrictionOn(e, bear1) == nil {
		t.Fatalf("CantRegenerate effect does not remember the damaged bear: %+v", e.continuous)
	}
	castAt(t, e, smiteID, bear1)
	if o := e.G.Obj(bear1); o.Zone != state.ZGraveyard {
		t.Fatalf("phase A bear regenerated through Incinerate's CantRegenerate: zone=%v", o.Zone)
	}

	// Phase B — the same shape, then the battlefield departure.
	bear2 := onBoard(t, e, 1, forgetRegenerator)
	regenEffect(e, bear2, "Regenerate", nil)
	addMana(t, e, 0, "RC")
	castAt(t, e, findCardObj(t, e, 0, "Incinerate", state.ZHand), bear2)
	if o := e.G.Obj(bear2); o.Zone != state.ZBattlefield || o.Damage != 3 {
		t.Fatalf("phase B bear: zone=%v damage=%d, want battlefield/3", o.Zone, o.Damage)
	}
	if incinerateRestrictionOn(e, bear2) == nil {
		t.Fatalf("CantRegenerate effect does not remember bear2: %+v", e.continuous)
	}
	// Blink out: the departure itself must sweep the object from the effect.
	e.pending = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear2, From: state.ZBattlefield, To: state.ZExile})
	if ce := incinerateRestrictionOn(e, bear2); ce != nil {
		t.Fatalf("CantRegenerate effect still remembers the creature that left the battlefield: %+v", ce.Remembered)
	}
	// ...and back in: a new object, the rider textually over.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear2, From: state.ZExile, To: state.ZBattlefield})
	regenEffect(e, bear2, "Regenerate", nil)
	castAt(t, e, findCardObj(t, e, 0, "Smite", state.ZHand), bear2)
	o := e.G.Obj(bear2)
	if o.Zone != state.ZBattlefield || o.Counter("Shield") != 0 || !o.Tapped || o.Damage != 0 {
		t.Fatalf("regeneration did not work after the forget: zone=%v shields=%d tapped=%v damage=%d",
			o.Zone, o.Counter("Shield"), o.Tapped, o.Damage)
	}
}

// TestValakutExplorationGrantForgetsWhenExileDeparts pins the may-play
// grant's ForgetOnMoved$ Exile on the real corpus Valakut Exploration, on
// the non-cast departure arm: the enchantment's own end-step trigger returns
// the exiled card to its owner's graveyard, and the grant must stop
// remembering it ("you may play this card for as long as it remains exiled").
func TestValakutExplorationGrantForgetsWhenExileDeparts(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	val := choiceCorpusCard(t, "Valakut Exploration")
	e := corpusEngine(t, reg, []*cards.Card{val}, nil)
	findCardObj(t, e, 0, "Valakut Exploration", state.ZBattlefield)
	// Landfall: the land entering digs the (Mountain-only) library's top card
	// to exile and registers the may-play grant on it. The exile zone also
	// parks the trigger's own ability object after it resolves (CR 608.2m),
	// so the dug CARD is identified through the grant's remembered set.
	battlefieldFixture(t, e, 0, "Name:Land\nTypes:Land\nOracle:x\n")
	e.pending = nil
	e.priorityRound()
	passUntilStackEmpty(t, e, 30)
	// The dug CARD: the exile zone also parks the trigger's own ability
	// object after it resolves (CR 608.2m), so skip Card==nil entries.
	var exiledID state.ObjID
	for _, id := range e.G.Zone(state.ZExile, 0) {
		if o := e.G.Obj(id); o != nil && o.Card != nil && o.Zone == state.ZExile {
			if exiledID != 0 {
				t.Fatalf("two cards in exile after the landfall dig: %v", e.G.Zone(state.ZExile, 0))
			}
			exiledID = id
		}
	}
	if exiledID == 0 {
		t.Fatalf("no card in exile after the landfall dig: %v", e.G.Zone(state.ZExile, 0))
	}
	var grant *state.ContinuousEffect
	for i := range e.continuous {
		if ce := &e.continuous[i]; ce.MayPlay && objIDIn(ce.Remembered, exiledID) {
			grant = ce
		}
	}
	if grant == nil {
		t.Fatalf("may-play grant does not remember the exiled card %d: %+v", exiledID, e.continuous)
	}
	if grant.ForgetOnMoved != "Exile" {
		t.Fatalf("grant ForgetOnMoved = %q, want Exile", grant.ForgetOnMoved)
	}

	// The end step: Valakut's own trigger moves the exiled card to the
	// graveyard — a departure that is not a cast — and the sweep must drop it.
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(exiledID); o.Zone != state.ZGraveyard {
		t.Fatalf("exiled card zone = %v, want graveyard (the end-step trigger should have moved it)", o.Zone)
	}
	for i := range e.continuous {
		if ce := &e.continuous[i]; ce.MayPlay && objIDIn(ce.Remembered, exiledID) {
			t.Fatalf("may-play grant still remembers the card that left exile: %+v", ce.Remembered)
		}
	}
}
