package rules

// Restored from effects/changecombatants_test.go and the Commandeer case of
// effects/choose_control_test.go (W3 legacy removal): the defender reselect
// and redirect asks, answered through the resolution kernel on a real engine.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kr0AttackingBoard: a 3-seat engine with a seat-0 attacker pointed at seat
// 1 and blocked by a seat-1 Memnite, as the declare steps would leave it.
func kr0AttackingBoard(t *testing.T) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	e := kr0Engine(t, 3)
	att := kr0Src(t, e, 0, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	blk := kr0Src(t, e, 1, "Name:Memnite\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	o := e.G.Obj(att)
	o.IsAttacking = true
	o.Attacking = 1
	o.BlockedBy = []state.ObjID{blk}
	e.G.NoteBlockers()
	return e, att, blk
}

func kr0Retargets(evs []events.Event) []events.Event {
	var out []events.Event
	for _, ev := range evs {
		if ev.Kind == events.CombatRetarget {
			out = append(out, ev)
		}
	}
	return out
}

// kr0PlayerOpt is the index of d's option naming seat p.
func kr0PlayerOpt(t *testing.T, d *decision.Decision, p state.PlayerID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == p {
			return o.Index
		}
	}
	t.Fatalf("no option for seat %d in %+v", p, d.Options)
	return -1
}

// TestChangeCombatantsAnsweredRetargetsAndUnblocksKernel: answering a new
// defender re-points the attack (one CombatRetarget) and clears BlockedBy,
// with no Note.
func TestChangeCombatantsAnsweredRetargetsAndUnblocksKernel(t *testing.T) {
	t.Parallel()
	e, att, _ := kr0AttackingBoard(t)
	start := len(e.L.Events)
	d := kr0Run(t, e, kr0SA(t, "DB$ ChangeCombatants | Defined$ Self | Attacking$ True"), func() *effects.Ctx {
		return &effects.Ctx{Source: att, Controller: 0}
	}, nil)
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 || len(d.Options) != 2 {
		t.Fatalf("reselect ask = %+v, want seat 0 choosing between seats 1 and 2", d)
	}
	kr0Answer(t, e, kr0PlayerOpt(t, d, 2))
	evs := kr0Since(e, start)
	got := kr0Retargets(evs)
	if len(got) != 1 || got[0].Obj != att || got[0].Player != 2 {
		t.Fatalf("retarget events = %+v, want one naming %d -> seat 2", got, att)
	}
	o := e.G.Obj(att)
	if o.Attacking != 2 {
		t.Fatalf("attacker.Attacking = %d, want 2", o.Attacking)
	}
	if len(o.BlockedBy) != 0 {
		t.Fatalf("BlockedBy = %v, want cleared (the re-pointed attack is unblocked)", o.BlockedBy)
	}
	if n := kr0Count(evs, events.Note); n != 0 {
		t.Fatalf("answered path emitted %d note(s): %+v", n, evs)
	}
}

// TestChangeCombatantsKeepCurrentDefenderIsATrueNoOpKernel: answering the
// attacker's CURRENT defender emits no CombatRetarget and keeps BlockedBy.
func TestChangeCombatantsKeepCurrentDefenderIsATrueNoOpKernel(t *testing.T) {
	t.Parallel()
	e, att, blk := kr0AttackingBoard(t)
	start := len(e.L.Events)
	d := kr0Run(t, e, kr0SA(t, "DB$ ChangeCombatants | Defined$ Self | Attacking$ True"), func() *effects.Ctx {
		return &effects.Ctx{Source: att, Controller: 0}
	}, nil)
	if d == nil {
		t.Fatal("no reselect ask posed")
	}
	kr0Answer(t, e, kr0PlayerOpt(t, d, 1))
	evs := kr0Since(e, start)
	if got := kr0Retargets(evs); len(got) != 0 {
		t.Fatalf("keep-current answer emitted %+v, want no retarget", got)
	}
	o := e.G.Obj(att)
	if o.Attacking != 1 || len(o.BlockedBy) != 1 || o.BlockedBy[0] != blk {
		t.Fatalf("attack = %d blocked by %v, want unchanged (1, [%d])", o.Attacking, o.BlockedBy, blk)
	}
	if n := kr0Count(evs, events.Note); n != 0 {
		t.Fatalf("keep-current answer emitted %d note(s)", n)
	}
}

