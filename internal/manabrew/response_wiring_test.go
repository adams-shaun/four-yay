//go:build manabrew

package manabrew

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// This file closes the gap the controller found after MB-4/5/6 landed:
// translateResponse (errors.go) only routed the six MB-4 prompt types, so
// every prompt type MB-5/6's own builders emit (promptChoose's five
// wire shapes, promptArrange's two, promptOrder's one, promptPayment's one)
// fell into the "has no response mapping" default arm -- a client could
// receive the prompt but never have its answer accepted. Each test below
// goes through the one real entry point, Translator.TranslateResponse, not
// the per-kind parser directly, so it proves the routing as well as the
// parsing.

// TestChooseNumberRoundTrip covers both promptChoose shapes that build
// chooseNumber: a contiguous "x" range and a contiguous "number" range.
func TestChooseNumberRoundTrip(t *testing.T) {
	d := newDec(1, 0, decision.KChoose,
		decision.Option{Index: 0, Kind: "x", Label: "X = 0", Amount: 0},
		decision.Option{Index: 1, Kind: "x", Label: "X = 1", Amount: 1},
		decision.Option{Index: 2, Kind: "x", Label: "X = 2", Amount: 2})
	d.Min, d.Max = 1, 1
	p := pendingFor(d, view.View{})
	if p.Prompt.Input.Value.PromptType() != "chooseNumber" {
		t.Fatalf("prompt type = %s, want chooseNumber", p.Prompt.Input.Value.PromptType())
	}
	tr := New("table", 1, nil)
	two := 2
	o := tr.TranslateResponse(respFor(p, mb.NumberDecision{ChosenNumber: &two}), p, 0)
	in := mustIntent(t, o)
	if err := d.Validate(*in); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 2 {
		t.Fatalf("choices = %v, want [2] (the X=2 option)", in.Choices)
	}

	// An unoffered value is invalidShape, not a panic or a wrong pick.
	nine := 9
	o = tr.TranslateResponse(respFor(p, mb.NumberDecision{ChosenNumber: &nine}), p, 0)
	wantErrCode(t, o, mb.CodeInvalidShape)

	// A nil chosenNumber (the documented "no answer" null) is invalidShape
	// too: this package never offers a cancellable chooseNumber.
	o = tr.TranslateResponse(respFor(p, mb.NumberDecision{}), p, 0)
	wantErrCode(t, o, mb.CodeInvalidShape)
}

// TestChooseCardsRoundTrip covers a discard-style chooseCards ask: two
// offered cards, answered by their wire card ids.
func TestChooseCardsRoundTrip(t *testing.T) {
	d := newDec(2, 0, decision.KChoose,
		decision.Option{Index: 0, Kind: "discard", Label: "Island", Obj: 1},
		decision.Option{Index: 1, Kind: "discard", Label: "Bear", Obj: 2})
	d.Min, d.Max = 1, 1
	v := smallView()
	p := pendingFor(d, v)
	in, ok := p.Prompt.Input.Value.(mb.ChooseCardsInput)
	if !ok {
		t.Fatalf("prompt type = %T, want ChooseCardsInput", p.Prompt.Input.Value)
	}
	if len(in.Cards) != 2 {
		t.Fatalf("cards = %v, want 2", in.Cards)
	}
	tr := New("table", 1, nil)
	o := tr.TranslateResponse(respFor(p, mb.ChooseCardsDecision{ChosenCardIDs: []string{cardID(2)}}), p, 0)
	intent := mustIntent(t, o)
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(intent.Choices) != 1 || intent.Choices[0] != 1 {
		t.Fatalf("choices = %v, want [1] (the Bear option)", intent.Choices)
	}

	// A card id never offered is invalidShape.
	o = tr.TranslateResponse(respFor(p, mb.ChooseCardsDecision{ChosenCardIDs: []string{cardID(99)}}), p, 0)
	wantErrCode(t, o, mb.CodeInvalidShape)
}

