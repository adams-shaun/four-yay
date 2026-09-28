package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// delayedRememberedSrc mirrors the Flickerwisp delayed-trigger shape but reads
// the bare `Defined$ Remembered` spelling instead of the
// DelayTriggerRememberedLKI one. The ETB exiles a target with RememberChanged$
// True (writing both the resolution ctx and the source's persistent list), the
// DelayedTrigger registers a Mode$ Phase | Phase$ End of Turn firing, and
// TrigReturn returns the remembered card from exile with `Defined$ Remembered`.
// Written inline, never a .txt from .cards/ (GPL).
const delayedRememberedSrc = "Name:DelayedReturner\nManaCost:1 W\nTypes:Creature\nPT:2/2\n" +
	"T:Mode$ ChangesZone | ValidCard$ Card.Self | Origin$ Any | Destination$ Battlefield | Execute$ TrigExile\n" +
	"SVar:TrigExile:DB$ ChangeZone | ValidTgts$ Creature.Other | Mandatory$ True | Origin$ Battlefield | Destination$ Exile | RememberChanged$ True | SubAbility$ DelTrig\n" +
	"SVar:DelTrig:DB$ DelayedTrigger | Mode$ Phase | Phase$ End of Turn | Execute$ TrigReturn\n" +
	"SVar:TrigReturn:DB$ ChangeZone | Defined$ Remembered | Origin$ Exile | Destination$ Battlefield\n" +
	"Oracle:x\n"

// delayedRememberedFixture builds a 2-seat game with the inline delayed-return
// creature, casts it, resolves its ETB exiling a target, and returns the
// engine plus the exiled target and the source creature.
func delayedRememberedFixture(t *testing.T) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	e, _, find := etbConfig(t, 91,
		[]string{delayedRememberedSrc, "Name:TargetGuy\nTypes:Creature\nPT:2/2\nOracle:x\n",
			"Name:OtherGuy\nTypes:Creature\nPT:1/1\nOracle:x\n"}, nil)
	tg := find("TargetGuy", 0)
	other := find("OtherGuy", 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: tg, From: e.G.Obj(tg).Zone, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: other, From: e.G.Obj(other).Zone, To: state.ZBattlefield})
	e.pending = nil
	e.Advance()
	addMana(t, e, 0, "WW")
	castFirst(t, e, "cast")
	passUntilKind(t, e, decision.KTarget, 40)
	submitTarget(t, e, tg)
	passUntilStackEmpty(t, e, 40)
	if len(e.G.Delayed) != 1 {
		t.Fatalf("expected one delayed-trigger registration, got %d", len(e.G.Delayed))
	}
	if o := e.G.Obj(tg); o == nil || o.Zone != state.ZExile {
		t.Fatalf("target %d zone = %v, want exile after the ETB resolved", tg, zoneName(o))
	}
	src := findByName(e, "DelayedReturner", 0)
	if src == 0 {
		t.Fatal("source creature not found on the battlefield")
	}
	return e, tg, src
}

// TestDelayedDefinedRememberedIgnoresReplacedSourceMemory pins the delayed
// registration's saved capture against the source card's mutable persistent
// remembered list.
//
// The registration captures the exiled creature and carries it as
// TriggerContext.DelayedRemembered, with Ctx.Remembered/Captured seeded from
// that same set. Forge reads the delayed body's `Defined$ Remembered`
// independently of the source's later memory: a source whose remembered list is
// cleared or replaced before the delayed trigger fires (Turn to Mist's
// ForgetOtherTargets$ True after a recast) must still return the creature its
// own registration captured.
//
// Before the resolver fix the delayed body resolved `Defined$ Remembered`
// against the source's persistent list instead, so the exiled creature stayed
// exiled forever.
func TestDelayedDefinedRememberedIgnoresReplacedSourceMemory(t *testing.T) {
	t.Parallel()
	e, tg, src := delayedRememberedFixture(t)

	// Precondition: the source genuinely remembers the exiled creature, and the
	// delayed registration captured it -- so a vacuous setup fails loudly.
	if got := e.G.Obj(src).Remembered; len(got) != 1 || got[0].Obj != tg {
		t.Fatalf("precondition: source persistent memory = %+v, want the exiled target %d", got, tg)
	}
	if got := e.G.Obj(tg).Zone; got != state.ZExile {
		t.Fatalf("precondition: target zone = %v, want exile", got)
	}

	// Replace the source's persistent memory with a DIFFERENT battlefield
	// object, exactly the state a recast with ForgetOtherTargets$ True leaves.
	other := findByName(e, "OtherGuy", 0)
	if other == 0 {
		t.Fatal("precondition: other creature not on the battlefield")
	}
	e.G.Obj(src).Remembered = []state.Target{{Obj: other}}
	if got := e.G.Obj(src).Remembered; len(got) != 1 || got[0].Obj != other {
		t.Fatalf("precondition: replacement did not take: %+v", got)
	}

	driveToEndStep(t, e)
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(tg); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("exiled creature returned to %v, want the battlefield (delayed return lost its registration capture)", zoneName(o))
	}
	if o := e.G.Obj(src).Zone; o != state.ZBattlefield {
		t.Fatalf("precondition: source left the battlefield (%v); the delayed registration is source-independent but the test board changed", o)
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("delayed registration should be consumed by firing, got %d", len(e.G.Delayed))
	}
}
