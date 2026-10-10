package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Q3b part bounds against layer-4 type changes. A Sac/Discard/Tap part's type
// mask is read off the PRINTED face, but the payment gates match a candidate
// against its CURRENT types (the layer-4 derived-type table every filter
// reads). Living Lands makes every Forest a creature, so the printed
// "Forest is not a creature" mask under-counted Witch's Cauldron's
// Sac<1/Creature> candidates: the proof said quiet while the walk offered the
// ability, sacrificing the animated Forest. The part now counts any object the
// derived-type table carries as a candidate.

func TestQuietPartBoundsLayer4TypeChange(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cauldron := lookup(t, reg, "Witch's Cauldron")
	livingLands := lookup(t, reg, "Living Lands")
	forest := lookup(t, reg, "Forest")

	build := func(t *testing.T, withLivingLands bool) (*Engine, state.ObjID, state.ObjID) {
		t.Helper()
		extras := []*cards.Card{cauldron, forest}
		if withLivingLands {
			extras = append(extras, livingLands)
		}
		e := quietBaseLandWith(t, reg, "Swamp", extras)
		e.G.Players[0].Pool[state.MB] = 1
		e.G.Players[0].Pool[state.MC] = 1
		if withLivingLands {
			addZone(t, e, 0, livingLands, state.ZBattlefield)
		}
		fid := addZone(t, e, 0, forest, state.ZBattlefield)
		cid := addZone(t, e, 0, cauldron, state.ZBattlefield)
		// Precondition for both rows: no PRINTED creature anywhere on seat
		// 0's battlefield, so only a layer-4 type change can supply one.
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().IsCreature() {
				t.Fatalf("precondition: %s is a printed creature", o.Face().Name)
			}
		}
		return e, fid, cid
	}

	t.Run("animated land pays Sac<1/Creature>: the proof blocks", func(t *testing.T) {
		e, fid, cid := build(t, true)
		quietGroupWithPart(t, e, e.G.Obj(cid), pqSac)
		if !slices.Contains(e.Derived(fid).Types, "Creature") {
			t.Fatalf("precondition: Living Lands left the Forest a non-creature: %v", e.Derived(fid).Types)
		}
		opts, quiet, _ := quietContract(t, e, 0)
		if quietActivateFor(opts, cid) == nil {
			t.Fatalf("precondition: the walk did not offer the Cauldron ability: %v", optKinds(opts))
		}
		if quiet || e.quietBlocker(0) != qbBattlefieldAbility {
			t.Fatalf("proof quiet=%v blocker=%s, want blocked by the battlefield ability",
				quiet, quietBlockerNames[e.quietBlocker(0)])
		}
	})

	t.Run("same board without the animator: no creature, the proof is quiet", func(t *testing.T) {
		e, fid, cid := build(t, false)
		quietGroupWithPart(t, e, e.G.Obj(cid), pqSac)
		if slices.Contains(e.Derived(fid).Types, "Creature") {
			t.Fatalf("precondition: the Forest is a creature without Living Lands")
		}
		opts, quiet, _ := quietContract(t, e, 0)
		if quietActivateFor(opts, cid) != nil {
			t.Fatalf("precondition: the walk offered the Cauldron ability with no creature: %v", optKinds(opts))
		}
		if !quiet {
			t.Fatalf("proof blocked (%s) though no creature can be sacrificed",
				quietBlockerNames[e.quietBlocker(0)])
		}
	})
}

// quietObjectMatchesMask on a hand-built table: an object the table carries
// matches whatever its printed mask says, in BOTH mask directions -- a printed
// non-creature that became a creature (incl) and a printed land that stopped
// being a land (excl, "nonLand").
func TestQuietObjectMatchesMaskDerivedTable(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	forest := lookup(t, reg, "Forest")
	o := &state.Object{ID: 7, Card: forest, Zone: state.ZBattlefield}
	if o.Face() == nil || !o.Face().IsLand() || o.Face().IsCreature() {
		t.Fatalf("precondition: the fixture is not a printed non-creature land: %+v", o.Face())
	}
	table := []effects.ObjectTypes{{ID: 7, Types: []string{"Artifact", "Creature"}}}
	inclCreature := &quietPart{kind: pqSac, n: 1, incl: cards.TypeCreature}
	exclLand := &quietPart{kind: pqSac, n: 1, excl: cards.TypeLand}

	if quietObjectMatchesMask(o, state.ZBattlefield, inclCreature, nil) {
		t.Fatal("precondition: an untabled printed land matched the Creature mask")
	}
	if quietObjectMatchesMask(o, state.ZBattlefield, exclLand, nil) {
		t.Fatal("precondition: an untabled printed land matched the nonLand mask")
	}
	if !quietObjectMatchesMask(o, state.ZBattlefield, inclCreature, table) {
		t.Fatal("a tabled object (now a creature) must count for the Creature mask")
	}
	if !quietObjectMatchesMask(o, state.ZBattlefield, exclLand, table) {
		t.Fatal("a tabled object (no longer a land) must count for the nonLand mask")
	}
	other := []effects.ObjectTypes{{ID: 8, Types: []string{"Creature"}}}
	if quietObjectMatchesMask(o, state.ZBattlefield, inclCreature, other) {
		t.Fatal("a table entry for a different object must not widen this one")
	}
}
