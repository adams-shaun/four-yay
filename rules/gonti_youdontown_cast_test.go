package rules

// trig:SpellCast.ValidSAonCard -- the single-field Spell.YouDontOwn head.
//
// Gonti, Night Minister's real compiled SpellCast trigger carries
// `ValidSAonCard$ Spell.YouDontOwn | ValidActivatingPlayer$ Player`:
// "Whenever a player casts a spell they don't own, that player creates a
// Treasure token." Before this fix the head was unmodelled and failed closed,
// so the trigger could never fire. The discriminator that matters is WHOSE
// ownership the clause reads: "they don't own" is relative to the CASTER
// (ev.Player), not to the trigger source's controller -- Gonti's
// ValidActivatingPlayer$ Player lets any player trigger it. Case 3 below is
// the wrong-binding probe: seat 1 casting seat 1's own card must stay silent
// even though Gonti's controller (seat 0) does not own it.
//
// The probe spell is a freely-authored fixture (no Forge script text is
// committed); Gonti itself is the REAL corpus card, and the fixture gives the
// probe an owner that differs from its controller with a direct AddObject +
// SetZone patch (the cost_filter_grammar_test.go cfHand shape), so the
// ownership asymmetry the trigger reads is explicit and asserted.

import (
	"maps"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// The probe is an INSTANT so seat 1 can cast it during seat 0's main phase
// (cases 2 and 3); a creature could only be cast by the active seat.
const gontiProbeScript = "Name:Ownership Probe\nManaCost:1 G\nTypes:Instant\nOracle:x\n"

// gontiEngine puts the real Gonti, Night Minister on seat 0's battlefield and
// asserts the fixture's precondition: the carrier is a face-up permanent whose
// SpellCast trigger really carries the head under test.
func gontiEngine(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	e, _ := miscHandsEngine(t, reg, nil, nil, []string{"Gonti, Night Minister"}, nil)
	gonti := miscBoardObj(t, e, 0, "Gonti, Night Minister")
	o := e.G.Obj(gonti)
	if o == nil || o.Face() == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("test precondition: Gonti is not a face-up battlefield permanent: %+v", o)
	}
	var castTrig *cards.Trigger
	for i := range o.Face().Triggers {
		if o.Face().Triggers[i].Mode == "SpellCast" {
			castTrig = &o.Face().Triggers[i]
		}
	}
	if castTrig == nil || castTrig.ParamStr(cards.PKValidSAonCard) != "Spell.YouDontOwn" {
		t.Fatalf("test precondition: Gonti's SpellCast trigger does not carry ValidSAonCard$ Spell.YouDontOwn: %+v", o.Face().Triggers)
	}
	return e, gonti
}

// gontiProbeInHand adds the probe spell to seat p's hand with owner as its
// OWNER (p is its controller), then re-asks priority so the new cast option
// is offered. It returns the id; the caller asserts the owner/controller
// asymmetry it depends on.
func gontiProbeInHand(t *testing.T, e *Engine, p, owner state.PlayerID) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, gontiProbeScript), owner)
	o.Zone = state.ZHand
	o.Controller = p
	e.G.SetZone(state.ZHand, p, append(e.G.Zone(state.ZHand, p), o.ID))
	e.pending = nil
	e.priorityRound()
	return o.ID
}

// gontiCastProbe submits seat p's cast option for id and asserts the spell
// actually reached the stack -- the precondition every silence assertion in
// this file depends on (a cast that never happened cannot prove a matcher
// silent).
func gontiCastProbe(t *testing.T, e *Engine, p state.PlayerID, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != p {
		t.Fatalf("test precondition: expected seat %d's priority before the cast, got %+v", p, d)
	}
	submitChoices(t, e, miscCastOption(t, e, id))
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZStack {
		t.Fatalf("test precondition: the probe spell is in %v after the cast submission, want the stack", o)
	}
}

// TestGontiNightMinisterTriggersOnSpellYouDontOwn (case 1): Gonti is seat 0's;
// seat 0 casts a spell OWNED by seat 1. The caster does not own the spell, so
// the trigger fires and the caster -- seat 0 -- gets the Treasure.
func TestGontiNightMinisterTriggersOnSpellYouDontOwn(t *testing.T) {
	t.Parallel()
	e, _ := gontiEngine(t)
	probe := gontiProbeInHand(t, e, 0, 1)
	o := e.G.Obj(probe)
	if o.Owner != 1 || o.Controller != 0 || o.Owner == o.Controller {
		t.Fatalf("test precondition: probe owner %d controller %d, want owner 1 controller 0 (owner != caster)", o.Owner, o.Controller)
	}
	addMana(t, e, 0, "GG")
	evBefore := len(e.L.Events)
	gontiCastProbe(t, e, 0, probe)
	passUntilStackEmpty(t, e, 30)
	if !spellCastTriggerFires(e, evBefore) {
		t.Fatal("Gonti's trigger did not fire on a spell its caster does not own")
	}
	if got := tokensNamed(e, 0, "Treasure"); got != 1 {
		t.Fatalf("the caster (seat 0) has %d Treasures, want exactly 1", got)
	}
	if got := tokensNamed(e, 1, "Treasure"); got != 0 {
		t.Fatalf("seat 1 has %d Treasures, want 0 (the caster owns the token)", got)
	}
}

