package rules

// W3 step 4 (the suspension holders): the engine-posed resolution-time
// payment windows (window_tape.go) are served from the tape -- the
// triggered-effect Cost$ window's pay/decline election, its mana window as
// a multi-intent answer, and its component walk. Every case runs on legacy
// and on the kernel and must stay byte-identical.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// tapeWindowPick answers a window: pay (or decline) the election, activate
// the first offered mana source, Done once none is offered; anything else
// takes tapePick.
func tapeWindowPick(pay bool) func(d *decision.Decision) []int {
	return func(d *decision.Decision) []int {
		if d.Kind == decision.KChoose && d.ResumeKind == "" {
			for _, o := range d.Options {
				if o.Kind == "activate" {
					return []int{o.Index}
				}
			}
			for _, o := range d.Options {
				switch o.Kind {
				case "done":
					return []int{o.Index}
				case "trigger_cost_pay", "cumulative_pay", "echo_pay":
					if pay {
						return []int{o.Index}
					}
				case "trigger_cost_decline", "cumulative_sac", "echo_sac":
					if !pay {
						return []int{o.Index}
					}
				}
			}
		}
		return tapePick(d)
	}
}

func tapeWindowETB(name, cost, body string) string {
	return "Name:" + name + "\nManaCost:B\nTypes:Creature Bear\nPT:2/2\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigBody | TriggerDescription$ x\n" +
		"SVar:TrigBody:AB$ " + body + " | Cost$ " + cost + "\nOracle:x\n"
}

// tapeWindowUpkeep puts the fixture onto seat 0's battlefield in its first
// main phase, then plays on under pick until seat 0's next turn reaches its
// first main phase: the upkeep trigger (cumulative upkeep, echo) resolves on
// the way.
func tapeWindowUpkeep(name string, lands int, pick func(d *decision.Decision) []int) func(t *testing.T, e *Engine) {
	return func(t *testing.T, e *Engine) {
		t.Helper()
		toMain1(t, e)
		moveByName(t, e, 0, name, state.ZBattlefield)
		for i := 0; i < lands; i++ {
			moveByName(t, e, 0, "Mountain", state.ZBattlefield)
		}
		start := e.G.Turn
		for i := 0; i < 2000; i++ {
			if e.G.Over || (e.G.Turn >= start+2 && e.G.Step == state.StepMain1) {
				return
			}
			d := e.Pending()
			if d == nil {
				e.Advance()
				continue
			}
			ch := []int(nil)
			if d.Kind == decision.KPriority {
				ch = []int{tapePassIndex(d)}
			} else if d.Kind == decision.KAttackers || d.Kind == decision.KBlockers {
				ch = nil
			} else {
				ch = pick(d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		}
		t.Fatal("never reached the next turn")
	}
}

func tapeUnlessScenarioResolve(t *testing.T, e *Engine, pick func(d *decision.Decision) []int) {
	t.Helper()
	for i := 0; i < 400; i++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			return
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				return
			}
			submitChoices(t, e, tapePassIndex(d))
			continue
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: pick(d)}); err != nil {
			t.Fatalf("submit %s/%s: %v", d.Kind, d.ResumeKind, err)
		}
	}
	t.Fatal("stack never drained")
}
