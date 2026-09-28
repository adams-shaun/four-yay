package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The generic effEffect Triggers$ path must own the registration by the
// Effect's EffectOwner$ seat, not by the creating card's controller. This is
// Valiant Batrider's shape verbatim: `DB$ Effect | Triggers$ Boon |
// EffectOwner$ TriggeredTarget` on a DamageDone trigger, where the damaged
// PLAYER gets a one-time boon (`ValidActivatingPlayer$ You` inside the boon
// resolves to that player). The synthetic card mirrors the corpus trigger
// line because all six real EffectOwner$ carriers ride a longer Effect
// lifetime the separate lifetime ticket (agent-20260923T034553Z-5e9f663a)
// still withholds.
func TestEffectTriggerOwnerTriggeredTarget(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:BatriderPromise\nManaCost:U\nTypes:Sorcery\n"+
		"SVar:Grant:DB$ Effect | Triggers$ Hook | EffectOwner$ TriggeredTarget\n"+
		"SVar:Hook:Mode$ DamageDone | ValidTarget$ You | TriggerZones$ Command | Execute$ Pain\n"+
		"SVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e := handEngine(t, promise)
	id := e.G.Zone(state.ZHand, 0)[0]
	src := e.G.Obj(id)
	if src == nil || src.Controller != 0 || src.Face() == nil {
		t.Fatalf("precondition: source must be seat 0's face-up card: %+v", src)
	}
	// The trigger's captured role: the damaged PLAYER, seat 1. The
	// precondition asserts the role actually resolves to a seat different
	// from the source controller, or the test would pass vacuously.
	ctx := &effects.Ctx{Source: id, Controller: 0,
		TriggerContext: effects.TriggerContext{TriggerTarget: state.Target{Player: 1, IsPlayer: true}}}
	effects.SetSVars(ctx, src.Face().SVars)
	owners, ok := effects.EffectOwnerPlayers(e, ctx, "TriggeredTarget")
	if !ok || len(owners) != 1 || owners[0] != 1 || owners[0] == src.Controller {
		t.Fatalf("precondition: EffectOwner$ TriggeredTarget must resolve to seat 1: %v (ok=%v)", owners, ok)
	}
	effects.Resolve(e, ctx, cards.ResolveSVar(src.Face().SVars, "Grant"))
	if len(e.G.Delayed) != 1 || !e.G.Delayed[0].EffectRepeat ||
		e.G.Delayed[0].Controller != 1 || e.G.Delayed[0].Source != id {
		t.Fatalf("precondition: expected a recurring Effect registration owned by seat 1: %+v", e.G.Delayed)
	}
	// Seat 0 is the creating card's controller; only seat 1's damage may fire.
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 1})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("creating controller's damage fired the effect owned by seat 1: %+v", e.pendingTriggers)
	}
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Controller != 1 {
		t.Fatalf("effect owner's damage must fire their trigger: %+v", e.pendingTriggers)
	}
}

