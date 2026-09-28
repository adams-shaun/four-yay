package rules

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// dealDamage builds the DB DealDamage ability the other Wither tests resolve.
func dealDamage(defined string, n int) *cards.SA {
	return &cards.SA{Kind: "DB", API: "DealDamage",
		Params: map[string]string{"Defined": defined, "NumDmg": dmgAmount(n)}}
}

func dmgAmount(n int) string { return fmt.Sprintf("%d", n) }

// tormentActive asserts the corpus card's S:Mode$ WitherDamage line is both
// printed on the face and visible to the engine's battlefield static walk —
// the precondition every assertion in this file reads through.
func tormentActive(t *testing.T, e *Engine, torment state.ObjID) {
	t.Helper()
	face := e.G.Obj(torment).Face()
	if face == nil {
		t.Fatal("precondition: Everlasting Torment has no face on the battlefield")
	}
	printed := false
	for _, s := range face.Statics {
		if s.Mode == "WitherDamage" {
			printed = true
		}
	}
	if !printed {
		t.Fatal("precondition: Everlasting Torment's WitherDamage static is not printed on the face")
	}
	if got := e.activeStatics("WitherDamage"); len(got) != 1 {
		t.Fatalf("precondition: active WitherDamage statics = %d, want 1", len(got))
	}
}

func TestEverlastingTormentWitherDamage(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	torment := onBoardCard(t, e, 0, witherCorpusCard(t, "e/everlasting_torment.txt"))
	source := onBoard(t, e, 0, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	target := onBoard(t, e, 1, "Name:Target\nTypes:Creature\nPT:4/4\nOracle:x\n")
	if e.HasKeyword(source, "Wither") || e.HasKeyword(source, "Infect") {
		t.Fatal("precondition: source already carries Wither/Infect, the static is not what is under test")
	}
	if e.G.Obj(torment).Zone != state.ZBattlefield || e.G.Obj(target).Zone != state.ZBattlefield || !e.IsCreature(target) {
		t.Fatal("precondition: torment or target is not live on the battlefield")
	}
	if e.G.Obj(target).Damage != 0 || e.G.Obj(target).Counter("M1M1") != 0 || e.G.Players[1].Life != 20 {
		t.Fatal("precondition: target or defender does not start clean")
	}
	tormentActive(t, e, torment)

	// Non-combat damage from an ordinary source to a creature is counters.
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0, Targets: []state.Target{{Obj: target}}},
		dealDamage("Targeted", 2))
	assertWitherCounter(t, e, target, 2)
	if got := witherCounterChanges(e, target, 2); got != 1 {
		t.Fatalf("static non-combat M1M1 CounterChange count = %d, want 1", got)
	}

	// Damage to a player stays ordinary (Wither has no player form).
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0},
		dealDamage("Opponent", 3))
	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("player life with WitherDamage static = %d, want 17", got)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Counter == "M1M1" && ev.Obj == 0 {
			t.Fatal("static player damage emitted creature counters")
		}
	}

	// The conversion stops the moment the static leaves the battlefield.
	e.emit(events.Event{Kind: events.MoveZone, Obj: torment, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.activeStatics("WitherDamage"); len(got) != 0 {
		t.Fatal("precondition: static removal left the WitherDamage static active")
	}
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0, Targets: []state.Target{{Obj: target}}},
		dealDamage("Targeted", 1))
	if got := e.G.Obj(target).Damage; got != 1 {
		t.Fatalf("ordinary damage after static removal = %d, want 1", got)
	}
	if got := e.G.Obj(target).Counter("M1M1"); got != 2 {
		t.Fatalf("target -1/-1 counters after removal = %d, want the pre-existing 2", got)
	}
}

func TestEverlastingTormentCombatDamage(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	torment := onBoardCard(t, e, 0, witherCorpusCard(t, "e/everlasting_torment.txt"))
	atk := onBoard(t, e, 0, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Blocker\nTypes:Creature\nPT:4/4\nOracle:x\n")
	e.G.Obj(atk).SummonSick = false
	if e.HasKeyword(atk, "Wither") || e.HasKeyword(atk, "Infect") {
		t.Fatal("precondition: attacker already carries Wither/Infect, the static is not what is under test")
	}
	if e.G.Obj(atk).Zone != state.ZBattlefield || e.G.Obj(blk).Zone != state.ZBattlefield || !e.IsCreature(blk) {
		t.Fatal("precondition: attacker or blocker is not live on the battlefield")
	}
	if e.G.Obj(blk).Damage != 0 || e.G.Obj(blk).Counter("M1M1") != 0 {
		t.Fatal("precondition: blocker does not start clean")
	}
	tormentActive(t, e, torment)

	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e, blk)
	assertWitherCounter(t, e, blk, 2)
	if got := witherCounterChanges(e, blk, 2); got != 1 {
		t.Fatalf("static combat M1M1 CounterChange count = %d, want 1", got)
	}

	// A static removal flips the same source back to ordinary damage.
	e.emit(events.Event{Kind: events.MoveZone, Obj: torment, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.activeStatics("WitherDamage"); len(got) != 0 {
		t.Fatal("precondition: static removal left the WitherDamage static active")
	}
	effects.Resolve(e, &effects.Ctx{Source: atk, Controller: 0, Targets: []state.Target{{Obj: blk}}},
		dealDamage("Targeted", 1))
	if got := e.G.Obj(blk).Damage; got != 1 {
		t.Fatalf("ordinary damage after static removal = %d, want 1", got)
	}
	if got := e.G.Obj(blk).Counter("M1M1"); got != 2 {
		t.Fatalf("blocker -1/-1 counters after removal = %d, want the pre-existing 2", got)
	}
}
