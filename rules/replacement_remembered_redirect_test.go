package rules

// The remembered-recipient damage redirect: an Effect-created damage magnet
// whose replacement body is
//
//	SVar:X:DB$ ReplaceEffect | VarName$ Affected | VarValue$ Remembered
//
// must rewrite the HELD Damage event's recipient to the object the Effect
// remembered (Heroic Sacrifice: "Until end of turn, all damage that would be
// dealt to you and creatures you control is dealt to the chosen creature
// instead", CR 614.6's replacement applied through CR 120.3a's recipient
// change). The remembered binding travels as in-flight replacement state
// (Engine.replRemembered, seeded by runReplaceWith from the body's
// ctx.Remembered) because ReplaceEvent sits on the effects.Host interface and
// carries no context. Without the binding the switch arm has nothing to
// resolve and the redirect silently does nothing -- the damage lands on the
// original recipient and the magnet absorbs none.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// resolveHeroicSacrificeEffect resolves the real corpus card's SP$ Effect
// head against a chosen creature exactly as casting the spell does, so the
// DamageMagnet replacement registers with RememberObjects$ Targeted's capture
// of the chosen creature. It asserts its own precondition: the registration
// exists and carries exactly that capture, so a registration regression fails
// loudly instead of letting the redirect assertions below pass vacuously.
func resolveHeroicSacrificeEffect(t *testing.T, e *Engine, reg *cards.Registry, controller state.PlayerID, chosen state.ObjID) {
	t.Helper()
	src := e.G.AddObject(mustCorpusCard(t, reg, "Heroic Sacrifice"), controller)
	src.Zone = state.ZStack
	effects.Resolve(e, &effects.Ctx{
		Source:         src.ID,
		Controller:     controller,
		Targets:        []state.Target{{Obj: chosen}},
		TargetsOffered: true,
		SVars:          src.Face().SVars,
	}, src.Face().SpellAbility())
	if obj := e.G.Obj(chosen); obj == nil || obj.Zone != state.ZBattlefield {
		t.Fatalf("chosen creature = %+v, want on the battlefield before the magnet registers", obj)
	}
	found := false
	for _, ce := range e.active() {
		if ce.ReplacementEvent != "DamageDone" || ce.Source != src.ID {
			continue
		}
		found = true
		if len(ce.Remembered) != 1 || ce.Remembered[0] != chosen {
			t.Fatalf("registered DamageDone replacement remembered = %v, want exactly the chosen creature %d", ce.Remembered, chosen)
		}
	}
	if !found {
		t.Fatalf("no DamageDone replacement registered for the Heroic Sacrifice effect (source %d): the magnet never registered", src.ID)
	}
}

// heroSacrificeBoard builds the shared board: seat 0 holds the chosen magnet
// creature and a second creature of its own (the ValidTarget$
// You,Creature.YouCtrl class), seat 1 holds the damage source.
func heroSacrificeBoard(t *testing.T, e *Engine, reg *cards.Registry) (chosen, friend, source state.ObjID) {
	t.Helper()
	chosen = onBoard(t, e, 0, "Name:Chosen\nTypes:Creature\nPT:3/3\nOracle:x\n")
	friend = onBoard(t, e, 0, "Name:Friend\nTypes:Creature\nPT:2/2\nOracle:x\n")
	source = onBoard(t, e, 1, "Name:Source\nManaCost:R\nTypes:Creature\nPT:1/1\nOracle:x\n")
	resolveHeroicSacrificeEffect(t, e, reg, 0, chosen)
	e.pending = nil
	return chosen, friend, source
}

func TestHeroicSacrificeRedirectsDamageToRememberedCreature(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)

	t.Run("damage to the controller redirects to the chosen creature", func(t *testing.T) {
		t.Parallel()
		e := newSeats(t, 2)
		chosen, _, source := heroSacrificeBoard(t, e, reg)
		e.damaging = source
		applied := e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
		e.damaging = 0
		if applied.Kind != events.Damage || applied.Obj != chosen {
			t.Fatalf("applied = %+v, want damage redirected to the chosen creature %d", applied, chosen)
		}
		if got := e.G.Players[0].Life; got != 20 {
			t.Fatalf("controller life = %d, want 20 (the redirect absorbed the damage)", got)
		}
		if got := e.G.Obj(chosen).Damage; got != 3 {
			t.Fatalf("chosen creature damage = %d, want 3", got)
		}
	})

	t.Run("damage to a creature you control redirects too", func(t *testing.T) {
		t.Parallel()
		e := newSeats(t, 2)
		chosen, friend, source := heroSacrificeBoard(t, e, reg)
		e.damaging = source
		applied := e.emit(events.Event{Kind: events.Damage, Obj: friend, Amount: 3})
		e.damaging = 0
		if applied.Kind != events.Damage || applied.Obj != chosen {
			t.Fatalf("applied = %+v, want damage to your other creature redirected to the chosen creature %d", applied, chosen)
		}
		if got := e.G.Obj(friend).Damage; got != 0 {
			t.Fatalf("original recipient damage = %d, want 0 after the redirect", got)
		}
		if got := e.G.Obj(chosen).Damage; got != 3 {
			t.Fatalf("chosen creature damage = %d, want 3", got)
		}
	})

	t.Run("damage to the opponent is not redirected", func(t *testing.T) {
		t.Parallel()
		e := newSeats(t, 2)
		chosen, _, source := heroSacrificeBoard(t, e, reg)
		e.damaging = source
		applied := e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 3})
		e.damaging = 0
		if applied.Kind != events.Damage || applied.Player != 1 || applied.Obj != 0 {
			t.Fatalf("applied = %+v, want the damage to land on the opponent untouched (ValidTarget$ You,Creature.YouCtrl excludes them)", applied)
		}
		if got := e.G.Players[1].Life; got != 17 {
			t.Fatalf("opponent life = %d, want 17 (the redirect must not reach the other side)", got)
		}
		if got := e.G.Obj(chosen).Damage; got != 0 {
			t.Fatalf("chosen creature damage = %d, want 0 (opponent damage is outside the magnet's class)", got)
		}
	})
}
