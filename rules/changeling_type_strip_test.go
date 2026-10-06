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
		if effects.MatchesSpecCtx(e.G, spec, outcast, e.specCtx(0, 0)) {
			t.Errorf("stripped Outcast still matches %s", spec)
		}
	}
	if !effects.MatchesSpecCtx(e.G, "Creature.nonSurrakar", outcast, e.specCtx(0, 0)) {
		t.Error("stripped Outcast must match Creature.nonSurrakar")
	}
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
				if !effects.MatchesSpecCtx(e.G, spec, id, e.specCtx(0, 0)) {
					t.Errorf("set creature types must match %s", spec)
				}
			}
			if effects.MatchesSpecCtx(e.G, "Creature.Surrakar", id, e.specCtx(0, 0)) {
				t.Error("set creature types resurrected the intrinsic CDA")
			}
		})
	}
}
