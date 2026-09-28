package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The "whenever X becomes attached" trigger class (trig:Attached), pinned
// end to end on real corpus cards: Siona, Captain of the Pyleas; Brood
// Keeper; Enormous Energy Blade; and Eriette, the Beguiler's deliberate
// silence. The matcher reads the engine's one real Attach event -- the same
// event the Aura cast path (K:Enchant -> SP$ Attach), the Equip activation
// and the logged-Attach fixture style all emit -- so every shape here rides
// the live emit path, and every game is replay-checked.

const attachedBearSrc = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

const attachedAuraSrc = "Name:Test Aura\nManaCost:G\nTypes:Enchantment Aura\nK:Enchant:Creature\nOracle:x\n"

// castCardOption returns the "cast" option in the pending priority decision
// whose Obj is id (a specific hand card), not merely the first castable one.
func castCardOption(t *testing.T, e *Engine, id state.ObjID) decision.Option {
	t.Helper()
	for _, o := range castOptions(t, e) {
		if o.Obj == id {
			return o
		}
	}
	t.Fatalf("no cast option for card %d in %+v", id, e.Pending().Options)
	return decision.Option{}
}

// TestAttachedSionaCreatesHumanSoldierOnAuraAttach is the filing card: Siona
// on the battlefield, an Aura cast at a creature she controls -- the Aura
// resolves, the Attach event fires her trigger, and one Human Soldier token
// is created.
func TestAttachedSionaCreatesHumanSoldierOnAuraAttach(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	siona := mustCorpusCard(t, reg, "Siona, Captain of the Pyleas")
	rancor := mustCorpusCard(t, reg, "Rancor")
	e, cfg := tokenReplGame(t, 91, siona, rancor)
	rancorID := moveSeededCard(t, e, 0, rancor, state.ZHand)
	moveSeededCard(t, e, 0, siona, state.ZBattlefield)
	// Push Siona's queued ETB Dig (Optional, zero eligible Auras left in
	// the library with Rancor in hand -- it resolves as a silent no-op)
	// onto the stack and drain it, so the priority decision below offers
	// the cast options again.
	e.pending = nil
	e.Advance()
	passUntilStackEmpty(t, e, 20)
	bear := putToken(t, e, 0, attachedBearSrc, state.ZBattlefield)

	addMana(t, e, 0, "G")
	submitChoices(t, e, castCardOption(t, e, rancorID).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("aura cast target decision: %+v", d)
	}
	idx := indexOfObjOption(d, bear)
	if idx < 0 {
		t.Fatalf("aura target ask does not offer the bear: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	if e.G.Obj(rancorID).AttachedTo != bear {
		t.Fatalf("rancor attached to %d, want bear %d", e.G.Obj(rancorID).AttachedTo, bear)
	}
	if n := countTokensNamedOnSeat(t, e, 0, "Human Soldier Token"); n != 1 {
		t.Fatalf("Human Soldier tokens on seat 0 = %d, want 1 (Siona's Attached trigger fired)", n)
	}
	replayCheck(t, e, cfg)
}

// The negative halves of Siona's gate: an OPPONENT-controlled Aura
// attaching does not fire ValidSource$ Aura.YouCtrl, and with no Siona on
// the battlefield the same attach creates no token.
func TestAttachedOpponentAuraAndNoSionaDoNotFire(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	siona := mustCorpusCard(t, reg, "Siona, Captain of the Pyleas")
	rancor := mustCorpusCard(t, reg, "Rancor")
	bearCard := card(t, attachedBearSrc)
	oppAura := card(t, "Name:Opp Aura\nManaCost:G\nTypes:Enchantment Aura\nK:Enchant:Creature\nOracle:x\n")
	e, cfg := tokenReplGameSeats(t, 92, []*cards.Card{siona}, []*cards.Card{oppAura, bearCard})

	moveSeededCard(t, e, 0, siona, state.ZBattlefield)
	e.pending = nil
	e.Advance()
	passUntilStackEmpty(t, e, 20)
	before := countTokenCreates(e, 0)

	// Seat 1's Aura enters and attaches to seat 1's bear through real
	// logged events (the combat_damage_trigger_test logged-Attach style).
	oppAuraID := moveSeededCard(t, e, 1, oppAura, state.ZBattlefield)
	oppBear := moveSeededCard(t, e, 1, bearCard, state.ZBattlefield)
	e.emit(events.Event{Kind: events.Attach, Obj: oppAuraID, IDs: []state.ObjID{oppBear}})
	if e.G.Obj(oppAuraID).AttachedTo != oppBear {
		t.Fatalf("opponent aura attached to %d", e.G.Obj(oppAuraID).AttachedTo)
	}
	if n := countTokenCreates(e, 0); n != before {
		t.Fatalf("opponent Aura attaching created %d seat-0 tokens, want 0 (ValidSource$ Aura.YouCtrl must fail)", n-before)
	}

	// The same attach with no Siona on the battlefield: seat 0's own Aura
	// enters and attaches, no token. The Rancor is seeded but never placed,
	// so use the authored fixture via putToken-free logged moves.
	fixtureAura := card(t, attachedAuraSrc)
	e2, cfg2 := tokenReplGameSeats(t, 93, []*cards.Card{rancor, fixtureAura}, nil)
	fixtureAuraID := moveSeededCard(t, e2, 0, fixtureAura, state.ZBattlefield)
	bear2 := putToken(t, e2, 0, attachedBearSrc, state.ZBattlefield)
	e2.emit(events.Event{Kind: events.Attach, Obj: fixtureAuraID, IDs: []state.ObjID{bear2}})
	if e2.G.Obj(fixtureAuraID).AttachedTo != bear2 {
		t.Fatalf("fixture aura attached to %d", e2.G.Obj(fixtureAuraID).AttachedTo)
	}
	if n := countTokensNamedOnSeat(t, e2, 0, "Human Soldier Token"); n != 0 {
		t.Fatalf("no-Siona attach created %d tokens, want 0", n)
	}
	replayCheck(t, e, cfg)
	replayCheck(t, e2, cfg2)
}

// TestAttachedBroodKeeperCreatesDragonOnAuraAttach: the bearer-side shape --
// ValidTarget$ Card.Self reads "Self" as the trigger's source (the keeper),
// so an Aura attaching to the keeper herself makes her controller's Dragon.
func TestAttachedBroodKeeperCreatesDragonOnAuraAttach(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	keeper := mustCorpusCard(t, reg, "Brood Keeper")
	rancor := mustCorpusCard(t, reg, "Rancor")
	e, cfg := tokenReplGame(t, 94, keeper, rancor)
	rancorID := moveSeededCard(t, e, 0, rancor, state.ZHand)
	keeperID := moveSeededCard(t, e, 0, keeper, state.ZBattlefield)
	passUntilStackEmpty(t, e, 20)

	addMana(t, e, 0, "G")
	submitChoices(t, e, castCardOption(t, e, rancorID).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("aura cast target decision: %+v", d)
	}
	idx := indexOfObjOption(d, keeperID)
	if idx < 0 {
		t.Fatalf("aura target ask does not offer the keeper: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	if e.G.Obj(rancorID).AttachedTo != keeperID {
		t.Fatalf("rancor attached to %d, want keeper %d", e.G.Obj(rancorID).AttachedTo, keeperID)
	}
	if n := countTokensNamedOnSeat(t, e, 0, "Dragon Token"); n != 1 {
		t.Fatalf("Dragon tokens on seat 0 = %d, want 1 (Brood Keeper's Attached trigger fired)", n)
	}
	replayCheck(t, e, cfg)
}

// TestAttachedEnormousEnergyBladeTapsTheBearer: the role-capture shape --
// ValidSource$ Card.Self (the attaching blade) and an execute reading
// TriggeredTargetLKICopy, so the TAP lands on the BEARER the capture
// recorded, driven through the real Equip activation.
func TestAttachedEnormousEnergyBladeTapsTheBearer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	blade := mustCorpusCard(t, reg, "Enormous Energy Blade")
	e, cfg := tokenReplGame(t, 95, blade)
	bladeID := moveSeededCard(t, e, 0, blade, state.ZBattlefield)
	bear := putToken(t, e, 0, attachedBearSrc, state.ZBattlefield)
	passUntilStackEmpty(t, e, 20)

	addMana(t, e, 0, "BB")
	submitChoices(t, e, abilityOption(t, e, bladeID, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("equip target decision: %+v", d)
	}
	idx := indexOfObjOption(d, bear)
	if idx < 0 {
		t.Fatalf("equip ask does not offer the bear: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	if e.G.Obj(bladeID).AttachedTo != bear {
		t.Fatalf("blade attached to %d, want bear %d", e.G.Obj(bladeID).AttachedTo, bear)
	}
	if !e.G.Obj(bear).Tapped {
		t.Fatal("the bearer (TriggeredTargetLKICopy) should be tapped by the blade's Attached trigger")
	}
	if p := e.Power(bear); p != 6 {
		t.Fatalf("bearer power = %d, want 6 (2/2 plus the blade's +4/+0)", p)
	}
	replayCheck(t, e, cfg)
}

// TestAttachedBearerCaptureDoesNotLeakIntoBecomesTarget is the canary for
// the bearer capture's SCOPING: TriggerBearer is set only by the Attached
// referent walk, so the shared TriggeredTargetLKICopy spelling keeps every
// other mode's pre-existing resolution. Horobi, Death's Wail (BecomesTarget,
// Defined$ TriggeredTargetLKICopy) is the real-corpus probe: his trigger's
// Remembered entry is the TARGETING SPELL, so the Destroy resolves against
// a stack object and no-ops -- Horobi survives the bolt at the bear. Reading
// the mode-agnostic TriggerTarget role through this spelling would instead
// bind his own source permanent (the referent walk's BecomesTarget capture)
// and Horobi would destroy HIMSELF whenever any other creature became
// targeted -- the regression this pin forbids.
func TestAttachedBearerCaptureDoesNotLeakIntoBecomesTarget(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	horobi := mustCorpusCard(t, reg, "Horobi, Death's Wail")
	bolt := card(t, "Name:Probe Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	bearCard := card(t, attachedBearSrc)
	e, cfg := tokenReplGameSeats(t, 98, []*cards.Card{horobi, bolt}, []*cards.Card{bearCard})
	horobiID := moveSeededCard(t, e, 0, horobi, state.ZBattlefield)
	boltID := moveSeededCard(t, e, 0, bolt, state.ZHand)
	oppBear := moveSeededCard(t, e, 1, bearCard, state.ZBattlefield)
	passUntilStackEmpty(t, e, 20)

	addMana(t, e, 0, "R")
	submitChoices(t, e, castCardOption(t, e, boltID).Index)
	d := e.Pending()
	idx := indexOfObjOption(d, oppBear)
	if d == nil || d.Kind != decision.KTarget || idx < 0 {
		t.Fatalf("bolt target ask: %+v", d)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(horobiID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Horobi in zone %v, want battlefield -- the bearer capture leaked into BecomesTarget provenance and his trigger destroyed him", o)
	}
	replayCheck(t, e, cfg)
}

// TestAttachedDetachDoesNotFire: events.Attach with NO IDs is a detach (the
// rules/attach.go SBA emits) -- the matcher must ignore it, so after Brood
// Keeper made her Dragon off the real attach, the detach makes no second
// Dragon.
func TestAttachedDetachDoesNotFire(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	keeper := mustCorpusCard(t, reg, "Brood Keeper")
	rancor := mustCorpusCard(t, reg, "Rancor")
	e, cfg := tokenReplGame(t, 96, keeper, rancor)
	rancorID := moveSeededCard(t, e, 0, rancor, state.ZHand)
	keeperID := moveSeededCard(t, e, 0, keeper, state.ZBattlefield)
	passUntilStackEmpty(t, e, 20)

	addMana(t, e, 0, "G")
	submitChoices(t, e, castCardOption(t, e, rancorID).Index)
	d := e.Pending()
	idx := indexOfObjOption(d, keeperID)
	if d == nil || d.Kind != decision.KTarget || idx < 0 {
		t.Fatalf("aura target ask: %+v", d)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if n := countTokensNamedOnSeat(t, e, 0, "Dragon Token"); n != 1 {
		t.Fatalf("Dragon tokens before the detach = %d, want 1", n)
	}

	// The detach: an Attach event with no IDs, the shape rules/attach.go's
	// SBA emits. The Rancor un-attaches (and the SBA then sends it to the
	// graveyard, firing its own return-to-hand trigger -- neither may
	// satisfy the Attached matcher).
	e.emit(events.Event{Kind: events.Attach, Obj: rancorID})
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(rancorID).AttachedTo != 0 {
		t.Fatalf("rancor still attached to %d after the detach emit", e.G.Obj(rancorID).AttachedTo)
	}
	if n := countTokensNamedOnSeat(t, e, 0, "Dragon Token"); n != 1 {
		t.Fatalf("Dragon tokens after the detach = %d, want 1 (a detach is not a becomes-attached)", n)
	}
	replayCheck(t, e, cfg)
}

// TestAttachedErietteStaysSilent: Eriette's line carries only
// TargetRelativeToSource$ (unread everywhere in this build) and no
// ValidTarget$ -- the matcher fails closed, so her trigger never fires and
// an Aura you control attaching to an opponent's permanent gains nothing.
func TestAttachedErietteStaysSilent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	eriette := mustCorpusCard(t, reg, "Eriette, the Beguiler")
	rancor := mustCorpusCard(t, reg, "Rancor")
	bearCard := card(t, attachedBearSrc)
	e, cfg := tokenReplGameSeats(t, 97, []*cards.Card{eriette, rancor}, []*cards.Card{bearCard})
	rancorID := moveSeededCard(t, e, 0, rancor, state.ZHand)
	moveSeededCard(t, e, 0, eriette, state.ZBattlefield)
	passUntilStackEmpty(t, e, 20)
	oppBear := moveSeededCard(t, e, 1, bearCard, state.ZBattlefield)

	addMana(t, e, 0, "G")
	submitChoices(t, e, castCardOption(t, e, rancorID).Index)
	d := e.Pending()
	idx := indexOfObjOption(d, oppBear)
	if d == nil || d.Kind != decision.KTarget || idx < 0 {
		t.Fatalf("aura target ask: %+v", d)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	if e.G.Obj(rancorID).AttachedTo != oppBear {
		t.Fatalf("rancor attached to %d, want opponent bear %d", e.G.Obj(rancorID).AttachedTo, oppBear)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.ControlChange {
			t.Fatalf("Eriette's silent Attached trigger gained control: %+v", ev)
		}
	}
	if e.controllerOf(oppBear) != 1 {
		t.Fatalf("opponent bear now controlled by %d, want 1", e.controllerOf(oppBear))
	}
	replayCheck(t, e, cfg)
}
