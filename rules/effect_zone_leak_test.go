// effect_zone_leak_test.go pins the recurring-Effect matching scope's
// LIFETIME, review round 2 of the trigmatch-zone look-back ticket.
//
// checkEventDelayedTriggers armed the read-only matching overlay before each
// registration's own matching but, before the fix, cleared it only there --
// never after the registration loop. An emit whose delayed walk ended on a
// live EffectRepeat registration therefore returned with the overlay still
// armed, and the NEXT matching walk trusted it: controllerOf and the
// trigmatch_zone CR 603.10a look-back guard consult effectMatchControllerFor
// precisely because it is only true inside a registration's matching. The
// concrete casualty is a permanent that (a) holds a live self-Effect
// registration and (b) carries a PRINTED controller-relative dies trigger:
// stolen and destroyed, the printed trigger must read the departing
// permanent's last-known controller (the thief, CR 603.10a) -- under the
// leaked scope it read the registration's owner instead and silently did not
// fire.

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// leakProbeCard is one permanent carrying BOTH halves of the casualty shape:
// a printed "whenever a creature you control dies" trigger (the Warhost's
// Frenzy body, printed rather than Effect-delivered) and an AB$ Effect whose
// registration is a Mode$ SpellCast repeatable trigger (the Chancellor of the
// Annex shape -- a SpellCast registration cannot match a MoveZone, so the
// intervening event arms the scope for it and matches nothing).
const leakProbeCard = "Name:Leak Probe\nTypes:Creature Human Soldier\nPT:1/1\n" +
	"T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Creature.YouCtrl | Execute$ PrintDraw\n" +
	"A:AB$ Effect | Triggers$ TrigCast\n" +
	"SVar:TrigCast:Mode$ SpellCast | Execute$ EffectDraw\n" +
	"SVar:PrintDraw:DB$ Draw\n" +
	"SVar:EffectDraw:DB$ Draw\n" +
	"Oracle:x\n"

// TestEffectMatchScopeDoesNotLeakPastDelayedWalk: after an event whose
// delayed walk ends on a live EffectRepeat registration, the printed
// controller-relative dies trigger of the registration's own source must
// still fire with its ordinary LKI semantics when the permanent is stolen
// and destroyed.
func TestEffectMatchScopeDoesNotLeakPastDelayedWalk(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	e.pending = nil
	src := onBoard(t, e, 0, leakProbeCard)
	// Precondition: the probe has a printed ChangesZone trigger of its own
	// and its A: ability is the Effect registration (Abilities and Triggers
	// are separate face slices, so index 0 is the A: line).
	o := e.G.Obj(src)
	if o == nil || o.Face() == nil || len(o.Face().Triggers) != 1 || len(o.Face().Abilities) != 1 {
		t.Fatalf("precondition: probe must carry one printed trigger and one ability: %+v", o.Face())
	}
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: o.Face().SVars}, o.Face().Abilities[0])
	if len(e.G.Delayed) != 1 || !e.G.Delayed[0].EffectRepeat || e.G.Delayed[0].Source != src ||
		e.G.Delayed[0].EventMode != "SpellCast" {
		t.Fatalf("precondition: Effect must arm one repeatable SpellCast registration from %d: %+v", src, e.G.Delayed)
	}
	if !e.hasEffectRepeatDelayed() {
		t.Fatal("precondition: a live EffectRepeat registration must be outstanding")
	}

	// The intervening event: a DIFFERENT, seat 1's, creature dies. Its face
	// walk runs with no scope armed yet (the printed trigger's
	// Creature.YouCtrl does not match a seat 1 creature), then the delayed
	// walk arms the scope for the registration and -- before the fix --
	// returns with it still armed.
	by1 := onBoard(t, e, 1, "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: by1, From: state.ZBattlefield, To: state.ZGraveyard})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("precondition: the intervening death must fire nothing: %+v", e.pendingTriggers)
	}

	// The probe is stolen...
	e.emit(events.Event{Kind: events.ControlChange, Obj: src, Player: 1})
	if got := e.G.Obj(src).Controller; got != 1 {
		t.Fatalf("precondition: probe must now be seat 1's, got %d", got)
	}
	e.pendingTriggers = nil

	// ...and destroyed. The printed trigger must fire: with the scope
	// cleared, the look-back branch reads the LKI controller (seat 1, the
	// thief, CR 603.10a) and Creature.YouCtrl matches. Under the leaked
	// scope the guard trusted the registration's owner (seat 0) and the
	// trigger silently did not fire.
	e.emit(events.Event{Kind: events.MoveZone, Obj: src, From: state.ZBattlefield, To: state.ZGraveyard})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("printed dies trigger must fire on the stolen probe's death (leaked scope?): %+v", e.pendingTriggers)
	}
}
