package chars

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The no-layer-4 fast path must apply the impending switch to BOTH the type
// words and the semantic CDA bit. A printed CDA alone cannot override the
// dormant permanent's loss of Creature and its creature subtypes.
func TestTypesAllCreatureCDAImpendingDormantFastPath(t *testing.T) {
	card, diags := cards.ParseBytes("impending-all-types.txt", []byte("Name:Impending All Types\nManaCost:2 U\nTypes:Legendary Artifact Creature Golem\nPT:3/3\nK:Impending:2:1 U\nS:Mode$ Continuous | Affected$ Card.Self | CharacteristicDefining$ True | AddAllCreatureTypes$ True\nOracle:x\n"))
	if card == nil || len(card.Faces) != 1 || !card.Faces[0].AllCreatureTypesCDA() {
		t.Fatalf("precondition: fixture did not parse an all-creature-types CDA: card=%+v diags=%v", card, diags)
	}
	game := state.NewGame([]string{"A"})
	id := game.AddObject(card, 0).ID
	events.Apply(game, events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	obj := game.Obj(id)
	if obj == nil || obj.Zone != state.ZBattlefield {
		t.Fatalf("precondition: card not on battlefield: %+v", obj)
	}
	board := allTypesTestBoard{game: game} // deliberately no active layer-4 effects
	before, beforeAll := TypesAndAllCreatureTypes(board, nil, id, 0)
	if !beforeAll || !slices.Contains(before, "Creature") || !slices.Contains(before, "Golem") {
		t.Fatalf("precondition: live CDA not observed by fast path: types=%v all=%v", before, beforeAll)
	}
	events.Apply(game, events.Event{Kind: events.CastInfo, Obj: id, Counter: events.FlagsString(state.FlagImpending)})
	events.Apply(game, events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: 2})
	if !obj.ImpendingDormant() {
		t.Fatal("precondition: impending cast flag and time counters did not establish dormancy")
	}
	got, all := TypesAndAllCreatureTypes(board, nil, id, 0)
	if slices.Contains(got, "Creature") || slices.Contains(got, "Golem") || !slices.Contains(got, "Artifact") || !slices.Contains(got, "Legendary") {
		t.Fatalf("precondition: impending switch failed to remove creature words while keeping other types: %v", got)
	}
	if all {
		t.Fatalf("dormant permanent retained all-creature-types marker despite removed creature types: %v", got)
	}
}
