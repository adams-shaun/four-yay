package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestFaceAbilitiesNameExiledWithSource pins the A:-line scan: a source whose
// ability param names ExiledWithSource needs exile provenance; a source whose
// only mention is SpellDescription$ prose ("exiled with it") does not.
func TestFaceAbilitiesNameExiledWithSource(t *testing.T) {
	named := mkCard(t, "Name:Names It\nTypes:Artifact\nOracle:x\n"+
		"A:AB$ Clone | Cost$ 2 | ValidTgts$ Creature.ExiledWithSource | TgtPrompt$ Select | SpellDescription$ Becomes a copy.\n")
	prose := mkCard(t, "Name:Prose Only\nTypes:Artifact\nOracle:x\n"+
		"A:AB$ Clone | Cost$ 2 | ValidTgts$ Creature | TgtPrompt$ Select | SpellDescription$ Becomes a copy of target creature card exiled with it.\n")
	if len(named.Faces[0].Abilities) != 1 || len(prose.Faces[0].Abilities) != 1 {
		t.Fatalf("precondition: ability counts = %d, %d, want 1, 1", len(named.Faces[0].Abilities), len(prose.Faces[0].Abilities))
	}
	h := newHost(t, 2)
	namedObj := h.g.AddObject(named, 0)
	proseObj := h.g.AddObject(prose, 0)
	if !faceAbilitiesNameExiledWithSource(h, namedObj.ID) {
		t.Fatal("ability param naming ExiledWithSource not detected")
	}
	if faceAbilitiesNameExiledWithSource(h, proseObj.ID) {
		t.Fatal("prose-only SpellDescription$ mention was treated as provenance")
	}
	// The source ID the exile mover stamps: a Ctx bound to the host reads the
	// same answer through moveZoneEvent, an unbound one keeps the SVar read.
	bound := &Ctx{Source: namedObj.ID, Host: h}
	if ev := moveZoneEvent(bound, 99, state.ZGraveyard, state.ZExile); len(ev.IDs) != 1 || ev.IDs[0] != namedObj.ID || ev.Kind != events.MoveZone {
		t.Fatalf("bound exile event IDs = %v, want [%d]", ev.IDs, namedObj.ID)
	}
	if ev := moveZoneEvent(&Ctx{Source: proseObj.ID, Host: h}, 99, state.ZGraveyard, state.ZExile); len(ev.IDs) != 0 {
		t.Fatalf("prose-only exile event IDs = %v, want none", ev.IDs)
	}
	if ev := moveZoneEvent(bound, 99, state.ZGraveyard, state.ZHand); len(ev.IDs) != 0 {
		t.Fatalf("non-exile move IDs = %v, want none", ev.IDs)
	}
}
