package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

func ashiokPayLifeGame(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	ashiok := tokenReplCorpusCard(t, "Ashiok, Wicked Manipulator")
	e, _ := tokenReplGame(t, 0xA510, ashiok)
	var id state.ObjID
	for _, p := range e.G.AliveFrom(0) {
		for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
			for _, oid := range e.G.Zone(z, p) {
				o := e.G.Obj(oid)
				if o != nil && o.Face() != nil && o.Face().Name == "Ashiok, Wicked Manipulator" {
					id = oid
				}
			}
		}
	}
	if id == 0 {
		t.Fatal("precondition: real Ashiok card is not in the seeded game")
	}
	o := e.G.Obj(id)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZBattlefield})
	if o = e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Ashiok = %+v, want seat 0 battlefield", o)
	}
	return e, id
}

func libraryToExile(e *Engine, p state.PlayerID) []state.ObjID {
	var ids []state.ObjID
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZExile && ev.Player == p {
			ids = append(ids, ev.Obj)
		}
	}
	return ids
}

func TestAshiokPayLifeReplacement(t *testing.T) {
	t.Parallel()
	t.Run("enough library replaces payment", func(t *testing.T) {
		e, source := ashiokPayLifeGame(t)
		lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
		if len(lib) < 3 {
			t.Fatalf("precondition: library has %d cards, need at least 3", len(lib))
		}
		if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Ashiok source %d is not active", source)
		}
		lifeBefore := e.G.Players[0].Life
		if lifeBefore < 3 {
			t.Fatalf("precondition: life %d cannot pay 3", lifeBefore)
		}
		if !pay.PayMana(asPayer(e), 0, cost.ParseCost("PayLife<3>")) {
			t.Fatal("PayLife<3> cost was not payable")
		}
		if got := e.G.Players[0].Life; got != lifeBefore {
			t.Fatalf("life = %d, want unchanged %d", got, lifeBefore)
		}
		if got := libraryToExile(e, 0); !slices.Equal(got, lib[:3]) {
			t.Fatalf("exiled library cards = %v, want exactly top three %v; events=%+v", got, lib[:3], e.L.Events)
		}
	})
	t.Run("ValidPlayer You excludes the opponent", func(t *testing.T) {
		e, source := ashiokPayLifeGame(t)
		lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 1)...)
		if len(lib) < 3 {
			t.Fatalf("precondition: opponent library has %d cards, need at least 3", len(lib))
		}
		if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
			t.Fatalf("precondition: Ashiok source = %+v, want active under seat 0", o)
		}
		lifeBefore := e.G.Players[1].Life
		if !pay.PayMana(asPayer(e), 1, cost.ParseCost("PayLife<3>")) {
			t.Fatal("opponent PayLife<3> cost was not payable")
		}
		if got := e.G.Players[1].Life; got != lifeBefore-3 {
			t.Fatalf("opponent life = %d, want %d", got, lifeBefore-3)
		}
		if got := libraryToExile(e, 1); len(got) != 0 {
			t.Fatalf("Ashiok replaced opponent's payment and exiled %v", got)
		}
	})
	t.Run("short library falls back to payment", func(t *testing.T) {
		e, _ := ashiokPayLifeGame(t)
		lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
		if len(lib) < 3 {
			t.Fatalf("precondition: expected seeded library >= 3, got %d", len(lib))
		}
		for _, id := range lib[2:] {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
		}
		short := e.G.Zone(state.ZLibrary, 0)
		if len(short) != 2 || len(short) >= 3 {
			t.Fatalf("precondition: library has %d cards, need exactly 2", len(short))
		}
		lifeBefore := e.G.Players[0].Life
		if !pay.PayMana(asPayer(e), 0, cost.ParseCost("PayLife<3>")) {
			t.Fatal("PayLife<3> cost was not payable")
		}
		if got := e.G.Players[0].Life; got != lifeBefore-3 {
			t.Fatalf("life = %d, want %d after fallback payment", got, lifeBefore-3)
		}
		if got := libraryToExile(e, 0); len(got) != 0 {
			t.Fatalf("exiled %v despite a library shorter than the payment", got)
		}
	})
	t.Run("ordinary life loss is not a payment", func(t *testing.T) {
		e, source := ashiokPayLifeGame(t)
		lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
		if len(lib) < 3 {
			t.Fatalf("precondition: library has %d cards, need at least 3", len(lib))
		}
		if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Ashiok source %d is not active", source)
		}
		lifeBefore := e.G.Players[0].Life
		e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -3})
		if got := e.G.Players[0].Life; got != lifeBefore-3 {
			t.Fatalf("life = %d, want %d after ordinary life loss", got, lifeBefore-3)
		}
		if got := libraryToExile(e, 0); len(got) != 0 {
			t.Fatalf("ordinary life loss exiled library cards: %v", got)
		}
	})
}

func TestPayLifeReplacementCarrierCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	if len(reg.Cards) == 0 {
		t.Fatal("precondition: corpus registry is empty")
	}
	got := map[string]bool{}
	for _, c := range reg.Cards {
		if c == nil {
			continue
		}
		for _, f := range c.Faces {
			for _, r := range f.Repls {
				if r.EventKind() == cards.ReplPayLife {
					got[f.Name] = true
				}
			}
		}
	}
	var names []string
	for name := range got {
		names = append(names, name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"Ashiok, Wicked Manipulator"}) {
		t.Fatalf("PayLife replacement carriers = %q, want exactly Ashiok, Wicked Manipulator", names)
	}
}

func TestPayLifeReplacementPrimitiveIsRegistered(t *testing.T) {
	if !effects.Supported()["repl:PayLife"] {
		t.Fatal(`effects.Supported() is missing "repl:PayLife"`)
	}
}