// EffectOwner$ TargetedOwner names the OWNER (CR 108.3) of the resolving
// ability's target -- Palace Jailer's shape. The fixture gives a permanent an
// owner that differs from its controller, so a controller-derived read is
// distinguishable from an owner-derived one.
func TestEffectTriggerOwnerTargetedOwner(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:JailerPromise\nManaCost:U\nTypes:Sorcery\n"+
		"SVar:Grant:DB$ Effect | Triggers$ Hook | EffectOwner$ TargetedOwner\n"+
		"SVar:Hook:Mode$ DamageDone | ValidTarget$ You | TriggerZones$ Command | Execute$ Pain\n"+
		"SVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e := handEngine(t, promise)
	id := e.G.Zone(state.ZHand, 0)[0]
	creature := onBoard(t, e, 0, "Name:ExiledThing\nTypes:Creature\nPT:2/2\nOracle:x\n")
	o := e.G.Obj(creature)
	o.Owner = 1 // owned by seat 1, controlled by seat 0
	if o.Owner == o.Controller {
		t.Fatalf("precondition: owner %d must differ from controller %d", o.Owner, o.Controller)
	}
	ctx := &effects.Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: creature}}}
	effects.SetSVars(ctx, e.G.Obj(id).Face().SVars)
	owners, ok := effects.EffectOwnerPlayers(e, ctx, "TargetedOwner")
	if !ok || len(owners) != 1 || owners[0] != 1 {
		t.Fatalf("precondition: TargetedOwner must resolve to seat 1: %v (ok=%v)", owners, ok)
	}
	effects.Resolve(e, ctx, cards.ResolveSVar(e.G.Obj(id).Face().SVars, "Grant"))
	if len(e.G.Delayed) != 1 {
		t.Fatalf("expected one registration: %+v", e.G.Delayed)
	}
	if got := e.G.Delayed[0].Controller; got != 1 {
		t.Fatalf("registration owner = %d, want 1 (the target's OWNER, not controller 0)", got)
	}
}

// EffectOwner$ Targeted names the target itself when it is a player (Loch
// Larent's "target opponent gets a one-time boon").
func TestEffectTriggerOwnerTargetedPlayer(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:LarentPromise\nManaCost:U\nTypes:Sorcery\n"+
		"SVar:Grant:DB$ Effect | Triggers$ Hook | EffectOwner$ Targeted\n"+
		"SVar:Hook:Mode$ DamageDone | ValidTarget$ You | TriggerZones$ Command | Execute$ Pain\n"+
		"SVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e := handEngine(t, promise)
	id := e.G.Zone(state.ZHand, 0)[0]
	ctx := &effects.Ctx{Source: id, Controller: 0, Targets: []state.Target{{Player: 1, IsPlayer: true}}}
	effects.SetSVars(ctx, e.G.Obj(id).Face().SVars)
	effects.Resolve(e, ctx, cards.ResolveSVar(e.G.Obj(id).Face().SVars, "Grant"))
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].Controller != 1 {
		t.Fatalf("EffectOwner$ Targeted (player) registration = %+v, want controller 1", e.G.Delayed)
	}
}

// An EffectOwner$ this build cannot resolve registers NOTHING and says so
// loudly: it must never fall back to the source controller, which would hand
// the wrong seat the effect.
func TestEffectTriggerOwnerUnresolvableFailsClosed(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:BadOwnerPromise\nManaCost:U\nTypes:Sorcery\n"+
		"SVar:Grant:DB$ Effect | Triggers$ Hook | EffectOwner$ RememberedOwner.Opponent\n"+
		"SVar:Hook:Mode$ DamageDone | ValidTarget$ You | TriggerZones$ Command | Execute$ Pain\n"+
		"SVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e := handEngine(t, promise)
	id := e.G.Zone(state.ZHand, 0)[0]
	ctx := &effects.Ctx{Source: id, Controller: 0}
	effects.SetSVars(ctx, e.G.Obj(id).Face().SVars)
	effects.Resolve(e, ctx, cards.ResolveSVar(e.G.Obj(id).Face().SVars, "Grant"))
	if len(e.G.Delayed) != 0 {
		t.Fatalf("unresolvable EffectOwner$ must register nothing, got %+v", e.G.Delayed)
	}
	if len(effectNotesContaining(e, "unresolvable EffectOwner$ RememberedOwner.Opponent")) == 0 {
		t.Fatalf("unresolvable EffectOwner$ must be named loudly in a Note; notes: %v", effectNoteTexts(e))
	}
}

