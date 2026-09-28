package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 701.38b requires a goaded creature to attack a PLAYER, not a battle
// protected by a non-goader. The battle remains attackable without the goad.
func TestBattleDoesNotSatisfyGoadPlayerAttackDuty(t *testing.T) {
	t.Parallel()
	e, battle, attacker, protector := battleAttackBoard(t)
	b := e.G.Obj(battle)
	if b == nil || b.Zone != state.ZBattlefield || !b.ProtectorValid || b.Protector != protector {
		t.Fatalf("precondition: protected battlefield battle required, got %+v", b)
	}
	if a := e.G.Obj(attacker); a == nil || a.Zone != state.ZBattlefield || a.Controller != e.G.Active || !e.canAttack(attacker) {
		t.Fatalf("precondition: ready active-player attacker required, got %+v", a)
	}
	goader := state.PlayerID(2)
	if goader == protector || goader == e.G.Active || e.G.Players[goader].Lost {
		t.Fatal("precondition: goader must be a living opponent distinct from the protector")
	}
	battleOffered := false
	for _, of := range e.attackOffers() {
		if of.id == attacker && of.battle == battle {
			battleOffered = true
		}
	}
	if !battleOffered {
		t.Fatal("precondition: battle must be offered without goad")
	}

	e.emit(events.Event{Kind: events.Goad, Obj: attacker, Player: goader})
	if !e.attackRequirements(attacker).goad || !e.goadedBy(e.G.Obj(attacker), goader) {
		t.Fatal("precondition: creature must have an active goad")
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no attacker decision: %+v", d)
	}
	var playerOption, battleOption *decision.Option
	for i := range d.Options {
		o := &d.Options[i]
		if o.Obj != attacker {
			continue
		}
		if o.Battle == battle {
			battleOption = o
		}
		if o.Battle == 0 && o.Player == protector {
			playerOption = o
		}
	}
	if playerOption == nil {
		t.Fatalf("precondition: non-goader PLAYER attack must be offered, got %+v", d.Options)
	}
	if battleOption != nil {
		t.Fatalf("battle attack was offered as satisfying goad: %+v", battleOption)
	}
	if !playerOption.Required {
		t.Fatal("available player attack was not marked required by goad")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player}); err == nil {
		t.Fatal("goaded creature could skip an available player attack")
	}
	// The production bot reads the same filtered option set and must submit
	// a player attack the engine accepts, rather than retrying a battle attack.
	in := newTestBot(7).answer(e, d)
	if len(in.Choices) != 1 {
		t.Fatalf("bot omitted its required player attack: %+v", in)
	}
	chosen := d.Chosen(in)
	if len(chosen) != 1 || chosen[0].Battle != 0 || !chosen[0].Required {
		t.Fatalf("bot chose a battle rather than a required player attack: %+v", chosen)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("bot's player attack rejected: %v", err)
	}
}
