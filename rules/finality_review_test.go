package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestFinalityOnlyExilesCreatureDeaths(t *testing.T) {
	t.Parallel()
	creature := "Name:Finality Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	artifact := "Name:Finality Rock\nTypes:Artifact\nOracle:x\n"
	e, _, bear := newFixtureDeck(t, 91201, creature, artifact)
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "FINALITY", Amount: 1})
	if o := e.G.Obj(bear); o.Zone != state.ZBattlefield || o.Counter("FINALITY") != 1 {
		t.Fatalf("precondition: bear = %+v", o)
	}
	e.emit(events.Event{Kind: events.Damage, Obj: bear, Amount: 2})
	e.checkStateBased()
	if got := e.G.Obj(bear).Zone; got != state.ZExile {
		t.Fatalf("finality creature zone = %s, want exile", got)
	}

	var rock state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if e.G.Obj(id).Face().Name == "Finality Rock" {
			rock = id
			break
		}
	}
	if rock == 0 {
		t.Fatal("precondition: artifact not found")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: rock, From: state.ZLibrary, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.CounterChange, Obj: rock, Counter: "FINALITY", Amount: 1})
	e.emit(events.Event{Kind: events.MoveZone, Obj: rock, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.G.Obj(rock).Zone; got != state.ZGraveyard {
		t.Fatalf("finality noncreature zone = %s, want graveyard", got)
	}
}

func TestWinterCynicalOpportunistRejectsInsufficientTypes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	winter := mustCorpusCard(t, reg, "Winter, Cynical Opportunist")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	hill := mustCorpusCard(t, reg, "Hill Giant")
	island := mustCorpusCard(t, reg, "Island")
	deck := []*cards.Card{winter, bear, hill}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	cfg := seatZeroStart(Config{Seed: 91202, Names: []string{"winter", "opponent"}, Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	var wid state.ObjID
	var from state.Zone
	var grave []state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if e.G.Obj(id).Face().Name == winter.Faces[0].Name {
				wid, from = id, z
			}
		}
	}
	if wid == 0 {
		t.Fatal("precondition: Winter was not dealt")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: wid, From: from, To: state.ZBattlefield})
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range append([]state.ObjID{}, e.G.Zone(z, 0)...) {
			name := e.G.Obj(id).Face().Name
			if name == "Grizzly Bears" || name == "Hill Giant" {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZGraveyard})
				grave = append(grave, id)
			}
		}
	}
	if len(grave) != 2 {
		t.Fatalf("precondition: graveyard candidates = %d, want 2", len(grave))
	}
	if got := e.G.Obj(wid).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition: Winter zone = %s", got)
	}
	ability := cards.ResolveSVar(winter.Faces[0].SVars, "TrigExileDelirium")
	if ability == nil {
		t.Fatal("precondition: Winter TriggerExileDelirium did not compile")
	}
	if ability.Params["WithTotalCardTypes"] != "4" {
		t.Fatalf("precondition: compiled Winter param = %q, params=%v", ability.Params["WithTotalCardTypes"], ability.Params)
	}
	effects.Resolve(e, &effects.Ctx{Source: wid, Controller: 0, X: 2}, ability)
	if e.Pending() == nil || e.Pending().Kind != decision.KChoose {
		t.Fatalf("precondition: Winter did not ask for its hidden pick: %+v", e.Pending())
	}
	submitChoices(t, e, 0, 1)
	if o := e.G.Obj(grave[0]); o.Zone != state.ZGraveyard {
		t.Fatalf("insufficient pick moved first card to %s", o.Zone)
	}
	if o := e.G.Obj(grave[1]); o.Zone != state.ZGraveyard {
		t.Fatalf("insufficient pick moved second card to %s", o.Zone)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "hidden pick fails WithTotalCardTypes$ requirement" {
			return
		}
	}
	t.Fatalf("Winter resolution did not record the WithTotalCardTypes failure")
}
