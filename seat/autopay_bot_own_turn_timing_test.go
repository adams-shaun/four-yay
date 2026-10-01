package seat

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestAutoPayHoldsPlannedInstantOnOwnNonMainStep(t *testing.T) {
	bot := NewBot(19).EnableAutoPayMana()
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options:        []decision.Option{{Index: 0, Kind: "activate"}, {Index: 1, Kind: "pass"}},
		PaymentActions: []decision.PaymentAction{paymentAction("pay", 42)},
	}
	brd := botpolicy.Board{Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{
		42: {Creature: true, Power: 3, CMC: 3, Castable: true, InstantSpeed: true},
	})}

	brd.MyTurn = true
	in := bot.decide(brd, &d)
	if in.Payment != nil || len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("own non-main intent = %+v, want pass (no payment)", in)
	}

	brd.IsMain = true
	in = bot.decide(brd, &d)
	if in.Payment == nil || in.Payment.ActionID != "pay" {
		t.Fatalf("own main intent = %+v, want offered payment", in)
	}

	brd.IsMain = false
	brd.MyTurn = false
	in = bot.decide(brd, &d)
	if in.Payment == nil || in.Payment.ActionID != "pay" {
		t.Fatalf("opponent-turn intent = %+v, want offered payment", in)
	}
}
