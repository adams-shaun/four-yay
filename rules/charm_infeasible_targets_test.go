package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A triggered Charm announces its modes before it asks for targets. Unlike a
// cast, its mode announcement does not filter out modes without legal targets.
func TestTriggeredDistinctCharmWithEmptyModeTargetFizzles(t *testing.T) {
	t.Parallel()
	charm := "Name:Infeasible Charm Bearer\nManaCost:1 B\nTypes:Creature Bear\nPT:2/2\n" +
		"T:Mode$ Phase | Phase$ BeginCombat | ValidPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigCharm\n" +
		"SVar:TrigCharm:DB$ Charm | CharmNum$ 2 | Choices$ PlayerMode,CreatureMode\n" +
		"SVar:PlayerMode:DB$ LoseLife | ValidTgts$ Player | LifeAmount$ 2\n" +
		"SVar:CreatureMode:DB$ Destroy | ValidTgts$ Creature.OppCtrl\n" +
		"Oracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 6311, charm)
	bearer := putCreature(t, e, 0, charm)
	if o := e.G.Obj(bearer); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("trigger source not on battlefield: %+v", o)
	}
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("trigger stack = %v, want one ability", e.G.Stack)
	}
	id := e.G.Stack[0]
	face := e.G.Obj(bearer).Face()
	if n := len(e.legalTargetCandidates(0, id, id, cards.ResolveSVar(face.SVars, "PlayerMode"))); n == 0 {
		t.Fatal("fixture has no legal target for the first mode")
	}
	if n := len(e.legalTargetCandidates(0, id, id, cards.ResolveSVar(face.SVars, "CreatureMode"))); n != 0 {
		t.Fatalf("fixture has %d legal targets for the second mode, want zero", n)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || len(d.Options) != 2 {
		t.Fatalf("placement = %+v, want two selectable modes", d)
	}
	life := e.G.Players[0].Life
	submitChoices(t, e, 0, 1)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("unanswerable target decision: %+v", d)
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile {
		t.Fatalf("untargetable ability zone = %+v, want exile", o)
	}
	if got := e.G.Players[0].Life; got != life {
		t.Fatalf("first mode ran despite infeasible second mode: life %d -> %d", life, got)
	}
	replayCheck(t, e, cfg)
}

func TestCastDistinctCharmWithEmptyModeTargetCannotAnnounceBoth(t *testing.T) {
	t.Parallel()
	charm := "Name:Infeasible Cast Charm\nManaCost:B\nTypes:Instant\n" +
		"A:SP$ Charm | CharmNum$ 2 | Choices$ PlayerMode,CreatureMode\n" +
		"SVar:PlayerMode:DB$ LoseLife | ValidTgts$ Player | LifeAmount$ 2\n" +
		"SVar:CreatureMode:DB$ Destroy | ValidTgts$ Creature\n" +
		"Oracle:x\n"
	e, cfg, id := newFixtureDeck(t, 6312, charm)
	addMana(t, e, 0, "B")
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("spell not in hand: %+v", o)
	}
	// Both modes are mandatory. The cast-offer census withholds a spell
	// whose second mode has no legal target, before any mode or target ask.
	face := e.G.Obj(id).Face()
	if n := len(e.legalTargetCandidates(0, id, id, cards.ResolveSVar(face.SVars, "PlayerMode"))); n == 0 {
		t.Fatal("fixture has no legal player target")
	}
	if n := len(e.legalTargetCandidates(0, id, id, cards.ResolveSVar(face.SVars, "CreatureMode"))); n != 0 {
		t.Fatalf("fixture has %d creature targets, want zero", n)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority", d)
	}
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == id {
			t.Fatalf("impossible modal cast offered: %+v", opt)
		}
	}
	if e.G.Obj(id).Zone != state.ZHand {
		t.Fatalf("withheld spell zone=%s, want hand", e.G.Obj(id).Zone)
	}
	replayCheck(t, e, cfg)
}
