package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestNoAbilitiesPredicate is the effects leaf for Forge's Card.NoAbilities
// filter property (CR 113.12): a vanilla creature matches, an ability-bearing
// creature does not, a land's mana ability is skipped, and the negated
// `Creature.!NoAbilities` inverts. Every object is a REAL corpus card, never a
// synthetic face. Each case first asserts the precondition the result depends
// on (the vanilla face really is empty; the non-matching face really carries a
// keyword / ability / static), so the test cannot pass vacuously.
func TestNoAbilitiesPredicate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})

	vanilla := corpusObject(t, reg, g, "Grizzly Bears")        // Creature Bear, no abilities
	runeclaw := corpusObject(t, reg, g, "Runeclaw Bear")       // Creature Bear, no abilities
	memnite := corpusObject(t, reg, g, "Memnite")              // Artifact Creature, no abilities
	forest := corpusObject(t, reg, g, "Forest")                // Basic Land, AB:Mana only
	elves := corpusObject(t, reg, g, "Llanowar Elves")         // Creature, AB:Mana
	fang := corpusObject(t, reg, g, "Fang-Druid Summoner")     // Creature, Reach + a trigger
	ruxa := corpusObject(t, reg, g, "Ruxa, Patient Professor") // Creature, statics + triggers

	// Preconditions: prove the fixture actually carries the shapes the
	// assertions below turn on.
	if f := vanilla.Face(); f == nil || len(f.Keywords) != 0 || len(f.Statics) != 0 ||
		len(f.Repls) != 0 || len(f.Triggers) != 0 || len(f.Abilities) != 0 {
		t.Fatalf("precondition: Grizzly Bears should be a printed vanilla creature, face = %+v", f)
	}
	if f := forest.Face(); f == nil || len(f.Abilities) != 1 || f.Abilities[0].Kind != "AB" || f.Abilities[0].API != "Mana" {
		t.Fatalf("precondition: Forest should carry exactly one AB:Mana, face = %+v", f)
	}
	if f := elves.Face(); f == nil || len(f.Abilities) != 1 || f.Abilities[0].Kind != "AB" {
		t.Fatalf("precondition: Llanowar Elves should carry an activated AB:Mana, face = %+v", f)
	}
	if f := fang.Face(); f == nil || !f.HasKeyword("Reach") {
		t.Fatalf("precondition: Fang-Druid Summoner should carry Reach, face = %+v", f)
	}
	if f := ruxa.Face(); f == nil || len(f.Statics) == 0 {
		t.Fatalf("precondition: Ruxa should carry a printed static, face = %+v", f)
	}

	matches := func(o *state.Object) bool {
		return MatchesObjectCtx(g, "Creature.NoAbilities", o, SpecContext{You: 0})
	}

	for _, o := range []*state.Object{vanilla, runeclaw, memnite} {
		if !matches(o) {
			t.Errorf("Creature.NoAbilities must match vanilla creature %q", o.Face().Name)
		}
	}
	// A land's mana ability is skipped, so a vanilla Forest is "no abilities"
	// even under the Creature-narrowed spec the predicate itself does not
	// narrow (the base type word does).
	if !MatchesObjectCtx(g, "Card.NoAbilities", forest, SpecContext{You: 0}) {
		t.Error("Card.NoAbilities must match a vanilla Forest (land mana ability is skipped)")
	}
	for _, o := range []*state.Object{elves, fang, ruxa} {
		if matches(o) {
			t.Errorf("Creature.NoAbilities must not match ability-bearing %q", o.Face().Name)
		}
	}

	// Negation (Jasmine Boreal's `ValidBlocker$ Creature.!NoAbilities`).
	neg := func(o *state.Object) bool {
		return MatchesObjectCtx(g, "Creature.!NoAbilities", o, SpecContext{You: 0})
	}
	if !neg(elves) {
		t.Error("Creature.!NoAbilities must match an ability-bearing creature (Llanowar Elves)")
	}
	if neg(vanilla) {
		t.Error("Creature.!NoAbilities must not match a vanilla creature (Grizzly Bears)")
	}

	// Recognition: the census must not report a token the matcher evaluates.
	if got := UnknownPredicates("Creature.NoAbilities"); len(got) != 0 {
		t.Fatalf("UnknownPredicates(Creature.NoAbilities) = %v, want empty", got)
	}
	if got := UnknownPredicates("Creature.!NoAbilities"); len(got) != 0 {
		t.Fatalf("UnknownPredicates(Creature.!NoAbilities) = %v, want empty", got)
	}
	// And the fail-closed contract still holds for a genuinely unknown token.
	if got := UnknownPredicates("Creature.someMechanicWeDoNotModel"); len(got) != 1 || got[0] != "someMechanicWeDoNotModel" {
		t.Fatalf("UnknownPredicates(Creature.someMechanicWeDoNotModel) = %v, want [someMechanicWeDoNotModel]", got)
	}
	if MatchesObjectCtx(g, "Creature.someMechanicWeDoNotModel", vanilla, SpecContext{You: 0}) {
		t.Error("an unknown predicate must not match (fail closed)")
	}
}

// TestNoAbilitiesPredicateFailClosed pins the two degenerate inputs: a nil
// object and an object with no face both answer FALSE ("has abilities"), never
// true -- "no abilities" is not proven by missing data, so the selection must
// never silently widen. A face-down battlefield permanent answers TRUE (CR
// 708.2), the opposite polarity of objectHasAbility's guard.
func TestNoAbilitiesPredicateFailClosed(t *testing.T) {
	g := state.NewGame([]string{"you", "them"})
	if noAbilitiesPermanent(g, nil, 0, 0) {
		t.Error("NoAbilities must fail closed on a nil object (false, not true)")
	}
	if noAbilitiesPermanent(g, &state.Object{}, 0, 0) {
		t.Error("NoAbilities must fail closed on an object with no face (false, not true)")
	}

	// A face-down battlefield permanent has no abilities (CR 708.2). Use a real
	// corpus card so the FaceDown guard is exercised against a real face.
	reg := testutil.CorpusRegistry(t)
	vanilla := corpusObject(t, reg, g, "Grizzly Bears")
	vanilla.FaceDown = true
	vanilla.Zone = state.ZBattlefield
	if !noAbilitiesPermanent(g, vanilla, 0, 0) {
		t.Error("NoAbilities must be true for a face-down battlefield permanent (CR 708.2)")
	}
}
