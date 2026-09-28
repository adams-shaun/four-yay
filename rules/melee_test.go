package rules

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Parse afresh rather than reading pre-expanded corpus IR: a stale cache
// must not make the registration test pass with the expander removed.
func meleeCorpus(t *testing.T, path string) *cards.Card {
	t.Helper()
	c, ds := cards.Parse(filepath.Join("..", ".cards", "cardsfolder", path))
	if c == nil || len(ds) != 0 {
		t.Fatalf("parse corpus card %s: %v", path, ds)
	}
	if ds := c.Link(); len(ds) != 0 {
		t.Fatalf("link corpus card %s: %v", path, ds)
	}
	return c
}

// meleeAttack submits one actual declare-attackers decision: unlike synthetic
// per-defender events, it exercises the whole-batch opponent snapshot.
func meleeAttack(t *testing.T, e *Engine, choices map[state.ObjID]state.PlayerID) {
	t.Helper()
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attacker decision, got %+v", d)
	}
	var picks []int
	for _, opt := range d.Options {
		if p, ok := choices[opt.Obj]; ok && p == opt.Player && opt.Battle == 0 {
			picks = append(picks, opt.Index)
		}
	}
	if len(picks) != len(choices) {
		t.Fatalf("found %d of %d attacker/defender options in %+v", len(picks), len(choices), d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}); err != nil {
		t.Fatal(err)
	}
	answerTriggerOrders(t, e)
	e.priorityRound()
	answerTriggerOrders(t, e)
	passUntilStackEmpty(t, e, 40)
}

func TestTitaniaMeleeCountsDistinctAttackedOpponents(t *testing.T) {
	t.Parallel()
	titania := meleeCorpus(t, "t/titania_proud_pummeler.txt")
	bear := meleeCorpus(t, "g/grizzly_bears.txt")
	if !titania.Faces[0].HasKeyword("Melee") {
		t.Fatal("real Titania has no printed Melee")
	}
	printed := false
	for _, tr := range titania.Faces[0].Triggers {
		if tr.Params["Keyword"] == "Melee" && tr.Effect != nil {
			printed = true
		}
	}
	if !printed {
		t.Fatal("freshly linked real Titania has no Melee attack trigger")
	}
	for _, tc := range []struct {
		name        string
		bearAttacks bool
		want        int32
	}{
		{"one opponent even with two attackers", false, 1},
		{"two attacked opponents", true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := threeSeatEngine(t)
			tid := onBoardCard(t, e, 0, titania)
			bid := onBoardCard(t, e, 0, bear)
			e.G.Obj(tid).SummonSick = false
			e.G.Obj(bid).SummonSick = false
			if e.G.Obj(tid).Zone != state.ZBattlefield || e.Derived(tid).Power != 3 {
				t.Fatalf("Titania not on battlefield as a 3/3 before the attack: %+v", e.G.Obj(tid))
			}
			if !e.HasKeyword(bid, "Melee") {
				t.Fatal("Titania did not grant Melee to the Bear")
			}
			attackers := map[state.ObjID]state.PlayerID{tid: 1, bid: 1}
			if tc.bearAttacks {
				attackers[bid] = 2
			}
			meleeAttack(t, e, attackers)
			if got := e.Derived(tid); got.Power != 3+tc.want || got.Toughness != 3+tc.want {
				t.Fatalf("Titania after attack: %d/%d, want %d/%d", got.Power, got.Toughness, 3+tc.want, 3+tc.want)
			}
			if got := e.Derived(bid); got.Power != 2+tc.want || got.Toughness != 2+tc.want {
				t.Fatalf("granted Melee after attack: %d/%d, want %d/%d", got.Power, got.Toughness, 2+tc.want, 2+tc.want)
			}
		})
	}
}

// CR 702.121b counts the protector of an attacked battle, not the battle as
// an extra opponent. The two declarations below differ only in whether the
// Bear attacks that same protector or a second seat.
func TestTitaniaMeleeBattleCountsProtectorOnce(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	titania := mustCorpusCard(t, reg, "Titania, Proud Pummeler")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	for _, second := range []bool{false, true} {
		t.Run(map[bool]string{false: "same opponent", true: "second opponent"}[second], func(t *testing.T) {
			e, battle, _, protector := battleAttackBoard(t)
			id := onBoardCard(t, e, 0, titania)
			bid := onBoardCard(t, e, 0, bear)
			e.G.Obj(id).SummonSick = false
			e.G.Obj(bid).SummonSick = false
			if e.G.Obj(battle).Zone != state.ZBattlefield || !e.G.Obj(battle).ProtectorValid ||
				protector == 0 || e.Derived(id).Power != 3 {
				t.Fatalf("bad battle or Melee preconditions: battle=%+v Titania=%+v", e.G.Obj(battle), e.G.Obj(id))
			}
			e.askAttackers()
			d := e.Pending()
			if d == nil || d.Kind != decision.KAttackers {
				t.Fatalf("no attackers decision: %+v", d)
			}
			picks := []int{}
			other := state.PlayerID(1)
			if other == protector {
				other = 2
			}
			for _, opt := range d.Options {
				if opt.Obj == id && opt.Battle == battle && opt.Player == protector {
					picks = append(picks, opt.Index)
				}
				if opt.Obj == bid && opt.Battle == 0 && ((second && opt.Player == other) || (!second && opt.Player == protector)) {
					picks = append(picks, opt.Index)
				}
			}
			if len(picks) != 2 {
				t.Fatalf("battle and player attacks were not both offered: %+v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}); err != nil {
				t.Fatal(err)
			}
			answerTriggerOrders(t, e)
			e.priorityRound()
			answerTriggerOrders(t, e)
			passUntilStackEmpty(t, e, 40)
			want := int32(4)
			if second {
				want = 5
			}
			if got := e.Derived(id); got.Power != want || got.Toughness != want {
				t.Fatalf("battle protector Melee: %d/%d, want %d/%d", got.Power, got.Toughness, want, want)
			}
		})
	}
}

func TestTitaniaAndAdrianaGrantTwoMeleeInstances(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	titania := mustCorpusCard(t, reg, "Titania, Proud Pummeler")
	adriana := mustCorpusCard(t, reg, "Adriana, Captain of the Guard")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e := threeSeatEngine(t)
	a := onBoardCard(t, e, 0, titania)
	b := onBoardCard(t, e, 0, adriana)
	id := onBoardCard(t, e, 0, bear)
	for _, obj := range []state.ObjID{a, b, id} {
		e.G.Obj(obj).SummonSick = false
		if e.G.Obj(obj).Zone != state.ZBattlefield {
			t.Fatalf("source %d not on the battlefield", obj)
		}
	}
	instances := 0
	for _, k := range e.Derived(id).Keywords {
		if cards.KeywordHead(k) == "Melee" {
			instances++
		}
	}
	if instances != 2 || e.Derived(id).Power != 2 {
		t.Fatalf("Bear before attack: %d Melee instances, power %d; want 2 and 2", instances, e.Derived(id).Power)
	}
	// Titania and Adriana grant to each other as well. The Bear's two
	// grants each trigger once for two opponents, totalling +4/+4.
	meleeAttack(t, e, map[state.ObjID]state.PlayerID{a: 1, id: 2})
	if got := e.Derived(id); got.Power != 6 || got.Toughness != 6 {
		t.Fatalf("two Melee instances: Bear is %d/%d, want 6/6", got.Power, got.Toughness)
	}
}
