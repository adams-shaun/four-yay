package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The layer-4 "all creature types" grant (task
// param-stat-continuous-addallcreaturetypes): Forge's AddAllCreatureTypes$
// True -- Maskwood Nexus's S:Mode$ Continuous static and the manland
// family's AB$ Animate line. The grant rides state.ContinuousEffect as a
// FLAG, never a materialised registration-time type list: rules'
// typeCharacteristics appends the shared CreatureTypeWords vocabulary into
// the type walk's list for affected objects, so every type predicate the
// effects filter evaluates through ExtraTypes -- bare bases and dotted
// predicates alike -- answers an arbitrary creature subtype, and a later
// static's Affected$ (the layer walk's matchesWithTypes) matches too. The
// same CreatureTypeWords vocabulary changelingType uses, so a non-creature
// word (Arcane/Alara/Ajani) can never leak onto an affected object. Corpus
// cards here are REAL compiled corpus cards; the Goblin-lord fixtures are
// inline, per the no-Forge-script-text rule.

// goblinLordFixture is a minimal layer-7 lord whose Affected$ names an
// arbitrary creature subtype: its pump landing on an object proves that
// object's derived type list carries that subtype (the filter grammar
// reaches it through ExtraTypes).
const goblinLordFixture = "Name:Goblin Lord\nTypes:Creature Goblin\nPT:1/1\nS:Mode$ Continuous | Affected$ Goblin | AddPower$ 1 | AddToughness$ 1\nOracle:x\n"

// arbitrarySubtypes are creature subtypes NO fixture or corpus card in
// these games prints -- only the all-creature-types grant can supply them.
var arbitrarySubtypes = []string{"Goblin", "Wizard", "Surrakar", "Dinosaur", "Shapeshifter"}

// nonCreatureTypeWords must never appear in a granted type list: the
// vocabulary is the positive CreatureTypeWords set, not a complement.
var nonCreatureTypeWords = []string{"Arcane", "Alara", "Ajani", "Aura"}

func TestMaskwoodNexusGrantsEveryCreatureType(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Maskwood Nexus")}, []*cards.Card{})
	moveByName(t, e, 0, "Maskwood Nexus", state.ZBattlefield)
	human := onBoard(t, e, 0, "Name:Vanilla Human\nTypes:Creature Human\nPT:1/1\nOracle:x\n")
	onBoard(t, e, 0, goblinLordFixture)

	// The Goblin lord's static must reach the Maskwood-affected Human: the
	// layer walk's Affected$ match sees the granted subtype through
	// ExtraTypes.
	d := e.Derived(human)
	if d.Power != 2 || d.Toughness != 2 {
		t.Fatalf("Maskwood Human through a Goblin lord = %d/%d, want 2/2", d.Power, d.Toughness)
	}
	for _, want := range arbitrarySubtypes {
		if !slices.Contains(d.Types, want) {
			t.Fatalf("Maskwood-affected creature's derived types missing %q", want)
		}
	}
	for _, bad := range nonCreatureTypeWords {
		if slices.Contains(d.Types, bad) {
			t.Fatalf("Maskwood-affected creature's derived types must not carry non-creature word %q", bad)
		}
	}
	// The printed types stay: every creature type is IN ADDITION.
	if !slices.Contains(d.Types, "Human") {
		t.Fatalf("Maskwood-affected creature lost its printed type Human")
	}
}

func TestMaskwoodNexusCreatureCardInHandIsEveryType(t *testing.T) {
	t.Parallel()
	// AffectedZone$ All: "The same is true for creature spells you control
	// and creature cards you own that aren't on the battlefield."
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Maskwood Nexus"), lookup(t, reg, "Llanowar Elves")}, []*cards.Card{})
	moveByName(t, e, 0, "Maskwood Nexus", state.ZBattlefield)
	elf := moveByName(t, e, 0, "Llanowar Elves", state.ZHand)

	d := e.Derived(elf)
	for _, want := range arbitrarySubtypes {
		if !slices.Contains(d.Types, want) {
			t.Fatalf("Maskwood-affected hand card's derived types missing %q", want)
		}
	}
	if slices.Contains(d.Types, "Arcane") {
		t.Fatalf("Maskwood-affected hand card's derived types must not carry Arcane")
	}
}

func TestMutavaultAnimationGrantsAllCreatureTypes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Mutavault")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Mutavault", state.ZBattlefield)
	onBoard(t, e, 0, goblinLordFixture)

	// Before the animation the land matches no creature subtype.
	if d := e.Derived(id); slices.Contains(d.Types, "Goblin") {
		t.Fatalf("unanimated Mutavault's types carry Goblin: %v", d.Types)
	}
	addMana(t, e, 0, "C")
	submitChoices(t, e, animateAbilityOption(t, e, id).Index)
	settleActivation(t, e)

	d := e.Derived(id)
	if !slices.Contains(d.Types, "Creature") || !slices.Contains(d.Types, "Land") {
		t.Fatalf("animated Mutavault types %v missing Creature/Land", d.Types)
	}
	for _, want := range arbitrarySubtypes {
		if !slices.Contains(d.Types, want) {
			t.Fatalf("animated Mutavault's derived types missing %q", want)
		}
	}
	// The Goblin lord's Affected$ reaches the animated manland.
	if d.Power != 3 || d.Toughness != 3 {
		t.Fatalf("animated Mutavault through a Goblin lord = %d/%d, want 3/3", d.Power, d.Toughness)
	}
	// The animation grants no colour.
	if got := e.Colors(id); got != "" {
		t.Fatalf("animated Mutavault colours = %q, want colourless", got)
	}
	if !e.IsCreature(id) {
		t.Fatalf("animated Mutavault is not a creature")
	}
}