// TestChooseColorRoundTrip covers a repeatable colour ask (a hybrid/filter
// mana choice: pick 2 total, colours may repeat) and asserts the sorted-key
// walk is deterministic regardless of map iteration order by running it
// several times.
func TestChooseColorRoundTrip(t *testing.T) {
	d := newDec(3, 0, decision.KChoose,
		decision.Option{Index: 0, Kind: "mana", Label: "Add W", ManaSymbol: "W"},
		decision.Option{Index: 1, Kind: "mana", Label: "Add U", ManaSymbol: "U"})
	d.Min, d.Max = 2, 2
	d.Repeatable = true
	p := pendingFor(d, view.View{})
	if p.Prompt.Input.Value.PromptType() != "chooseColor" {
		t.Fatalf("prompt type = %s, want chooseColor", p.Prompt.Input.Value.PromptType())
	}
	tr := New("table", 1, nil)
	for i := 0; i < 20; i++ {
		o := tr.TranslateResponse(respFor(p, mb.ColorDecision{ChosenColors: map[string]int{"W": 1, "U": 1}}), p, 0)
		in := mustIntent(t, o)
		if err := d.Validate(*in); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if len(in.Choices) != 2 {
			t.Fatalf("choices = %v, want 2 entries", in.Choices)
		}
		// Sorted key order ("U" before "W") makes the walk deterministic
		// regardless of Go's own map iteration order.
		if in.Choices[0] != 1 || in.Choices[1] != 0 {
			t.Fatalf("choices = %v, want [1 0] (U then W, by sorted colour code)", in.Choices)
		}
	}

	// A colour never offered is invalidShape.
	o := tr.TranslateResponse(respFor(p, mb.ColorDecision{ChosenColors: map[string]int{"B": 2}}), p, 0)
	wantErrCode(t, o, mb.CodeInvalidShape)

	// A single, non-repeated colour pick (the KReplacement mana window's own
	// shape: exactly one colour, Amount 1, not repeatable).
	single := newDec(4, 0, decision.KReplacement, decision.Option{Index: 0, Kind: "mana", Label: "Add W", ManaSymbol: "W"})
	single.Min, single.Max = 1, 1
	sp := pendingFor(single, view.View{})
	so := tr.TranslateResponse(respFor(sp, mb.ColorDecision{ChosenColors: map[string]int{"W": 1}}), sp, 0)
	sIntent := mustIntent(t, so)
	if err := single.Validate(*sIntent); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(sIntent.Choices) != 1 || sIntent.Choices[0] != 0 {
		t.Fatalf("choices = %v, want [0]", sIntent.Choices)
	}
}

// TestChooseBooleanRoundTrip covers all three confirm/deny discoveries
// booleanOptionIndex supports: an explicit yes/no pair, an unless-pay
// Option.Mode pair, and the positional-fallback "asunblocked" pair
// (rules/combat.go's askNextCombatAsk, which carries no distinguishing
// Kind at all).
func TestChooseBooleanRoundTrip(t *testing.T) {
	tr := New("table", 1, nil)

	yesNo := newDec(5, 0, decision.KChoose,
		decision.Option{Index: 0, Kind: "yes", Label: "Yes"},
		decision.Option{Index: 1, Kind: "no", Label: "No"})
	yesNo.Min, yesNo.Max = 1, 1
	p := pendingFor(yesNo, view.View{})
	o := tr.TranslateResponse(respFor(p, mb.BooleanDecision{Value: true}), p, 0)
	in := mustIntent(t, o)
	if err := yesNo.Validate(*in); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if in.Choices[0] != 0 {
		t.Fatalf("true -> choices %v, want [0] (yes)", in.Choices)
	}
	o = tr.TranslateResponse(respFor(p, mb.BooleanDecision{Value: false}), p, 0)
	in = mustIntent(t, o)
	if in.Choices[0] != 1 {
		t.Fatalf("false -> choices %v, want [1] (no)", in.Choices)
	}

	unlessPay := newDec(6, 0, decision.KModes,
		decision.Option{Index: 0, Kind: "unless_pay", Label: "Pay 3", Mode: decision.ModeUnlessPay},
		decision.Option{Index: 1, Kind: "unless_decline", Label: "Decline", Mode: decision.ModeUnlessDecline})
	unlessPay.Min, unlessPay.Max = 1, 1
	up := pendingFor(unlessPay, view.View{})
	o = tr.TranslateResponse(respFor(up, mb.BooleanDecision{Value: true}), up, 0)
	in = mustIntent(t, o)
	if in.Choices[0] != 0 {
		t.Fatalf("pay -> choices %v, want [0]", in.Choices)
	}
	o = tr.TranslateResponse(respFor(up, mb.BooleanDecision{Value: false}), up, 0)
	in = mustIntent(t, o)
	if in.Choices[0] != 1 {
		t.Fatalf("decline -> choices %v, want [1]", in.Choices)
	}

	asUnblocked := newDec(7, 0, decision.KChoose,
		decision.Option{Index: 0, Kind: "asunblocked", Label: "assign normally (blocked)"},
		decision.Option{Index: 1, Kind: "asunblocked", Label: "assign as though not blocked"})
	asUnblocked.Min, asUnblocked.Max = 1, 1
	ap := pendingFor(asUnblocked, view.View{})
	o = tr.TranslateResponse(respFor(ap, mb.BooleanDecision{Value: true}), ap, 0)
	in = mustIntent(t, o)
	if in.Choices[0] != 1 {
		t.Fatalf("true -> choices %v, want [1] (assign as unblocked)", in.Choices)
	}
	o = tr.TranslateResponse(respFor(ap, mb.BooleanDecision{Value: false}), ap, 0)
	in = mustIntent(t, o)
	if in.Choices[0] != 0 {
		t.Fatalf("false -> choices %v, want [0] (assign normally)", in.Choices)
	}

	// commander_zone/leave (promptMisc's KCommanderZone shape).
	cz := newDec(8, 0, decision.KCommanderZone,
		decision.Option{Index: 0, Kind: "command_zone", Label: "Command zone"},
		decision.Option{Index: 1, Kind: "leave", Label: "Leave"})
	cz.Min, cz.Max = 1, 1
	cp := pendingFor(cz, view.View{})
	o = tr.TranslateResponse(respFor(cp, mb.BooleanDecision{Value: true}), cp, 0)
	in = mustIntent(t, o)
	if in.Choices[0] != 0 {
		t.Fatalf("command zone -> choices %v, want [0]", in.Choices)
	}
}

