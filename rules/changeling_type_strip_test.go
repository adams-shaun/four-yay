package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// CR 613.1d/613.7: Changeling's CDA precedes timestamped type changes.
func TestChangelingCreatureTypeStrip(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Amoeboid Changeling"), lookup(t, reg, "Changeling Outcast")}, nil)
	amoeboid := moveByName(t, e, 0, "Amoeboid Changeling", state.ZBattlefield)
	outcast := moveByName(t, e, 0, "Changeling Outcast", state.ZBattlefield)
	if e.G.Obj(outcast).Zone != state.ZBattlefield || !e.HasKeyword(outcast, "Changeling") || !e.Derived(outcast).AllCreatureTypes {
		t.Fatal("precondition: battlefield Outcast must have Changeling and all creature types")
	}
	if !effects.MatchesSpecCtx(e.G, "Creature.Surrakar", outcast, e.specCtx(0, 0)) {
		t.Fatal("precondition: Outcast must match an unprinted creature subtype")
	}
	activateTargetsValid(t, e, amoeboid, 1, outcast)
	settleActivation(t, e)
	if len(e.G.Stack) != 0 || e.G.Obj(outcast).Zone != state.ZBattlefield {
		t.Fatal("precondition: activation must resolve with Outcast still on the battlefield")
	}
	if !slices.ContainsFunc(e.active(), func(ce state.ContinuousEffect) bool { return ce.Layer == state.LType && ce.RemoveCreatureTypes }) {
		t.Fatal("precondition: Amoeboid's creature-type strip did not register")
	}
	if got := e.Derived(outcast); got.AllCreatureTypes || !slices.Equal(got.Types, []string{"Creature"}) {
		t.Errorf("stripped Outcast = types %v, all creature types %v; want [Creature], false", got.Types, got.AllCreatureTypes)
	}
	if !e.HasKeyword(outcast, "Changeling") {
		t.Fatal("type strip must not remove the Changeling ability in layer 6")
	}
	for _, spec := range []string{"Surrakar", "Creature.Surrakar", "Creature.Shapeshifter"} {
		assertChangelingTypePredicate(t, e, outcast, spec, false)
	}
	assertChangelingTypePredicate(t, e, outcast, "Creature.nonSurrakar", true)
	// A later type-gated effect must not resurrect the intrinsic CDA either.
	e.AddContinuous(state.ContinuousEffect{Source: outcast, Affects: "Creature.Surrakar", Layer: state.LPT, Sub: state.SubModify, AddPower: 3})
	if got := e.Power(outcast); got != 1 {
		t.Errorf("Surrakar-only pump affected stripped Outcast: power %d, want 1", got)
	}
	e.EndOfTurnCleanup()
	if !e.Derived(outcast).AllCreatureTypes || !effects.MatchesSpecCtx(e.G, "Creature.Surrakar", outcast, e.specCtx(0, 0)) {
		t.Fatal("all creature types must return when Amoeboid's strip expires")
	}
}

func TestChangelingSetCreatureTypes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Changeling Outcast", "Mistform Ultimus"} {
		t.Run(name, func(t *testing.T) {
			e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, name)}, nil)
			id := moveByName(t, e, 0, name, state.ZBattlefield)
			if e.G.Obj(id).Zone != state.ZBattlefield || !e.Derived(id).AllCreatureTypes {
				t.Fatal("precondition: CDA source must be on the battlefield with all creature types")
			}
			want := []string{"Creature", "Frog"}
			if slices.Contains(e.Derived(id).Types, "Legendary") {
				want = append([]string{"Legendary"}, want...)
			}
			e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LType, SetCreatureTypes: true, AddTypes: []string{"Frog"}})
			if got := e.Derived(id); got.AllCreatureTypes || !slices.Equal(got.Types, want) {
				t.Errorf("set creature types = %v, all %v; want %v, false", got.Types, got.AllCreatureTypes, want)
			}
			for _, spec := range []string{"Frog", "Creature.Frog", "Creature.nonSurrakar"} {
				assertChangelingTypePredicate(t, e, id, spec, true)
			}
			assertChangelingTypePredicate(t, e, id, "Creature.Surrakar", false)
			// Also exercise a semantic-only change: the printed type list can
			// be restored without restoring the CDA's all-types marker.
			printed := e.G.Obj(id).Face().Types
			printedSubtype := printed[len(printed)-1]
			if !effects.CreatureTypeWords(printedSubtype) {
				t.Fatal("precondition: last printed type must be a creature subtype")
			}
			e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LType, SetCreatureTypes: true, AddTypes: []string{printedSubtype}})
			if !slices.Equal(e.Derived(id).Types, e.G.Obj(id).Face().Types) {
				t.Fatal("precondition: setter must reproduce the printed type list")
			}
			assertChangelingTypePredicate(t, e, id, "Creature.Surrakar", false)
			e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LType, AddAllCreatureTypes: true})
			if !e.Derived(id).AllCreatureTypes {
				t.Fatal("later all-types grant must override the setter")
			}
			assertChangelingTypePredicate(t, e, id, "Creature.Surrakar", true)
		})
	}
}

// Assert both filter implementations, not merely whichever sidecar the engine
// happened to install for this match's cards.
func assertChangelingTypePredicate(t *testing.T, e *Engine, id state.ObjID, spec string, want bool) {
	t.Helper()
	sc := e.specCtx(0, 0)
	sc.PredicatePrograms = nil
	if got := effects.MatchesSpecCtx(e.G, spec, id, sc); got != want {
		t.Errorf("textual %s = %v, want %v", spec, got, want)
	}
	sc.PredicatePrograms = effects.CompilePredicatePrograms([]string{spec})
	verdict := sc.PredicatePrograms.Evaluate(spec, e.G, e.G.Obj(id), sc)
	if verdict == effects.PredicateMaybe {
		t.Fatalf("precondition: %s must have a definite compiled type predicate", spec)
	}
	if got := verdict == effects.PredicateYes; got != want {
		t.Errorf("compiled %s = %v, want %v", spec, got, want)
	}
}
