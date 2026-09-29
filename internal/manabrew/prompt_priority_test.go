package manabrew

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// cardsManaProd is the projection one Island face carries: one white unit
// (state.ManaIndex order W,U,B,R,G,C).
var cardsManaProd = cards.ManaProduction{Colour: [6]int32{1, 0, 0, 0, 0, 0}}

// TestChooseActionPromptAdvertisesActions pins the §6.3 chooseAction build:
// pass and concede are never actions, the other option kinds become the
// typed actions, the payment actions ride as pay-<ID> casts, and the
// announcing shape names the source card.
func TestChooseActionPromptAdvertisesActions(t *testing.T) {
	d := &decision.Decision{Seq: 41, Player: 1, Kind: decision.KPriority, Min: 1, Max: 1,
		PaymentActions: []decision.PaymentAction{{ID: "mc1", Label: "Cast Shock", Cast: decision.PlannedCast{Object: 2}}},
		Options: []decision.Option{
			{Index: 0, Kind: "pass", Label: "Pass priority"},
			{Index: 1, Kind: "cast", Label: "Cast Shock", Obj: 2},
			{Index: 2, Kind: "play_land", Label: "Play Island", Obj: 1},
			{Index: 3, Kind: "activate", Label: "{T}: Add {W}", Obj: 1, Ability: 0},
			{Index: 4, Kind: "ability", Label: "Scry 1", Obj: 6, Ability: 2},
			{Index: 5, Kind: "concede", Label: "Concede"},
		},
		Source: 2}
	v := battleView()
	msg := pendingMustBuild(t, d, v)
	if msg.PromptID != promptID(d) {
		t.Fatalf("promptId = %d, want %d", msg.PromptID, promptID(d))
	}
	if msg.DecidingPlayerID != "player-1" {
		t.Fatalf("decidingPlayerId = %q, want player-1", msg.DecidingPlayerID)
	}
	in, ok := msg.Input.Value.(mb.ChooseActionInput)
	if !ok {
		t.Fatalf("input type = %s, want chooseAction", msg.Input.Value.PromptType())
	}
	// Precondition: the option kinds above are all distinct and the fixture
	// view resolves the Island and the Shock spell.
	if findCard(&v, 1) == nil || findCard(&v, 2) == nil || findCard(&v, 6) == nil {
		t.Fatalf("fixture view must resolve objects 1, 2 and 6: %#v", v)
	}
	var ids []string
	for _, a := range in.Actions {
		ids = append(ids, a.ID)
	}
	want := []string{"opt-1", "opt-2", "opt-3", "opt-4", "pay-mc1"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("advertised actions = %v, want %v (pass and concede must be absent)", ids, want)
	}
	acts := make(map[string]mb.AvailableAction)
	for _, a := range in.Actions {
		acts[a.ID] = a
	}
	a := acts["opt-1"]
	if a.Type != "cast" || a.CardID != "o2" || a.Mode != "" || a.Label != "Cast Shock" {
		t.Fatalf("cast action: %#v", a)
	}
	a = acts["opt-2"]
	if a.Type != "cast" || a.CardID != "o1" || a.Mode != "play" {
		t.Fatalf("play_land action must be a cast with mode play: %#v", a)
	}
	a = acts["opt-3"]
	if a.Type != "activateAbility" || !a.IsManaAbility || a.CardID != "o1" {
		t.Fatalf("mana activate: %#v", a)
	}
	// The Island's projection says one white unit; the action must carry it.
	if len(a.ProducedMana) != 1 || a.ProducedMana[0] != (mb.Mana{Color: mb.ColorWhite, Amount: 1}) {
		t.Fatalf("producedMana = %v, want [{W 1}]", a.ProducedMana)
	}
	a = acts["opt-4"]
	if a.Type != "activateAbility" || a.IsManaAbility || a.AbilityIndex != 2 {
		t.Fatalf("ability action: %#v", a)
	}
	a = acts["pay-mc1"]
	if a.Type != "cast" || a.CardID != "o2" || a.Label != "Cast Shock" {
		t.Fatalf("payment action: %#v", a)
	}
	if msg.SourceCard == nil || msg.SourceCard.ID != "o2" {
		t.Fatalf("sourceCard must name the acting source (o2): %#v", msg.SourceCard)
	}
}

