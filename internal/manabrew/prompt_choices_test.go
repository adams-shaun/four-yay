package manabrew

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
)

func TestChooseKinds(t *testing.T) {
	cases := []struct {
		name   string
		kind   string
		resume string
		opts   []decision.Option
		want   string
	}{
		{"look ack", "yes", "look_ack", []decision.Option{{Index: 0, Kind: "yes", Label: "Continue"}}, "chooseFromSelection"},
		{"damage split", "damage_split", "", []decision.Option{{Index: 0, Kind: "damage_split", Label: "A", Amount: 1}, {Index: 1, Kind: "damage_split", Label: "B", Amount: 1}}, "chooseFromSelection"},
		{"division", "division", "", []decision.Option{{Index: 0, Kind: "division", Label: "1 to Bear", Amount: 1}}, "chooseFromSelection"},
		{"name", "name", "", []decision.Option{{Index: 0, Kind: "name", Label: "Bear"}}, "chooseFromSelection"},
		{"type", "type", "", []decision.Option{{Index: 0, Kind: "type", Label: "Creature"}}, "chooseFromSelection"},
		{"number", "number", "", []decision.Option{{Index: 0, Kind: "number", Label: "2", Amount: 2}, {Index: 1, Kind: "number", Label: "3", Amount: 3}}, "chooseNumber"},
		{"x", "x", "", []decision.Option{{Index: 0, Kind: "x", Label: "X = 0", Amount: 0}, {Index: 1, Kind: "x", Label: "X = 1", Amount: 1}}, "chooseNumber"},
		{"color", "color", "", []decision.Option{{Index: 0, Kind: "color", Label: "White"}}, "chooseColor"},
		{"mana color", "mana", "", []decision.Option{{Index: 0, Kind: "mana", Label: "Add W", ManaSymbol: "W"}}, "chooseColor"},
		{"yes no", "yes", "", []decision.Option{{Index: 0, Kind: "yes", Label: "Yes"}, {Index: 1, Kind: "no", Label: "No"}}, "chooseBoolean"},
		{"unblocked", "asunblocked", "", []decision.Option{{Index: 0, Kind: "asunblocked", Label: "Unblocked"}}, "chooseBoolean"},
		{"card pick", "discard", "", []decision.Option{{Index: 0, Kind: "discard", Label: "Card", Obj: 9}}, "chooseCards"},
		{"exile", "exile", "", []decision.Option{{Index: 0, Kind: "exile", Label: "Card", Obj: 9}}, "chooseCards"},
		{"sacrifice", "sacrifice", "", []decision.Option{{Index: 0, Kind: "sacrifice", Label: "Card", Obj: 9}}, "chooseCards"},
		{"search", "search", "", []decision.Option{{Index: 0, Kind: "search", Label: "Card", Obj: 9}}, "chooseCards"},
		{"dig", "dig", "", []decision.Option{{Index: 0, Kind: "dig", Label: "Card", Obj: 9}}, "chooseCards"},
		{"keep", "keep", "", []decision.Option{{Index: 0, Kind: "keep", Label: "Card", Obj: 9}}, "chooseCards"},
		{"dungeon", "dungeon", "", []decision.Option{{Index: 0, Kind: "dungeon", Label: "Undercity"}}, "chooseFromSelection"},
		{"room", "room", "", []decision.Option{{Index: 0, Kind: "room", Label: "Secret"}}, "chooseFromSelection"},
		{"roll", "roll", "", []decision.Option{{Index: 0, Kind: "roll", Label: "4"}}, "chooseFromSelection"},
		{"generic choice", "choice", "", []decision.Option{{Index: 0, Kind: "choice", Label: "Choice"}}, "chooseFromSelection"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDec(8, 0, decision.KChoose, tc.opts...)
			d.ResumeKind = tc.resume
			d.Min = 1
			d.Max = 1
			p, err := New("t", 1, nil).Prompt(d, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := p.Input.Value.PromptType(); got != tc.want {
				t.Fatalf("prompt type=%s want %s", got, tc.want)
			}
			wire, err := json.Marshal(p.Input.Value)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(wire), `"description":"Choose 1 to 1."`) {
				t.Fatalf("choice constraints missing from presentation.description: %s", wire)
			}
			if tc.name == "number" {
				in := p.Input.Value.(mb.ChooseNumberInput)
				if in.Min != 2 || in.Max != 3 {
					t.Fatalf("number bounds = %d..%d, want 2..3", in.Min, in.Max)
				}
			}
		})
	}
}

func TestModes(t *testing.T) {
	d := newDec(2, 0, decision.KModes, decision.Option{Index: 0, Kind: "mode", Label: "Draw"}, decision.Option{Index: 1, Kind: "mode", Label: "Destroy"})
	d.Min, d.Max = 1, 1
	p, err := New("t", 1, nil).Prompt(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Input.Value.PromptType() != "chooseFromSelection" {
		t.Fatalf("got %s", p.Input.Value.PromptType())
	}
}
func TestUnlessPay(t *testing.T) {
	d := newDec(2, 0, decision.KModes, decision.Option{Index: 0, Kind: "unless_pay", Label: "Pay 3"}, decision.Option{Index: 1, Kind: "unless_decline", Label: "Decline"})
	d.Min, d.Max = 1, 1
	d.Options[0].Mode = decision.ModeUnlessPay
	d.Options[1].Mode = decision.ModeUnlessDecline
	p, err := New("t", 1, nil).Prompt(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Input.Value.PromptType() != "chooseBoolean" {
		t.Fatalf("got %s", p.Input.Value.PromptType())
	}
}
func TestReplacement(t *testing.T) {
	tr := New("t", 1, nil)
	for _, tc := range []struct {
		opts []decision.Option
		want string
	}{{[]decision.Option{{Index: 0, Kind: "replacement", Label: "A"}, {Index: 1, Kind: "replacement", Label: "B"}}, "chooseFromSelection"}, {[]decision.Option{{Index: 0, Kind: "mana", Label: "W", ManaSymbol: "W"}}, "chooseColor"}, {[]decision.Option{{Index: 0, Kind: "apply", Label: "Apply"}, {Index: 1, Kind: "decline", Label: "Decline"}}, "chooseBoolean"}} {
		d := newDec(3, 0, decision.KReplacement, tc.opts...)
		d.Min, d.Max = 1, 1
		p, err := tr.Prompt(d, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Input.Value.PromptType(); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}
func TestStartingPlayer(t *testing.T) {
	d := newDec(4, state.PlayerID(0), decision.KStartingPlayer, decision.Option{Index: 0, Kind: "player", Label: "Alice", Player: 0}, decision.Option{Index: 1, Kind: "player", Label: "Bob", Player: 1})
	d.Min, d.Max = 1, 1
	p, err := New("t", 1, nil).Prompt(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Input.Value.PromptType() != "chooseFromSelection" {
		t.Fatalf("got %s", p.Input.Value.PromptType())
	}
}
func TestCommanderZone(t *testing.T) {
	d := newDec(4, 0, decision.KCommanderZone, decision.Option{Index: 0, Kind: "command_zone", Label: "Command zone"}, decision.Option{Index: 1, Kind: "leave", Label: "Leave"})
	d.Min, d.Max = 1, 1
	p, err := New("t", 1, nil).Prompt(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	in, ok := p.Input.Value.(mb.ChooseBooleanInput)
	if !ok {
		t.Fatalf("got %T", p.Input.Value)
	}
	if in.ConfirmLabel != "Command zone" || in.DenyLabel != "Leave" {
		t.Fatalf("labels: %+v", in)
	}
}