// TestChooseFromSelectionRoundTrip covers the direct index passthrough
// across several of the shapes that build chooseFromSelection: a KModes
// modal pick, a KChoose damage-split (mixed card/player options), and a
// KStartingPlayer pick.
func TestChooseFromSelectionRoundTrip(t *testing.T) {
	tr := New("table", 1, nil)

	modes := newDec(9, 0, decision.KModes,
		decision.Option{Index: 0, Kind: "mode", Label: "Draw a card"},
		decision.Option{Index: 1, Kind: "mode", Label: "Destroy a creature"})
	modes.Min, modes.Max = 1, 1
	mp := pendingFor(modes, view.View{})
	o := tr.TranslateResponse(respFor(mp, mb.SelectionDecision{ChosenIndices: []int{1}}), mp, 0)
	in := mustIntent(t, o)
	if err := modes.Validate(*in); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("choices = %v, want [1]", in.Choices)
	}

	split := newDec(10, 0, decision.KChoose,
		decision.Option{Index: 0, Kind: "card", Label: "Bear", Obj: 9},
		decision.Option{Index: 1, Kind: "player", Label: "Bob", Player: 1})
	split.ResumeKind = "damage_split"
	split.Min, split.Max = 3, 3
	split.Repeatable = true
	sp := pendingFor(split, view.View{})
	if sp.Prompt.Input.Value.PromptType() != "chooseFromSelection" {
		t.Fatalf("prompt type = %s, want chooseFromSelection", sp.Prompt.Input.Value.PromptType())
	}
	o = tr.TranslateResponse(respFor(sp, mb.SelectionDecision{ChosenIndices: []int{0, 0, 1}}), sp, 0)
	in = mustIntent(t, o)
	if err := split.Validate(*in); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(in.Choices) != 3 || in.Choices[0] != 0 || in.Choices[1] != 0 || in.Choices[2] != 1 {
		t.Fatalf("choices = %v, want [0 0 1]", in.Choices)
	}

	sPlayer := newDec(11, 1, decision.KStartingPlayer,
		decision.Option{Index: 0, Kind: "player", Label: "Alice", Player: 0},
		decision.Option{Index: 1, Kind: "player", Label: "Bob", Player: 1})
	sPlayer.Min, sPlayer.Max = 1, 1
	spp := pendingFor(sPlayer, view.View{})
	o = tr.TranslateResponse(respFor(spp, mb.SelectionDecision{ChosenIndices: []int{0}}), spp, 1)
	in = mustIntent(t, o)
	if len(in.Choices) != 1 || in.Choices[0] != 0 {
		t.Fatalf("choices = %v, want [0]", in.Choices)
	}

	// Out-of-range index rejects through Decision.Validate.
	o = tr.TranslateResponse(respFor(mp, mb.SelectionDecision{ChosenIndices: []int{5}}), mp, 0)
	wantErrCode(t, o, mb.CodeInvalidShape)
}

