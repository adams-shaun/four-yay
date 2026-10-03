package rules

// A NON-optional api:Repeat whose body poses a mid-resolution ask used to
// drop every remaining iteration: effRepeat returned on the suspension, the
// enclosing Resolve loop recorded a plain continuation at Repeat.Sub, and the
// loop cursor was lost. Only RepeatOptional$ reported a loop frame.
// SuspendRepeatBody now parks every Repeat's cursor and resolved bound, so
// the counted form (MaxRepeat$) runs its remaining iterations and the gated
// form (RepeatCheckSVar$/RepeatDefined$) re-checks its gate after the
// answered body (CR 608.2c).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestRemorselessPunishmentRepeatsAfterAnsweredBody: Remorseless Punishment
// is `Repeat | MaxRepeat$ 2` over a GenericChoice the TARGET opponent answers
// and an unless-pay that opponent then declines -- both asks suspend the
// resolution inside iteration 1. The opponent must face the process twice
// and lose 5 life each time (20 -> 10); before the fix the second iteration
// was dropped and the spell resolved at 15.
func TestRemorselessPunishmentRepeatsAfterAnsweredBody(t *testing.T) {
	t.Parallel()
	e, cfg, id, caster := corpusCardConfig(t, 7311, "Remorseless Punishment")
	addMana(t, e, caster, "BBBBB") // {3}{B}{B}
	opp := 1 - caster
	d := castFixture(t, e, id, int(opp))
	choices, declines := 0, 0
	for i := 0; i < 40 && d != nil && len(e.G.Stack) > 0; i++ {
		switch {
		case d.Kind == decision.KPriority:
			castFirst(t, e, "pass")
		case d.ResumeKind == "unless_pay":
			idx := -1
			for _, o := range d.Options {
				if o.Mode == decision.ModeUnlessDecline {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("unless-pay ask has no decline option: %+v", d.Options)
			}
			declines++
			submitChoices(t, e, idx)
		default:
			if d.Player != opp {
				t.Fatalf("body ask %q went to seat %d, want the target opponent %d", d.ResumeKind, d.Player, opp)
			}
			choices++
			submitChoices(t, e, d.Options[0].Index)
		}
		d = e.Pending()
	}
	if len(e.G.Stack) > 0 {
		t.Fatalf("the spell never finished resolving (pending %+v)", d)
	}
	if choices != 2 || declines != 2 {
		t.Fatalf("the opponent faced %d choices and %d unless-pay asks, want 2 and 2 (one per iteration)", choices, declines)
	}
	if life := e.G.Players[opp].Life; life != 10 {
		t.Fatalf("opponent life = %d, want 10 (5 per iteration, MaxRepeat$ 2)", life)
	}
	replayCheck(t, e, cfg)
}

// TestCultivatorColossusRepeatGateRunsAfterAnsweredBody: Cultivator
// Colossus's ETB is a GATED Repeat (RepeatDefined$ Remembered | RepeatPresent$
// Card) whose body is a hand pick (ChangeZone Origin$ Hand) -- an ask in every
// iteration. Each answered pick must put the land in, draw, re-check the gate
// and ask again; declining empties Remembered and the gate ends the loop.
// Before the fix only the first pick happened.
func TestCultivatorColossusRepeatGateRunsAfterAnsweredBody(t *testing.T) {
	t.Parallel()
	e, cfg, id, caster := corpusCardConfig(t, 7311, "Cultivator Colossus")
	addMana(t, e, caster, "GGGGGGG") // {4}{G}{G}{G}
	const picks = 3
	handToBattlefield := func() int {
		n := 0
		for _, ev := range e.L.Events {
			if ev.Kind == events.MoveZone && ev.From == state.ZHand && ev.To == state.ZBattlefield && ev.Obj != id {
				n++
			}
		}
		return n
	}
	drawsBefore := countDraw(e)
	d := castFixture(t, e, id, -1)
	asks := 0
	for i := 0; i < 60 && d != nil && len(e.G.Stack) > 0; i++ {
		switch {
		case d.Kind == decision.KPriority:
			castFirst(t, e, "pass")
		case d.Kind == decision.KTriggerOptional:
			submitChoices(t, e, d.Options[0].Index) // "you may": yes
		case d.ResumeKind == "hand_move":
			asks++
			if asks <= picks {
				submitChoices(t, e, d.Options[0].Index)
			} else {
				if d.Min != 0 {
					t.Fatalf("the hand pick is not optional (Min %d)", d.Min)
				}
				submitChoices(t, e) // decline: the gate ends the loop
			}
		default:
			t.Fatalf("unexpected ask %s/%q", d.Kind, d.ResumeKind)
		}
		d = e.Pending()
	}
	if len(e.G.Stack) > 0 {
		t.Fatalf("the trigger never finished resolving (pending %+v)", d)
	}
	if asks != picks+1 {
		t.Fatalf("the hand pick was posed %d times, want %d (one per land put in, then the declined one)", asks, picks+1)
	}
	if got := handToBattlefield(); got != picks {
		t.Fatalf("%d lands entered from hand, want %d", got, picks)
	}
	if got := countDraw(e) - drawsBefore; got != picks {
		t.Fatalf("drew %d cards, want %d (one per land put in)", got, picks)
	}
	replayCheck(t, e, cfg)
}
