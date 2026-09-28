package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Mistform Ultimus's characteristic-defining "is every creature type" static
// (task mistform-cda1). The ability is the card's OWN AddAllCreatureTypes$
// True on a CharacteristicDefining$ True, Affected$ Card.Self line, so — like
// Changeling's keyword — it must answer every creature-subtype filter in
// EVERY zone (CR 604.3/613.4a: characteristic-defining abilities function in
// all zones), through the plain filter read, not only through the battlefield
// layer walk. The answer lives in effects' hasType beside Changeling (the
// intrinsic-CDA precedent), and it materialises NOTHING into
// Derived().Types off the battlefield.

// mistformSubtypes are creature subtypes no card in these games prints, so
// only the CDA itself can supply them.
var mistformSubtypes = []string{"Goblin", "Surrakar", "Wizard", "Dinosaur"}

// mistformNonCreatureWords must never leak from the all-creature-types read:
// the vocabulary is the positive CreatureTypeWords set, not a complement.
var mistformNonCreatureWords = []string{"Arcane", "Alara", "Ajani", "Aura"}

// mistformMove moves an object already in play to a zone, wherever it
// currently sits (moveByName only scans hand/library).
func mistformMove(t *testing.T, e *Engine, id state.ObjID, to state.Zone) {
	t.Helper()
	for z := state.Zone(0); z < state.ZCeased; z++ {
		for _, p := range []state.PlayerID{0, 1} {
			for _, other := range e.G.Zone(z, p) {
				if other == id {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
					return
				}
			}
		}
	}
	t.Fatalf("object %d not found in any zone", id)
}

func TestMistformUltimusIsEveryCreatureTypeInEveryZone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Mistform Ultimus")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Mistform Ultimus", state.ZHand)

	assert := func(zone state.Zone) {
		t.Helper()
		mistformMove(t, e, id, zone)
		for _, sub := range mistformSubtypes {
			if !effects.MatchesSpec(e.G, sub, id, 0) {
				t.Errorf("Mistform Ultimus in %v must match %q through the plain filter read", zone, sub)
			}
			if !effects.MatchesSpec(e.G, "Creature."+sub, id, 0) {
				t.Errorf("Mistform Ultimus in %v must match Creature.%q", zone, sub)
			}
		}
		for _, bad := range mistformNonCreatureWords {
			if effects.MatchesSpec(e.G, "Creature."+bad, id, 0) {
				t.Errorf("Mistform Ultimus in %v must NOT match Creature.%q", zone, bad)
			}
		}
	}

	// Every zone the oracle names: on the stack too — a Mistform spell on
	// the stack is still every creature type (CR 613.4a).
	assert(state.ZLibrary)
	assert(state.ZHand)
	assert(state.ZBattlefield)
	assert(state.ZGraveyard)
	assert(state.ZExile)

	// The intrinsic read materialises NOTHING into Derived().Types off the
	// battlefield (the Changeling precedent); on the battlefield the layer
	// walk's LType emission is what feeds the derived list and is kept.
	mistformMove(t, e, id, state.ZHand)
	if d := e.Derived(id); slices.Contains(d.Types, "Goblin") {
		t.Fatalf("Mistform Ultimus in hand must not materialise Goblin into Derived().Types: %v", d.Types)
	}
	mistformMove(t, e, id, state.ZBattlefield)
	if !slices.Contains(e.Derived(id).Types, "Goblin") {
		t.Fatalf("Mistform Ultimus on the battlefield: the layer walk must still feed the derived list")
	}
}

func TestMistformAllCreatureTypesCDAIsNotAGrant(t *testing.T) {
	t.Parallel()
	// The Card.Self gate: Maskwood Nexus's AddAllCreatureTypes$ True is an
	// Affected$ Creature.YouCtrl GRANT handled by the layer walk, not an
	// intrinsic CDA. With the Nexus off the battlefield its grant must reach
	// nobody, and — the defect this gate prevents — the plain filter read
	// must not treat every card as carrying the ability.
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Maskwood Nexus"), lookup(t, reg, "Llanowar Elves")}, []*cards.Card{})
	elf := moveByName(t, e, 0, "Llanowar Elves", state.ZHand)
	if effects.MatchesSpec(e.G, "Goblin", elf, 0) {
		t.Fatalf("a hand Elf with the Nexus off the battlefield must not match Goblin")
	}
	for _, f := range lookup(t, reg, "Llanowar Elves").Faces {
		if f.AllCreatureTypesCDA() {
			t.Fatalf("Llanowar Elves prints no all-creature-types CDA")
		}
	}
	// Positive control: with the Nexus on the battlefield, its grant is
	// real through the layer walk (the pre-existing Maskwood behaviour,
	// unchanged). Note the grant reaches type predicates through the layer
	// walk's ExtraTypes/Derived, not through the plain hasType read — that
	// path is exactly what the intrinsic CDA above added, and the grant
	// must NOT ride it (the negative half asserted that off the
	// battlefield).
	moveByName(t, e, 0, "Maskwood Nexus", state.ZBattlefield)
	if d := e.Derived(elf); !slices.Contains(d.Types, "Goblin") {
		t.Fatalf("with the Nexus on the battlefield the hand Elf's derived types %v must carry Goblin", d.Types)
	}
}