// TestArrangeReorderRoundTripViaTranslateResponse proves the "reorder" prompt
// type now routes through TranslateResponse for the KArrange single-list
// shape, with no directional flip (see promptArrange's doc).
func TestArrangeReorderRoundTripViaTranslateResponse(t *testing.T) {
	d := &decision.Decision{Seq: 20, Player: 0, Kind: decision.KArrange, Min: 2, Max: 2, Prompt: "Rearrange",
		Options: []decision.Option{
			{Index: 0, Kind: "top", Label: "Island", Obj: 1},
			{Index: 1, Kind: "top", Label: "Bear", Obj: 2},
		}}
	v := arrangeView()
	p := pendingFor(d, v)
	if p.Prompt.Input.Value.PromptType() != "reorder" {
		t.Fatalf("prompt type = %s, want reorder", p.Prompt.Input.Value.PromptType())
	}
	tr := New("table", 1, nil)
	o := tr.TranslateResponse(respFor(p, mb.ReorderDecision{OrderedIDs: []string{actionID(1), actionID(0)}}), p, 0)
	in := mustIntent(t, o)
	if err := d.Validate(*in); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(in.Choices) != 2 || in.Choices[0] != 1 || in.Choices[1] != 0 {
		t.Fatalf("choices = %v, want [1 0] (no direction flip)", in.Choices)
	}
}

// TestTriggerOrderRoundTripViaTranslateResponse proves the "reorder" prompt
// type routes to the DIFFERENT parser (with the direction flip) when the
// pending decision's Kind is KTriggerOrder rather than KArrange -- the
// ambiguity a shared wire prompt type creates, resolved by Pending.Decision.
func TestTriggerOrderRoundTripViaTranslateResponse(t *testing.T) {
	d := &decision.Decision{Seq: 21, Player: 0, Kind: decision.KTriggerOrder, Min: 2, Max: 2, Prompt: "Order your triggers",
		Options: []decision.Option{
			{Index: 0, Kind: "trigger", Label: "Alpha"},
			{Index: 1, Kind: "trigger", Label: "Beta"},
		}}
	v := arrangeView()
	p := pendingFor(d, v)
	if p.Prompt.Input.Value.PromptType() != "reorder" {
		t.Fatalf("prompt type = %s, want reorder", p.Prompt.Input.Value.PromptType())
	}
	tr := New("table", 1, nil)
	// Client answer: Alpha (opt-0) resolves first.
	o := tr.TranslateResponse(respFor(p, mb.ReorderDecision{OrderedIDs: []string{actionID(0), actionID(1)}}), p, 0)
	in := mustIntent(t, o)
	if err := d.Validate(*in); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// The direction flip: Choices[0] is placed FIRST and resolves LAST, so
	// the trigger the client wants to resolve first (opt-0) must be LAST.
	if len(in.Choices) != 2 || in.Choices[0] != 1 || in.Choices[1] != 0 {
		t.Fatalf("choices = %v, want [1 0] (direction flip)", in.Choices)
	}
}

// TestArrangeScryRoundTripViaTranslateResponse proves the "scry" prompt type
// routes through TranslateResponse.
func TestArrangeScryRoundTripViaTranslateResponse(t *testing.T) {
	d := &decision.Decision{Seq: 22, Player: 0, Kind: decision.KArrange, Min: 0, Max: 2, Prompt: "Scry 2",
		Options: []decision.Option{
			{Index: 0, Kind: "bottom", Label: "Island", Obj: 1},
			{Index: 1, Kind: "bottom", Label: "Bear", Obj: 2},
		}}
	v := arrangeView()
	p := pendingFor(d, v)
	if p.Prompt.Input.Value.PromptType() != "scry" {
		t.Fatalf("prompt type = %s, want scry", p.Prompt.Input.Value.PromptType())
	}
	tr := New("table", 1, nil)
	o := tr.TranslateResponse(respFor(p, mb.ScryDecision{ZoneCardIDs: [][]string{{cardID(2)}, {cardID(1)}}}), p, 0)
	in := mustIntent(t, o)
	if err := d.Validate(*in); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("choices = %v, want [1] (Bear kept on top)", in.Choices)
	}
}

