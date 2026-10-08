package seat

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestPaymentIntentScratchIsInvisible pins the heap-object POC cut that gives
// a Bot a reusable scratch Decision, two maps and an Options backing array.
// One Bot must answer a sequence of decisions -- with distinct plans, so a
// plan left in payPlans, an index left in payTo/payLeg or an option left in
// payOpts would surface -- exactly as a fresh Bot would, and paymentIntent
// must return the offered decision unmutated (the private candidate is a
// shallow copy the policy only reads).
func TestPaymentIntentScratchIsInvisible(t *testing.T) {
	brd := botpolicy.Board{IsMain: true, Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{
		42: {Creature: true, Power: 3, CMC: 3, Castable: true},
		99: {Creature: true, Power: 3, CMC: 3, Castable: true},
	})}
	mk := func(seq uint64, id string, obj state.ObjID) decision.Decision {
		return decision.Decision{Seq: seq, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
			Options:        []decision.Option{{Index: 0, Kind: "activate"}, {Index: 1, Kind: "pass"}},
			PaymentActions: []decision.PaymentAction{paymentAction(id, obj)},
		}
	}
	fresh := func(d decision.Decision) decision.Intent {
		return NewBot(19).EnableAutoPayMana().decide(brd, &d)
	}
	bot := NewBot(19).EnableAutoPayMana()
	for _, tc := range []struct {
		id  string
		obj state.ObjID
	}{
		{"pay-a", 42}, {"pay-b", 99}, {"pay-a", 42}, {"pay-b", 99},
	} {
		d := mk(7, tc.id, tc.obj)
		before := d.Clone()
		got := bot.decide(brd, &d)
		want := fresh(mk(7, tc.id, tc.obj))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: reused scratch = %+v, fresh = %+v", tc.id, got, want)
		}
		if got.Payment == nil || got.Payment.ActionID != tc.id {
			t.Fatalf("%s: intent = %+v, want the offered plan selected", tc.id, got)
		}
		if !reflect.DeepEqual(d, *before) {
			t.Fatalf("%s: paymentIntent mutated the offered decision: %+v, was %+v", tc.id, d, *before)
		}
	}
}
