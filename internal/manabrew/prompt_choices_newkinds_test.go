//go:build manabrew

package manabrew

import (
	"errors"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// Committed pins for the two option-kind mappings the MBX-6 cardfuzz lane's
// seed sweep found (report-r2 MAJOR), so the suite no longer depends on a
// seed-dependent smoke run to reach them.

// TestMoveCounterMapsToChooseNumber pins the MoveCounter CounterNum$ Any
// amount pick (effects/counters.go, ResumeKind "move_counter"): one option
// per legal amount, 0..max, each carrying the number in Amount. It maps onto
// chooseNumber over that contiguous range, and parseChooseNumber reads the
// answer back off the option whose Amount equals the chosen value.
func TestMoveCounterMapsToChooseNumber(t *testing.T) {
	d := newDec(11, 0, decision.KChoose,
		decision.Option{Index: 0, Kind: "move_counter", Label: "0", Amount: 0},
		decision.Option{Index: 1, Kind: "move_counter", Label: "1", Amount: 1},
		decision.Option{Index: 2, Kind: "move_counter", Label: "2", Amount: 2})
	d.Min, d.Max = 0, 2 // the real ask: 0..max counters of the chosen kind
	d.ResumeKind = "move_counter"
	msg := pendingMustBuild(t, d, battleView())
	in, ok := msg.Input.Value.(mb.ChooseNumberInput)
	if !ok {
		t.Fatalf("prompt type = %s, want chooseNumber", msg.Input.Value.PromptType())
	}
	if in.Min != 0 || in.Max != 2 {
		t.Fatalf("number bounds = %d..%d, want the offered amount range 0..2", in.Min, in.Max)
	}
	p := pendingFor(d, battleView())
	two := 2
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.NumberDecision{ChosenNumber: &two}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("amount-2 answer rejected by the decision's own validator: %v", err)
	}
	if intent.Choices[0] != 2 {
		t.Fatalf("intent choice = %d, want option index 2 (amount 2)", intent.Choices[0])
	}
	// An off-range value stays fenced by the parser.
	five := 5
	wantErrCode(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.NumberDecision{ChosenNumber: &five}), p, d.Player), mb.CodeInvalidShape)
}

// TestDredgeElectionMapsToChooseFromSelection pins the dredge election
// (effects/cardflow.go drawFor, ResumeKind "dredge"): a KModes ask whose
// options are one per legal dredger (Kind "dredge") plus the ordinary draw
// (Kind "draw") -- a mixed-Kind, Min==Max==1 labelled election. It maps onto
// chooseFromSelection, and a modes ask that is NOT a pick-one labelled
// election stays fail-closed.
func TestDredgeElectionMapsToChooseFromSelection(t *testing.T) {
	d := newDec(12, 0, decision.KModes,
		decision.Option{Index: 0, Kind: "dredge", Label: "Dredge 3 (mill, then return Golgari Thug to hand)", Obj: 50},
		decision.Option{Index: 1, Kind: "draw", Label: "Draw card"})
	d.Min, d.Max = 1, 1
	d.ResumeKind = "dredge"
	msg := pendingMustBuild(t, d, battleView())
	in, ok := msg.Input.Value.(mb.ChooseFromSelectionInput)
	if !ok {
		t.Fatalf("prompt type = %s, want chooseFromSelection", msg.Input.Value.PromptType())
	}
	if len(in.Options) != 2 || in.Options[0].Label == "" || in.Options[1].Label == "" {
		t.Fatalf("options = %+v, want one labelled option per side", in.Options)
	}
	p := pendingFor(d, battleView())
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.SelectionDecision{ChosenIndices: []int{1}}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("draw answer rejected by the decision's own validator: %v", err)
	}
	if intent.Choices[0] != 1 {
		t.Fatalf("intent choice = %d, want option index 1 (draw)", intent.Choices[0])
	}
	// The dredge side is equally legal and maps to its own option.
	intent = mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.SelectionDecision{ChosenIndices: []int{0}}), p, d.Player))
	if err := d.Validate(*intent); err != nil || intent.Choices[0] != 0 {
		t.Fatalf("dredge answer: intent %#v err %v", intent, err)
	}
}

