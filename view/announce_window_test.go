package view

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The announced mana window's readout (announce-then-pay spec §4.1) is
// projected with the decision, owned by the projection, and on the wire under
// mana_payment; its per-source "mana" options keep their Obj so the board's
// card-options index can highlight them.
func TestCopyDecisionManaPaymentWindow(t *testing.T) {
	d := &decision.Decision{Kind: decision.KChoose, Min: 1, Max: 1, Source: 40,
		Options: []decision.Option{
			{Index: 0, Kind: "mana", Obj: 7, Label: "Add U", ManaSymbol: "U"},
			{Index: 1, Kind: decision.OptCancelCast, Label: "Cancel cast"},
		},
		ManaPayment: &decision.ManaPaymentWindow{Card: 40,
			Cost:     decision.PaymentCost{Generic: 1, Mana: decision.ManaAmount{0, 1}},
			Owed:     decision.PaymentCost{Generic: 1, Mana: decision.ManaAmount{0, 1}},
			AutoFill: []state.ObjID{7, 8}},
	}
	cp := copyDecision(d)
	if cp == nil || cp.ManaPayment == nil || cp.ManaPayment.Card != 40 || cp.Options[0].Obj != 7 {
		t.Fatalf("projection lost the window: %#v", cp)
	}
	cp.ManaPayment.AutoFill[0] = 99
	cp.ManaPayment.Owed.Generic = 5
	if d.ManaPayment.AutoFill[0] != 7 || d.ManaPayment.Owed.Generic != 1 {
		t.Fatal("projected readout aliases the engine's decision")
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"mana_payment":{"card":40`, `"owed":{"generic":5`, `"autofill":[99,8]`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("wire %s lacks %s", raw, want)
		}
	}
}
