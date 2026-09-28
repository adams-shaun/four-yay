package rules

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func witherCorpusCard(t *testing.T, path string) *cards.Card {
	t.Helper()
	c, ds := cards.Parse(filepath.Join("..", ".cards", "cardsfolder", path))
	if len(ds) != 0 {
		t.Fatalf("parse %s: %v", path, ds)
	}
	if ds = c.Link(); len(ds) != 0 {
		t.Fatalf("link %s: %v", path, ds)
	}
	return c
}

func assertWitherCounter(t *testing.T, e *Engine, target state.ObjID, amount int32) {
	t.Helper()
	if got := e.G.Obj(target).Counter("M1M1"); got != amount {
		t.Fatalf("target -1/-1 counters = %d, want %d", got, amount)
	}
	if got := e.G.Obj(target).Damage; got != 0 {
		t.Fatalf("target marked damage = %d, want 0", got)
	}
}

func witherCounterChanges(e *Engine, target state.ObjID, amount int32) int {
	count := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == target && ev.Counter == "M1M1" && ev.Amount == amount {
			count++
		}
	}
	return count
}

func TestWitherVillagePillagersETBDamageAllDealsCounters(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	target := onBoard(t, e, 1, "Name:Target\nTypes:Creature\nPT:4/4\nOracle:x\n")
	source := onBoardCard(t, e, 0, witherCorpusCard(t, "v/village_pillagers.txt"))
	face := e.G.Obj(source).Face()
	if face == nil || len(face.Triggers) < 1 || face.Triggers[0].Mode != "ChangesZone" || face.Triggers[0].Effect == nil || face.Triggers[0].Effect.API != "DamageAll" {
		t.Fatalf("precondition: Village Pillagers ETB DamageAll trigger = %+v", face)
	}
	if !e.HasKeyword(source, "Wither") || e.G.Obj(target).Zone != state.ZBattlefield || !e.IsCreature(target) {
		t.Fatal("precondition: real Village Pillagers source/creature target not live")
	}
	if e.G.Obj(target).Damage != 0 || e.G.Obj(target).Counter("M1M1") != 0 {
		t.Fatal("precondition: target is already damaged or countered")
	}
	// onBoardCard is eventless setup; make the real card enter so its compiled
	// ChangesZone trigger, rather than a synthetic DamageAll body, resolves.
	e.G.SetZone(state.ZBattlefield, 0, nil)
	e.G.Obj(source).Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), source))
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZHand, To: state.ZBattlefield})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("Village Pillagers ETB pending triggers = %d, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("Village Pillagers ETB stack = %d, want 1", len(e.G.Stack))
	}
	e.resolveTop()
	assertWitherCounter(t, e, target, 1)
	if got := witherCounterChanges(e, target, 1); got != 1 {
		t.Fatalf("Village Pillagers M1M1 CounterChange count = %d, want 1", got)
	}
}

func TestWitherGrantedKeywordStartsAndStopsWithStatic(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	grantor := onBoardCard(t, e, 0, witherCorpusCard(t, "m/massacre_girl_known_killer.txt"))
	source := onBoard(t, e, 0, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	target := onBoard(t, e, 1, "Name:Target\nTypes:Creature\nPT:4/4\nOracle:x\n")
	if e.G.Obj(source).Face() == nil || e.G.Obj(source).Face().HasKeyword("Wither") || !e.HasKeyword(grantor, "Menace") {
		t.Fatal("precondition: source prints Wither or grantor is not live")
	}
	if !e.HasKeyword(source, "Wither") {
		t.Fatal("precondition: static grant did not make source Wither")
	}
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0, Targets: []state.Target{{Obj: target}}},
		&cards.SA{Kind: "DB", API: "DealDamage", Params: map[string]string{"Defined": "Targeted", "NumDmg": "1"}})
	assertWitherCounter(t, e, target, 1)
	e.emit(events.Event{Kind: events.MoveZone, Obj: grantor, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.HasKeyword(source, "Wither") {
		t.Fatal("grantor removal left derived Wither live")
	}
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0, Targets: []state.Target{{Obj: target}}},
		&cards.SA{Kind: "DB", API: "DealDamage", Params: map[string]string{"Defined": "Targeted", "NumDmg": "1"}})
	if e.G.Obj(target).Damage != 1 {
		t.Fatalf("ordinary damage after grant removal = %d, want 1", e.G.Obj(target).Damage)
	}
}

func TestWitherCombatDamageToBlockerUsesCounterForm(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardCard(t, e, 0, witherCorpusCard(t, "n/necroskitter.txt"))
	e.G.Obj(atk).SummonSick = false
	blk := onBoard(t, e, 1, "Name:Blocker\nTypes:Creature\nPT:4/4\nOracle:x\n")
	if !e.HasKeyword(atk, "Wither") || e.G.Obj(blk).Zone != state.ZBattlefield || !e.IsCreature(blk) {
		t.Fatal("precondition: printed-Wither attacker or blocker not live")
	}
	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e, blk)
	assertWitherCounter(t, e, blk, 1)
}

func TestWitherDamageToPlayerRemainsOrdinary(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	source := onBoardCard(t, e, 0, witherCorpusCard(t, "v/village_pillagers.txt"))
	before := e.G.Players[1].Life
	if !e.HasKeyword(source, "Wither") || before != 20 {
		t.Fatal("precondition: printed Wither source or player life is wrong")
	}
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0},
		&cards.SA{Kind: "DB", API: "DealDamage", Params: map[string]string{"Defined": "Opponent", "NumDmg": "2"}})
	if got := e.G.Players[1].Life; got != before-2 {
		t.Fatalf("player life = %d, want %d", got, before-2)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Counter == "M1M1" {
			t.Fatal("Wither player damage emitted creature counters")
		}
	}
}

