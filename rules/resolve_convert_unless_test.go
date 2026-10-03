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

func TestTapeConvertUnlessPay(t *testing.T) {
	const gain = "\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2 | Defined$ You"
	cases := []struct {
		name, body string
		seats      int
		mana       string
		lands      int
		pay        bool
		served     int64
		// window: the unless mana window asks the legacy run must pose.
		window int
		life   int32
	}{
		// The pool pays at once.
		{name: "Tape Tithe", seats: 2, mana: "BR", pay: true, served: 1, life: 5,
			body: "A:SP$ GainLife | LifeAmount$ 3 | UnlessCost$ 1 | UnlessPayer$ You | UnlessSwitched$ True | SubAbility$ DBGain" + gain},
		// Declined: the switched body does not run, the sub still does.
		{name: "Tape Tithe Declined", seats: 2, mana: "BR", served: 1, life: 2,
			body: "A:SP$ GainLife | LifeAmount$ 3 | UnlessCost$ 1 | UnlessPayer$ You | UnlessSwitched$ True | SubAbility$ DBGain" + gain},
		// A life payment settles in line too.
		{name: "Tape Blood Tithe", seats: 2, mana: "B", pay: true, served: 1, life: 1,
			body: "A:SP$ GainLife | LifeAmount$ 3 | UnlessCost$ PayLife<2> | UnlessPayer$ You | UnlessSwitched$ True"},
		// Every payer declines in turn: one election per seat.
		{name: "Tape Each Tithe", seats: 4, mana: "B", served: 4, life: -1,
			body: "A:SP$ LoseLife | LifeAmount$ 1 | Defined$ You | UnlessCost$ 1 | UnlessPayer$ Player"},
		// The mana window: an empty pool and two untapped Mountains -- one
		// activation, then Done once the pool covers {1}.
		{name: "Tape Window Tithe", seats: 2, mana: "B", lands: 2, pay: true, served: 3, window: 2, life: 5,
			body: "A:SP$ GainLife | LifeAmount$ 3 | UnlessCost$ 1 | UnlessPayer$ You | UnlessSwitched$ True | SubAbility$ DBGain" + gain},
		// A two-pip cost: two activations, then Done.
		{name: "Tape Wide Window", seats: 2, mana: "B", lands: 3, pay: true, served: 4, window: 3, life: 3,
			body: "A:SP$ GainLife | LifeAmount$ 3 | UnlessCost$ 2 | UnlessPayer$ You | UnlessSwitched$ True"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := tapeUnlessSorcery(tc.name, tc.body)
			seed := 13100 + uint64(i)
			scenario := tapeUnlessScenario(tc.name, tc.mana, tc.lands, tapeUnlessPick(tc.pay))
			legacy, _ := tapeFixture(t, tc.seats, seed, false, src)
			life := legacy.G.Players[0].Life
			window := 0
			pick := tapeUnlessPick(tc.pay)
			tapeUnlessScenario(tc.name, tc.mana, tc.lands, func(d *decision.Decision) []int {
				if d.ResumeKind == unlessManaKind {
					window++
				}
				return pick(d)
			})(t, legacy)
			if window != tc.window {
				t.Fatalf("legacy posed %d unless mana windows, want %d", window, tc.window)
			}
			if got := legacy.G.Players[0].Life - life; got != tc.life {
				t.Fatalf("legacy life change %d, want %d", got, tc.life)
			}
			_, st := tapeDual(t, tc.seats, seed, scenario, src)
			if st.Served < tc.served || st.LegacySwitch != 0 || st.Aborts != 0 {
				t.Fatalf("the unless election was not served from the tape: %+v", st)
			}
		})
	}
}

// A choice-bearing unless cost: the served election's component step is
// driven in line (tapeUnlessComponents), its pick served from the tape too.
func TestTapeConvertUnlessComponents(t *testing.T) {
	src := tapeUnlessSorcery("Tape Discard Tithe",
		"A:SP$ GainLife | LifeAmount$ 3 | UnlessCost$ Discard<1/Card> | UnlessPayer$ You | UnlessSwitched$ True")
	_, st := tapeDual(t, 2, 13150, func(t *testing.T, e *Engine) {
		moveByName(t, e, 0, "Mountain", state.ZHand)
		tapeUnlessScenario("Tape Discard Tithe", "B", 0, tapeUnlessPick(true))(t, e)
	}, src)
	if st.Served < 2 || st.LegacySwitch != 0 || st.Aborts != 0 || st.Unservable != 0 {
		t.Fatalf("the component step was not served from the tape: %+v", st)
	}
}
