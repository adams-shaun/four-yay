package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestCR614RememberedDamageRedirectToPlayer(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 3)
	glarecaster := onBoard(t, e, 0, "Name:Glarecaster\nManaCost:4 W W\nTypes:Creature Bird Cleric\nPT:3/3\nOracle:x\n")
	card := mustCorpusCard(t, reg, "Glarecaster")
	var ability *cards.SA
	for _, sa := range card.Faces[0].Abilities {
		if sa.API == "Effect" && sa.Params["ReplacementEffects"] == "SelflessDamage" {
			ability = sa
			break
		}
	}
	if ability == nil {
		t.Fatal("Glarecaster's DamageDone Effect ability was not found")
	}
	effects.Resolve(e, &effects.Ctx{
		Source: glarecaster, Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}, TargetsOffered: true,
		SVars: card.Faces[0].SVars,
	}, ability)
	if obj := e.G.Obj(glarecaster); obj == nil || obj.Zone != state.ZBattlefield {
		t.Fatalf("Glarecaster = %+v, want battlefield precondition", obj)
	}
	registered := false
	for _, ce := range e.active() {
		if ce.Source == glarecaster && ce.ReplacementEvent == "DamageDone" {
			registered = true
			if len(ce.RememberedPlayers) != 1 || ce.RememberedPlayers[0] != 1 {
				t.Fatalf("registered remembered players = %v, want [1]", ce.RememberedPlayers)
			}
		}
	}
	if !registered {
		t.Fatal("Glarecaster's DamageDone replacement did not register")
	}
	e.pending = nil

	// CR 614.6 redirects the damage recipient; the logged Damage event is the
	// authoritative observable, in addition to the resulting life totals.
	redirected := e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
	if redirected.Kind != events.Damage || redirected.Player != 1 || redirected.Obj != 0 {
		t.Fatalf("redirected event = %+v, want damage to remembered player 1", redirected)
	}
	logged := false
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		if e.L.Events[i].Kind == events.Damage {
			logged = e.L.Events[i].Player == 1 && e.L.Events[i].Obj == 0 && e.L.Events[i].Amount == 3
			break
		}
	}
	if !logged {
		t.Fatal("event log does not record the redirected damage to player 1")
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Errorf("original recipient life = %d, want 20", got)
	}
	if got := e.G.Players[1].Life; got != 17 {
		t.Errorf("remembered player life = %d, want 17", got)
	}

	untouched := e.emit(events.Event{Kind: events.Damage, Player: 2, Amount: 2})
	if untouched.Kind != events.Damage || untouched.Player != 2 || untouched.Obj != 0 {
		t.Fatalf("negative-control event = %+v, want damage to player 2 untouched", untouched)
	}
	if got := e.G.Players[2].Life; got != 18 {
		t.Errorf("negative-control player life = %d, want 18", got)
	}
	if got := e.G.Players[1].Life; got != 17 {
		t.Errorf("remembered player life after negative control = %d, want unchanged 17", got)
	}
}