// TestGontiNightMinisterMirrorCasterSeatOne (case 2): the mirrored caster. The
// same board fires for seat 1 casting a seat-0-owned spell, and the Treasure
// is seat 1's -- two caster seats, so a single-value fixture cannot pass by
// coincidence.
func TestGontiNightMinisterMirrorCasterSeatOne(t *testing.T) {
	t.Parallel()
	e, _ := gontiEngine(t)
	probe := gontiProbeInHand(t, e, 1, 0)
	o := e.G.Obj(probe)
	if o.Owner != 0 || o.Controller != 1 || o.Owner == o.Controller {
		t.Fatalf("test precondition: probe owner %d controller %d, want owner 0 controller 1 (owner != caster)", o.Owner, o.Controller)
	}
	addMana(t, e, 1, "GG")
	passToCast(t, e, probe)
	evBefore := len(e.L.Events)
	gontiCastProbe(t, e, 1, probe)
	passUntilStackEmpty(t, e, 30)
	if !spellCastTriggerFires(e, evBefore) {
		t.Fatal("Gonti's trigger did not fire on seat 1's cast of a spell seat 1 does not own")
	}
	if got := tokensNamed(e, 1, "Treasure"); got != 1 {
		t.Fatalf("the caster (seat 1) has %d Treasures, want exactly 1", got)
	}
	if got := tokensNamed(e, 0, "Treasure"); got != 0 {
		t.Fatalf("seat 0 has %d Treasures, want 0 (the caster owns the token)", got)
	}
}

// TestGontiNightMinisterYouBindingFollowsCaster (case 3): the you-binding
// discriminator. Gonti is controlled by seat 0, but seat 1 casts seat 1's OWN
// card: the caster owns the spell, so the trigger must stay SILENT even though
// Gonti's controller does not own it. A fix that binds `you` to the source's
// controller (0) instead of the caster (1) fires here. The second cast is the
// in-test positive control: with the ownership flipped, the same board fires,
// so the silence above cannot be a dead matcher.
func TestGontiNightMinisterYouBindingFollowsCaster(t *testing.T) {
	t.Parallel()
	e, gonti := gontiEngine(t)
	own := gontiProbeInHand(t, e, 1, 1)
	o := e.G.Obj(own)
	if o.Owner != 1 || o.Controller != 1 || o.Owner != o.Controller {
		t.Fatalf("test precondition: probe owner %d controller %d, want owner == controller == 1 (the caster owns it)", o.Owner, o.Controller)
	}
	if e.controllerOf(gonti) == o.Owner {
		t.Fatal("test precondition: the wrong-binding fixture needs Gonti's controller to differ from the caster/owner")
	}
	addMana(t, e, 1, "GGGG")
	passToCast(t, e, own)
	evBefore := len(e.L.Events)
	gontiCastProbe(t, e, 1, own)
	passUntilStackEmpty(t, e, 30)
	if spellCastTriggerFires(e, evBefore) {
		t.Fatal("Gonti's trigger fired on the caster's OWN spell; `you` was bound to the source's controller, not ev.Player")
	}
	if got := tokensNamed(e, 0, "Treasure") + tokensNamed(e, 1, "Treasure"); got != 0 {
		t.Fatalf("the silent cast created %d Treasures, want 0", got)
	}

	// Positive control: the same seat, same Gonti, same turn, a spell the
	// caster does NOT own -- must fire and hand seat 1 the Treasure.
	theirs := gontiProbeInHand(t, e, 1, 0)
	if o := e.G.Obj(theirs); o.Owner == o.Controller {
		t.Fatal("test precondition: control probe owner must differ from its caster")
	}
	passToCast(t, e, theirs)
	evControl := len(e.L.Events)
	gontiCastProbe(t, e, 1, theirs)
	passUntilStackEmpty(t, e, 30)
	if !spellCastTriggerFires(e, evControl) {
		t.Fatal("control cast did not fire Gonti's trigger: the silence above was a dead matcher, not the binding")
	}
	if got := tokensNamed(e, 1, "Treasure"); got != 1 {
		t.Fatalf("control cast gave seat 1 %d Treasures, want exactly 1", got)
	}
}

