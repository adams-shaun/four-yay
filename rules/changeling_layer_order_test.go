package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// CR 613.1d/613.1f: Changeling defines types in layer 4, before layer 6
// removes the ability. A type strip still overrides that CDA in layer 4.
func TestChangelingTypeCDASettlesBeforeAbilityRemoval(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Amoeboid Changeling"), lookup(t, reg, "Changeling Outcast")}, nil)
	amoeboid := moveByName(t, e, 0, "Amoeboid Changeling", state.ZBattlefield)
	outcast := moveByName(t, e, 0, "Changeling Outcast", state.ZBattlefield)
	if e.G.Obj(outcast).Zone != state.ZBattlefield || !e.HasKeyword(outcast, "Changeling") || !e.Derived(outcast).AllCreatureTypes {
		t.Fatal("precondition: battlefield Outcast must have Changeling and all creature types")
	}
	assertChangelingTypePredicate(t, e, outcast, "Creature.Surrakar", true)

	e.AddContinuous(state.ContinuousEffect{Source: outcast, Affects: "Card.Self", Layer: state.LAbilities, RemoveAbilities: true})
	if e.HasKeyword(outcast, "Changeling") {
		t.Fatal("precondition: the layer-6 ability removal must remove Changeling")
	}
	if !e.Derived(outcast).AllCreatureTypes {
		t.Fatal("layer-6 ability removal must not undo Changeling's layer-4 CDA")
	}
	assertChangelingTypePredicate(t, e, outcast, "Creature.Surrakar", true)

	activateTargetsValid(t, e, amoeboid, 1, outcast)
	settleActivation(t, e)
	if len(e.G.Stack) != 0 || e.G.Obj(outcast).Zone != state.ZBattlefield || !slices.ContainsFunc(e.active(), func(ce state.ContinuousEffect) bool {
		return ce.Source == outcast && ce.Layer == state.LType && ce.RemoveCreatureTypes
	}) {
		t.Fatal("precondition: Amoeboid's activation must resolve and register its type strip")
	}
	if got := e.Derived(outcast); got.AllCreatureTypes || !slices.Equal(got.Types, []string{"Creature"}) {
		t.Errorf("stripped Outcast = types %v, all creature types %v; want [Creature], false", got.Types, got.AllCreatureTypes)
	}
	assertChangelingTypePredicate(t, e, outcast, "Creature.Surrakar", false)
	assertChangelingTypePredicate(t, e, outcast, "Creature.nonSurrakar", true)
}
