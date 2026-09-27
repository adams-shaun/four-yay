package rules

// Adeline, Resplendent Cathar's real corpus AttackersDeclared trigger is the
// pin for the non-True TokenAttacking$ selector forms (task
// agent-20260920T064028Z-a7f115d7):
//
//	T:Mode$ AttackersDeclared | AttackingPlayer$ You | Execute$ DBRepeat
//	SVar:DBRepeat:DB$ RepeatEach | RepeatPlayers$ Opponent | ChangeZoneTable$ True | RepeatSubAbility$ DBToken
//	SVar:DBToken:DB$ Token | TokenScript$ w_1_1_human | TokenTapped$ True | TokenAttacking$ RememberedPlayer & Valid Planeswalker.ControlledBy Remembered | TokenOwner$ You
//
// The RepeatEach loop binds the current opponent as the resolution's
// Remembered PLAYER, so `TokenAttacking$ RememberedPlayer` must resolve to
// that seat and mint the Human tapped and attacking it. Before the fix the
// selector fell through the loud-Note degrade and every Human entered tapped,
// unmarked -- a sitting duck.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// adelineTokenAttacks drives Adeline's real trigger once: Adeline and a
// Grizzly Bears are under seat 0, seat 0's Bear attacks seat 1, and the stack
// (Adeline's Execute$ DBRepeat chain) drains. It returns the engine and the
// attacking Bear's id.
func adelineTokenAttacks(t *testing.T) (*Engine, Config) {
	t.Helper()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Adeline, Resplendent Cathar"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{})
	adeline := moveByName(t, e, 0, "Adeline, Resplendent Cathar", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	// Precondition: the trigger's source and its attacker are really where
	// the rule looks -- battlefield, controlled by seat 0. A vacuous setup
	// must fail here, not pass silently.
	if o := e.G.Obj(adeline); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Adeline = %+v, want battlefield under seat 0", o)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Bear = %+v, want battlefield under seat 0", o)
	}
	// Player in a DeclareAttackers event is the DEFENDING seat
	// (rules/trigger_referents.go binds DefendingPlayer = ev.Player and
	// AttackingPlayer = controllerOf(IDs[0])), so seat 1 is the sole opponent
	// and `AttackingPlayer$ You` sees seat 0's Bear.
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{bear}})
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatalf("Adeline's AttackersDeclared trigger did not reach the stack")
	}
	passUntilStackEmpty(t, e, 40)
	return e, cfg
}

// TestAdelineTokenAttackingRememberedPlayerAttacksThatOpponent is the pin:
// the RepeatEach loop's current opponent is the token's defender, the token
// enters tapped, and the selector's player half is what marked it.
func TestAdelineTokenAttackingRememberedPlayerAttacksThatOpponent(t *testing.T) {
	e, cfg := adelineTokenAttacks(t)

	var humans []*state.Object
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Human Token" {
			humans = append(humans, o)
		}
	}
	if len(humans) != 1 {
		t.Fatalf("Adeline minted %d Human tokens, want 1 (one opponent)", len(humans))
	}
	tok := humans[0]
	// Precondition for the attack assertion: the token exists on the
	// battlefield and is not the Bear.
	if tok.Zone != state.ZBattlefield || tok.Controller != 0 {
		t.Fatalf("precondition: Human zone=%s controller=%d, want battlefield under seat 0", tok.Zone, tok.Controller)
	}
	if !tok.Tapped || !tok.IsAttacking {
		t.Fatalf("Human tapped=%v attacking=%v, want tapped and attacking", tok.Tapped, tok.IsAttacking)
	}
	if tok.Attacking != 1 {
		t.Fatalf("Human attacks seat %d, want the RepeatEach opponent seat 1", tok.Attacking)
	}
	// The two-valued selector's planeswalker half is unmodelled and must say
	// so loudly -- exactly once -- rather than silently dropping the choice.
	// This also proves the selector handler ran: without the fix the call
	// emits the does-not-attack degrade instead of this note.
	var planeNotes, degradeNotes int
	for _, ev := range e.L.Events {
		if ev.Kind != events.Note {
			continue
		}
		if strings.Contains(ev.Text, "planeswalker arm is not implemented") {
			planeNotes++
		}
		if strings.Contains(ev.Text, "is not implemented; the token enters but does not attack") {
			degradeNotes++
		}
	}
	if planeNotes != 1 {
		t.Fatalf("%d planeswalker-arm notes, want exactly 1; log=%+v", planeNotes, e.L.Events)
	}
	if degradeNotes != 0 {
		t.Fatalf("%d selector degrade notes, want none (the player half resolved)", degradeNotes)
	}
	replayCheck(t, e, cfg)
}