// TestChooseActionResponses maps the three outputs and the announce arm.
func TestChooseActionResponses(t *testing.T) {
	tr := New("table", 2, nil)
	v := smallView()
	d := &decision.Decision{Seq: 9, Player: 1, Kind: decision.KPriority, Min: 1, Max: 1,
		PaymentActions: []decision.PaymentAction{{ID: "mc1", Cast: decision.PlannedCast{Object: 2}, Plans: []decision.PaymentPlan{{}}}},
		Options: []decision.Option{
			{Index: 0, Kind: "cast", Label: "Cast Shock", Obj: 2},
			{Index: 1, Kind: "activate", Label: "{T}: Add {W}", Obj: 1},
			{Index: 2, Kind: "pass", Label: "Pass priority"},
			{Index: 3, Kind: "concede", Label: "Concede"},
		}}
	p := pendingFor(d, v)

	// act opt-0: an ordinary cast answer.
	in := mustIntent(t, tr.TranslateResponse(respFor(p, mb.ActOutput{ActionID: "opt-0"}), p, 1))
	if in.Seq != 9 || in.Player != 1 || !reflect.DeepEqual(in.Choices, []int{0}) {
		t.Fatalf("act intent = %+v", in)
	}
	// act opt-1: an ability activation.
	in = mustIntent(t, tr.TranslateResponse(respFor(p, mb.ActOutput{ActionID: "opt-1"}), p, 1))
	if !reflect.DeepEqual(in.Choices, []int{1}) {
		t.Fatalf("ability intent = %+v", in)
	}
	// act pay-mc1: the announce arm (§6.1 pay-<ID> → Intent.Announce).
	in = mustIntent(t, tr.TranslateResponse(respFor(p, mb.ActOutput{ActionID: "pay-mc1"}), p, 1))
	if in.Announce == nil || in.Announce.ActionID != "mc1" || in.Payment != nil || len(in.Choices) != 0 {
		t.Fatalf("announce intent = %+v", in)
	}
	if err := d.Validate(*in); err != nil {
		t.Fatalf("announce intent must pass Validate: %v", err)
	}
	// restoreSnapshot with the current promptId is an undo request; anything
	// else is invalidShape.
	if o := tr.TranslateResponse(respFor(p, mb.RestoreSnapshotOutput{CheckpointID: p.Prompt.PromptID}), p, 1); o.Undo == nil {
		t.Fatalf("restoreSnapshot at the open prompt: want an undo request, got %v", o)
	}
	wantErrCode(t, tr.TranslateResponse(respFor(p, mb.RestoreSnapshotOutput{CheckpointID: 12345}), p, 1), mb.CodeInvalidShape)
}

// TestChooseActionConcedeDirective maps the G-2 queue: the concession is
// answered immediately only at an open priority decision for the seat;
// otherwise it queues.
func TestChooseActionConcedeDirective(t *testing.T) {
	tr := New("table", 2, nil)
	v := smallView()
	pd := &decision.Decision{Seq: 9, Player: 1, Kind: decision.KPriority, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "pass", Label: "Pass priority"},
		{Index: 1, Kind: "concede", Label: "Concede"},
	}}
	p := pendingFor(pd, v)
	o := tr.TranslateResponse(dirFor(), p, 1)
	in := mustIntent(t, o)
	if o.Queued {
		t.Fatal("an open priority ask for the seat must be answered, not queued")
	}
	if !reflect.DeepEqual(in.Choices, []int{1}) {
		t.Fatalf("concede intent = %+v, want the concede option", in)
	}
	if err := pd.Validate(*in); err != nil {
		t.Fatalf("concede intent must pass Validate: %v", err)
	}
	// Non-priority ask open: queued.
	td := &decision.Decision{Seq: 10, Player: 1, Kind: decision.KTarget, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "permanent", Label: "Bear", Obj: 2}}}
	tp := pendingFor(td, v)
	if o := tr.TranslateResponse(dirFor(), tp, 1); !o.Queued || o.Intent != nil {
		t.Fatalf("concede at a non-priority ask must queue, got %v", o)
	}
	// Nothing pending: queued, not an error (G-2's whole point).
	if o := tr.TranslateResponse(dirFor(), nil, 1); !o.Queued || o.Err != nil {
		t.Fatalf("concede with nothing pending must queue, got %v", o)
	}
	// A seat that is not the deciding player queues too (the transport only
	// answers its own seat).
	if o := tr.TranslateResponse(dirFor(), p, 0); !o.Queued {
		t.Fatalf("concede from the wrong seat must queue, got %v", o)
	}
	// ConcedeIntent itself rejects a non-priority decision.
	if _, err := ConcedeIntent(td); err == nil {
		t.Fatal("ConcedeIntent must refuse a non-priority decision")
	}
}

