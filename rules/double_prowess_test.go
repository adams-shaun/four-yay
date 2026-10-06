package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestDoubleProwessTriggersTwice pins CR 702.108b ("If a creature has multiple
// instances of prowess, each triggers separately") at the engine level. The
// fixture prints Prowess on two separate K: lines -- the Thor Odinson / Ruric
// Thar, Biomagus shape -- so castings Shock, a noncreature spell, must pump
// the creature +2/+2, not +1/+1. Before cards' duplicate-line fix the second
// K:Prowess line was collapsed into the first and only one trigger fired.
func TestDoubleProwessTriggersTwice(t *testing.T) {
	t.Parallel()
	// 4/4 base: after two Prowess triggers it is 6/6 until end of turn.
	thor := "Name:Thor\nManaCost:2 R\nTypes:Creature God Warrior\nPT:4/4\nK:Prowess\nK:Prowess\nOracle:x\n"
	shock := "Name:Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 91, thor, shock)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	e.priorityRound()
	if e.Power(id) != 4 || e.Toughness(id) != 4 {
		t.Fatalf("precondition: prowess creature is %d/%d, want 4/4 before the cast", e.Power(id), e.Toughness(id))
	}
	bolt := addToHand(t, e, 0, "Name:Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2\nOracle:x\n")
	addMana(t, e, 0, "R")
	e.Advance()
	// Cast Shock by hand rather than through castObj: with two Prowess
	// triggers queued the engine interposes the CR 603.3b trigger_order
	// decision, which castObj's priority-only drain would fatal on. Answer
	// the order (the two +1/+1 pumps are symmetric, so either order is
	// fine) and then drain.
	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision before casting Shock")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bolt {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Shock: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if td := e.Pending(); td != nil && td.Kind == decision.KTarget && len(td.Options) > 0 {
		submitChoices(t, e, td.Options[0].Index)
	}
	// Two simultaneous Prowess triggers force the order ask. Require it:
	// its presence is itself the doubled-instance signal, so a regression
	// to a single trigger fails here rather than at the power assert.
	od := e.Pending()
	if od == nil || od.Kind != decision.KTriggerOrder || len(od.Options) != 2 {
		t.Fatalf("want a 2-option trigger_order for two Prowess triggers, got %+v", od)
	}
	submitChoices(t, e, 0, 1)
	passUntilStackEmpty(t, e, 20)
	if p := e.Power(id); p != 6 {
		t.Fatalf("double Prowess left power %d, want 6 (+2/+2 from two triggers)", p)
	}
	if tf := e.Toughness(id); tf != 6 {
		t.Fatalf("double Prowess left toughness %d, want 6 (+2/+2 from two triggers)", tf)
	}
	replayCheck(t, e, cfg)
}