// TestGontiNightMinisterOwnCastIsSilent (case 4): seat 0 casts seat 0's own
// card -- owner equals caster, so the trigger stays silent. The second cast is
// the in-test positive control that proves the matcher is live.
func TestGontiNightMinisterOwnCastIsSilent(t *testing.T) {
	t.Parallel()
	e, _ := gontiEngine(t)
	own := gontiProbeInHand(t, e, 0, 0)
	o := e.G.Obj(own)
	if o.Owner != 0 || o.Controller != 0 || o.Owner != o.Controller {
		t.Fatalf("test precondition: probe owner %d controller %d, want owner == controller == 0 (the caster owns it)", o.Owner, o.Controller)
	}
	addMana(t, e, 0, "GGGG")
	evBefore := len(e.L.Events)
	gontiCastProbe(t, e, 0, own)
	passUntilStackEmpty(t, e, 30)
	if spellCastTriggerFires(e, evBefore) {
		t.Fatal("Gonti's trigger fired on the caster's OWN spell")
	}
	if got := tokensNamed(e, 0, "Treasure") + tokensNamed(e, 1, "Treasure"); got != 0 {
		t.Fatalf("the silent cast created %d Treasures, want 0", got)
	}

	// Positive control: seat 0 casts a seat-1-owned spell -- must fire.
	theirs := gontiProbeInHand(t, e, 0, 1)
	if o := e.G.Obj(theirs); o.Owner == o.Controller {
		t.Fatal("test precondition: control probe owner must differ from its caster")
	}
	evControl := len(e.L.Events)
	gontiCastProbe(t, e, 0, theirs)
	passUntilStackEmpty(t, e, 30)
	if !spellCastTriggerFires(e, evControl) {
		t.Fatal("control cast did not fire Gonti's trigger: the silence above was a dead matcher, not the ownership read")
	}
	if got := tokensNamed(e, 0, "Treasure"); got != 1 {
		t.Fatalf("control cast gave seat 0 %d Treasures, want exactly 1", got)
	}
}

// TestDragonlordKolaghanCompoundHeadStillFailsClosed drives the real compiled
// Dragonlord Kolaghan SpellCast trigger (the census's remaining unmodelled
// ValidSAonCard$ head, `Spell.Creature+sharesNameWith YourGraveyard,...`)
// through the matcher against a synthetic opponent cast. The compound head
// must still fail closed; the trimmed-trigger control proves the false result
// is attributable to the ValidSAonCard$ gate and not to an earlier arm.
func TestDragonlordKolaghanCompoundHeadStillFailsClosed(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	kolaghan := searchCorpusCard(t, reg, "Dragonlord Kolaghan")
	e := handEngine(t, kolaghan)
	id := e.G.Zone(state.ZHand, 0)[0]
	face := e.G.Obj(id).Face()
	if face == nil {
		t.Fatal("test precondition: Dragonlord Kolaghan has no face")
	}
	var trg *cards.Trigger
	for i := range face.Triggers {
		if face.Triggers[i].Mode == "SpellCast" {
			trg = &face.Triggers[i]
		}
	}
	if trg == nil || !strings.Contains(trg.ParamStr(cards.PKValidSAonCard), "sharesNameWith") {
		t.Fatalf("test precondition: Kolaghan's SpellCast trigger does not carry the compound ValidSAonCard$ head: %+v", face.Triggers)
	}
	ev := events.Event{Kind: events.PutOnStack, Obj: id, Player: 1, From: state.ZHand, To: state.ZStack}
	e.emit(ev)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack {
		t.Fatalf("test precondition: the synthetic cast left Kolaghan in %v, want the stack", o)
	}
	if trigmatch.SpellCastEval(boardOf(e), *trg, id, ev) {
		t.Fatal("Dragonlord Kolaghan's compound ValidSAonCard$ head matched: the matcher widened past the unmodelled shape")
	}
	// Control: strip ONLY the ValidSAonCard$ param from a copy of the same
	// trigger; the identical event must now match, so the false above is the
	// gate's doing.
	trimmed := *trg
	trimmed.Params = maps.Clone(trg.Params)
	delete(trimmed.Params, "ValidSAonCard")
	if !trigmatch.SpellCastEval(boardOf(e), trimmed, id, ev) {
		t.Fatal("control: Kolaghan's trigger without ValidSAonCard$ still does not match; the closed result is not attributable to the compound head")
	}
}
