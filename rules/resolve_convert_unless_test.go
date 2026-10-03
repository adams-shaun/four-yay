package rules

// W3 step 3 (batch d, the unless-pay window): an UnlessCost$ election is
// served from the tape and settled in line -- an immediate pay or decline,
// the next payer after a decline, and the CR 601.2g mana window as a
// multi-intent answer (one served intent per source activated, then Done).
// The choice-bearing component continuation is driven in line too (W3 step
// 4e). Every case runs on
// legacy and on the kernel and must stay byte-identical.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// tapeUnlessPick answers the unless asks deterministically: the election
// takes pay when offered (decline when payDecline is false), the mana window
// activates its first offered source and takes Done once none is offered;
// anything else takes tapePick.
func tapeUnlessPick(pay bool) func(d *decision.Decision) []int {
	return func(d *decision.Decision) []int {
		switch d.ResumeKind {
		case "unless_pay":
			for _, o := range d.Options {
				if (o.Mode == decision.ModeUnlessPay) == pay {
					return []int{o.Index}
				}
			}
		case unlessManaKind:
			for _, o := range d.Options {
				if o.Kind == "activate" {
					return []int{o.Index}
				}
			}
			return []int{len(d.Options) - 1}
		}
		return tapePick(d)
	}
}

// tapeUnlessScenario casts name for mana with lands Mountains on seat 0's
// battlefield and resolves everything under pick.
func tapeUnlessScenario(name, mana string, lands int, pick func(d *decision.Decision) []int) func(t *testing.T, e *Engine) {
	return func(t *testing.T, e *Engine) {
		t.Helper()
		for i := 0; i < lands; i++ {
			moveByName(t, e, 0, "Mountain", state.ZBattlefield)
		}
		addMana(t, e, 0, mana)
		id := fixtureInHand(t, e, name)
		submitChoices(t, e, castOptionFor(t, e, id).Index)
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
}

func tapeUnlessSorcery(name, body string) string {
	return "Name:" + name + "\nManaCost:B\nTypes:Sorcery\n" + body + "\nOracle:x\n"
}
