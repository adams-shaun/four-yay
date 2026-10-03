package rules

// W3 step 4 (the suspension holders): the engine-posed resolution-time
// payment windows (window_tape.go) are served from the tape -- the
// triggered-effect Cost$ window's pay/decline election, its mana window as
// a multi-intent answer, and its component walk. Every case runs on legacy
// and on the kernel and must stay byte-identical.

import (
	"strings"
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

func TestTapeConvertTriggerCost(t *testing.T) {
	cases := []struct {
		name, cost, body string
		mana             string
		lands, hand      int
		pay              bool
		served           int64
		life             int32
	}{
		{name: "Tape Chalice", cost: "1", body: "GainLife | LifeAmount$ 3 | Defined$ You", mana: "BR", pay: true, served: 1, life: 3},
		{name: "Tape Chalice Declined", cost: "1", body: "GainLife | LifeAmount$ 3 | Defined$ You", mana: "BR", served: 1},
		{name: "Tape Vault Window", cost: "2", body: "GainLife | LifeAmount$ 3 | Defined$ You", mana: "B", lands: 3, pay: true, served: 4, life: 3},
		{name: "Tape Discard Fee", cost: "Discard<1/Card>", body: "GainLife | LifeAmount$ 3 | Defined$ You", mana: "B", hand: 2, pay: true, served: 2, life: 3},
		{name: "Tape Life Fee", cost: "PayLife<2>", body: "Draw | NumCards$ 1 | Defined$ You", mana: "B", pay: true, served: 1, life: -2},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := tapeWindowETB(tc.name, tc.cost, tc.body)
			seed := 13300 + uint64(i)
			asks := 0
			pick := tapeWindowPick(tc.pay)
			counting := func(d *decision.Decision) []int {
				if d.Kind == decision.KChoose && strings.Contains(d.Prompt, tc.name) {
					asks++
				}
				return pick(d)
			}
			setup := func(lands, hand int, p func(d *decision.Decision) []int) func(t *testing.T, e *Engine) {
				return func(t *testing.T, e *Engine) {
					for j := 0; j < hand; j++ {
						moveByName(t, e, 0, "Mountain", state.ZHand)
					}
					tapeUnlessScenario(tc.name, tc.mana, lands, p)(t, e)
				}
			}
			legacy, _ := tapeFixture(t, 2, seed, false, src)
			life := legacy.G.Players[0].Life
			setup(tc.lands, tc.hand, counting)(t, legacy)
			if asks == 0 {
				t.Fatal("legacy never posed the trigger cost window")
			}
			if got := legacy.G.Players[0].Life - life; got != tc.life {
				t.Fatalf("legacy life change %d, want %d", got, tc.life)
			}
			_, st := tapeDual(t, 2, seed, setup(tc.lands, tc.hand, pick), src)
			if st.Served < tc.served || st.LegacySwitch != 0 || st.Aborts != 0 {
				t.Fatalf("the trigger cost window was not served from the tape: %+v", st)
			}
		})
	}
}
