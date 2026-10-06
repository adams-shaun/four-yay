package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const mostProminentBody = "Count$MostProminentCreatureType Creature.YouCtrl"

// A Changeling whose creature types were stripped (layer 4) keeps the keyword
// (layer 6) but no longer joins every group; a creature that gained a subtype
// joins that group.
func TestMostProminentCreatureTypeAfterStrip(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		lookup(t, reg, "Amoeboid Changeling"), lookup(t, reg, "Changeling Outcast"),
		lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Llanowar Elves"),
	}, nil)
	amoeboid := moveByName(t, e, 0, "Amoeboid Changeling", state.ZBattlefield)
	outcast := moveByName(t, e, 0, "Changeling Outcast", state.ZBattlefield)
	if e.G.Obj(outcast).Zone != state.ZBattlefield || !e.HasKeyword(outcast, "Changeling") || !e.Derived(outcast).AllCreatureTypes {
		t.Fatal("precondition: battlefield Outcast must have Changeling and all creature types")
	}
	activateTargetsValid(t, e, amoeboid, 1, outcast)
	settleActivation(t, e)
	if got := e.Derived(outcast); got.AllCreatureTypes || !slices.Equal(got.Types, []string{"Creature"}) {
		t.Fatalf("precondition: strip did not resolve: types %v, all %v", got.Types, got.AllCreatureTypes)
	}
	if !e.HasKeyword(outcast, "Changeling") {
		t.Fatal("precondition: the strip must leave the Changeling keyword")
	}
	// Amoeboid is a changeling too (and tapped from its activation): strip it
	// directly so only the Bears supply a group.
	e.AddContinuous(state.ContinuousEffect{Source: amoeboid, Affects: "Card.Self", Layer: state.LType, RemoveCreatureTypes: true})
	if e.Derived(amoeboid).AllCreatureTypes {
		t.Fatal("precondition: Amoeboid must be stripped as well")
	}
	bears := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if e.G.Obj(bears).Zone != state.ZBattlefield {
		t.Fatal("precondition: Bears must be on the battlefield")
	}
	if got := evalHead(t, e, 0, 0, mostProminentBody); got != 1 {
		t.Errorf("stripped changelings must not join the Bear group: got %d, want 1", got)
	}

	t.Run("granted subtype", func(t *testing.T) {
		elves := moveByName(t, e, 0, "Llanowar Elves", state.ZBattlefield)
		if e.G.Obj(elves).Zone != state.ZBattlefield {
			t.Fatal("precondition: Elves must be on the battlefield")
		}
		before := evalHead(t, e, 0, 0, mostProminentBody)
		e.AddContinuous(state.ContinuousEffect{Source: elves, Affects: "Card.Self", Layer: state.LType, AddTypes: []string{"Bear"}})
		if !slices.Contains(e.Derived(elves).Types, "Bear") {
			t.Fatal("precondition: Elves must have gained Bear")
		}
		after := evalHead(t, e, 0, 0, mostProminentBody)
		if before != 1 || after != 2 {
			t.Errorf("granted Bear: count %d -> %d, want 1 -> 2", before, after)
		}
	})
}
