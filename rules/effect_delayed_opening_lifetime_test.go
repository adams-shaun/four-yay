package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file is the fix-round companion to effect_delayed_lifetime_test.go and
// effect_delayed_riders_test.go.
//
// The MAJOR it pins: a Duration$ Permanent api:Effect trigger's source-relative
// ending (CR 611.2, the source leaving the battlefield) applies ONLY when the
// source WAS a battlefield permanent at registration. An opening-hand Effect
// (Chancellor of the Annex -- its source stays in hand) or an emblem/
// command-zone source has no battlefield incarnation to lose, so its Permanent
// promise is unbounded and must survive. Before the fix the generic effEffect
// path admitted Permanent (so the pregame registerOpeningEffectTriggers pass
// stood down as a duplicate), then the battlefield liveness rule removed the
// registration on the next scan: e.G.Delayed fell to empty and the opening
// trigger never fired.
//
// The MINORs it answers: the ForgetOnCast and ImprintOnHost lifetimes are
// driven through their real call sites -- a completed cast (payCast ->
// fireDeferredCastTrigger -> sweepEffectDelayedCast) and the host's effect-token
// exile (effects/zone.go's `Origin$ Command | Destination$ Exile` body ->
// EndImprintedEffects) -- not by calling the sweeps directly.

// TestEffectDelayedOpeningHandPermanentSurvives is the MAJOR regression: the
// real Chancellor of the Annex's generic registration must be born with the
// Permanent lifetime and an explicit "source was not a battlefield permanent"
// bit, survive a TurnChange (the bug removed it immediately), and then fire
// its one-shot counter on the opponent's first cast.
func TestEffectDelayedOpeningHandPermanentSurvives(t *testing.T) {
	t.Parallel()
	annex := corpusAlternativeCard(t, "Chancellor of the Annex")
	e := handEngine(t, annex)
	id := e.G.Zone(state.ZHand, 0)[0]
	if e.G.Obj(id).Zone != state.ZHand {
		t.Fatal("precondition: Chancellor source must be in hand, not on the battlefield")
	}
	e.pending = nil
	e.applyOpeningEffect(openingEffect{player: 0, card: id, svar: "RevealCard"})
	if len(e.G.Delayed) != 1 {
		t.Fatalf("opening Effect registered %d delayed triggers, want 1: %+v", len(e.G.Delayed), e.G.Delayed)
	}
	dt := e.G.Delayed[0]
	if dt.EffectDuration != "permanent" {
		t.Fatalf("precondition: registration lifetime = %q, want permanent", dt.EffectDuration)
	}
	if dt.SourceBattlefield {
		t.Fatal("a source in hand must NOT be marked as a battlefield source")
	}
	if dt.EventMode != "SpellCast" || dt.Controller != 1 {
		t.Fatalf("registration = %+v, want the opponent-owned SpellCast promise", dt)
	}
	if !e.delayedRegistrationLive(&e.G.Delayed[0]) {
		t.Fatal("opening-hand Permanent promise is not live")
	}
	// A turn boundary must not retire an unbounded permanent promise.
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: e.G.Turn + 1})
	if len(e.G.Delayed) != 1 || !e.delayedRegistrationLive(&e.G.Delayed[0]) {
		t.Fatalf("opening-hand Permanent promise did not survive the turn boundary: %+v", e.G.Delayed)
	}
	// The non-vacuous contrast: the SAME Permanent lifetime on a battlefield
	// source dies when that source leaves, proving the SourceBattlefield bit
	// (not a no-op predicate) is what keeps the opening-hand promise alive.
	battle := onBoard(t, e, 0, "Name:BattleSource\nTypes:Creature\nPT:2/2\nA:AB$ Effect | Triggers$ Hook | Duration$ Permanent\nSVar:Hook:Mode$ DamageDone | ValidTarget$ Player | Execute$ Pain\nSVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	bf := e.G.Obj(battle).Face()
	effects.Resolve(e, &effects.Ctx{Source: battle, Controller: 0, SVars: bf.SVars}, bf.Abilities[0])
	if len(e.G.Delayed) != 2 || !e.G.Delayed[1].SourceBattlefield {
		t.Fatalf("battlefield Permanent registration = %+v, want SourceBattlefield true", e.G.Delayed)
	}
	battleID := e.G.Delayed[1].ID
	openingID := e.G.Delayed[0].ID
	e.emit(events.Event{Kind: events.MoveZone, Obj: battle, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(battle).Zone == state.ZBattlefield {
		t.Fatal("precondition: battlefield source did not leave")
	}
	// The MoveZone emit is what retires the battlefield registration (its
	// lifetime predicate now fails); the opening-hand one must be untouched.
	var haveOpening, haveBattle bool
	for i := range e.G.Delayed {
		switch e.G.Delayed[i].ID {
		case battleID:
			haveBattle = true
		case openingID:
			haveOpening = true
		}
	}
	if haveBattle {
		t.Fatal("battlefield source's Permanent promise survived its own departure")
	}
	if !haveOpening {
		t.Fatal("opening-hand Permanent promise was collaterally killed with the battlefield one")
	}
}

// TestEffectDelayedPermanentRiderSuffixRoundTrip guards the DelayedRegister
// suffix ordering: a Duration$ Permanent registration that ALSO carries a
// value-bearing rider (ForgetOnMoved$) must decode both fields cleanly, with
// neither suffix bleeding into the stored Execute/duration. |SB is a bare flag
// appended last and stripped first; a value-bearing suffix appended after it
// would otherwise end up inside EffectDuration.
func TestEffectDelayedPermanentRiderSuffixRoundTrip(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	moved := onBoard(t, e, 0, "Name:Subject\nTypes:Creature\nPT:1/1\nOracle:x\n")
	src := onBoard(t, e, 0, "Name:RiderSource\nTypes:Creature\nPT:2/2\nA:AB$ Effect | Triggers$ Hook | Duration$ Permanent | ForgetOnMoved$ Battlefield | RememberObjects$ Remembered\nSVar:Hook:Mode$ DamageDone | ValidTarget$ Player | Execute$ Pain\nSVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	face := e.G.Obj(src).Face()
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars,
		Remembered: []state.Target{{Obj: moved}}}, face.Abilities[0])
	if len(e.G.Delayed) != 1 {
		t.Fatalf("registration = %+v", e.G.Delayed)
	}
	dt := e.G.Delayed[0]
	if dt.EffectDuration != "permanent" || dt.ForgetOnMoved != "Battlefield" ||
		dt.Execute != "Pain" || dt.Trigger != "Hook" || !dt.SourceBattlefield {
		t.Fatalf("suffix round-trip corrupted the registration: %+v", dt)
	}
}

// TestEffectDelayedNonBattlefieldPermanentStaysLive is the same MAJOR class for
// an emblem/command-zone-shaped source: a Duration$ Permanent Effect whose
// source is NOT on the battlefield must never be killed by the battlefield
// liveness rule, while a true battlefield source still is.
func TestEffectDelayedNonBattlefieldPermanentStaysLive(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	// A source in hand (the emblem/opening-hand class: no battlefield
	// incarnation to lose).
	handCard := card(t, "Name:Emblemish\nTypes:Creature\nPT:2/2\nA:AB$ Effect | Triggers$ Hook | Duration$ Permanent\nSVar:Hook:Mode$ DamageDone | ValidTarget$ Player | Execute$ Pain\nSVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	o := e.G.AddObject(handCard, 0)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), o.ID))
	face := o.Face()
	effects.Resolve(e, &effects.Ctx{Source: o.ID, Controller: 0, SVars: face.SVars}, face.Abilities[0])
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EffectDuration != "permanent" || e.G.Delayed[0].SourceBattlefield {
		t.Fatalf("non-battlefield Permanent registration = %+v", e.G.Delayed)
	}
	if !e.delayedRegistrationLive(&e.G.Delayed[0]) {
		t.Fatal("non-battlefield Permanent promise is not live")
	}
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("non-battlefield Permanent promise did not fire: %+v", e.pendingTriggers)
	}
}

// TestEffectDelayedHostExileEndsRegistration drives the ImprintOnHost lifetime
// through its real call site: a `DB$ ChangeZone | Defined$ Imprinted | Origin$
// Command | Destination$ Exile` body resolved with the host as its source, the
// idiom effects/zone.go uses to end an imprinted effect. The host's
// EndImprintedEffects must retire the delayed registration through a logged
// DelayedRemove.
func TestEffectDelayedHostExileEndsRegistration(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	host := onBoard(t, e, 0, "Name:ImprintHost\nTypes:Creature\nPT:2/2\nA:AB$ Effect | Triggers$ Hook | ImprintOnHost$ True\nSVar:Hook:Mode$ DamageDone | ValidTarget$ Player | Execute$ Pain\nSVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	face := e.G.Obj(host).Face()
	effects.Resolve(e, &effects.Ctx{Source: host, Controller: 0, SVars: face.SVars}, face.Abilities[0])
	if len(e.G.Delayed) != 1 || !e.G.Delayed[0].ImprintOnHost {
		t.Fatalf("precondition: live imprint registration absent: %+v", e.G.Delayed)
	}
	// The real ender body: a separate card's ability, resolved with the HOST as
	// its effect source, exactly as the dig-and-play idiom resolves it.
	ender := onBoard(t, e, 0, "Name:Ender\nTypes:Sorcery\nA:AB$ ChangeZone | Defined$ Imprinted | Origin$ Command | Destination$ Exile\nOracle:x\n")
	ebody := e.G.Obj(ender).Face().Abilities[0]
	// The body's precondition in effects/zone.go keys on the ORIGIN token, not
	// the host's current zone, so resolve it with the host as the source.
	effects.Resolve(e, &effects.Ctx{Source: host, Controller: 0, SVars: e.G.Obj(host).Face().SVars}, ebody)
	if len(e.G.Delayed) != 0 {
		t.Fatalf("host imprint exile did not retire the registration: %+v", e.G.Delayed)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedRemove {
			found = true
		}
	}
	if !found {
		t.Fatal("no DelayedRemove logged for the retired imprint registration")
	}
}

// TestEffectDelayedForgetOnCastRealCast drives the ForgetOnCast lifetime
// through a completed cast: payCast -> fireDeferredCastTrigger ->
// sweepEffectDelayedCast. A proposal that reaches the stack and is paid for
// ends the grant.
func TestEffectDelayedForgetOnCastRealCast(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:OneCast\nTypes:Creature\nPT:2/2\nA:AB$ Effect | Triggers$ Hook | ForgetOnCast$ Card\nSVar:Hook:Mode$ DamageDone | ValidTarget$ Player | Execute$ Pain\nSVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	face := e.G.Obj(src).Face()
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars}, face.Abilities[0])
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].ForgetOnCast != "Card" {
		t.Fatalf("precondition: live ForgetOnCast registration absent: %+v", e.G.Delayed)
	}
	// Put a zero-cost spell in seat 0's hand and cast it for real.
	spell := slowSpellCard(t)
	o := e.G.AddObject(spell, 0)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), o.ID))
	e.G.Active, e.G.Priority = 0, 0
	e.G.Step = state.StepMain1
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, o.ID))
	passUntilStackEmpty(t, e, 40)
	if len(e.G.Delayed) != 0 {
		t.Fatalf("a completed cast did not end the ForgetOnCast grant: %+v", e.G.Delayed)
	}
}
