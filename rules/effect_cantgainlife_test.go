package rules

// effect_cantgainlife_test.go — the READER half of the Effect-delivered
// CR 614.1 CantGainLife static (task cantgainlife1): lifeGainForbidden must
// consult the REGISTERED CantGainLife restrictions the Effect route creates
// (rules/replacement_life.go), beside the printed battlefield statics it
// already read. Each test asserts its own precondition (the lock binds the
// right player before any life moves, and the two life totals actually
// differ) so a vacuous setup fails loudly.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestEffectCantGainLifeRegisteredRestrictionPreventsGain(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Lock\nManaCost:2 R\nTypes:Enchantment\nOracle:x\n")
	if e.lifeGainForbidden(0) || e.lifeGainForbidden(1) {
		t.Fatal("precondition failed: no restriction registered yet, lifeGainForbidden already true")
	}
	e.AddContinuous(ContinuousEffect{Source: src, Controller: 0,
		Restriction:    "CantGainLife",
		RestrictParams: map[string]string{"ValidPlayer": "Player"}})
	// Precondition: the registered lock is live and binds BOTH players
	// (ValidPlayer$ Player, Skullcrack / Call In a Professional's spelling).
	if !e.lifeGainForbidden(1) || !e.lifeGainForbidden(0) {
		t.Fatal("registered ValidPlayer$ Player lock not seen by lifeGainForbidden")
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 3})
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("opponent life = %d, want prevented gain at 20", got)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "prevented: cannot gain life") {
			found = true
		}
	}
	if !found {
		t.Fatal("prevented gain logged no 'prevented: cannot gain life' Note")
	}
}

func TestEffectCantGainLifeOpponentScopeLetsControllerGain(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Lock\nManaCost:R G\nTypes:Instant\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: src, Controller: 0,
		Restriction:    "CantGainLife",
		RestrictParams: map[string]string{"ValidPlayer": "Player.Opponent"}})
	// Precondition: Atarka's Command / Roiling Vortex's spelling scopes the
	// lock to the controller's opponents only.
	if !e.lifeGainForbidden(1) || e.lifeGainForbidden(0) {
		t.Fatal("ValidPlayer$ Player.Opponent did not bind exactly the controller's opponent")
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 3})
	if got := e.G.Players[0].Life; got != 23 {
		t.Fatalf("controller life = %d, want 23 (out of the lock's scope)", got)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 3})
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("opponent life = %d, want prevented gain at 20", got)
	}
}

// TestEffectCantGainLifeRememberedScope pins the IsRemembered resolution:
// ValidPlayer$ Player.IsRemembered (Screaming Nemesis / Stigma Lasher) binds
// exactly the player the Effect captured, and an EMPTY captured set binds
// nobody (fail closed -- the pre-fix behaviour must never become
// "nobody gains").
func TestEffectCantGainLifeRememberedScope(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Lock\nTypes:Creature\nPT:3/3\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: src, Controller: 0,
		Restriction:       "CantGainLife",
		RestrictParams:    map[string]string{"ValidPlayer": "Player.IsRemembered"},
		RememberedPlayers: []state.PlayerID{1}})
	if !e.lifeGainForbidden(1) || e.lifeGainForbidden(0) {
		t.Fatal("ValidPlayer$ Player.IsRemembered did not bind exactly the remembered player")
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 3})
	if got := e.G.Players[0].Life; got != 23 {
		t.Fatalf("controller life = %d, want 23 (not the remembered player)", got)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 3})
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("remembered player life = %d, want prevented gain at 20", got)
	}

	// Fail-closed direction: an Effect whose remember capture yielded
	// nothing (e.g. Stigma Lasher's RememberObjects$ TriggeredTarget, whose
	// player-remember case effects' effectRememberedPlayers does not read
	// yet) must lock out NOBODY, never blanket.
	e2 := layerEngine(t)
	src2 := onBoard(t, e2, 0, "Name:Lock2\nTypes:Creature\nPT:3/3\nOracle:x\n")
	e2.AddContinuous(ContinuousEffect{Source: src2, Controller: 0,
		Restriction:    "CantGainLife",
		RestrictParams: map[string]string{"ValidPlayer": "Player.IsRemembered"}})
	if e2.lifeGainForbidden(1) || e2.lifeGainForbidden(0) {
		t.Fatal("empty remembered set bound somebody: IsRemembered over-applied blanket")
	}
	e2.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 3})
	if got := e2.G.Players[1].Life; got != 23 {
		t.Fatalf("fail-closed empty remember still blocked the gain: life = %d, want 23", got)
	}
}
