package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
)

// TestTitanOfLittjaraDrawXPayAskCountedOnce is the end-to-end ask-count pin on
// the ticket's canonical carrier (Titan of Littjara's `Cost$ Draw<X/You>`
// trigger body). TestTitanOfLittjaraDrawXCost asserts the STRUCTURAL shape --
// the ask right after the answered pay election is the body's discard
// (ResumeKind "discard") -- which cannot see a LATE re-entry that poses a
// second pay ask AFTER the body has already run. This test walks the whole
// resolution instead and counts every decision that offers `trigger_cost_pay`,
// so a duplicate window anywhere in the chain (before OR after the body) fails.
//
// The window machinery poses its ONE election through the shared
// startTriggeredEffectCost / triggeredCostPaymentAsk call site
// (rules/cumulative.go), which every arming call site funnels through, so this
// real-corpus carrier exercises the single-ask guarantee for the whole
// T:$Execute$-names-an-AB$-with-Cost$ shape (819 corpus files) rather than a
// synthetic construction.
func TestTitanOfLittjaraDrawXPayAskCountedOnce(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, titan := titanBearFixture(t, reg)

	// Preconditions the count assertion depends on. A vacuous fixture (the
	// trigger never pushed, or the fold not resolving to exactly one Bear)
	// must fail here, not let a `want 1` pass silently.
	n, ok := e.drawCostCount(titan, 0, drawCostPart())
	if !ok || n != 1 {
		t.Fatalf("drawCostCount(Titan) = %d, %v; want exactly 1 (the one other Bear sharing the chosen type)", n, ok)
	}
	if pushed := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.TriggerPush && ev.Obj == titan
	}); pushed != 1 {
		t.Fatalf("Titan TriggerPush events = %d, want exactly 1 before counting asks", pushed)
	}

	// Drive the FULL resolution: from the pushed trigger, through the answered
	// pay election and the discard body's ask, to an empty stack. At every
	// step inspect the pending decision and count whether it offers a
	// `trigger_cost_pay` option. Pay asks are answered "pay"; the body's own
	// discard ask an actual discard choice; priority is passed.
	payAsks := 0
	discards := 0
	draws := 0
	mark := len(e.L.Events)
	for i := 0; i < 80 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) == 0 {
				break
			}
			t.Fatalf("no decision while draining (stack depth %d, step %d)", len(e.G.Stack), i)
		}
		// The resolution is complete once the stack is empty and the only ask
		// left is the ordinary turn priority. Any other ask with an empty
		// stack is a mid-resolution re-entry (a late duplicate cost window),
		// which must still be seen and counted before stopping.
		if len(e.G.Stack) == 0 && d.Kind == decision.KPriority {
			break
		}
		if hasTriggerCostPay(d) {
			payAsks++
		}
		if d.ResumeKind == "discard" {
			discards++
		}
		switch d.Kind {
		case decision.KPriority:
			passIdx := -1
			for _, op := range d.Options {
				if op.Kind == "pass" {
					passIdx = op.Index
				}
			}
			if passIdx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{passIdx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		case decision.KChoose:
			if hasTriggerCostPay(d) {
				// Answer the election as "pay": option 0 is trigger_cost_pay
				// in the payable shape this carrier builds.
				payIdx := -1
				for _, op := range d.Options {
					if op.Kind == "trigger_cost_pay" {
						payIdx = op.Index
					}
				}
				if payIdx < 0 {
					t.Fatalf("an ask offering no trigger_cost_pay option: %+v", d.Options)
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{payIdx}}); err != nil {
					t.Fatalf("submit pay: %v", err)
				}
				continue
			}
			if d.ResumeKind == "discard" {
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
					t.Fatalf("submit discard: %v", err)
				}
				continue
			}
			if len(d.Options) == 0 {
				t.Fatalf("choose decision with no options: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit choose: %v", err)
			}
		default:
			if len(d.Options) == 0 {
				t.Fatalf("%v decision with no options: %+v", d.Kind, d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
				t.Fatalf("submit %v: %v", d.Kind, err)
			}
		}
	}

	// The real assertion. Exactly ONE decision in the entire resolution
	// offered the pay/decline election; a second (a duplicate window, before
	// OR after the body) fails here.
	if payAsks != 1 {
		t.Fatalf("decisions offering trigger_cost_pay over the full resolution = %d, want exactly 1 (a duplicate window re-poses the election)", payAsks)
	}
	// The election was actually answered as a payment, so the body ran: the
	// precondition that makes the count meaningful (a decline-only run would
	// never reach the body's discard).
	if discards != 1 {
		t.Fatalf("the discard body's ask was posed %d times, want exactly 1 (the paid window must run the body once)", discards)
	}
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
	}
	if draws != 1 {
		t.Fatalf("the resolution drew %d cards for the payer, want exactly the folded count 1", draws)
	}
}

// hasTriggerCostPay reports whether d offers the triggered-cost pay election.
// The pay ask is a KChoose whose options carry the trigger_cost_pay kind; the
// decline-only hard-decline shape carries only trigger_cost_decline and so
// does not count as a posed election a second time.
func hasTriggerCostPay(d *decision.Decision) bool {
	if d == nil || d.Kind != decision.KChoose {
		return false
	}
	for _, op := range d.Options {
		if op.Kind == "trigger_cost_pay" {
			return true
		}
	}
	return false
}