// TestPassPolicy exercises the §6.5 auto-pass rule: the until stop, the
// exhaustStack stop and its early-stop edge, the clearing edges, and the
// AutoPass arm's intent shape.
func TestPassPolicy(t *testing.T) {
	tr := New("table", 2, nil)
	v := smallView()

	// until{player-1, main1}: pass while Bob is not the active player in
	// main1; stop exactly there.
	pol, err := newPassPolicy(mb.PassOutput{Until: &mb.PassUntil{PlayerID: "player-1", Phase: "main1"}}, v)
	if err != nil {
		t.Fatal(err)
	}
	pd := &decision.Decision{Seq: 2, Player: 1, Kind: decision.KPriority, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "pass", Label: "Pass priority"}}}
	if !pol.ShouldPass(pd, v) {
		t.Fatal("policy must still pass before its stop point")
	}
	stopped := v
	stopped.Active = 1
	if pol.ShouldPass(pd, stopped) {
		t.Fatal("policy must stop when the view shows player-1 active in main1")
	}
	phaseOnly := stopped
	phaseOnly.Step = "combat"
	if !pol.ShouldPass(pd, phaseOnly) {
		t.Fatal("active player matching but a different step must not stop the policy")
	}
	// The AutoPass arm returns an ordinary pass intent.
	in, ok := tr.AutoPass(&pol, pd, v)
	if !ok || !reflect.DeepEqual(in.Choices, []int{0}) || in.Seq != 2 {
		t.Fatalf("AutoPass = %+v, ok %v", in, ok)
	}

	// exhaustStack: pass until the stack empties; a NEW stack object stops
	// it early (§6.5).
	stackV := smallView()
	stackV.Stack = []view.StackView{{ID: 9}, {ID: 10}}
	sp, err := newPassPolicy(mb.PassOutput{ExhaustStack: true}, stackV)
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.stackAtPass) != 2 {
		t.Fatalf("exhaustStack must snapshot the stack at pass time, got %v", sp.stackAtPass)
	}
	one := stackV
	one.Stack = []view.StackView{{ID: 10}}
	if !sp.ShouldPass(pd, one) {
		t.Fatal("one resolving object is not an empty stack")
	}
	empty := stackV
	empty.Stack = nil
	if sp.ShouldPass(pd, empty) {
		t.Fatal("policy must stop when the stack is empty")
	}
	// Early stop: an object that was not on the stack when the pass was
	// sent appears.
	grew := stackV
	grew.Stack = []view.StackView{{ID: 9}, {ID: 10}, {ID: 11}}
	if sp.ShouldPass(pd, grew) {
		t.Fatal("a new stack object must stop the policy early")
	}

	// Clearing edges: a plain pass installs an inactive policy; a
	// non-priority prompt stops any policy.
	plain, err := newPassPolicy(mb.PassOutput{}, v)
	if err != nil {
		t.Fatal(err)
	}
	if plain.ShouldPass(pd, v) {
		t.Fatal("a plain pass must install no policy")
	}
	td := &decision.Decision{Seq: 3, Player: 1, Kind: decision.KTarget, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "permanent", Label: "Bear", Obj: 2}}}
	if pol.ShouldPass(td, v) || sp.ShouldPass(td, v) {
		t.Fatal("a non-priority prompt must clear the policy")
	}
	if _, ok := tr.AutoPass(&pol, td, v); ok {
		t.Fatal("AutoPass must decline a non-priority prompt")
	}

	// A malformed until clause is invalidShape.
	if _, err := newPassPolicy(mb.PassOutput{Until: &mb.PassUntil{PlayerID: "o2", Phase: "main1"}}, v); err == nil {
		t.Fatal("until playerId must be a player id")
	}
	if _, err := newPassPolicy(mb.PassOutput{Until: &mb.PassUntil{PlayerID: "player-1", Phase: ""}}, v); err == nil {
		t.Fatal("until phase is required")
	}
}

// TestPassResponseCarriesPolicy proves the pass output maps to an ordinary
// pass intent AND installs the policy, and that a pass response to a prompt
// with no pass option is rejected (an engine priority ask always offers
// pass; a hand-built decision would be a mapping bug).
func TestPassPolicyResponseCarriesPolicy(t *testing.T) {
	tr := New("table", 2, nil)
	v := smallView()
	d := &decision.Decision{Seq: 5, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "pass", Label: "Pass priority"},
		{Index: 1, Kind: "concede", Label: "Concede"},
	}}
	p := pendingFor(d, v)
	o := tr.TranslateResponse(respFor(p, mb.PassOutput{ExhaustStack: true}), p, 0)
	in := mustIntent(t, o)
	if !reflect.DeepEqual(in.Choices, []int{0}) {
		t.Fatalf("pass intent = %+v", in)
	}
	if o.Policy == nil || !o.Policy.ExhaustStack || o.Policy.ShouldPass(d, v) == false {
		t.Fatalf("pass response must install the exhaustStack policy, got %+v", o.Policy)
	}
	// A priority decision without a pass option: pass is refused.
	noPass := &decision.Decision{Seq: 5, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "concede", Label: "Concede"}}}
	np := pendingFor(noPass, v)
	wantErrCode(t, tr.TranslateResponse(respFor(np, mb.PassOutput{}), np, 0), mb.CodeInvalidShape)
}
