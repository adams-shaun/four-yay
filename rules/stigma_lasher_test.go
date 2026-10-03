package rules

// stigma_lasher_test.go — the RememberObjects$ TriggeredTarget capture, end to
// end, on the real Stigma Lasher corpus card.
//
// Stigma Lasher's DamageDone trigger creates a DB$ Effect with
// `RememberObjects$ TriggeredTarget`, whose CantGainLife static's
// ValidPlayer$ Player.IsRemembered clause the registered-restriction reader
// (rules/replacement_life.go's lifeGainForbidden, CR 614.1 substitution
// prohibition) consults through restrictionPlayerSpecMatches. Before the
// effectRememberedPlayers TriggeredTarget case the capture yielded an EMPTY
// player set, so the clause bound nobody (fail closed, never blanket) and the
// damaged player still gained life.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// stigmaLasherBoard deals seat 0 a real Stigma Lasher and parks the clock at
// seat 0's first main phase. Every placement is a logged MoveZone, so the
// board replays.
func stigmaLasherBoard(t *testing.T) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Stigma Lasher"}, nil, nil, nil)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	return e, cfg
}

func TestStigmaLasherLocksDamagedPlayerFromGainingLife(t *testing.T) {
	t.Parallel()
	e, _ := stigmaLasherBoard(t)
	lasher := findBattlefield(t, e, 0, "Stigma Lasher", 0)
	if o := e.G.Obj(lasher); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Stigma Lasher not on the battlefield: %+v", o)
	}
	if e.lifeGainForbidden(1) || e.lifeGainForbidden(0) {
		t.Fatal("precondition failed: no damage dealt yet, a lock is already live")
	}

	// Stigma Lasher deals damage to a PLAYER: the Damage event carries Obj==0
	// and Player=1, so its trigger's TriggeredTarget binds seat 1 (CR 121.6:
	// the remembered player rides the trigger into the Effect's resolution).
	// The damage source is published (SetDamageSource), the noncombat route
	// trigmatch.DamageMatches' ValidSource$ reads through trigmatch.DamageEventSource.
	prev := e.SetDamageSource(lasher)
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.SetDamageSource(prev)
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("pendingTriggers after damage = %d, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatal("precondition: the DamageDone trigger did not reach the stack")
	}
	e.resolveTop()
	if len(e.G.Stack) != 0 {
		t.Fatalf("precondition: the trigger did not resolve, stack = %v", e.G.Stack)
	}

	// Precondition: the lock binds exactly the damaged player, before any
	// life moves; and the two life totals are the values the test compares.
	if !e.lifeGainForbidden(1) {
		t.Fatal("damaged player is not locked out: TriggeredTarget captured nobody")
	}
	if e.lifeGainForbidden(0) {
		t.Fatal("the damage source's controller is locked out: the capture over-applied")
	}
	if e.G.Players[0].Life == e.G.Players[1].Life {
		t.Fatalf("precondition: both life totals %d, the comparison proves nothing", e.G.Players[0].Life)
	}
	lockedLife := e.G.Players[1].Life // the post-damage total the prevented gain must not move

	// Negative direction first: the non-damaged controller still gains.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 3})
	if got := e.G.Players[0].Life; got != 23 {
		t.Fatalf("controller life = %d, want 23 (not the damaged player)", got)
	}
	// The damaged player's gain is prohibited. Stigma Lasher's 2 damage left
	// seat 1 at 18; the +3 must be prevented, not applied.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 3})
	if got := e.G.Players[1].Life; got != lockedLife {
		t.Fatalf("damaged player life = %d, want prevented gain at %d", got, lockedLife)
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

// TestStigmaLasherObjectRecipientCapturesNobody pins the object direction: a
// DamageDone to a CREATURE leaves TriggerTarget an object, so the Effect
// captures no player and nobody is locked out -- never an unrelated chosen
// player target.
func TestStigmaLasherObjectRecipientCapturesNobody(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := combatTriggerBoard(t, reg, []string{"Stigma Lasher"}, nil, nil,
		[]string{"Name:Probe Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	lasher := findBattlefield(t, e, 0, "Stigma Lasher", 0)
	bear := findBattlefield(t, e, 1, "Probe Bear", 0)
	if o := e.G.Obj(lasher); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Stigma Lasher not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Probe Bear not on the battlefield: %+v", o)
	}

	// Deal damage to a creature controlled by seat 1. Stigma Lasher's
	// ValidTarget$ Player gate does not fire on a creature recipient, so no
	// Effect is registered at all; and even if a TriggeredTarget capture ran
	// it must contribute no player.
	prev := e.SetDamageSource(lasher)
	e.emit(events.Event{Kind: events.Damage, Obj: bear, Amount: 2})
	e.SetDamageSource(prev)
	if e.lifeGainForbidden(0) || e.lifeGainForbidden(1) {
		t.Fatal("object-recipient damage registered a player lock: TriggeredTarget over-applied")
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 3})
	if got := e.G.Players[1].Life; got != 23 {
		t.Fatalf("seat 1 life = %d, want 23 (no player was damaged)", got)
	}
}