// TestChangeTextWordHalvesMapsToChooseFromSelection pins api:ChangeText's
// word ask (effects/changetext.go, ResumeKind "changetext"): a two-half
// combined ask (one changetext_from list + one changetext_to list, Min==Max
// ==2) maps onto chooseFromSelection because the answered option's Kind
// carries which half it is, and a one-half ask maps through the same shape.
func TestChangeTextWordHalvesMapsToChooseFromSelection(t *testing.T) {
	d := newDec(14, 0, decision.KChoose,
		decision.Option{Index: 0, Kind: "changetext_from", Label: "White"},
		decision.Option{Index: 1, Kind: "changetext_from", Label: "Blue"},
		decision.Option{Index: 2, Kind: "changetext_to", Label: "Red"},
		decision.Option{Index: 3, Kind: "changetext_to", Label: "Green"})
	d.Min, d.Max = 2, 2
	d.ResumeKind = "changetext"
	msg := pendingMustBuild(t, d, battleView())
	in, ok := msg.Input.Value.(mb.ChooseFromSelectionInput)
	if !ok {
		t.Fatalf("prompt type = %s, want chooseFromSelection", msg.Input.Value.PromptType())
	}
	if in.MinTotal != 2 || in.MaxTotal != 2 || len(in.Options) != 4 {
		t.Fatalf("min/max/options = %d/%d/%d, want 2/2/4", in.MinTotal, in.MaxTotal, len(in.Options))
	}
	p := pendingFor(d, battleView())
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.SelectionDecision{ChosenIndices: []int{0, 2}}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("one-from-one-to answer rejected by the decision's own validator: %v", err)
	}
	if intent.Choices[0] != 0 || intent.Choices[1] != 2 {
		t.Fatalf("intent choices = %v, want [0 2]", intent.Choices)
	}
	// A one-half ask (uniform changetext_from, Min==Max==1) maps through the
	// same shape via the labelled-pick fallback.
	half := newDec(15, 0, decision.KChoose,
		decision.Option{Index: 0, Kind: "changetext_from", Label: "White"},
		decision.Option{Index: 1, Kind: "changetext_from", Label: "Blue"})
	half.ResumeKind = "changetext"
	halfMsg := pendingMustBuild(t, half, battleView())
	if halfMsg.Input.Value.PromptType() != "chooseFromSelection" {
		t.Fatalf("one-half prompt type = %s, want chooseFromSelection", halfMsg.Input.Value.PromptType())
	}
}

// TestCastContributionMappingIsPinned pins the convoke announcement mapping
// (rules/cast.go convokeAsk, MBX-6 round 1): a KChoose whose options are all
// contribution kinds ("convoke_<color>"/"convoke_generic"/...) maps onto
// chooseFromSelection over the labels, with the ask's own 0..outstanding
// slot bounds. The payment GROUPS are not wire-expressible (spec G-4;
// Validate is the fence), but the branch itself must stay live: the reviewer
// round-2 sweep proved this was the one mapping no committed test reached.
func TestCastContributionMappingIsPinned(t *testing.T) {
	d := newDec(17, 0, decision.KChoose,
		decision.Option{Index: 0, Kind: "convoke_G", Label: "Tap Bear for G", Obj: 70, Group: "unit-1"},
		decision.Option{Index: 1, Kind: "convoke_generic", Label: "Tap Bird for 1", Obj: 71, Group: "unit-2"})
	d.Min, d.Max = 0, 2
	msg := pendingMustBuild(t, d, battleView())
	in, ok := msg.Input.Value.(mb.ChooseFromSelectionInput)
	if !ok {
		t.Fatalf("prompt type = %s, want chooseFromSelection", msg.Input.Value.PromptType())
	}
	if in.MinTotal != 0 || in.MaxTotal != 2 || len(in.Options) != 2 {
		t.Fatalf("min/max/options = %d/%d/%d, want 0/2/2", in.MinTotal, in.MaxTotal, len(in.Options))
	}
	p := pendingFor(d, battleView())
	// One contribution (a pick-1) validates; the empty answer does too.
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.SelectionDecision{ChosenIndices: []int{0}}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("one-contribution answer rejected by the decision's own validator: %v", err)
	}
	// The exclusive-group contract stays the validator's: one pick per unit.
	bad := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}
	if err := d.Validate(bad); err != nil {
		t.Fatalf("distinct-group picks must still be legal together: %v", err)
	}
}

