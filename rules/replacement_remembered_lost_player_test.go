package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A remembered player who has left the game cannot receive redirected damage.
func TestCR614RememberedDamageSkipsLostPlayer(t *testing.T) {
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
	// Give the remembered seat a distinct life total before registering the
	// replacement, so both recipients' post-damage totals are diagnostic.
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	effects.Resolve(e, &effects.Ctx{
		Source: glarecaster, Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}, TargetsOffered: true,
		SVars: card.Faces[0].SVars,
	}, ability)
	if obj := e.G.Obj(glarecaster); obj == nil || obj.Zone != state.ZBattlefield {
		t.Fatalf("Glarecaster = %+v, want battlefield precondition", obj)
	}
	e.emit(events.Event{Kind: events.PlayerLost, Player: 1, Text: "conceded"})
	if !e.G.Players[1].Lost || e.G.Players[0].Lost || e.G.Over {
		t.Fatalf("precondition: lost target must leave a running match: lost=%v controllerLost=%v over=%v", e.G.Players[1].Lost, e.G.Players[0].Lost, e.G.Over)
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
		t.Fatal("precondition: damage replacement did not survive the target's departure")
	}
	e.pending = nil
	originalLife, lostLife := e.G.Players[0].Life, e.G.Players[1].Life
	if originalLife == lostLife {
		t.Fatalf("precondition: original and lost player life both %d; test could pass without a redirect", originalLife)
	}
	applied := e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
	if applied.Kind != events.Damage || applied.Player != 0 || applied.Obj != 0 {
		t.Fatalf("applied = %+v, want damage to original recipient (lost referent skipped)", applied)
	}
	if got := e.G.Players[0].Life; got != originalLife-3 {
		t.Errorf("original recipient life = %d, want %d", got, originalLife-3)
	}
	if got := e.G.Players[1].Life; got != lostLife {
		t.Errorf("lost player's life = %d, want unchanged %d", got, lostLife)
	}
	logged := false
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		if ev := e.L.Events[i]; ev.Kind == events.Damage {
			logged = ev.Player == 0 && ev.Obj == 0 && ev.Amount == 3
			break
		}
	}
	if !logged {
		t.Fatal("log did not record damage to original recipient")
	}

	// The same walk must skip a lost first referent and select a later live one.
	e.replRemembered = []state.Target{{Player: 1, IsPlayer: true}, {Player: 2, IsPlayer: true}}
	if e.G.Players[2].Lost {
		t.Fatal("precondition: fallback player is lost")
	}
	ev := events.Event{Kind: events.Damage, Player: 0, Amount: 1}
	e.replacingEvent = &ev
	e.ReplaceEvent("Affected", "Remembered", 0)
	e.replacingEvent = nil
	if ev.Obj != 0 || ev.Player != 2 {
		t.Fatalf("fallback recipient = %+v, want live remembered player 2", ev)
	}
}
