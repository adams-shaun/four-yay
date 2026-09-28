package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestGameAwarePredicates is the effects leaf for the predicate families this
// seat closed that need more than the object alone: the live game (the active
// player, the commander list), the object's own zone/counters, or the effect's
// source (for combat pairing). Each is pinned in the positive AND negative
// sense and, critically, each is asserted to be recognised by
// UnknownPredicates so the matcher and the census cannot disagree.
//
// The families:
//   - inZoneStack: the object is a spell/ability currently on the stack.
//   - ActivePlayerCtrl: the object is controlled by the active player.
//   - HasCounters: the object carries at least one counter of any kind.
//   - Historic: artifact, legendary, or Saga (the Historic keyword reminder).
//   - IsCommander: the object is one of a seat's commanders.
//   - blockingSource: the object is a creature blocking the source.
//   - blockedBySource: the object is being blocked by the source.
//   - nonColorless: the object has at least one colour (the negation of the
//     wordColorless classifier is now generically recognised).
func TestGameAwarePredicates(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})
	// corpusObject writes into g.Objs, which reallocates as objects are added,
	// so a *state.Object pointer is only valid until the next AddObject. Every
	// mutation below therefore re-fetches by ID through g.Obj, never through a
	// held pointer.
	src := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID) // the effect's source

	// Leaf 1: inZoneStack -- the object is on the stack, not the battlefield.
	spell := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	spell.Zone = state.ZStack
	if !MatchesObjectCtx(g, "Card.inZoneStack", spell, SpecContext{You: 0}) {
		t.Errorf("Card.inZoneStack must match an object on the stack")
	}
	if MatchesObjectCtx(g, "Card.inZoneStack", src, SpecContext{You: 0}) {
		t.Errorf("Card.inZoneStack must not match a battlefield object")
	}

	// Leaf 2: ActivePlayerCtrl -- controlled by the active player (seat 0).
	ctrl := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	ctrl.Controller = 0
	other := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	other.Controller = 1
	if !MatchesObjectCtx(g, "Creature.ActivePlayerCtrl", ctrl, SpecContext{You: 0}) {
		t.Errorf("Creature.ActivePlayerCtrl must match a creature the active player controls")
	}
	if MatchesObjectCtx(g, "Creature.ActivePlayerCtrl", other, SpecContext{You: 0}) {
		t.Errorf("Creature.ActivePlayerCtrl must not match a creature another player controls")
	}

	// Leaf 3: HasCounters.
	countered := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	countered.AddCounter("P1P1", 1)
	plain := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	if !MatchesObjectCtx(g, "Creature.HasCounters", countered, SpecContext{You: 0}) {
		t.Errorf("Creature.HasCounters must match a creature with a counter")
	}
	if MatchesObjectCtx(g, "Creature.HasCounters", plain, SpecContext{You: 0}) {
		t.Errorf("Creature.HasCounters must not match a creature with no counter")
	}

	// Leaf 4: Historic -- an artifact, a legendary and a Saga are all historic;
	// a plain Bear is not.
	artifact := g.Obj(corpusObject(t, reg, g, "Expedition Map").ID)
	legend := g.Obj(corpusObject(t, reg, g, "Isamaru, Hound of Konda").ID) // Legendary Creature
	saga := g.Obj(corpusObject(t, reg, g, "The Elder Dragon War").ID)      // Enchantment Saga
	if !MatchesObjectCtx(g, "Permanent.Historic", artifact, SpecContext{You: 0}) {
		t.Errorf("Permanent.Historic must match an artifact (Expedition Map)")
	}
	if !MatchesObjectCtx(g, "Permanent.Historic", legend, SpecContext{You: 0}) {
		t.Errorf("Permanent.Historic must match a legendary (Isamaru, Hound of Konda)")
	}
	if !MatchesObjectCtx(g, "Permanent.Historic", saga, SpecContext{You: 0}) {
		t.Errorf("Permanent.Historic must match a Saga (The Elder Dragon War)")
	}
	if MatchesObjectCtx(g, "Permanent.Historic", plain, SpecContext{You: 0}) {
		t.Errorf("Permanent.Historic must not match a plain Grizzly Bears")
	}

	// Leaf 5: IsCommander.
	commander := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	g.Players[0].Commanders = []state.ObjID{commander.ID}
	notCommander := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	if !MatchesObjectCtx(g, "Creature.IsCommander", commander, SpecContext{You: 0}) {
		t.Errorf("Creature.IsCommander must match a designated commander")
	}
	if MatchesObjectCtx(g, "Creature.IsCommander", notCommander, SpecContext{You: 0}) {
		t.Errorf("Creature.IsCommander must not match a non-commander")
	}

	// Leaf 6: blockingSource -- the object blocks the source; the source's
	// BlockedBy names the object as one of its blockers.
	blocker := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	// src was fetched early; g.Obj reallocates as objects accumulate, so
	// re-fetch by ID before mutating it, never through the held pointer.
	g.Obj(src.ID).BlockedBy = append(g.Obj(src.ID).BlockedBy, blocker.ID)
	g.NoteBlockers() // written directly, not by DeclareBlockers
	if !MatchesObjectCtx(g, "Creature.blockingSource", blocker, SpecContext{You: 0, Source: src.ID}) {
		t.Errorf("Creature.blockingSource must match a creature blocking the source")
	}
	if MatchesObjectCtx(g, "Creature.blockingSource", plain, SpecContext{You: 0, Source: src.ID}) {
		t.Errorf("Creature.blockingSource must not match a non-blocker")
	}

	// Leaf 7: blockedBySource -- the object is being blocked by the source; the
	// object's BlockedBy names the source.
	blocked := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	blocked.BlockedBy = append(blocked.BlockedBy, src.ID)
	g.NoteBlockers()
	if !MatchesObjectCtx(g, "Creature.blockedBySource", blocked, SpecContext{You: 0, Source: src.ID}) {
		t.Errorf("Creature.blockedBySource must match a creature the source is blocking")
	}
	if MatchesObjectCtx(g, "Creature.blockedBySource", plain, SpecContext{You: 0, Source: src.ID}) {
		t.Errorf("Creature.blockedBySource must not match a non-blocked creature")
	}

	// Leaf 8: nonColorless -- a coloured card matches, a colourless one does not.
	colored := g.Obj(corpusObject(t, reg, g, "Terminate").ID) // {B}{R}
	colourless := g.Obj(corpusObject(t, reg, g, "Expedition Map").ID)
	if !MatchesObjectCtx(g, "Card.nonColorless", colored, SpecContext{You: 0}) {
		t.Errorf("Card.nonColorless must match a coloured card")
	}
	if MatchesObjectCtx(g, "Card.nonColorless", colourless, SpecContext{You: 0}) {
		t.Errorf("Card.nonColorless must not match a colourless card")
	}

	// UnknownPredicates must agree with the matcher: every predicate above is
	// recognised, so none is reported unknown.
	recognised := []string{
		"Card.inZoneStack",
		"Creature.ActivePlayerCtrl",
		"Creature.HasCounters",
		"Permanent.Historic",
		"Creature.IsCommander",
		"Creature.blockingSource",
		"Creature.blockedBySource",
		"Card.nonColorless",
	}
	for _, spec := range recognised {
		if un := UnknownPredicates(spec); len(un) != 0 {
			t.Errorf("UnknownPredicates(%q) = %v, want none (the matcher recognises it)", spec, un)
		}
	}
}
