package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Round 2 of cli-20260923T060218Z: a Static$ True body cannot carry the
// OptionalDecider$ election. rules' checkEventDelayedTriggers resolves a
// static-marked delayed registration INLINE at fire time -- never minting a
// stack object -- so resolveTop's CR 603.5 optional gate cannot pose the
// yes/no there, and registering such a body would execute the "you may"
// mandatorily. effEffect withholds the shape with a loud Note, and the static
// firing arm carries a matching fail-closed guard for any future "|OD="
// minter.

// TestEffectOptionalStaticTriggerIsWithheld drives the registration side: an
// api:Effect whose Triggers$ body carries BOTH Static$ True and
// OptionalDecider$ must be withheld with the loud withhold Note and must not
// reach the delayed set (where the static arm would run it mandatorily).
func TestEffectOptionalStaticTriggerIsWithheld(t *testing.T) {
	t.Parallel()
	staticOD := card(t, "Name:Static OD Effect\nManaCost:0\nTypes:Enchantment\n"+
		"A:SP$ Effect | Triggers$ TrigStaticOD | SpellDescription$ x\n"+
		"SVar:TrigStaticOD:Mode$ ChangesZone | ValidCard$ Creature | Origin$ Any | "+
		"Destination$ Battlefield | OptionalDecider$ You | Static$ True | Execute$ TrigStaticODBody\n"+
		"SVar:TrigStaticODBody:DB$ Draw | Defined$ You | NumCards$ 1\n"+
		"Oracle:x\n")
	bear := card(t, "Name:Test FreeBear\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	e := handEngine(t, staticOD, bear, bear)
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)

	// The withhold Note must name the shape -- without it the registration
	// either happened silently or the body was dropped silently.
	notes := effectNotesContaining(e, "unmodelled Effect trigger Static$ with OptionalDecider$")
	if len(notes) != 1 {
		t.Fatalf("withhold Note = %v, want exactly one", notes)
	}
	// Precondition for the behavioural half: NO delayed registration exists,
	// so the static firing arm never sees this shape.
	if hasRegistration(e, "ChangesZone") {
		t.Fatalf("the Static$+OptionalDecider$ body was registered anyway: %+v", e.G.Delayed)
	}
	// Behavioural half: a creature entering must draw nothing -- if the body
	// HAD been registered, the static arm would have run it mandatorily.
	drawsBefore := countDraw(e)
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)
	if got := countDraw(e); got != drawsBefore {
		t.Fatalf("withheld Static$+OptionalDecider$ body still executed mandatorily: drew %d cards", got-drawsBefore)
	}
}

// TestOptionalStaticDelayedTriggerIsNotExecutedInline drives the firing arm
// itself: a static-marked delayed registration that somehow carries "|OD="
// (minted directly here, the shape a future registration site could produce)
// must not execute its body inline -- the guard Note fires, the one-shot is
// spent, and nothing is drawn.
func TestOptionalStaticDelayedTriggerIsNotExecutedInline(t *testing.T) {
	t.Parallel()
	src := card(t, "Name:Static OD Source\nManaCost:0\nTypes:Enchantment\n"+
		"SVar:TrigStaticOD:Mode$ SpellCast | OptionalDecider$ You | Static$ True | Execute$ TrigStaticODBody\n"+
		"SVar:TrigStaticODBody:DB$ Draw | Defined$ You | NumCards$ 1\n"+
		"Oracle:x\n")
	bear := card(t, "Name:Test FreeBear\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	e := handEngine(t, src, bear)
	id := onBoardCard(t, e, 0, src)

	// Mint the registration directly: an EffectRepeat SpellCast registration
	// whose body is Static$ True and whose Text carries the OptionalDecider$
	// spec the round-1 encoding rides. Precondition: it decoded into the
	// delayed set with the spec intact.
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: id, Player: 0,
		Step: e.G.Step, Counter: "TrigStaticODBody",
		Text: "SpellCast:TrigStaticOD|TT=9|OD=You|EF"})
	var reg *state.DelayedTrigger
	for i := range e.G.Delayed {
		if e.G.Delayed[i].EventMode == "SpellCast" {
			reg = &e.G.Delayed[i]
		}
	}
	if reg == nil {
		t.Fatalf("no SpellCast registration after DelayedRegister: %+v", e.G.Delayed)
	}
	if reg.OptionalSpec != "You" {
		t.Fatalf("registration OptionalSpec = %q, want \"You\"", reg.OptionalSpec)
	}

	// Fire it: casting a spell makes the SpellCast scan match, and the static
	// arm -- not the ordinary stack drain -- owns this registration's body.
	drawsBefore := countDraw(e)
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 12)
	notes := effectNotesContaining(e, "optional static delayed trigger has no election channel")
	if len(notes) != 1 {
		t.Fatalf("static-arm guard Note = %v, want exactly one (the guard did not run)", notes)
	}
	if got := countDraw(e); got != drawsBefore {
		t.Fatalf("the guarded static body still executed inline: drew %d cards", got-drawsBefore)
	}
	// The EffectRepeat registration survives (the ask the body never posed
	// cannot be re-armed either way); what matters is it was not EXECUTED.
	if !hasRegistration(e, "SpellCast") {
		t.Fatalf("the EffectRepeat registration was consumed by the guarded fire: %+v", e.G.Delayed)
	}
	// A spell cast on the optional ask channel the body can no longer pose:
	// no decision must be pending after the resolution drains.
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOptional {
		t.Fatalf("the static arm posed the election anyway: %+v", d)
	}
}
