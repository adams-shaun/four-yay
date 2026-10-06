package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// destinationListScribe is the Three Tree Scribe shape: a leaves-the-battlefield
// trigger whose Destination$ is a comma list that omits the graveyard.
const destinationListScribe = `Name:Scribe
ManaCost:1 G
Types:Creature Treefolk
PT:2/2
T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Ante,Command,Exile,Hand,Library | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ x
SVar:TrigDraw:DB$ GainLife | Defined$ You | LifeAmount$ 1
Oracle:x
`

// destinationListSyr is the Syr Vondam shape: a list that names the graveyard
// first and exile second.
const destinationListSyr = `Name:Syr
ManaCost:1 W
Types:Creature Human
PT:2/2
T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard,Exile | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ x
SVar:TrigDraw:DB$ GainLife | Defined$ You | LifeAmount$ 1
Oracle:x
`

// TestChangesZoneDestinationListMatchesEachNamedZone moves the source out of
// the battlefield into every zone and holds the trigger to fire exactly for
// the zones the Destination$ list names (Forge's "Ante" names no zone this
// engine models, so it adds nothing).
func TestChangesZoneDestinationListMatchesEachNamedZone(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		to   state.Zone
		fire bool
	}{
		{"scribe/hand", destinationListScribe, state.ZHand, true},
		{"scribe/library", destinationListScribe, state.ZLibrary, true},
		{"scribe/exile", destinationListScribe, state.ZExile, true},
		{"scribe/command", destinationListScribe, state.ZCommand, true},
		{"scribe/graveyard", destinationListScribe, state.ZGraveyard, false},
		{"scribe/sideboard", destinationListScribe, state.ZSideboard, false},
		{"syr/graveyard", destinationListSyr, state.ZGraveyard, true},
		{"syr/exile", destinationListSyr, state.ZExile, true},
		{"syr/hand", destinationListSyr, state.ZHand, false},
		{"syr/library", destinationListSyr, state.ZLibrary, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := layerEngine(t)
			id := onBoard(t, e, 0, c.src)
			if got := e.G.Obj(id).Zone; got != state.ZBattlefield {
				t.Fatalf("precondition: source in %s, want battlefield", got)
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: c.to})
			if got := e.G.Obj(id).Zone; got != c.to {
				t.Fatalf("precondition: source moved to %s, want %s", got, c.to)
			}
			e.putTriggersOnStack()
			if fired := len(e.G.Stack) == 1; fired != c.fire {
				t.Fatalf("move to %s: stack = %v, trigger fired = %v, want %v", c.to, e.G.Stack, fired, c.fire)
			}
		})
	}
}

// TestMovedReplacementDestinationListMatchesEachNamedZone is the replacement
// reader's side of the same list: a Moved replacement whose Destination$ is
// "Graveyard,Exile" applies to a move into either zone and to no other.
func TestMovedReplacementDestinationListMatchesEachNamedZone(t *testing.T) {
	t.Parallel()
	const src = `Name:Phoenix
ManaCost:2 R
Types:Creature Phoenix
PT:2/2
R:Event$ Moved | Destination$ Graveyard,Exile | ValidCard$ Card.Self | ReplaceWith$ RepHand | Description$ x
SVar:RepHand:DB$ ChangeZone | Origin$ All | Destination$ Hand | Defined$ ReplacedCard
Oracle:x
`
	for _, c := range []struct {
		to       state.Zone
		replaced bool
	}{
		{state.ZGraveyard, true},
		{state.ZExile, true},
		{state.ZLibrary, false},
	} {
		e := layerEngine(t)
		id := onBoard(t, e, 0, src)
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: c.to})
		got := e.G.Obj(id).Zone
		if c.replaced && got != state.ZHand {
			t.Errorf("move to %s: zone = %s, want the replacement to send it to hand", c.to, got)
		}
		if !c.replaced && got != c.to {
			t.Errorf("move to %s: zone = %s, want the move untouched", c.to, got)
		}
	}
}
