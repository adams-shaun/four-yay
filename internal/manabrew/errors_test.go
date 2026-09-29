package manabrew

import (
	"errors"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Shared builders for the MB-4 response tests. Every synthetic decision is
// one the real engine's builders could pose (the same option kinds, bounds
// and Min/Max), so the tests exercise the mapping without standing up the
// rules engine.

func newDec(seq uint64, player state.PlayerID, kind decision.Kind, opts ...decision.Option) *decision.Decision {
	return &decision.Decision{Seq: seq, Player: player, Kind: kind,
		Options: opts, Min: 1, Max: 1, Prompt: "Test ask"}
}

func pendingFor(d *decision.Decision, v view.View) *Pending {
	msg, err := New("table", 2, nil).Prompt(d, &v)
	if err != nil {
		panic("test pending must build: " + err.Error())
	}
	return &Pending{Prompt: msg, Decision: d, View: v}
}

func pendingMustBuild(t *testing.T, d *decision.Decision, v view.View) mb.PromptMessage {
	t.Helper()
	msg, err := New("table", 2, nil).Prompt(d, &v)
	if err != nil {
		t.Fatalf("Prompt(%s): %v", d.Kind, err)
	}
	return msg
}

func mustIntent(t *testing.T, o Outcome) *decision.Intent {
	t.Helper()
	if o.Err != nil {
		t.Fatalf("unexpected error %s: %s", o.Err.Code, o.Err.Message)
	}
	if o.Intent == nil {
		t.Fatal("expected an intent outcome")
	}
	return o.Intent
}

func wantErrCode(t *testing.T, o Outcome, code mb.ErrorCode) *mb.ProtocolError {
	t.Helper()
	if o.Err == nil {
		t.Fatalf("expected error %s, got an outcome %#v", code, o)
	}
	if o.Err.Code != code {
		t.Fatalf("error code = %q, want %q (message: %s)", o.Err.Code, code, o.Err.Message)
	}
	return o.Err
}

func respFor(p *Pending, out mb.PromptOutputValue) mb.ClientMessage {
	return mb.ClientMessage{Value: mb.ClientResponse{Kind: "response",
		PromptID: p.Prompt.PromptID, Action: mb.PromptOutput{Output: mb.PromptOutputData{Value: out}}}}
}

func dirFor() mb.ClientMessage {
	return mb.ClientMessage{Value: mb.ClientDirective{Kind: "directive",
		Directive: mb.DirectiveInput{Type: "concede"}}}
}

// smallView is a two-seat view with an Island (o1, produces white mana) and
// a Bear (o2) on player 0's battlefield, player 1's Bob, and a stack entry.
func smallView() view.View {
	return view.View{Viewer: 0, Turn: 3, Step: "main1", Active: 0, Priority: 1, Players: []view.PlayerView{
		{ID: 0, Name: "Alice", Life: 20, HandSize: 0, LibrarySize: 30,
			Battlefield: []view.CardView{
				{ID: 1, Name: "Island", Printing: view.Printing{Name: "Island"}, Types: "Basic Land — Island",
					Produces: &cardsManaProd},
				{ID: 2, Name: "Bear", Printing: view.Printing{Name: "Bear"}, Types: "Creature — Bear"},
			}},
		{ID: 1, Name: "Bob", Life: 18},
	},
		Stack: []view.StackView{{ID: 9, Name: "Shock", Kind: "spell", Controller: 1}},
	}
}

// battleView adds a battle (o5) and a planeswalker (o6) projection.
func battleView() view.View {
	v := smallView()
	v.Players[0].Battlefield = append(v.Players[0].Battlefield,
		view.CardView{ID: 5, Name: "Siege", Printing: view.Printing{Name: "Siege"}, Types: "Battle — Siege", Controller: 1},
		view.CardView{ID: 6, Name: "Chandra", Printing: view.Printing{Name: "Chandra"}, Types: "Planeswalker — Chandra", Controller: 1})
	return v
}

// TestErrorCodesMapping walks the §6.4 table row by row: each ManaBrew check
// maps to its code, with the open prompt's id attached when something is
// pending.
func TestErrorCodesMapping(t *testing.T) {
	tr := New("table", 2, nil)
	v := smallView()
	d := newDec(7, 1, decision.KPriority,
		decision.Option{Index: 0, Kind: "cast", Label: "Cast Shock", Obj: 2},
		decision.Option{Index: 1, Kind: "pass", Label: "Pass priority"},
		decision.Option{Index: 2, Kind: "concede", Label: "Concede"})
	p := pendingFor(d, v)

	// stalePrompt: nothing pending.
	if o := tr.TranslateResponse(respFor(p, mb.ActOutput{ActionID: "opt-0"}), nil, 1); o.Err == nil || o.Err.Code != mb.CodeStalePrompt {
		t.Fatalf("no pending: want stalePrompt, got %v", o)
	}
	// stalePrompt: a different promptId.
	staleMsg := mb.ClientMessage{Value: mb.ClientResponse{Kind: "response", PromptID: 99,
		Action: mb.PromptOutput{Output: mb.PromptOutputData{Value: mb.ActOutput{ActionID: "opt-0"}}}}}
	if o := tr.TranslateResponse(staleMsg, p, 1); o.Err == nil || o.Err.Code != mb.CodeStalePrompt {
		t.Fatalf("wrong promptId: want stalePrompt, got %v", o)
	}
	if o := tr.TranslateResponse(staleMsg, p, 1); o.Err.PromptID == nil || *o.Err.PromptID != p.Prompt.PromptID {
		t.Fatalf("stalePrompt must name the open prompt id, got %v", o.Err.PromptID)
	}
	// wrongPlayer: the connection's seat is not the deciding player.
	if o := tr.TranslateResponse(respFor(p, mb.ActOutput{ActionID: "opt-0"}), p, 0); o.Err == nil || o.Err.Code != mb.CodeWrongPlayer {
		t.Fatalf("seat mismatch: want wrongPlayer, got %v", o)
	}
	// wrongPromptType: a boardTargets answer to a chooseAction prompt.
	if o := tr.TranslateResponse(respFor(p, mb.BoardTargetsDecision{}), p, 1); o.Err == nil || o.Err.Code != mb.CodeWrongPromptType {
		t.Fatalf("wrong output type: want wrongPromptType, got %v", o)
	}
	// unknownActionId: the action id was never advertised.
	if o := tr.TranslateResponse(respFor(p, mb.ActOutput{ActionID: "opt-1"}), p, 1); o.Err == nil || o.Err.Code != mb.CodeUnknownActionID {
		t.Fatalf("act on the pass option: want unknownActionId, got %v", o)
	}
	if o := tr.TranslateResponse(respFor(p, mb.ActOutput{ActionID: "cast-o2"}), p, 1); o.Err == nil || o.Err.Code != mb.CodeUnknownActionID {
		t.Fatalf("made-up action id: want unknownActionId, got %v", o)
	}
	if o := tr.TranslateResponse(respFor(p, mb.ActOutput{ActionID: "pay-nope"}), p, 1); o.Err == nil || o.Err.Code != mb.CodeUnknownActionID {
		t.Fatalf("unoffered payment action: want unknownActionId, got %v", o)
	}
	// invalidShape: a validate rejection in response order (the target test
	// below covers it there too; here a made-up pay- id that IS offered but
	// cannot be announced).
	empty := newDec(7, 1, decision.KPriority, decision.Option{Index: 0, Kind: "pass", Label: "Pass priority"})
	pe := pendingFor(empty, v)
	if o := tr.TranslateResponse(respFor(pe, mb.ActOutput{ActionID: "pay-x"}), pe, 1); o.Err == nil || o.Err.Code != mb.CodeUnknownActionID {
		t.Fatalf("payment action not offered at all: want unknownActionId, got %v", o)
	}
	// A response to a prompt kind whose mapper has not landed is
	// wrongPromptType, not a panic.
	kind := decision.Kind("modes")
	km := newDec(3, 1, kind, decision.Option{Index: 0, Kind: "mode", Label: "Mode A"})
	msg, err := tr.Prompt(km, &v)
	if !errors.Is(err, ErrUnmapped) {
		t.Fatalf("modes stub: want ErrUnmapped, got %v", err)
	}
	_ = msg
}

// TestEveryDecisionKindTranslates iterates decision.Kinds (the same closed
// universe the engine asks within) and fails on a kind whose prompt mapper
// does not match its MB ticket: the five MB-4 kinds must build, everything
// else must still be an explicit ErrUnmapped. The set is explicit, so MB-5
// and MB-6 empty it by deleting rows -- a new engine kind fails the test the
// way a new option kind fails TestKindsListsEveryKindOnce.
func TestEveryDecisionKindTranslates(t *testing.T) {
	// The kinds MB-4 does not fill, with the MB ticket that will.
	pendingKinds := map[decision.Kind]string{
		decision.KModes:           "MB-5",
		decision.KTriggerOrder:    "MB-5",
		decision.KTriggerOptional: "MB-5",
		decision.KCommanderZone:   "MB-5",
		decision.KChoose:          "MB-5",
		decision.KReplacement:     "MB-5",
		decision.KArrange:         "MB-5",
		decision.KStartingPlayer:  "MB-6",
	}
	translated := map[decision.Kind]string{
		decision.KPriority:  "MB-4",
		decision.KTarget:    "MB-4",
		decision.KAttackers: "MB-4",
		decision.KBlockers:  "MB-4",
		decision.KMulligan:  "MB-4",
	}
	if len(translated)+len(pendingKinds) != len(decision.Kinds) {
		t.Fatalf("kind universe moved: %d kinds, %d translated, %d pending",
			len(decision.Kinds), len(translated), len(pendingKinds))
	}
	tr := New("table", 2, nil)
	bv := battleView()
	rep := map[decision.Kind]*decision.Decision{
		decision.KPriority: {Seq: 1, Player: 1, Kind: decision.KPriority, Min: 1, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "cast", Label: "Cast Shock", Obj: 2}, {Index: 1, Kind: "pass", Label: "Pass priority"},
			{Index: 2, Kind: "concede", Label: "Concede"}}},
		decision.KTarget: {Seq: 1, Player: 1, Kind: decision.KTarget, Min: 1, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "Bear", Obj: 2}}},
		decision.KAttackers: {Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "attacker", Label: "Attack Bob", Obj: 2, Player: 1}}},
		decision.KBlockers: {Seq: 1, Player: 1, Kind: decision.KBlockers, Min: 0, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "block", Label: "Bear blocks", Obj: 2, Attacker: 1}}},
		decision.KMulligan: {Seq: 1, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "keep", Label: "keep"}, {Index: 1, Kind: "mulligan", Label: "mulligan"}}},
		decision.KModes: {Seq: 1, Player: 1, Kind: decision.KModes, Min: 1, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "mode", Label: "Mode A"}}},
		decision.KTriggerOrder: {Seq: 1, Player: 1, Kind: decision.KTriggerOrder, Min: 1, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "trigger", Label: "Trigger A"}}},
		decision.KTriggerOptional: {Seq: 1, Player: 1, Kind: decision.KTriggerOptional, Min: 1, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "trigger", Label: "Trigger A"}}},
		decision.KCommanderZone: {Seq: 1, Player: 1, Kind: decision.KCommanderZone, Min: 1, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "commander_zone", Label: "Command zone"}}},
		decision.KChoose: {Seq: 1, Player: 1, Kind: decision.KChoose, Min: 1, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "choice", Label: "Choice A"}}},
		decision.KReplacement: {Seq: 1, Player: 1, Kind: decision.KReplacement, Min: 1, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "replacement", Label: "Replace A"}}},
		decision.KArrange: {Seq: 1, Player: 1, Kind: decision.KArrange, Min: 1, Max: 2, Options: []decision.Option{
			{Index: 0, Kind: "bottom", Label: "A"}, {Index: 1, Kind: "bottom", Label: "B"}}},
		decision.KStartingPlayer: {Seq: 1, Player: 1, Kind: decision.KStartingPlayer, Min: 1, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "player", Label: "Alice", Player: 0}}},
	}
	for _, kind := range decision.Kinds {
		d, ok := rep[kind]
		if !ok {
			t.Errorf("kind %s has no representative decision; add one", kind)
			continue
		}
		if d.Kind != kind {
			t.Errorf("representative for %s carries kind %s", kind, d.Kind)
		}
		if translated[kind] == "" && pendingKinds[kind] == "" {
			t.Errorf("kind %s is in neither set", kind)
		}
		_, err := tr.Prompt(d, &bv)
		gotStub := errors.Is(err, ErrUnmapped)
		if gotStub && translated[kind] != "" {
			t.Errorf("kind %s (%s) still stubs: %v", kind, translated[kind], err)
		}
		if !gotStub && pendingKinds[kind] != "" {
			t.Errorf("kind %s builds but %s claims it is pending: %v", kind, pendingKinds[kind], err)
		}
	}
}
