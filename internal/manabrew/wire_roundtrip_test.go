package manabrew

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// TestWireRoundTripThenTranslate is the MB-11 regression for the pointer/
// value mismatch normalize.go used to paper over: protocol/manabrew's
// promptOutputCases (and promptInputCases) construct pointers so
// json.Unmarshal has an addressable target, but every consumer in this
// package type-switches on the VALUE (out.(mb.PassOutput), not
// out.(*mb.PassOutput)). A test that builds a mb.ClientResponse by hand and
// calls TranslateResponse in-process never exercises Decode at all, so it
// never caught this; this test goes all the way through Encode -> Decode ->
// TranslateResponse, the same path a real ManaBrew client's bytes take.
func TestWireRoundTripThenTranslate(t *testing.T) {
	t.Run("pass/priority", func(t *testing.T) {
		tr := New("table", 2, nil)
		v := smallView()
		d := newDec(7, 1, decision.KPriority,
			decision.Option{Index: 0, Kind: "cast", Label: "Cast Shock", Obj: 2},
			decision.Option{Index: 1, Kind: "pass", Label: "Pass priority"},
			decision.Option{Index: 2, Kind: "concede", Label: "Concede"})
		p := pendingFor(d, v)

		client := mb.ClientMessage{Value: mb.ClientResponse{
			Kind:     "response",
			PromptID: p.Prompt.PromptID,
			Action: mb.PromptOutput{
				Type:   "chooseAction",
				Output: mb.PromptOutputData{Value: mb.PassOutput{ExhaustStack: true}},
			},
		}}

		wire, err := mb.Encode(client)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		var decoded mb.ClientMessage
		if unknown, err := mb.Decode(wire, &decoded); err != nil {
			t.Fatalf("Decode: %v", err)
		} else if len(unknown) != 0 {
			t.Fatalf("Decode reported unknown paths for a well-formed message: %v", unknown)
		}

		resp, ok := decoded.Value.(mb.ClientResponse)
		if !ok {
			t.Fatalf("decoded.Value is %T, want mb.ClientResponse", decoded.Value)
		}
		if _, ok := resp.Action.Output.Value.(mb.PassOutput); !ok {
			t.Fatalf("decoded output.Value is %T, want mb.PassOutput (a value, not a pointer)", resp.Action.Output.Value)
		}

		outcome := tr.TranslateResponse(decoded, p, 1)
		if outcome.Err != nil {
			t.Fatalf("unexpected error %s: %s", outcome.Err.Code, outcome.Err.Message)
		}
		if outcome.Intent == nil {
			t.Fatal("expected a pass intent")
		}
		if outcome.Policy == nil || !outcome.Policy.ExhaustStack {
			t.Fatalf("expected an exhaustStack policy, got %#v", outcome.Policy)
		}
		idx := passOptionIndex(d)
		if len(outcome.Intent.Choices) != 1 || outcome.Intent.Choices[0] != idx {
			t.Fatalf("intent choices = %v, want [%d]", outcome.Intent.Choices, idx)
		}
	})

	t.Run("boardTargets", func(t *testing.T) {
		tr := New("table", 2, nil)
		v := smallView()
		d := &decision.Decision{Seq: 12, Player: 1, Kind: decision.KTarget, Min: 1, Max: 1, Prompt: "Choose a target",
			TargetEffect: &decision.TargetEffect{API: "Destroy"},
			Options: []decision.Option{
				{Index: 0, Kind: "permanent", Label: "Bear", Obj: 2},
				{Index: 1, Kind: "player", Label: "Alice", Player: 0},
			}}
		p := pendingFor(d, v)

		client := mb.ClientMessage{Value: mb.ClientResponse{
			Kind:     "response",
			PromptID: p.Prompt.PromptID,
			Action: mb.PromptOutput{
				Type: "chooseBoardTargets",
				Output: mb.PromptOutputData{Value: mb.BoardTargetsDecision{
					Chosen: []mb.TargetRef{{Kind: mb.RefCard, ID: cardID(2)}},
				}},
			},
		}}

		wire, err := mb.Encode(client)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		var decoded mb.ClientMessage
		if _, err := mb.Decode(wire, &decoded); err != nil {
			t.Fatalf("Decode: %v", err)
		}

		resp, ok := decoded.Value.(mb.ClientResponse)
		if !ok {
			t.Fatalf("decoded.Value is %T, want mb.ClientResponse", decoded.Value)
		}
		if _, ok := resp.Action.Output.Value.(mb.BoardTargetsDecision); !ok {
			t.Fatalf("decoded output.Value is %T, want mb.BoardTargetsDecision (a value, not a pointer)", resp.Action.Output.Value)
		}

		outcome := tr.TranslateResponse(decoded, p, 1)
		if outcome.Err != nil {
			t.Fatalf("unexpected error %s: %s", outcome.Err.Code, outcome.Err.Message)
		}
		if outcome.Intent == nil {
			t.Fatal("expected a target intent")
		}
		if len(outcome.Intent.Choices) != 1 || outcome.Intent.Choices[0] != 0 {
			t.Fatalf("intent choices = %v, want [0] (the Bear)", outcome.Intent.Choices)
		}
	})
}
