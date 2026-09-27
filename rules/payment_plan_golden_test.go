package rules

import (
	"encoding/hex"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The DecisionMade.Text of a planned submission is hash-chained (spec
// 2026-09-24-cast-payment-plans §7): the canonical payment suffix
// ";payment:<action ID>:<plan ID>" binds the submitted witness into replay
// history next to the legacy "<kind>:<choices>" spelling. These goldens
// freeze those exact bytes -- and the resulting chain head -- on a fixed
// seed whose plan is UNIQUE (one untapped Island, one {U} spell, empty
// pool), so a later planner ranking or eligibility change cannot move the
// golden; what is pinned here is the encoding, not the planner. A change to
// either ID's derivation or to the separator format makes these fail, and
// that is a replay-compat break that must be named in a ticket, not absorbed
// silently.

// goldenPlannedSpell is the {U} instant of the golden fixture: castable at
// the first priority ask with one untapped Island, so the planner offers
// exactly one action with exactly one plan.
const goldenPlannedSpellSrc = "Name:Golden Cast\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n"

// passOptionIndex returns the index of the "pass" option in d's offer,
// failing if it is not offered.
func passOptionIndex(t *testing.T, d *decision.Decision) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == "pass" {
			return o.Index
		}
	}
	t.Fatalf("pass option not offered in %#v", d.Options)
	return -1
}

// isHex64 asserts a canonical V1 digest shape: 64 lower-case hex characters.
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

// lastDecisionMadeText returns the Text of the most recent DecisionMade
// event, failing if there is none.
func lastDecisionMadeText(t *testing.T, e *Engine) string {
	t.Helper()
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		if e.L.Events[i].Kind == events.DecisionMade {
			return e.L.Events[i].Text
		}
	}
	t.Fatal("no DecisionMade event in the log")
	return ""
}

func TestPaymentPlanDecisionMadeGolden(t *testing.T) {
	t.Run("planned submission binds the witness bytes", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9107, goldenPlannedSpellSrc)
		island := onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("pending = %#v, want priority", d)
		}
		d.PaymentActions = e.PaymentActionsForPriority(0, d.Seq)
		// Uniqueness precondition: with one funded source and one castable
		// spell the planner must offer exactly one action with exactly one
		// plan, so nothing downstream can reorder or re-rank this fixture.
		if len(d.PaymentActions) != 1 {
			t.Fatalf("payment actions = %#v, want exactly one", d.PaymentActions)
		}
		a := d.PaymentActions[0]
		if a.Cast.Object != spell {
			t.Fatalf("action cast object = %d, want the golden spell %d", a.Cast.Object, spell)
		}
		if len(a.Plans) != 1 {
			t.Fatalf("plans = %#v, want exactly one", a.Plans)
		}
		plan := a.Plans[0]
		if len(plan.Activations) != 1 {
			t.Fatalf("plan activations = %#v, want exactly the Island's", plan.Activations)
		}
		// Digest-shape precondition: both IDs must be full canonical V1
		// digests, or the golden below would pin a degenerate ID.
		if !isHex64(a.ID) {
			t.Fatalf("action ID %q is not 64 lower-case hex", a.ID)
		}
		if !isHex64(plan.ID) {
			t.Fatalf("plan ID %q is not 64 lower-case hex", plan.ID)
		}
		in := decision.Intent{Seq: d.Seq, Player: 0, Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: plan}}
		if err := e.Submit(in); err != nil {
			t.Fatalf("Submit planned cast: %v", err)
		}
		// The golden moment is real: the witness executed.
		if !e.G.Obj(island).Tapped {
			t.Fatal("the golden's Island was not tapped through mana activation")
		}
		if e.G.Obj(spell).Zone != state.ZStack {
			t.Fatalf("spell zone = %s, want stack", e.G.Obj(spell).Zone)
		}
		made := lastDecisionMadeText(t, e)
		want := goldenPlannedDecisionMade
		if made != want {
			t.Fatalf("DecisionMade.Text = %q, want frozen golden %q", made, want)
		}
		if got := e.L.Head(); got != goldenPlannedHead {
			t.Fatalf("chain head = %s, want frozen golden %s", got, goldenPlannedHead)
		}
	})

	t.Run("manual submission leaves the legacy text untouched", func(t *testing.T) {
		e, _, _ := newFixtureDeck(t, 9107, goldenPlannedSpellSrc)
		onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("pending = %#v, want priority", d)
		}
		in := decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{passOptionIndex(t, d)}}
		if err := e.Submit(in); err != nil {
			t.Fatalf("Submit manual pass: %v", err)
		}
		made := lastDecisionMadeText(t, e)
		// Byte-exact legacy spelling ("<kind>:[<choice>]", fmt.Sprintf's exact
		// spacing), with no payment suffix: a payment-less submission must be
		// indistinguishable from pre-plans history.
		want := "priority:[" + strconv.Itoa(passOptionIndex(t, d)) + "]"
		if made != want {
			t.Fatalf("manual DecisionMade.Text = %q, want legacy %q", made, want)
		}
		if strings.Contains(made, ";payment:") {
			t.Fatalf("manual DecisionMade.Text = %q carries a payment suffix", made)
		}
	})
}

// Frozen goldens (seed 9107 fixture above). Regenerating one of these by
// re-running the test after an intentional encoding change is a REPLAY-COMPAT
// BREAK: every recorded chain that carries the old bytes stops replaying.
const (
	goldenPlannedDecisionMade = "priority:[];payment:03ccf15155d5eebb6d4e7a135bc12588ea81bb72b8fbb6dbdbd8fff86cce1323:f451a94e881b45da32bdb5b2d0188b6130c17036f78f686fea46bb0a5249efa6"
	goldenPlannedHead         = "9b31f25522aef567"
)
