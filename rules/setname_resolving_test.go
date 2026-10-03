package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// TestResolvingEffectNameFilterSeesSetNameRename closes the setname1 remainder
// that the SpecContext-only bridge left open: a filter call made by a RESOLVING
// effect -- not one rules builds for the layer walk or a static -- must read
// the CR 613.1c layer-3 name, not the printed face.
//
// The seam is effects.Resolve: rules' Engine implements effects' optional
// layerTablesHost, so the top of every Resolve walk publishes the current rename
// table onto the resolving Ctx, which propagates it through
// (*Ctx).SpecContext and Ctx.MatchSpec. An effects test double with no such
// host method leaves it nil and reads the printed face (the documented
// fallback), so this is a value-slice publication, not a state.Game
// back-pointer.
//
// The effect is a real resolving primitive: a DB$ SacrificeAll whose
// ValidCards$ names the renamed creature, driven exactly as SubAbility$ drives
// it (effects.Resolve with the chain's Ctx). It asserts both directions --
// the renamed bear is sacrificed, and the never-renamed elf survives -- and
// its own preconditions: the layer walk must name the bear, the effective name
// must differ from the printed one, and a Ctx that never went through Resolve
// must NOT see the rename (otherwise the test would pass with the wiring
// absent).
func TestResolvingEffectNameFilterSeesSetNameRename(t *testing.T) {
	t.Parallel()
	e, bladeID, bearID, elfID := setNameScopeBoard(t)
	equip(t, e, bladeID, bearID)

	// Precondition 1: the layer walk names the equipped bear, and the
	// effective name actually differs from the printed face name.
	if got := e.Name(bearID); got != "First Name" {
		t.Fatalf("precondition: layer walk named the equipped bear %q, want First Name", got)
	}
	if e.G.Obj(bearID).Face() == nil || e.Name(bearID) == e.G.Obj(bearID).Face().Name {
		t.Fatalf("precondition: the effective name %q must differ from the printed name", e.Name(bearID))
	}
	if e.G.Obj(bearID).Zone != state.ZBattlefield {
		t.Fatalf("precondition: the bear must be on the battlefield, got %s", e.G.Obj(bearID).Zone)
	}

	// Precondition 2: a Ctx that never went through effects.Resolve carries no
	// names, so the assertion below can only pass because the wiring ran.
	ctx := &effects.Ctx{Source: bladeID, Controller: 0}
	if ctx.MatchSpec(e.G, "Creature.namedFirst_Name", bearID, 0) {
		t.Fatal("precondition: an unpublished Ctx must not see the rename")
	}

	sa := &cards.SA{API: "SacrificeAll", Params: map[string]string{
		"ValidCards": "Creature.namedFirst_Name",
	}}
	effects.Resolve(e, ctx, sa)

	if got := e.G.Obj(bearID).Zone; got != state.ZGraveyard {
		t.Fatalf("the resolving effect's filter read the printed name: the renamed bear is in %s, want graveyard (effective name %q)",
			got, e.Name(bearID))
	}
	if got := e.G.Obj(elfID).Zone; got != state.ZBattlefield {
		t.Fatalf("control: the never-renamed elf must survive, got %s", got)
	}
}

// TestResolvingNameFilterRefreshesBetweenSubAbilities pins the effect-boundary
// snapshot: a body can remove the SetName source, and the following body must
// not match the stale name published at Resolve entry.
func TestResolvingNameFilterRefreshesBetweenSubAbilities(t *testing.T) {
	t.Parallel()
	e, bladeID, bearID, _ := setNameScopeBoard(t)
	equip(t, e, bladeID, bearID)
	if e.Name(bearID) != "First Name" || e.Name(bearID) == e.G.Obj(bearID).Face().Name {
		t.Fatalf("precondition: bearer must have a differing effective name, got %q", e.Name(bearID))
	}

	chain := &cards.SA{API: "ChangeZone", Params: map[string]string{
		"Defined": "Self", "Origin": "Battlefield", "Destination": "Graveyard",
	}, Sub: &cards.SA{API: "SacrificeAll", Params: map[string]string{
		"ValidCards": "Creature.namedFirst_Name",
	}}}
	effects.Resolve(e, &effects.Ctx{Source: bladeID, Controller: 0}, chain)

	if got := e.G.Obj(bladeID).Zone; got != state.ZGraveyard {
		t.Fatalf("precondition body did not move SetName source: blade is in %s", got)
	}
	if got := e.G.Obj(bearID).Zone; got != state.ZBattlefield {
		t.Fatalf("second body used stale effective name and sacrificed the bearer: zone=%s", got)
	}
}
