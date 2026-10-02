//go:build manabrew

package manabrew

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// TestPayManaCostAnnounce covers the CR 601.2g announced window
// (rules/announce_pay.go's announcedManaWindowAsk): ManaPayment != nil, a
// "mana" option per (source, ability, colour), an "autofill" option and the
// unconditional "cancel_cast" tail -- no "done" here, since this fixture has
// no pay-life grant. Source points at a real battlefield permanent (the Bear)
// so findCard resolves it; a card mid-cast (on the stack, not yet on the
// battlefield) is a known gap findCard does not yet cover -- see the report.
func TestPayManaCostAnnounce(t *testing.T) {
	v := smallView()
	d := &decision.Decision{Seq: 1, Player: 1, Kind: decision.KChoose, Min: 1, Max: 1, Source: 2,
		Prompt:      "Pay for Bear",
		ManaPayment: &decision.ManaPaymentWindow{Card: 2, Cost: decision.PaymentCost{Generic: 1}, Owed: decision.PaymentCost{Generic: 1}},
		Options: []decision.Option{
			{Index: 0, Kind: "mana", Obj: 1, Ability: 0, ManaSymbol: "W", Label: "Add {W}"},
			{Index: 1, Kind: decision.OptAutoFill, Label: "Auto-fill: tap Island"},
			{Index: 2, Kind: decision.OptCancelCast, Label: "Cancel cast"},
		}}
	tr := New("table", 1, nil)

	msg, err := tr.promptPayment(d, &v)
	if err != nil {
		t.Fatalf("promptPayment: %v", err)
	}
	in, ok := msg.Input.Value.(mb.PayManaCostInput)
	if !ok {
		t.Fatalf("promptPayment did not build a payManaCost input: %#v", msg.Input.Value)
	}
	if in.CardID != cardID(2) {
		t.Fatalf("CardID = %q, want %q", in.CardID, cardID(2))
	}
	if in.CanConfirmFromPool {
		t.Fatal("CanConfirmFromPool must be false: this window offers no \"done\" option")
	}
	// Actions carries the per-step activation options: "mana" and "autofill",
	// never "cancel_cast" (answered through cancel, not act).
	if len(in.Actions) != 2 {
		t.Fatalf("Actions = %+v, want 2 entries (mana, autofill)", in.Actions)
	}
	if in.Actions[0].Type != "activateAbility" || !in.Actions[0].IsManaAbility {
		t.Fatalf("Actions[0] = %+v, want an activateAbility mana action", in.Actions[0])
	}
	if in.Actions[1].Type != "autofill" {
		t.Fatalf("Actions[1] = %+v, want autofill", in.Actions[1])
	}

	pending := &Pending{Prompt: msg, Decision: d}

	// "mana" -> act.
	if o := tr.parsePayManaCost(mb.ActOutput{ActionID: actionID(0)}, pending); o.Err != nil {
		t.Fatalf("act on mana: %s: %s", o.Err.Code, o.Err.Message)
	} else if o.Intent == nil || len(o.Intent.Choices) != 1 || o.Intent.Choices[0] != 0 {
		t.Fatalf("act on mana intent = %+v, want Choices [0]", o.Intent)
	}

	// "autofill" -> pay{auto:true}.
	if o := tr.parsePayManaCost(mb.PayOutput{Auto: true}, pending); o.Err != nil {
		t.Fatalf("pay auto: %s: %s", o.Err.Code, o.Err.Message)
	} else if o.Intent == nil || len(o.Intent.Choices) != 1 || o.Intent.Choices[0] != 1 {
		t.Fatalf("pay auto intent = %+v, want Choices [1]", o.Intent)
	}

	// pay{auto:false} -> invalidShape: this window offers no "done".
	if o := tr.parsePayManaCost(mb.PayOutput{Auto: false}, pending); o.Err == nil || o.Err.Code != mb.CodeInvalidShape {
		t.Fatalf("pay non-auto with no done option: want invalidShape, got %+v", o)
	}

	// "cancel_cast" -> cancel.
	if o := tr.parsePayManaCost(mb.CancelOutput{}, pending); o.Err != nil {
		t.Fatalf("cancel: %s: %s", o.Err.Code, o.Err.Message)
	} else if o.Intent == nil || len(o.Intent.Choices) != 1 || o.Intent.Choices[0] != 2 {
		t.Fatalf("cancel intent = %+v, want Choices [2]", o.Intent)
	}

	// A response of the wrong shape entirely.
	if o := tr.parsePayManaCost(mb.PassOutput{}, pending); o.Err == nil || o.Err.Code != mb.CodeWrongPromptType {
		t.Fatalf("wrong output type: want wrongPromptType, got %+v", o)
	}
}

