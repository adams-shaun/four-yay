package rules

// effect_zone_lookback_test.go pins the recurring-Effect matching overlay's
// reach into trigmatch.ZoneChangeMatches' CR 603.10a look-back branch. When the
// trigger's OWN source is the permanent that left the battlefield
// (source == ev.Obj), trigmatch.ZoneChangeMatches used to overwrite the overlay's
// virtual controller with lki.Controller, so a source-controller-relative
// predicate ("a creature YOU control dies") was matched against the creating
// card's last-known controller instead of the registration's owner.
//
// The corpus carriers are the "whenever a creature you control dies this
// turn" Effect promises -- Warhost's Frenzy and Waltz of Rage
// (`Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard |
// ValidCard$ Creature.YouCtrl`), whose Effect is created by a spell; the
// source-object case here puts the same body on an Effect created by a
// permanent (the Kjeldoran Guard shape: `DB$ Effect | Triggers$ ...`),
// because only a permanent's own departure reaches the LKI branch.

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// corpusZoneTrigger is the real Warhost's Frenzy / Waltz of Rage trigger and
// body text: "whenever a creature you control dies this turn, draw a card".
const corpusZoneTrigger = "Name:Kjeldoran Guard\nTypes:Creature Human Soldier\nPT:1/1\n" +
	"A:AB$ Effect | Triggers$ TrigDies\n" +
	"SVar:TrigDies:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Creature.YouCtrl | TriggerZones$ Command | Execute$ TrigDraw\n" +
	"SVar:TrigDraw:DB$ Draw\nOracle:x\n"

// armZoneEffect resolves src's Effect ability with the given owner and
// asserts the repeatable ChangesZone registration armed. It returns the
// source id.
func armZoneEffect(t *testing.T, e *Engine, src state.ObjID, owner state.PlayerID) {
	t.Helper()
	o := e.G.Obj(src)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
		t.Fatalf("precondition: source must be a battlefield permanent: %+v", o)
	}
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: owner, SVars: o.Face().SVars},
		o.Face().Abilities[0])
	if len(e.G.Delayed) != 1 || !e.G.Delayed[0].EffectRepeat || e.G.Delayed[0].EventMode != "ChangesZone" ||
		e.G.Delayed[0].Source != src {
		t.Fatalf("precondition: Effect must arm one repeatable ChangesZone trigger from %d: %+v", src, e.G.Delayed)
	}
}

// TestEffectZoneLookbackSeesOverlayController is the regression pin: an
// Effect whose source IS the leaving permanent must match a controller-
// relative ValidCard$ against the registration owner, not the creating
// permanent's last-known controller.
func TestEffectZoneLookbackSeesOverlayController(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	e.pending = nil
	src := onBoard(t, e, 0, corpusZoneTrigger)
	armZoneEffect(t, e, src, 0)
	if e.G.Delayed[0].Controller != 0 {
		t.Fatalf("precondition: registration owner = %d, want 0", e.G.Delayed[0].Controller)
	}
	// The permanent is stolen before it leaves: its LKI controller (1) now
	// differs from the registration owner (0), which is the whole point.
	e.emit(events.Event{Kind: events.ControlChange, Obj: src, Player: 1})
	if got := e.G.Obj(src).Controller; got != 1 {
		t.Fatalf("precondition: source must now be seat 1's, got %d", got)
	}
	e.pendingTriggers = nil

	// The (now seat 1's) source leaves. "creature YOU control" means the
	// Effect owner, seat 0; it must NOT fire.
	e.emit(events.Event{Kind: events.MoveZone, Obj: src, From: state.ZBattlefield, To: state.ZGraveyard})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("source's LKI controller fired an Effect owned by seat 0 (overlay bypassed): %+v", e.pendingTriggers)
	}
}

// TestEffectZoneLookbackPositiveControl is the counterweight: with no
// ownership mismatch the same body fires on the source's own departure, so
// the regression pin above is not passing because the branch is dead.
func TestEffectZoneLookbackPositiveControl(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	e.pending = nil
	src := onBoard(t, e, 0, corpusZoneTrigger)
	armZoneEffect(t, e, src, 0)
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: src, From: state.ZBattlefield, To: state.ZGraveyard})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Controller != 0 {
		t.Fatalf("source's own departure must fire the seat 0 Effect: %+v", e.pendingTriggers)
	}
}

// TestEffectZoneLookbackSeesCapturedMemory covers the predicate half of the
// same branch: ValidCard$ Card.IsRemembered reads the registration's captured
// objects (the Kjeldoran Guard `RememberObjects$ Targeted` shape), not the
// creating card's event-backed memory.
func TestEffectZoneLookbackSeesCapturedMemory(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	e.pending = nil
	// Kjeldoran Guard registers its promise from the Guard permanent.
	src := onBoard(t, e, 0, "Name:Kjeldoran Guard\nTypes:Creature Human Soldier\nPT:1/1\n"+
		"A:AB$ Effect | Triggers$ TrigSacGuard | RememberObjects$ Targeted\n"+
		"SVar:TrigSacGuard:Mode$ ChangesZone | ValidCard$ Card.IsRemembered | Origin$ Battlefield | Destination$ Any | TriggerZones$ Command | Execute$ EliteDefence\n"+
		"SVar:EliteDefence:DB$ Draw\nOracle:x\n")
	o := e.G.Obj(src)
	face := o.Face()
	// The ability's ValidTgts$ names a creature; capture a DIFFERENT creature
	// as the remembered one, so a match against the source itself would be a
	// false positive.
	other := onBoard(t, e, 1, "Name:Other\nTypes:Creature\nPT:1/1\nOracle:x\n")
	if other == src {
		t.Fatal("precondition: remembered subject and source must differ")
	}
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars,
		Remembered: []state.Target{{Obj: other}}}, face.Abilities[0])
	if len(e.G.Delayed) != 1 || !e.G.Delayed[0].EffectRepeat ||
		len(e.G.Delayed[0].Remembered) != 1 || e.G.Delayed[0].Remembered[0].Obj != other {
		t.Fatalf("precondition: Effect must capture the other creature: %+v", e.G.Delayed)
	}
	// The source itself leaves: it is NOT remembered, so it must not fire.
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: src, From: state.ZBattlefield, To: state.ZGraveyard})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("unremembered source's move fired the Effect: %+v", e.pendingTriggers)
	}

	// Positive arm (review round 2): the REMEMBERED object's own departure
	// must fire. Without this the test above is a negative-only pin -- an
	// empty SpecContext.Remembered never matches Card.IsRemembered either,
	// so it would pass with the memory overlay removed entirely; this arm
	// proves the registration's capture is SEEN.
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: other, From: state.ZBattlefield, To: state.ZGraveyard})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Controller != 0 {
		t.Fatalf("remembered object's own departure must fire the seat 0 Effect: %+v", e.pendingTriggers)
	}
}
