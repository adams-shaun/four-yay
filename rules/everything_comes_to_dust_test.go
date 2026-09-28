package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// End-to-end pin for the sharesCreatureTypeWith referent `Convoked` (CR
// 702.66's "each creature that convoked it"), on the one real corpus carrier:
// Everything Comes to Dust, whose ChangeType is
//
//	Creature.!sharesCreatureTypeWith Convoked,Artifact,Enchantment
//
// "Exile all creatures except those that share a creature type with a
// creature that convoked this spell, all artifacts, and all enchantments."
//
// Before the fix, `Convoked` classified wordUnknown so the whole first
// comma-alternative failed closed: NO creature was ever matched by
// `Creature.!sharesCreatureTypeWith Convoked` (a negated unrecognised shape
// is not a match), so the sweep exiled only artifacts and enchantments and
// left every creature alive with no Note. The referent now resolves from the
// SAME provenance the Defined$ Convoked selector reads (the cast's
// Object.Convoked, carried by the pay-time FlagConvoked CastInfo), so the two
// readings of "convoked" can never disagree.
//
// The scenario: seat 0 convokes with a Grizzly Bears (Bear). A second Grizzly
// Bears (Bear) shares a creature type with it and is SPARED; a Scathe Zombies
// (Zombie) shares none and is exiled; Sol Ring (artifact) and Glorious Anthem
// (enchantment) are exiled unconditionally.

// castEverythingComesToDustWithConvoke drives the whole cast flow of
// Everything Comes to Dust (in seat 0's hand) convoking convokeWith for a
// generic slot, then resolves the sweep. There are no targets, so the only
// in-cast ask is the convoke announcement.
func castEverythingComesToDustWithConvoke(t *testing.T, e *Engine, convokeWith state.ObjID) {
	t.Helper()
	id := conniveMoveTo(t, e, 0, "Everything Comes to Dust", state.ZHand)
	// 7WWW: ten pips. The one convoked creature covers a generic slot, so
	// nine white in the pool pays the rest (6 generic + WWW).
	addMana(t, e, 0, "WWWWWWWWW")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Everything Comes to Dust: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// CR 601.2b: the convoke announcement is the only ask before payment
	// (the spell names no targets).
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("after cast pending = %+v, want the convoke KChoose", d)
	}
	cIdx := -1
	for _, o := range d.Options {
		if o.Obj == convokeWith && o.Kind == "convoke_generic" {
			cIdx = o.Index
		}
	}
	if cIdx < 0 {
		t.Fatalf("convoke ask offers no generic option for %d: %+v", convokeWith, d.Options)
	}
	submitChoices(t, e, cIdx)
	passUntilStackEmpty(t, e, 40)
}

func TestEverythingComesToDustSparesConvokerTypeShares(t *testing.T) {
	t.Parallel()
	e, _ := conniveEngine(t,
		[]string{"Everything Comes to Dust", "Grizzly Bears", "Grizzly Bears", "Scathe Zombies", "Sol Ring", "Glorious Anthem"},
		[]string{"Grizzly Bears"})

	convoker := conniveMoveTo(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	spared := conniveMoveTo(t, e, 0, "Grizzly Bears", state.ZBattlefield, convoker)
	exiledCreature := conniveMoveTo(t, e, 0, "Scathe Zombies", state.ZBattlefield)
	artifact := conniveMoveTo(t, e, 0, "Sol Ring", state.ZBattlefield)
	enchantment := conniveMoveTo(t, e, 0, "Glorious Anthem", state.ZBattlefield)

	// PRECONDITIONS: every fixture is really a permanent of the named kind
	// on the battlefield before the sweep, so a vacuous setup fails loudly.
	for name, id := range map[string]state.ObjID{
		"convoker": convoker, "spared": spared, "exiled creature": exiledCreature,
		"artifact": artifact, "enchantment": enchantment,
	} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %s %d not on the battlefield: %+v", name, id, o)
		}
	}
	if o := e.G.Obj(convoker); !o.EffectiveIsCreature() || o.Tapped {
		t.Fatalf("precondition: convoker must be an untapped creature to convoke: %+v", o)
	}
	if o := e.G.Obj(exiledCreature); !o.EffectiveIsCreature() {
		t.Fatalf("precondition: exiled creature is not a creature: %+v", o)
	}

	castEverythingComesToDustWithConvoke(t, e, convoker)

	// The convoked creature itself shares a type with itself, so it is
	// spared too; the second Bear is the explicit sparing pin.
	for name, id := range map[string]state.ObjID{"convoker": convoker, "type-sharing Bear": spared} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("%s (shares a creature type with the convoker) should be SPARED, got %+v", name, o)
		}
	}
	// A creature sharing no creature type with any convoker is exiled.
	if o := e.G.Obj(exiledCreature); o == nil || o.Zone != state.ZExile {
		t.Fatalf("non-type-sharing creature should be EXILED, got %+v", o)
	}
	// Artifacts and enchantments are exiled unconditionally.
	if o := e.G.Obj(artifact); o == nil || o.Zone != state.ZExile {
		t.Fatalf("artifact should be EXILED, got %+v", o)
	}
	if o := e.G.Obj(enchantment); o == nil || o.Zone != state.ZExile {
		t.Fatalf("enchantment should be EXILED, got %+v", o)
	}
	// And the referent must have been READ, not silently unknown: the sweep
	// uses the real card. The convoke announcement is the feature's handler
	// having run, recorded as the pay-time "convoked" CastInfo flag.
	sawConvoke := false
	for _, ev := range e.L.Events {
		if ev.Counter == "convoked" {
			sawConvoke = true
		}
	}
	if !sawConvoke {
		t.Fatal("no \"convoked\" CastInfo provenance recorded: the convoke handler never ran")
	}
}
