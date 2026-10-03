package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestOblivionRingExiledWithResolvesOpponentOwnedTarget is the brief's leaf for
// the `Defined$ ExiledWith` reader half. It runs Oblivion Ring's real
// (owner seat 1, ring controlled by seat 0), which is exactly the case that
// proves the association must be read across EVERY player's exile zone: the
// exiled card sits in its owner's exile zone, not the controller's.
func TestOblivionRingExiledWithResolvesOpponentOwnedTarget(t *testing.T) {
	ring, exile := corpusSA(t, "Oblivion Ring", "TrigExile")
	if exile.API != "ChangeZone" || exile.Params["Destination"] != "Exile" {
		t.Fatalf("Oblivion Ring exile fixture changed: %+v", exile)
	}
	_, returnSA := corpusSA(t, "Oblivion Ring", "TrigReturn")
	if got := returnSA.Params["Defined"]; got != "ExiledWith" {
		t.Fatalf("Oblivion Ring return reads Defined$ %q, want ExiledWith", got)
	}

	h := newHost(t, 2)
	src := h.g.AddObject(ring, 0)
	// The target is OWNED by seat 1 and is a permanent on seat 1's
	// battlefield; the ring is controlled by seat 0.
	target := h.g.AddObject(mkCard(t, "Name:Target\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	for _, o := range []*state.Object{src, target} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	// Precondition: the association readers look at the zone the card lands
	// in is seat 1's exile zone, not the ring controller's.
	effChangeZone(h, &Ctx{Source: src.ID, Controller: 0, Targets: []state.Target{{Obj: target.ID}}}, exile)
	if o := h.g.Obj(target.ID); o == nil || o.Zone != state.ZExile || o.Owner != 1 {
		t.Fatalf("exiled target = %+v, want seat-1-owned card in exile", o)
	}
	if own := h.g.Zone(state.ZExile, 0); len(own) != 0 {
		t.Fatalf("seat 0 exile zone = %v, want empty (target owned by seat 1)", own)
	}

	got := Defined(h, &Ctx{Source: src.ID, Controller: 0}, returnSA)
	if len(got) != 1 || got[0].Obj != target.ID {
		t.Fatalf("Defined$ ExiledWith = %v, want [%d]", got, target.ID)
	}
}

// TestKohExiledWithSourceChoicePoolContainsExiledCreature is the brief's leaf
// for the `filter.go` half: the `Card.ExiledWithSource` predicate family that
// Koh, the Face Stealer's ChooseCard reads (`Choices$
// Creature.ExiledWithSource`).
func TestKohExiledWithSourceChoicePoolContainsExiledCreature(t *testing.T) {
	koh, exile := corpusSA(t, "Koh, the Face Stealer", "TrigExile1")
	if exile.API != "ChangeZone" || exile.Params["Destination"] != "Exile" {
		t.Fatalf("Koh exile fixture changed: %+v", exile)
	}
	choose := corpusSAByAPI(t, "Koh, the Face Stealer", "AB", "ChooseCard")
	if got := choose.Params["Choices"]; got != "Creature.ExiledWithSource" {
		t.Fatalf("Koh ChooseCard reads Choices$ %q, want Creature.ExiledWithSource", got)
	}

	h := newHost(t, 2)
	src := h.g.AddObject(koh, 0)
	target := h.g.AddObject(mkCard(t, "Name:Target\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	for _, o := range []*state.Object{src, target} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	effChangeZone(h, &Ctx{Source: src.ID, Controller: 0, Targets: []state.Target{{Obj: target.ID}}}, exile)
	if o := h.g.Obj(target.ID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: exiled target = %+v, want it in exile", o)
	}

	// The choice filter is evaluated from the source's perspective; the exiled
	// card must not be the source itself (a self-match would make the test
	// vacuous).
	if target.ID == src.ID {
		t.Fatal("precondition: target and source are the same object")
	}
	pool := cardChoices(h, &Ctx{Source: src.ID, Controller: 0, SVars: src.Face().SVars}, choose, 0)
	found := false
	for _, tt := range pool {
		found = found || tt.Obj == target.ID
	}
	if !found {
		t.Fatalf("Koh ChooseCard ExiledWithSource pool = %v, want it to contain %d", pool, target.ID)
	}
}

// TestDetentionSphereChangeZoneAllRecordsExiledWith is the brief's leaf for
// the writer half: `effChangeZoneAll` must record the same
// exiled-with association `effChangeZone` does, so Detention Sphere's return
// (`ChangeZoneAll | ChangeType$ Card.ExiledWithSource`) can find the swept
// permanents. With only the reader changes applied this test FAILS, because
// the ChangeZoneAll sweep writes no association at all.
func TestDetentionSphereChangeZoneAllRecordsExiledWith(t *testing.T) {
	sphere, _ := corpusSA(t, "Detention Sphere", "TrigExile")
	_, sweep := corpusSA(t, "Detention Sphere", "DBChangeZoneAll")
	if sweep.API != "ChangeZoneAll" || sweep.Params["Destination"] != "Exile" {
		t.Fatalf("Detention Sphere sweep fixture changed: %+v", sweep)
	}
	if got := sweep.Params["ChangeType"]; got != "Targeted.Self,Targeted.sameName" {
		t.Fatalf("Detention Sphere sweep ChangeType$ = %q", got)
	}

	h := newHost(t, 2)
	src := h.g.AddObject(sphere, 0)
	// The swept target is owned and controlled by seat 1; the sphere is
	// controlled by seat 0, so the association is only visible across seats.
	target := h.g.AddObject(mkCard(t, "Name:Target\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	for _, o := range []*state.Object{src, target} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	effChangeZoneAll(h, &Ctx{Source: src.ID, Controller: 0, Targets: []state.Target{{Obj: target.ID}}}, sweep)
	if o := h.g.Obj(target.ID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: swept target = %+v, want it in exile", o)
	}

	// Forward list: the source's ExiledCards must name the swept card.
	if got := h.g.Obj(src.ID).ExiledCards; len(got) != 1 || got[0] != target.ID {
		t.Fatalf("ChangeZoneAll sweep ExiledCards = %v, want [%d]", got, target.ID)
	}
	// And the shared predicate the return reads must match it.
	ctx := &Ctx{Source: src.ID, Controller: 0}
	if !MatchesSpecCtx(h.g, "Card.ExiledWithSource", target.ID, ctx.SpecContext(0)) {
		t.Fatalf("Card.ExiledWithSource did not match the swept target %d", target.ID)
	}
}