// TestProliferateRecipientPickMapsToChooseFromSelection pins the MBX-6
// round-3 MAJOR: effects/counters.go effProliferate's CR 701.27 any-number
// recipient pick (ResumeKind "proliferate") is a uniform-Kind ("proliferate")
// labelled multi-select whose options are NOT uniformly objects -- permanent
// recipients carry Obj, player recipients carry only Player with Obj 0 and
// Min is 0 -- so neither the object-pick fallback nor the Min==1 labelled
// fallback reaches it. It maps onto chooseFromSelection by index; the empty
// answer is the decline, and each chosen option's own Obj/Player is read by
// rules/resolution.go's proliferate resume arm.
func TestProliferateRecipientPickMapsToChooseFromSelection(t *testing.T) {
	d := newDec(18, 0, decision.KChoose,
		decision.Option{Index: 0, Kind: "proliferate", Label: "a player", Player: 1},
		decision.Option{Index: 1, Kind: "proliferate", Label: "Bear", Obj: 80})
	d.Min, d.Max = 0, 2
	d.ResumeKind = "proliferate"
	msg := pendingMustBuild(t, d, battleView())
	in, ok := msg.Input.Value.(mb.ChooseFromSelectionInput)
	if !ok {
		t.Fatalf("prompt type = %s, want chooseFromSelection", msg.Input.Value.PromptType())
	}
	if in.MinTotal != 0 || in.MaxTotal != 2 || len(in.Options) != 2 {
		t.Fatalf("min/max/options = %d/%d/%d, want 0/2/2", in.MinTotal, in.MaxTotal, len(in.Options))
	}
	p := pendingFor(d, battleView())
	// Both recipients at once: a permanent and a player, freely mixed.
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.SelectionDecision{ChosenIndices: []int{0, 1}}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("mixed permanent/player answer rejected by the decision's own validator: %v", err)
	}
	if intent.Choices[0] != 0 || intent.Choices[1] != 1 {
		t.Fatalf("intent choices = %v, want [0 1]", intent.Choices)
	}
	// The empty answer is the legal decline (Min 0).
	empty := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.SelectionDecision{ChosenIndices: []int{}}), p, d.Player))
	if err := d.Validate(*empty); err != nil {
		t.Fatalf("decline rejected by the decision's own validator: %v", err)
	}
}

// TestEnlistElectionMapsToChooseFromSelection pins the enlist may-election
// (rules/enlist.go askNextEnlist, CR 702.160a): Min 0..1, the decline option
// carries no Obj, so it is neither an object pick nor a Min==1 labelled
// alternative -- it maps onto chooseFromSelection where an empty answer IS
// the decline.
func TestEnlistElectionMapsToChooseFromSelection(t *testing.T) {
	d := newDec(16, 0, decision.KChoose,
		decision.Option{Index: 0, Kind: "enlist", Label: "Don't enlist for Giant"},
		decision.Option{Index: 1, Kind: "enlist", Label: "Enlist Bear", Obj: 60})
	d.Min, d.Max = 0, 1
	msg := pendingMustBuild(t, d, battleView())
	in, ok := msg.Input.Value.(mb.ChooseFromSelectionInput)
	if !ok {
		t.Fatalf("prompt type = %s, want chooseFromSelection", msg.Input.Value.PromptType())
	}
	if in.MinTotal != 0 || in.MaxTotal != 1 {
		t.Fatalf("min/max total = %d/%d, want 0/1", in.MinTotal, in.MaxTotal)
	}
	p := pendingFor(d, battleView())
	// The decline: an empty answer.
	empty := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.SelectionDecision{ChosenIndices: []int{}}), p, d.Player))
	if err := d.Validate(*empty); err != nil {
		t.Fatalf("decline rejected by the decision's own validator: %v", err)
	}
	if len(empty.Choices) != 0 {
		t.Fatalf("decline intent choices = %v, want none", empty.Choices)
	}
	// The enlist side names its own option.
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.SelectionDecision{ChosenIndices: []int{1}}), p, d.Player))
	if err := d.Validate(*intent); err != nil || intent.Choices[0] != 1 {
		t.Fatalf("enlist answer: intent %#v err %v", intent, err)
	}
}

// TestModesMixedKindsBeyondPickOneStayUnmapped pins the fallback's
// fail-closed side: a modes ask with mixed option kinds that is not a
// Min==Max==1 labelled election (here a repeatable two-of ask) must still be
// an ErrUnmapped, not silently flattened onto chooseFromSelection.
func TestModesMixedKindsBeyondPickOneStayUnmapped(t *testing.T) {
	d := newDec(13, 0, decision.KModes,
		decision.Option{Index: 0, Kind: "dredge", Label: "Dredge 3", Obj: 51},
		decision.Option{Index: 1, Kind: "draw", Label: "Draw card"})
	d.Min, d.Max = 2, 2
	d.ResumeKind = "dredge"
	_, err := New("table", 2, nil).Prompt(d, nil)
	if err == nil {
		t.Fatal("expected an ErrUnmapped for a mixed-kind modes ask that is not pick-one")
	}
	if !errors.Is(err, ErrUnmapped) {
		t.Fatalf("error = %v, want ErrUnmapped", err)
	}
}
