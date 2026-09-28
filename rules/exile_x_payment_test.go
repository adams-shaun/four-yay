package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// graveCostFodder moves the requested distinct cards from seat zero's zones
// into the graveyard through the log. The caller supplies the exact card IDs.
func graveCostFodder(t *testing.T, e *Engine, ids ...state.ObjID) {
	t.Helper()
	for _, id := range ids {
		o := e.G.Obj(id)
		if o == nil || o.Owner != 0 || o.Zone == state.ZGraveyard {
			t.Fatalf("invalid graveyard setup for %d: %+v", id, o)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZGraveyard})
		if e.G.Obj(id).Zone != state.ZGraveyard {
			t.Fatalf("fodder %d not in graveyard", id)
		}
	}
}

func exileCostChoices(t *testing.T, e *Engine, want []state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != len(want) || d.Max != len(want) {
		t.Fatalf("exile payment ask = %+v, want %d picks", d, len(want))
	}
	var picks []int
	for _, id := range want {
		found := false
		for _, o := range d.Options {
			if o.Obj == id && o.Kind == "exilecost" {
				picks = append(picks, o.Index)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("fodder %d not offered: %+v", id, d.Options)
		}
	}
	submitChoices(t, e, picks...)
}

func assertExilePayments(t *testing.T, e *Engine, ids ...state.ObjID) {
	t.Helper()
	for _, id := range ids {
		if o := e.G.Obj(id); o.Zone != state.ZExile {
			t.Fatalf("cost fodder %d zone %s, want exile", id, o.Zone)
		}
		n := 0
		for _, ev := range e.L.Events {
			if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZGraveyard && ev.To == state.ZExile && ev.Text == "exiled as a cost" {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("cost fodder %d had %d logged cost exiles, want one", id, n)
		}
	}
}

func TestNecropolisFiendAnnouncedExilePayment(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	fiend := mustCorpusCard(t, reg, "Necropolis Fiend")
	bear := card(t, "Name:ExileTarget\nTypes:Creature Bear\nPT:4/4\nOracle:x\n")
	e, cfg, _ := corpusDeckEngine(t, nil, []*cards.Card{fiend, bear})
	var source, target state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		switch e.G.Obj(id).Face().Name {
		case "Necropolis Fiend":
			source = id
		case "ExileTarget":
			target = id
		}
	}
	if source == 0 || target == 0 || e.Power(target) != 4 || e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatal("precondition: fiend and 4/4 target must be on the battlefield")
	}
	// A full turn under our control makes the printed {T} payable.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) < 2 {
		t.Fatal("precondition: need two library cards")
	}
	fodder := append([]state.ObjID(nil), lib[:2]...)
	graveCostFodder(t, e, fodder...)
	addMana(t, e, 0, "RRRRR") // mana bound exceeds the two-card graveyard bound
	e.priorityRound()
	submitChoices(t, e, abilityOption(t, e, source, 0).Index)
	if m := maxXOf(xAskOptions(t, e)); m != 2 {
		t.Fatalf("Fiend X max = %d, want 2", m)
	}
	chooseX(t, e, 2)
	exileCostChoices(t, e, fodder)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target decision = %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == target {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("4/4 not offered as target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	assertExilePayments(t, e, fodder...)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(target).Zone != state.ZBattlefield || e.Power(target) != 2 || e.Toughness(target) != 2 {
		t.Fatalf("Fiend's target zone/p/t = %s/%d/%d, want battlefield/2/2", e.G.Obj(target).Zone, e.Power(target), e.Toughness(target))
	}
	replayCheck(t, e, cfg)
}

func TestChillHauntingExileOnlyAnnouncesX(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	chill := mustCorpusCard(t, reg, "Chill Haunting")
	creature := mustCorpusCard(t, reg, "Grizzly Bears")
	targetCard := card(t, "Name:HauntingTarget\nTypes:Creature Bear\nPT:4/4\nOracle:x\n")
	e, cfg, spell := corpusDeckEngine(t, []*cards.Card{chill}, []*cards.Card{creature, creature, targetCard})
	if e.G.Obj(spell).Zone != state.ZHand {
		t.Fatal("precondition: Chill Haunting not in hand")
	}
	var target state.ObjID
	var fodder []state.ObjID
	for _, o := range e.G.Objs {
		if o.Owner != 0 {
			continue
		}
		if o.Card == creature {
			fodder = append(fodder, o.ID)
		}
		if o.Card == targetCard {
			target = o.ID
		}
	}
	if len(fodder) != 2 || target == 0 || e.G.Obj(target).Zone != state.ZBattlefield || e.Power(target) != 4 {
		t.Fatal("precondition: two creatures and a 4/4 target required")
	}
	graveCostFodder(t, e, fodder...)
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) == 0 {
		t.Fatal("precondition: need noncreature fodder")
	}
	nonmatch := lib[0]
	if e.G.Obj(nonmatch).Face().Name != "Mountain" {
		t.Fatalf("nonmatching card = %s, want Mountain", e.G.Obj(nonmatch).Face().Name)
	}
	graveCostFodder(t, e, nonmatch)
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 3 {
		t.Fatalf("precondition: graveyard size = %d, want 3", got)
	}
	addMana(t, e, 0, "BBBBBB")
	e.priorityRound()
	submitChoices(t, e, castOption(t, e, spell))
	if m := maxXOf(xAskOptions(t, e)); m != 2 {
		t.Fatalf("exile-only X max = %d, want 2 matching creatures, not mana or total graveyard", m)
	}
	chooseX(t, e, 2)
	exileCostChoices(t, e, fodder)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target ask = %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == target {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("4/4 not offered as target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	assertExilePayments(t, e, fodder...)
	if e.G.Obj(nonmatch).Zone != state.ZGraveyard {
		t.Fatal("nonmatching Mountain was exiled")
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(target).Zone != state.ZBattlefield || e.Power(target) != 2 || e.Toughness(target) != 2 {
		t.Fatalf("Chill Haunting target zone/p/t = %s/%d/%d, want battlefield/2/2", e.G.Obj(target).Zone, e.Power(target), e.Toughness(target))
	}
	replayCheck(t, e, cfg)
}
