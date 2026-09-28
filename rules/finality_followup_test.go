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

func TestFinalityDestroyRespectsDerivedNonCreatureType(t *testing.T) {
	t.Parallel()
	e, cfg, bear := newFixtureDeck(t, 91203, "Name:Finality Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "FINALITY", Amount: 1})
	e.AddContinuous(state.ContinuousEffect{Source: bear, Controller: 0, Affects: "Card.Self", Layer: state.LType,
		RemoveCardTypes: true})
	for _, typ := range e.Derived(bear).Types {
		if typ == "Creature" {
			t.Fatalf("precondition: derived types still include Creature: %v", e.Derived(bear).Types)
		}
	}
	if o := e.G.Obj(bear); o.Zone != state.ZBattlefield || o.Counter("FINALITY") != 1 {
		t.Fatalf("precondition: finality bear = %+v", o)
	}

	effects.Resolve(e, &effects.Ctx{Source: bear, Controller: 0, Targets: []state.Target{{Obj: bear}}},
		&cards.SA{Kind: "DB", API: "Destroy", Params: map[string]string{}})
	if got := e.G.Obj(bear).Zone; got != state.ZGraveyard {
		t.Fatalf("derived noncreature with finality went to %s, want graveyard", got)
	}
	replayCheck(t, e, cfg)
}

func TestWinterCynicalOpportunistReturnsFinalityPermanentAndReplays(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	winter := mustCorpusCard(t, reg, "Winter, Cynical Opportunist")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	artifact := mustCorpusCard(t, reg, "Ornithopter")
	instant := mustCorpusCard(t, reg, "Shock")
	island := mustCorpusCard(t, reg, "Island")
	deck := []*cards.Card{winter, bear, artifact, instant, island}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	cfg := seatZeroStart(Config{Seed: 91204, Names: []string{"winter", "opponent"}, Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()

	findAndMove := func(name string, to state.Zone) state.ObjID {
		t.Helper()
		for _, from := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range append([]state.ObjID(nil), e.G.Zone(from, 0)...) {
				if e.G.Obj(id).Face().Name == name {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: to})
					return id
				}
			}
		}
		t.Fatalf("precondition: %s not found in Winter deck", name)
		return 0
	}
	wid := findAndMove(winter.Faces[0].Name, state.ZBattlefield)
	grave := map[string]state.ObjID{
		"Grizzly Bears": findAndMove("Grizzly Bears", state.ZGraveyard),
		"Ornithopter":   findAndMove("Ornithopter", state.ZGraveyard),
		"Shock":         findAndMove("Shock", state.ZGraveyard),
		"Island":        findAndMove("Island", state.ZGraveyard),
	}
	if o := e.G.Obj(wid); o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Winter zone = %s, want battlefield", o.Zone)
	}
	ability := cards.ResolveSVar(winter.Faces[0].SVars, "TrigExileDelirium")
	if ability == nil || ability.Params["WithTotalCardTypes"] != "4" {
		t.Fatalf("precondition: compiled Winter delirium ability = %+v", ability)
	}
	graveIDs := []state.ObjID{grave["Grizzly Bears"], grave["Ornithopter"], grave["Shock"], grave["Island"]}
	remembered := make([]state.Target, 0, len(graveIDs))
	for _, id := range graveIDs {
		remembered = append(remembered, state.Target{Obj: id})
	}
	if !effects.MatchesSpecCtx(e.G, "PermanentCard.YouOwn+IsRemembered", grave["Grizzly Bears"], effects.SpecContext{You: 0, Source: wid, Remembered: remembered}) {
		t.Fatal("precondition: remembered permanent does not match normalized Winter DBReturn filter")
	}

	effects.Resolve(e, &effects.Ctx{Source: wid, Controller: 0, X: 4}, ability)
	d := e.Pending()
	if d == nil || len(d.Options) != 4 {
		t.Fatalf("precondition: Winter hidden pick = %+v, want four options", d)
	}
	candidates := map[state.ObjID]bool{}
	for _, id := range graveIDs {
		candidates[id] = true
	}
	firstPick := make([]int, 0, 4)
	for _, opt := range d.Options {
		if candidates[opt.Obj] {
			firstPick = append(firstPick, opt.Index)
		}
	}
	if len(firstPick) != 4 {
		t.Fatalf("precondition: Winter options do not expose all four card types: %+v", d.Options)
	}
	submitChoices(t, e, firstPick...)
	if got := e.G.Obj(wid).Remembered; len(got) != 4 {
		t.Fatalf("Winter did not retain four remembered exiles: %v", got)
	}

	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 {
		t.Fatalf("Winter did not ask which remembered permanent to return: %+v; log=%+v", d, e.L.Events)
	}
	returnChoice := -1
	for _, opt := range d.Options {
		if opt.Obj == grave["Grizzly Bears"] {
			returnChoice = opt.Index
			break
		}
	}
	if returnChoice < 0 {
		t.Fatalf("Winter return options omit remembered Grizzly Bears: %+v", d.Options)
	}
	submitChoices(t, e, returnChoice)
	if o := e.G.Obj(grave["Grizzly Bears"]); o.Zone != state.ZBattlefield || o.Counter("FINALITY") != 1 {
		t.Fatalf("Winter returned permanent = %+v, want battlefield with one FINALITY counter", o)
	}
	if got := e.G.Obj(wid).Remembered; len(got) != 0 {
		t.Fatalf("Winter cleanup retained remembered cards: %v", got)
	}
	replayCheck(t, e, cfg)
}