// TestPayManaCostLegacy covers the pre-announce window (rules/cast.go's
// manaWindowAsk): ManaPayment nil, one "activate" option per untapped source
// and an unconditional "done" -- no autofill, no undo, and (G-3) no cancel:
// this window has none, so a cancel response must come back invalidShape,
// never succeed.
func TestPayManaCostLegacy(t *testing.T) {
	v := smallView()
	d := &decision.Decision{Seq: 2, Player: 1, Kind: decision.KChoose, Min: 1, Max: 1, Source: 2,
		Prompt: "Activate mana abilities to pay for Bear",
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: 1, Label: "Activate Island for mana"},
			{Index: 1, Kind: "done", Label: "Done"},
		}}
	tr := New("table", 1, nil)

	msg, err := tr.promptPayment(d, &v)
	if err != nil {
		t.Fatalf("promptPayment: %v", err)
	}
	in, ok := msg.Input.Value.(mb.PayManaCostInput)
	if !ok {
		t.Fatalf("promptPayment did not build a payManaCost input: %#v", msg.Input.Value)
	}
	if !in.CanConfirmFromPool {
		t.Fatal("CanConfirmFromPool must be true: the legacy window always offers \"done\"")
	}
	if len(in.Actions) != 1 || in.Actions[0].Type != "activateAbility" {
		t.Fatalf("Actions = %+v, want exactly one activateAbility action", in.Actions)
	}

	pending := &Pending{Prompt: msg, Decision: d}

	// "activate" -> act.
	if o := tr.parsePayManaCost(mb.ActOutput{ActionID: actionID(0)}, pending); o.Err != nil {
		t.Fatalf("act on activate: %s: %s", o.Err.Code, o.Err.Message)
	} else if o.Intent == nil || len(o.Intent.Choices) != 1 || o.Intent.Choices[0] != 0 {
		t.Fatalf("act on activate intent = %+v, want Choices [0]", o.Intent)
	}

	// "done" -> pay{auto:false}.
	if o := tr.parsePayManaCost(mb.PayOutput{Auto: false}, pending); o.Err != nil {
		t.Fatalf("pay done: %s: %s", o.Err.Code, o.Err.Message)
	} else if o.Intent == nil || len(o.Intent.Choices) != 1 || o.Intent.Choices[0] != 1 {
		t.Fatalf("pay done intent = %+v, want Choices [1]", o.Intent)
	}

	// pay{auto:true} -> invalidShape: this window has no autofill.
	if o := tr.parsePayManaCost(mb.PayOutput{Auto: true}, pending); o.Err == nil || o.Err.Code != mb.CodeInvalidShape {
		t.Fatalf("pay auto with no autofill: want invalidShape, got %+v", o)
	}

	// cancel -> invalidShape (G-3): the legacy window has no cancel.
	if o := tr.parsePayManaCost(mb.CancelOutput{}, pending); o.Err == nil || o.Err.Code != mb.CodeInvalidShape {
		t.Fatalf("cancel on the legacy window: want invalidShape (G-3), got %+v", o)
	}
}