func TestWitherNoncombatPlayerHitRedirectsToCreatureCounters(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	source := onBoardCard(t, e, 0, witherCorpusCard(t, "v/village_pillagers.txt"))
	giant := onBoardCard(t, e, 1, witherCorpusCard(t, "p/palisade_giant.txt"))
	if !e.HasKeyword(source, "Wither") || !e.IsCreature(giant) || e.G.Obj(giant).Zone != state.ZBattlefield {
		t.Fatal("precondition: Wither source or Palisade Giant redirect target is not live")
	}
	if e.G.Players[1].Life != 20 || e.G.Obj(giant).Damage != 0 || e.G.Obj(giant).Counter("M1M1") != 0 {
		t.Fatal("precondition: redirected recipient does not start clean")
	}
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0},
		&cards.SA{Kind: "DB", API: "DealDamage", Params: map[string]string{"Defined": "Opponent", "NumDmg": "2"}})
	assertWitherCounter(t, e, giant, 2)
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("redirected player life = %d, want 20", got)
	}
	if got := witherCounterChanges(e, giant, 2); got != 1 {
		t.Fatalf("redirected M1M1 CounterChange count = %d, want 1", got)
	}
}

func TestWitherCombatPlayerHitRedirectsToCreatureCounters(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	e.G.Active = 1
	giant := onBoardCard(t, e, 0, witherCorpusCard(t, "p/palisade_giant.txt"))
	atk := onBoardCard(t, e, 1, witherCorpusCard(t, "v/village_pillagers.txt"))
	e.G.Obj(atk).SummonSick = false
	if !e.HasKeyword(atk, "Wither") || !e.IsCreature(giant) || e.G.Obj(giant).Zone != state.ZBattlefield {
		t.Fatal("precondition: combat Wither source or Palisade Giant is not live")
	}
	if e.G.Players[0].Life != 20 || e.G.Obj(giant).Damage != 0 || e.G.Obj(giant).Counter("M1M1") != 0 {
		t.Fatal("precondition: combat redirect recipient does not start clean")
	}
	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e)
	assertWitherCounter(t, e, giant, 5)
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("redirected player life = %d, want 20", got)
	}
	if got := witherCounterChanges(e, giant, 5); got != 1 {
		t.Fatalf("combat redirected M1M1 CounterChange count = %d, want 1", got)
	}
}

func TestWitherPreventedCombatHitPlacesNoCounter(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	source := onBoardCard(t, e, 0, witherCorpusCard(t, "v/village_pillagers.txt"))
	e.G.Obj(source).SummonSick = false
	target := onBoard(t, e, 1, "Name:Red Ward\nTypes:Creature\nPT:1/6\nK:Protection from Red\nOracle:x\n")
	if !e.HasKeyword(source, "Wither") || !e.HasKeyword(target, "Protection from Red") || e.G.Obj(target).Zone != state.ZBattlefield {
		t.Fatal("precondition: Wither source or protection target is not live")
	}
	e.askAttackers()
	submitAttackers(t, e, source)
	submitBlockers(t, e, target)
	if got := e.G.Obj(target).Damage; got != 0 {
		t.Fatalf("protected target marked damage = %d, want 0", got)
	}
	if got := e.G.Obj(target).Counter("M1M1"); got != 0 {
		t.Fatalf("protected target -1/-1 counters = %d, want 0", got)
	}
	prevented := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Obj == target && strings.Contains(ev.Text, "prevented: protection") {
			prevented = true
		}
		if ev.Kind == events.CounterChange && ev.Obj == target && ev.Counter == "M1M1" {
			t.Fatal("prevented Wither hit emitted an M1M1 CounterChange")
		}
	}
	if !prevented {
		t.Fatal("protection path never recorded its prevented-damage Note")
	}
}

func TestWitherCounterChangeTriggersWickersmithsTools(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	tools := onBoardCard(t, e, 0, witherCorpusCard(t, "w/wickersmiths_tools.txt"))
	source := onBoardCard(t, e, 0, witherCorpusCard(t, "v/village_pillagers.txt"))
	target := onBoard(t, e, 1, "Name:Target\nTypes:Creature\nPT:4/4\nOracle:x\n")
	face := e.G.Obj(tools).Face()
	if !e.HasKeyword(source, "Wither") || face == nil || len(face.Triggers) != 1 || face.Triggers[0].Mode != "CounterAddedOnce" || face.Triggers[0].Params["CounterType"] != "M1M1" {
		t.Fatalf("precondition: source/CounterAddedOnce trigger = source=%t tools=%+v", e.HasKeyword(source, "Wither"), face)
	}
	if e.G.Obj(target).Zone != state.ZBattlefield || !e.IsCreature(target) || e.G.Obj(tools).Counter("CHARGE") != 0 {
		t.Fatal("precondition: target or Wickersmith's Tools is not ready")
	}
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0, Targets: []state.Target{{Obj: target}}},
		&cards.SA{Kind: "DB", API: "DealDamage", Params: map[string]string{"Defined": "Targeted", "NumDmg": "2"}})
	assertWitherCounter(t, e, target, 2)
	if got := witherCounterChanges(e, target, 2); got != 1 {
		t.Fatalf("Wither M1M1 CounterChange count = %d, want 1", got)
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("Wickersmith's Tools pending CounterAddedOnce triggers = %d, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if got := e.G.Obj(tools).Counter("CHARGE"); got != 1 {
		t.Fatalf("Wickersmith's Tools charge counters = %d, want 1 from Wither CounterChange", got)
	}
}
