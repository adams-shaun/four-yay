package rules

// A layer-4 effect that adds a type the object already has must not list it
// twice (CR 205.1b: an object either has a type or it does not; CR 613.1d's
// "in addition to its other types" adds only what is missing). The report
// that opened this: Puppet Crafting ("Enchanted permanent is a Construct
// creature with base power and toughness 5/5 in addition to its other
// types") on an Ornithopter derived "Artifact Construct Creature Creature
// Thopter". Every type-adding route funnels through typeCharacteristics'
// chars.appendLandTypes / chars.appendAllCreatureTypes, so a printed static (Puppet
// Crafting), an Animate-registered effect and an all-creature-types grant
// are all pinned here.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// typeDuplicates names every type word that appears more than once
// (case-insensitively) in types.
func typeDuplicates(types []string) []string {
	seen := map[string]int{}
	var dup []string
	for _, t := range types {
		k := strings.ToLower(t)
		seen[k]++
		if seen[k] == 2 {
			dup = append(dup, t)
		}
	}
	return dup
}

func TestPuppetCraftingOnArtifactCreatureDoesNotDuplicateCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	orn := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Ornithopter"))
	aura := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Puppet Crafting"))
	e.emit(events.Event{Kind: events.Attach, Obj: aura, IDs: []state.ObjID{orn}})
	if e.G.Obj(aura).AttachedTo != orn {
		t.Fatalf("precondition: Puppet Crafting is not attached to Ornithopter")
	}
	d := e.Derived(orn)
	for _, want := range []string{"Artifact", "Creature", "Construct", "Thopter"} {
		if !hasTypeWord(d.Types, want) {
			t.Fatalf("derived types %v lack %s", d.Types, want)
		}
	}
	if dup := typeDuplicates(d.Types); len(dup) > 0 {
		t.Fatalf("derived types %v list %v more than once (CR 205.1b)", d.Types, dup)
	}
	if p, to := e.Power(orn), e.Toughness(orn); p != 5 || to != 5 {
		t.Fatalf("Ornithopter under Puppet Crafting is %d/%d, want base 5/5", p, to)
	}
	// A non-creature artifact gains Creature exactly once as well.
	e2 := layerEngine(t)
	rock := onBoardCard(t, e2, 0, mustCorpusCard(t, reg, "Mind Stone"))
	aura2 := onBoardCard(t, e2, 0, mustCorpusCard(t, reg, "Puppet Crafting"))
	e2.emit(events.Event{Kind: events.Attach, Obj: aura2, IDs: []state.ObjID{rock}})
	d2 := e2.Derived(rock)
	if !hasTypeWord(d2.Types, "Creature") || len(typeDuplicates(d2.Types)) > 0 {
		t.Fatalf("Mind Stone under Puppet Crafting derived %v, want Creature once", d2.Types)
	}
}

// TestAnimateAddingAnExistingTypeDoesNotDuplicate: the effect-registered
// route (api:Animate's Types$) shares the same type append, so animating an
// artifact creature as an "Artifact Creature Golem" lists each word once.
func TestAnimateAddingAnExistingTypeDoesNotDuplicate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	orn := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Ornithopter"))
	effects.Resolve(e, &effects.Ctx{Source: orn, Controller: 0, Targets: []state.Target{{Obj: orn}}},
		&cards.SA{Kind: "DB", API: "Animate", Params: map[string]string{
			"Defined": "Targeted", "Types": "Artifact,Creature,Golem", "Power": "3", "Toughness": "3",
		}})
	d := e.Derived(orn)
	if !hasTypeWord(d.Types, "Golem") {
		t.Fatalf("precondition: the Animate effect did not apply: %v", d.Types)
	}
	if dup := typeDuplicates(d.Types); len(dup) > 0 {
		t.Fatalf("derived types %v list %v more than once (CR 205.1b)", d.Types, dup)
	}
}

// TestAllCreatureTypesGrantDoesNotDuplicatePrintedSubtype: Maskwood Nexus
// ("Creatures you control are every creature type") over a printed Thopter
// lists Thopter once.
func TestAllCreatureTypesGrantDoesNotDuplicatePrintedSubtype(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Maskwood Nexus"))
	orn := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Ornithopter"))
	d := e.Derived(orn)
	if !hasTypeWord(d.Types, "Elf") {
		t.Fatalf("precondition: Maskwood Nexus did not grant every creature type: %v", d.Types)
	}
	if dup := typeDuplicates(d.Types); len(dup) > 0 {
		t.Fatalf("derived types list %v more than once (CR 205.1b)", dup)
	}
}
