package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestAnimatePowerOnlyKeepsToughness(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	t.Run("Turtle-Duck", func(t *testing.T) {
		duck := lookup(t, reg, "Turtle-Duck")
		e, _ := corpusEngineCfg(t, reg, []*cards.Card{duck}, nil)
		id := moveByName(t, e, 0, "Turtle-Duck", state.ZBattlefield)
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Turtle-Duck=%+v, want battlefield", o)
		}
		var animate *cards.SA
		for _, sa := range duck.Faces[0].Abilities {
			if sa.API == "Animate" {
				animate = sa
				break
			}
		}
		if animate == nil {
			t.Fatal("Turtle-Duck's real Animate ability was not loaded")
		}
		effects.Resolve(e, &effects.Ctx{Source: id, Controller: 0}, animate)
		assertDerivedPT(t, e, id, 4, 4)
	})

	t.Run("PuPu UFO with no Towns", func(t *testing.T) {
		ufo := lookup(t, reg, "PuPu UFO")
		e, _ := corpusEngineCfg(t, reg, []*cards.Card{ufo}, nil)
		id := moveByName(t, e, 0, "PuPu UFO", state.ZBattlefield)
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: PuPu UFO=%+v, want battlefield", o)
		}
		var animate *cards.SA
		for _, sa := range ufo.Faces[0].Abilities {
			if sa.API == "Animate" {
				animate = sa
				break
			}
		}
		if animate == nil {
			t.Fatal("PuPu UFO's real Animate ability was not loaded")
		}
		effects.Resolve(e, &effects.Ctx{Source: id, Controller: 0}, animate)
		assertDerivedPT(t, e, id, 0, 4)
	})

	t.Run("Belligerent Yearling", func(t *testing.T) {
		yearling := lookup(t, reg, "Belligerent Yearling")
		dinosaur := card(t, "Name:Test Entering Dinosaur\nTypes:Creature Dinosaur\nPT:0/3\nOracle:x\n")
		e, _ := corpusEngineCfg(t, reg, []*cards.Card{yearling, dinosaur}, nil)
		yid := moveByName(t, e, 0, "Belligerent Yearling", state.ZBattlefield)
		did := moveByName(t, e, 0, "Test Entering Dinosaur", state.ZBattlefield)
		if o := e.G.Obj(yid); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Yearling=%+v, want battlefield", o)
		}
		if o := e.G.Obj(did); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: entering Dinosaur=%+v, want battlefield", o)
		}
		animate := cards.ResolveSVar(yearling.Faces[0].SVars, "TrigAnimate")
		if animate == nil {
			t.Fatal("Belligerent Yearling's real TrigAnimate body was not loaded")
		}
		effects.Resolve(e, &effects.Ctx{Source: yid, Controller: 0,
			TriggerContext: effects.TriggerContext{TriggerCard: did}}, animate)
		assertDerivedPT(t, e, yid, 0, 2)
	})
}

func assertDerivedPT(t *testing.T, e *Engine, id state.ObjID, power, toughness int32) {
	t.Helper()
	e.checkStateBased()
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		var zone state.Zone
		if o != nil {
			zone = o.Zone
		}
		t.Fatalf("animated creature after state-based actions is in %s, want battlefield", zone)
	}
	got := e.Derived(id)
	if got.Power != power || got.Toughness != toughness {
		t.Fatalf("derived P/T = %d/%d, want %d/%d", got.Power, got.Toughness, power, toughness)
	}
}