// TestPayManaCostRoundTripViaTranslator covers the mid-cast payment window
// end to end through Translator.Prompt (the dispatch path used in
// production), not promptPayment/parsePayManaCost directly -- proving that a
// KChoose ManaPayment decision no longer falls into promptChoose's uniform-
// Kind gate and returns ErrUnmapped.
func TestPayManaCostRoundTripViaTranslator(t *testing.T) {
	v := smallView()
	d := &decision.Decision{Seq: 30, Player: 1, Kind: decision.KChoose, Min: 1, Max: 1, Source: 2,
		Prompt:      "Pay for Bear",
		ManaPayment: &decision.ManaPaymentWindow{Card: 2, Cost: decision.PaymentCost{Generic: 1}, Owed: decision.PaymentCost{Generic: 1}},
		Options: []decision.Option{
			{Index: 0, Kind: "mana", Obj: 1, Ability: 0, ManaSymbol: "W", Label: "Add {W}"},
			{Index: 1, Kind: decision.OptAutoFill, Label: "Auto-fill: tap Island"},
			{Index: 2, Kind: decision.OptCancelCast, Label: "Cancel cast"},
		}}
	tr := New("table", 1, nil)
	msg, err := tr.Prompt(d, &v)
	if err != nil {
		t.Fatalf("Prompt (via dispatch/promptChoose): %v", err)
	}
	if msg.Input.Value.PromptType() != "payManaCost" {
		t.Fatalf("prompt type = %s, want payManaCost", msg.Input.Value.PromptType())
	}
	p := &Pending{Prompt: msg, Decision: d, View: v}
	o := tr.TranslateResponse(respFor(p, mb.ActOutput{ActionID: actionID(0)}), p, 1)
	in := mustIntent(t, o)
	if err := d.Validate(*in); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 0 {
		t.Fatalf("choices = %v, want [0]", in.Choices)
	}

	// The legacy pre-announce window (activate/done, no ManaPayment) must
	// also route through promptChoose to promptPayment.
	legacy := &decision.Decision{Seq: 31, Player: 1, Kind: decision.KChoose, Min: 1, Max: 1, Source: 2,
		Prompt: "Activate mana abilities to pay for Bear",
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: 1, Label: "Activate Island for mana"},
			{Index: 1, Kind: "done", Label: "Done"},
		}}
	lmsg, err := tr.Prompt(legacy, &v)
	if err != nil {
		t.Fatalf("Prompt (legacy window via dispatch): %v", err)
	}
	if lmsg.Input.Value.PromptType() != "payManaCost" {
		t.Fatalf("legacy prompt type = %s, want payManaCost", lmsg.Input.Value.PromptType())
	}
}

// TestFindCardResolvesStackSpell covers gap 3: a card mid-cast lives on the
// stack, not the battlefield or hand, and a payment window's SourceCard (or
// any other lookup keyed on the same object id) must still resolve it.
func TestFindCardResolvesStackSpell(t *testing.T) {
	v := view.View{Viewer: 0, Players: []view.PlayerView{
		{ID: 0, Name: "Alice"},
		{ID: 1, Name: "Bob"},
	}, Stack: []view.StackView{
		{ID: 9, Kind: "spell", Name: "Shock", Controller: 1,
			Card: &view.CardView{ID: 9, Name: "Shock", Types: "Instant"}},
	}}
	c := findCard(&v, state.ObjID(9))
	if c == nil {
		t.Fatal("findCard did not resolve the stack spell's card")
	}
	if c.Name != "Shock" {
		t.Fatalf("resolved card = %+v, want Shock", c)
	}
	// An id naming the stack ITEM but not matching any Card is still a miss.
	if got := findCard(&v, state.ObjID(999)); got != nil {
		t.Fatalf("unexpected resolve for an unknown id: %+v", got)
	}
}
