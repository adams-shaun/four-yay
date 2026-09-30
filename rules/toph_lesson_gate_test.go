package rules

// Toph, Hardheaded Teacher's Lesson rider (task agent-20260930T003623Z-97c7643c).
// The SpellCast trigger's chained `DBPutCounter` sub gates on
// `ConditionDefined$ TriggeredCardLKICopy | ConditionPresent$ Lesson` — put an
// ADDITIONAL +1/+1 counter on the earthbent land only when the cast spell is a
// Lesson. Before the gate's LKI-copy spelling was admitted to
// effects/conditions.go's whitelist, the condition returned unresolved and the
// sub ran unconditionally, so EVERY cast put two counters on the land. Both
// leaves below pin the real corpus cards in both directions; the counters the
// two leaves assert differ by exactly one, which is the rider.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// castAndEarthbend drives the shared shape of both leaves: Toph and a Forest
// on seat 0's battlefield, a spell cast from hand (funded with mana), the
// spell's own target ask answered, and the earthbend settle answered with the
// Forest. It returns the Forest's object id.
func castAndEarthbend(t *testing.T, reg *cards.Registry, spell, mana string) (e *Engine, cfg Config, forest state.ObjID) {
	t.Helper()
	e, cfg = searchEngine(t, reg, "Toph, Hardheaded Teacher", spell)
	toph := searchMoveByName(t, e, "Toph, Hardheaded Teacher", state.ZBattlefield)
	if o := e.G.Obj(toph); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Toph not on the battlefield: %+v", o)
	}
	if !e.IsCreature(toph) {
		t.Fatal("precondition: Toph is not a creature on the battlefield")
	}
	forest = searchMoveByName(t, e, "Forest", state.ZBattlefield)
	if o := e.G.Obj(forest); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Forest not on the battlefield: %+v", o)
	}
	if n := e.G.Obj(forest).Counter("P1P1"); n != 0 {
		t.Fatalf("precondition: Forest already has %d +1/+1 counters before the cast", n)
	}
	if id := searchMoveByName(t, e, spell, state.ZHand); id == 0 {
		t.Fatalf("precondition: %q not in hand", spell)
	}
	addMana(t, e, 0, mana)
	idx := -1
	for _, o := range castOptions(t, e) {
		if o.Obj != 0 {
			if oo := e.G.Obj(o.Obj); oo != nil && oo.Face() != nil && oo.Face().Name == spell {
				idx = o.Index
			}
		}
	}
	if idx < 0 {
		t.Fatalf("%q was not offered as a cast option at priority", spell)
	}
	submitChoices(t, e, idx)
	// The spell's own target ask (Lightning Bolt targets a player/creature;
	// The Art of Tea with TargetMin$ 0 poses none). Answer a player ask with
	// the opponent.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		tidx := -1
		for _, o := range d.Options {
			if o.Obj == 0 && o.Player == 1 {
				tidx = o.Index
			}
		}
		if tidx >= 0 {
			// Lightning Bolt: target the opponent.
			submitChoices(t, e, tidx)
		} else {
			// The Art of Tea (TargetMin$ 0): cast it with no targets.
			submitChoices(t, e)
		}
	}
	earthbendSettleAnswering(t, e, forest, 40)
	return e, cfg, forest
}

func TestTophLessonRiderGatesOnTheCastSpell(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	t.Run("non-lesson-spell-earths-one-counter", func(t *testing.T) {
		t.Parallel()
		e, cfg, forest := castAndEarthbend(t, reg, "Lightning Bolt", "R")
		if n := e.G.Obj(forest).Counter("P1P1"); n != 1 {
			t.Fatalf("Lightning Bolt is not a Lesson: earthbent Forest has %d +1/+1 counters, want 1", n)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("lesson-adds-the-extra-counter", func(t *testing.T) {
		t.Parallel()
		e, cfg, forest := castAndEarthbend(t, reg, "The Art of Tea", "GG")
		if n := e.G.Obj(forest).Counter("P1P1"); n != 2 {
			t.Fatalf("The Art of Tea is a Lesson: earthbent Forest has %d +1/+1 counters, want 2", n)
		}
		replayCheck(t, e, cfg)
	})
}