// TestChangeCombatantsWindshaperAsksEachAttackerKernel: Windshaper
// Planetar's Defined$ Valid Creature.attacking sweep asks for each attacker
// in turn; nothing moves before the first answer, and each answer re-points
// exactly its own attacker.
func TestChangeCombatantsWindshaperAsksEachAttackerKernel(t *testing.T) {
	t.Parallel()
	e := kr0Engine(t, 3)
	a1 := kr0Src(t, e, 0, "Name:Vanquisher\nTypes:Creature\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	a2 := kr0Src(t, e, 0, "Name:Sentinel\nTypes:Creature\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	for _, id := range []state.ObjID{a1, a2} {
		e.G.Obj(id).IsAttacking = true
		e.G.Obj(id).Attacking = 1
	}
	start := len(e.L.Events)
	s := kr0SA(t, "DB$ ChangeCombatants | Defined$ Valid Creature.attacking | Optional$ True | Attacking$ True")
	d := kr0Run(t, e, s, func() *effects.Ctx { return &effects.Ctx{Source: a1, Controller: 0} }, nil)
	if d == nil {
		t.Fatal("no ask for the first attacker")
	}
	if got := kr0Retargets(kr0Since(e, start)); len(got) != 0 {
		t.Fatalf("emitted %+v before any answer", got)
	}
	d = kr0Answer(t, e, kr0PlayerOpt(t, d, 2))
	if d == nil {
		t.Fatal("the second attacker was never asked")
	}
	if next := kr0Answer(t, e, kr0PlayerOpt(t, d, 1)); next != nil {
		t.Fatalf("a third ask was posed: %+v", next)
	}
	got := kr0Retargets(kr0Since(e, start))
	if len(got) != 1 || got[0].Obj != a1 || got[0].Player != 2 {
		t.Fatalf("retarget events = %+v, want exactly attacker 1 (%d) -> seat 2", got, a1)
	}
	if e.G.Obj(a1).Attacking != 2 || e.G.Obj(a2).Attacking != 1 {
		t.Fatalf("attacks = %d/%d, want 2/1", e.G.Obj(a1).Attacking, e.G.Obj(a2).Attacking)
	}
}

// TestChangeTargetsCommandeerCorpusSAKernel: Commandeer's real
// DBChooseTargets on an opponent's player-targeted spell asks for the new
// target; an empty (decline) answer keeps the targets, and choosing seat 0
// redirects the spell.
func TestChangeTargetsCommandeerCorpusSAKernel(t *testing.T) {
	t.Parallel()
	s := kr0SVar(t, kr0Corpus(t, "Commandeer"), "DBChooseTargets")
	if s.API != "ChangeTargets" {
		t.Fatalf("unexpected SA: %+v", s)
	}
	const spellSrc = "Name:Target\nTypes:Instant\nManaCost:R\nA:SP$ DealDamage | ValidTgts$ Player | NumDmg$ 1\nOracle:x\n"
	for _, redirect := range []bool{false, true} {
		e := kr0Engine(t, 2)
		spell := e.G.AddObject(card(t, spellSrc), 1)
		spell.Zone = state.ZStack
		spell.Targets = []state.Target{{Player: 1, IsPlayer: true}}
		id := spell.ID
		e.G.Stack = append(e.G.Stack, id)
		d := kr0Run(t, e, s, func() *effects.Ctx {
			return &effects.Ctx{Controller: 0, Targets: []state.Target{{Obj: id}}}
		}, nil)
		if d == nil || d.ResumeKind != "choice" {
			t.Fatalf("Commandeer posed %+v, want the new-target choice", d)
		}
		if redirect {
			kr0Answer(t, e, kr0PlayerOpt(t, d, 0))
		} else {
			if d.Min != 0 {
				t.Fatalf("Commandeer's redirect is mandatory (Min %d); the decline leg needs Optional$", d.Min)
			}
			kr0Answer(t, e)
		}
		tg := e.G.Obj(id).Targets
		want := state.PlayerID(1)
		if redirect {
			want = 0
		}
		if len(tg) != 1 || !tg[0].IsPlayer || tg[0].Player != want {
			t.Fatalf("redirect=%v: targets = %+v, want seat %d", redirect, tg, want)
		}
	}
}
