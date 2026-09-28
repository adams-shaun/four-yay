package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Task tgtowner1: Chaos Warp's exact sub-ability shape, pinned end to end.
// The root poses the target ask (ValidTgts$ Permanent) and chains a DBDig
// sub carrying `Defined$ TargetedOwner` and NO ValidTgts of its own -- the
// shape where, before the grammar learned the spelling, the Dig's player
// list fell back to the resolving SOURCE (the caster) and the caster dug
// their own library instead of the targeted permanent's owner's. The root
// body here is an inert Tap because the shuffle half is not under test (and
// TapOrUntap would pose a mid-resolution tap/untap ask); the
// targeting-and-sub-Dig skeleton is the carrier's.
const chaosWarpOwnerDigSrc = "Name:Digw\nManaCost:R\nTypes:Instant\n" +
	"A:SP$ Tap | ValidTgts$ Permanent | SubAbility$ DBDig | SpellDescription$ The owner of target permanent digs.\n" +
	"SVar:DBDig:DB$ Dig | Defined$ TargetedOwner | DigNum$ 1 | Reveal$ True | DestinationZone$ Battlefield | DestinationZone2$ Library | LibraryPosition2$ 0 | ChangeNum$ All | ChangeValid$ Permanent\n" +
	"Oracle:x\n"

// TestDigTargetedOwnerDigsTheTargetOwnersLibrary: the targeted permanent is
// owned by seat 1 (the caster is seat 0), so the dig window is SEAT 1's
// library -- their top card is revealed publicly -- and seat 0's library is
// untouched. Seat 0's library top is pinned to a creature so a wrong-seat
// dig cannot hide behind an interchangeable mountain: without the fix it
// would dig the Digr Bear onto seat 0's battlefield.
func TestDigTargetedOwnerDigsTheTargetOwnersLibrary(t *testing.T) {
	t.Parallel()
	oppBear := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, id := newFixtureDeckWithOpponentCard(t, 4117, chaosWarpOwnerDigSrc, digBear, oppBear)
	bear := moveSeeded(t, e, 1, oppBear, state.ZBattlefield)

	// Precondition: the target really sits on seat 1's battlefield, owned
	// and controlled by seat 1 -- not the caster.
	if o := e.G.Obj(bear); o == nil || o.Owner != 1 || o.Controller != 1 || o.Zone != state.ZBattlefield {
		t.Fatalf("fixture precondition: bear = %+v, want on seat 1's battlefield, owner/controller 1", o)
	}
	// Put a known permanent on top of seat 1's library. The deck has
	// Mountain cards, so the assertion observes the owner's reveal and move
	// to the battlefield rather than depending on shuffle.
	seat1Lib := e.G.Zone(state.ZLibrary, 1)
	seat1Top := state.ObjID(0)
	order := make([]state.ObjID, 0, len(seat1Lib))
	for _, oid := range seat1Lib {
		o := e.G.Obj(oid)
		if seat1Top == 0 && o != nil && o.Face() != nil && o.Face().Name == "Mountain" {
			seat1Top = oid
			continue
		}
		order = append(order, oid)
	}
	if seat1Top == 0 {
		t.Fatal("fixture precondition: seat 1's library has no Mountain to reveal")
	}
	order = append([]state.ObjID{seat1Top}, order...)
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 1, IDs: order, Secret: true})
	e.pending = nil
	e.priorityRound()
	if o := e.G.Obj(seat1Top); o == nil || o.Zone != state.ZLibrary || o.Face().Name != "Mountain" {
		t.Fatalf("fixture precondition: seat 1's top = %+v, want a Mountain in Library", o)
	}

	// Pin seat 0's library top to the Digr Bear creature.
	lib0Before := digReorder(t, e, "Digr Bear", "Mountain")
	digrBear := lib0Before[0]
	if o := e.G.Obj(digrBear); o == nil || o.Face() == nil || o.Face().Name != "Digr Bear" {
		t.Fatalf("fixture precondition: seat 0's top = %v, want the Digr Bear creature", digrBear)
	}

	// Cast: fund, pick the cast option, answer the target ask with the Bear.
	addMana(t, e, 0, "R")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
	td := e.Pending()
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("target ask: %+v", td)
	}
	tidx := -1
	for _, o := range td.Options {
		if o.Obj == bear {
			tidx = o.Index
		}
	}
	if tidx < 0 {
		t.Fatalf("no option targeting the bear: %+v", td.Options)
	}
	submitChoices(t, e, tidx)
	passUntilStackEmpty(t, e, 20)

	// The dig window was seat 1's library: the mountain on top was revealed
	// publicly and moved to the battlefield as a permanent.
	if note := digPublicRevealNote(e, seat1Top); note == nil {
		t.Fatal("no public Note revealed seat 1's top card -- the dig did not read the target's owner's library")
	}
	if o := e.G.Obj(seat1Top); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("seat 1's dug mountain zone = %v, want Battlefield (permanent in the owner's dig window)", o)
	}
	if lib := e.G.Zone(state.ZLibrary, 1); len(lib) != len(seat1Lib)-1 {
		t.Fatalf("seat 1's library after dig has %d cards, want %d (one owner's card moved)", len(lib), len(seat1Lib)-1)
	}

	// Seat 0's library is untouched: the caster dug nothing of their own.
	if lib := e.G.Zone(state.ZLibrary, 0); len(lib) != len(lib0Before) || lib[0] != digrBear {
		t.Fatalf("seat 0's library after dig = %v, want %v untouched (caster must not dig)", lib, lib0Before)
	}
	if o := e.G.Obj(digrBear); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("seat 0's pinned Digr Bear zone = %v, want Library", o)
	}
	replayCheck(t, e, cfg)
}
