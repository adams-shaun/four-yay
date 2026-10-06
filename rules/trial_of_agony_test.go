package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestTrialOfAgonyLetsTargetedOpponentChooseDamage(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	spell, bearCard, angelCard := lookup(t, reg, "Trial of Agony"), lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Serra Angel")
	e, _ := corpusEngineCfg(t, reg, []*cards.Card{spell}, []*cards.Card{bearCard, angelCard})
	bear := moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	angel := moveByName(t, e, 1, "Serra Angel", state.ZBattlefield)
	spellID := moveByName(t, e, 0, "Trial of Agony", state.ZHand)
	for _, id := range []state.ObjID{bear, angel} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
			t.Fatalf("precondition: target %d must be controlled by seat 1 on battlefield: %+v", id, o)
		}
	}
	if bear == angel {
		t.Fatal("precondition: Trial of Agony needs two distinct targets")
	}
	addMana(t, e, 0, "R")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("before cast pending = %+v, want priority", d)
	}
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spellID {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("Trial of Agony cast option absent from %+v", d.Options)
	}
	submitChoices(t, e, cast)
	targetAsk := e.Pending()
	if targetAsk == nil || targetAsk.Kind != decision.KTarget || targetOptionIndex(targetAsk, bear) < 0 || targetOptionIndex(targetAsk, angel) < 0 {
		t.Fatalf("precondition: both creature targets must be offered: %+v", targetAsk)
	}
	submitChoices(t, e, targetOptionIndex(targetAsk, bear), targetOptionIndex(targetAsk, angel))
	choose := passUntilAskKind(t, e, decision.KChoose, 200)
	if choose.Player != 1 || choose.ResumeKind != "choice" || choose.Prompt != "Choose one to take 5 damage" || len(choose.Options) != 2 {
		t.Fatalf("Trial of Agony choice = %+v, want opponent seat 1 choosing one of two", choose)
	}
	chosen := choose.Options[0].Obj
	other := choose.Options[1].Obj
	if chosen != bear && chosen != angel || other != bear && other != angel || chosen == other {
		t.Fatalf("precondition: choice options must be the two targets: %+v", choose.Options)
	}
	submitChoices(t, e, choose.Options[0].Index)
	passUntilStackEmpty(t, e, 200)
	damage := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Amount == 5 {
			damage++
			if ev.Obj != chosen {
				t.Errorf("5 damage landed on %d, chose %d", ev.Obj, chosen)
			}
		}
	}
	if damage != 1 {
		t.Fatalf("5-damage events = %d, want exactly one", damage)
	}
	if got := e.G.Obj(chosen).Zone; got != state.ZGraveyard {
		t.Errorf("chosen creature %d zone = %s, want graveyard", chosen, got)
	}
	if got := e.G.Obj(other).Zone; got != state.ZBattlefield {
		t.Errorf("unchosen creature %d zone = %s, want battlefield", other, got)
	}
}
