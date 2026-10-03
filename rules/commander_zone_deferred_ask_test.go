package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// exileThenSearchFixtureSrc is Path to Exile's shape, authored freely: exile
// a target creature, then a SECOND step of the same resolution poses a
// decision -- the exiled creature's controller may search for a basic land.
const exileThenSearchFixtureSrc = `Name:Fixture Banish Then Search
ManaCost:0
Types:Instant
A:SP$ ChangeZone | Origin$ Battlefield | Destination$ Exile | ValidTgts$ Creature | SubAbility$ DBFetch | SpellDescription$ Banish a creature; its controller may fetch a basic land.
SVar:DBFetch:DB$ ChangeZone | Optional$ True | Origin$ Library | Destination$ Battlefield | Tapped$ True | ChangeType$ Land.Basic | DefinedPlayer$ TargetedController | ShuffleNonMandatory$ True
Oracle:Banish a creature; its controller may fetch a basic land.
`

// moveToHandByName moves the named seeded deck card from the library to
// seat p's hand through a LOGGED MoveZone, so log-only replay reconstructs it.
func moveToHandByName(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZLibrary, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
			return id
		}
	}
	for _, id := range e.G.Zone(state.ZHand, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("card %q not found in seat %d's library or hand", name, p)
	return 0
}
