package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestClayGolemRollDiceCostAndMonstrosity(t *testing.T) {
	t.Parallel()
	parsed := ParseCost("6 RollDice<1/8/X>")
	if parsed.Generic != 6 || len(parsed.Unknown) != 0 || len(parsed.RollDice) != 1 {
		t.Fatalf("RollDice cost parse = %+v, want {6} and one modelled roll", parsed)
	}
	part := parsed.RollDice[0]
	if part.N != 1 || part.Spec != "8" || part.Dyn != "X" {
		t.Fatalf("RollDice part = %+v, want N=1 sides=8 XVar=X", part)
	}
	for _, raw := range []string{"RollDice<bogus>", "RollDice<1/8/Y>"} {
		got := ParseCost(raw)
		if got.Generic != 1 || len(got.Unknown) != 1 || got.Unknown[0] != "RollDice" || len(got.RollDice) != 0 {
			t.Errorf("malformed %q parsed as %+v, want one generic and unknown RollDice", raw, got)
		}
	}

	e, cfg, id := monstrosityEngine(t, "Clay Golem", "CCCCCC")
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Monstrous || o.Counter("P1P1") != 0 {
		t.Fatalf("precondition: Clay Golem must be an unmarked battlefield permanent, got %+v", o)
	}
	idx := monstrosityAbilityIndex(t, e, id)
	if _, ok := findAbilityOption(e, id, idx); !ok {
		t.Fatal("precondition: {6} Clay Golem activation is not offered")
	}
	opt := abilityOption(t, e, id, idx)
	submitChoices(t, e, opt.Index)
	// Drain the activated ability and stop at the Berserk trigger's target ask
	// so the source remains available for the counter/mark assertions.
	for i := 0; i < 20 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while resolving Clay Golem ability")
		}
		if d.Kind == decision.KTarget {
			break
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision while resolving ability: %+v", d)
		}
		pass := -1
		for _, option := range d.Options {
			if option.Kind == "pass" {
				pass = option.Index
				break
			}
		}
		if pass < 0 {
			t.Fatalf("no pass option while resolving ability: %+v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
			t.Fatalf("pass priority: %v", err)
		}
	}

	rolls := 0
	var result int32
	for _, ev := range e.L.Events {
		_, sides, _, r, ok := effects.DieRollResult(ev)
		if ok {
			rolls++
			result = r
			if sides != 8 {
				t.Fatalf("cost roll Note has sides=%d, want d8: %+v", sides, ev)
			}
		}
	}
	if rolls != 1 || result < 1 || result > 8 {
		t.Fatalf("want exactly one d8 cost roll in [1,8], got count=%d result=%d", rolls, result)
	}
	o := e.G.Obj(id)
	if !o.Monstrous || o.Counter("P1P1") != result {
		t.Fatalf("RollDice X=%d: monstrous=%v +1/+1=%d, want marked with result counters", result, o.Monstrous, o.Counter("P1P1"))
	}
	marks := monstrousMarkEvents(e)
	if len(marks) != 1 || marks[0].Obj != id || marks[0].Amount != result {
		t.Fatalf("want one Monstrous mark carrying die result %d, got %+v", result, marks)
	}
	fired := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == id && ev.Amount == 0 {
			fired = true
		}
	}
	if !fired {
		t.Fatal("precondition: Clay Golem's BecomeMonstrous trigger was not pushed")
	}
	// DieRollNote is the canonical event RolledDie/RolledDieOnce consume;
	// this cost therefore enters the same trigger path as an effect roll.
	replayCheck(t, e, cfg)
}
