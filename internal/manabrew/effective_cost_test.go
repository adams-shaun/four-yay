//go:build manabrew

package manabrew

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

func TestVisibleCardEffectiveManaCost(t *testing.T) {
	tr := New("table", 1, nil)
	cv := view.CardView{ID: 54, ManaCost: "1", EffectiveManaCost: "2", Printing: view.Printing{Name: "Aether Vial"}}
	got, ok := tr.visibleCard(cv).Value.(mb.VisibleCard)
	if !ok {
		t.Fatalf("visibleCard returned %T, want visible DTO", tr.visibleCard(cv).Value)
	}
	if got.CardDto.ManaCost != "1" || got.CardDto.EffectiveManaCost != "2" {
		t.Fatalf("CardDto printed/effective costs = %q/%q, want 1/2", got.CardDto.ManaCost, got.CardDto.EffectiveManaCost)
	}
	wire, err := json.Marshal(got.CardDto)
	if err != nil {
		t.Fatalf("marshal DTO: %v", err)
	}
	if !strings.Contains(string(wire), `"effectiveManaCost":"2"`) {
		t.Fatalf("wire DTO omitted effective cost: %s", wire)
	}
	untaxed := tr.visibleCard(view.CardView{ID: 55, ManaCost: "1", Printing: view.Printing{Name: "Aether Vial"}}).Value.(mb.VisibleCard).CardDto
	wire, err = json.Marshal(untaxed)
	if err != nil {
		t.Fatalf("marshal untaxed DTO: %v", err)
	}
	if strings.Contains(string(wire), "effectiveManaCost") {
		t.Fatalf("untaxed DTO should omit effective cost: %s", wire)
	}
}

func TestPayManaCostAnnounceUsesOwedCost(t *testing.T) {
	v := smallView()
	d := &decision.Decision{Seq: 31, Player: 1, Kind: decision.KChoose, Min: 1, Max: 1, Source: 2,
		ManaPayment: &decision.ManaPaymentWindow{Card: 2, Cost: decision.PaymentCost{Generic: 2}, Owed: decision.PaymentCost{Generic: 1}},
		Options:     []decision.Option{{Index: 0, Kind: "done"}}}
	msg, err := New("table", 1, nil).promptPayment(d, &v)
	if err != nil {
		t.Fatalf("promptPayment: %v", err)
	}
	in, ok := msg.Input.Value.(mb.PayManaCostInput)
	if !ok {
		t.Fatalf("prompt input = %T, want PayManaCostInput", msg.Input.Value)
	}
	if in.ManaCost != "2" {
		t.Fatalf("ManaCost = %q, want total effective Cost 2 (Owed is partial payment 1)", in.ManaCost)
	}
}