// An opening-hand OneOff$ True Effect body is registered exactly ONCE: the
// generic effEffect path registers the one-shot body (no |EF, so its first
// firing consumes it), and the pregame registerOpeningEffectTriggers pass
// recognises that live registration instead of minting a second one. A double
// registration fires the body twice.
func TestEffectOneOffDoesNotDoubleFireFromOpening(t *testing.T) {
	t.Parallel()
	opening := card(t, "Name:OpeningOneOff\nTypes:Sorcery\n"+
		"SVar:Reveal:DB$ Reveal | RevealDefined$ Self | SubAbility$ DBEffect\n"+
		"SVar:DBEffect:DB$ Effect | Triggers$ TrigCast | Duration$ UntilEndOfTurn\n"+
		"SVar:TrigCast:Mode$ SpellCast | ValidCard$ Card | ValidActivatingPlayer$ You | OneOff$ True | TriggerZones$ Command | Execute$ Pain\n"+
		"SVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 1\nOracle:x\n")
	filler := slowSpellCard(t)
	e := handEngine(t, opening, filler, filler)
	id := e.G.Zone(state.ZHand, 0)[0]
	e.pending = nil
	e.applyOpeningEffect(openingEffect{player: 0, card: id, svar: "Reveal"})
	if len(e.G.Delayed) != 1 {
		t.Fatalf("opening OneOff$ effect registered %d delayed triggers, want exactly 1: %+v", len(e.G.Delayed), e.G.Delayed)
	}
	dt := e.G.Delayed[0]
	if dt.EventMode != "SpellCast" || dt.Trigger != "TrigCast" || dt.EffectRepeat {
		t.Fatalf("OneOff$ registration must be a one-shot SpellCast (no |EF): %+v", dt)
	}

	delayedPushes := func() int {
		n := 0
		for _, ev := range e.L.Events {
			if ev.Kind == events.DelayedPush {
				n++
			}
		}
		return n
	}
	castFiller := func() {
		e.G.Active, e.G.Priority = 0, 0
		e.askPriority(0)
		submitChoices(t, e, passToCast(t, e, e.G.Zone(state.ZHand, 0)[0]))
		passUntilStackEmpty(t, e, 20)
	}
	castFiller()
	if n := delayedPushes(); n != 1 {
		t.Fatalf("first cast fired the one-shot body %d times, want 1", n)
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("one-shot registration survived its firing: %+v", e.G.Delayed)
	}
	castFiller()
	if n := delayedPushes(); n != 1 {
		t.Fatalf("second cast re-fired a consumed one-shot body (%d DelayedPush total)", n)
	}
}

// The landed generic effect-created registration, pinned on the REAL corpus:
// Bonus Round's `A:SP$ Effect | Triggers$ TrigSpellCast` (Mode$ SpellCast, no
// Duration) registers a recurring Effect trigger. The per-mode pins already
// live in rules/effect_event_modes_test.go (DamageDone/SpellCast/ChangesZone)
// and rules/effect_frame_trigger_test.go (the one-shot self-exile frame); this
// adds the corpus carrier the report named.
func TestEffectGenericRegistrationHoldsOnCorpus(t *testing.T) {
	t.Parallel()
	bonus := corpusAlternativeCard(t, "Bonus Round")
	e := handEngine(t, bonus)
	id := e.G.Zone(state.ZHand, 0)[0]
	src := e.G.Obj(id)
	if src == nil || src.Face() == nil {
		t.Fatalf("precondition: Bonus Round must be a face-up hand card")
	}
	var grant *cards.SA
	for _, a := range src.Face().Abilities {
		if a.API == "Effect" {
			grant = a
			break
		}
	}
	if grant == nil || grant.Params["Triggers"] != "TrigSpellCast" || grant.Params["Duration"] != "" {
		t.Fatalf("precondition: Bonus Round Effect ability = %+v", grant)
	}
	ctx := &effects.Ctx{Source: id, Controller: 0}
	effects.SetSVars(ctx, src.Face().SVars)
	effects.Resolve(e, ctx, grant)
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != "SpellCast" || !e.G.Delayed[0].EffectRepeat {
		t.Fatalf("Bonus Round did not arm a recurring SpellCast Effect registration: %+v", e.G.Delayed)
	}
}
