package manabrew

// Tests for prompt_target_set.go's per-Group CAP rule: the prefix walks must
// admit options by Decision.GroupCapFor(group) -- the same cap
// Decision.Validate enforces -- so a decision whose GroupLimit or
// GroupLimits raises a group's cap above 1 gets an offered prefix that
// really is legal, instead of the walk's at-most-one-per-group reading
// fronting a set the validator rejects (or returning nil while a legal set
// exists). Each test asserts the precondition the real assertion depends on:
// a cap-1 twin of the same decision really refuses the same choices, so the
// fixture cannot pass when the cap is not what drives it.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// cap1Twin returns a copy of d with every raised group cap removed, so a
// test can prove its choices are exactly the ones the cap-1 exclusivity
// rule refuses (the precondition) while d itself accepts them.
func cap1Twin(d *decision.Decision) *decision.Decision {
	twin := *d
	twin.GroupLimit = 0
	twin.GroupLimits = nil
	return &twin
}

// TestConstrainedTargetPrefixGroupCapRaised: a decision-wide GroupLimit of 2
// lets the prefix take TWO options of one Group. Under the historical
// at-most-one walk the offered prefix would have skipped the second "g"
// option; under the cap-aware walk it fronts the pair and the decision's own
// Validate accepts it.
func TestConstrainedTargetPrefixGroupCapRaised(t *testing.T) {
	d := &decision.Decision{Seq: 11, Player: 1, Kind: decision.KTarget, Min: 2, Max: 2,
		Prompt: "Choose two", GroupLimit: 2,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "G one", Obj: 70, Group: "g"},
			{Index: 1, Kind: "permanent", Label: "G two", Obj: 71, Group: "g"},
			{Index: 2, Kind: "permanent", Label: "H", Obj: 72, Group: "h"},
		}}
	// Precondition: the cap-1 twin really refuses choices {0,1} -- the same
	// choices d must accept -- so the fixture is genuinely cap-driven.
	twin := cap1Twin(d)
	if err := twin.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err == nil {
		t.Fatal("precondition: the cap-1 twin accepted the same-group pair {0,1}")
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err != nil {
		t.Fatalf("the raised-cap decision refused the pair it must admit: %v", err)
	}
	in := targetPrefix(t, d)
	if !strings.Contains(in.Presentation.Description, "At most 2 options of each group may be chosen together.") {
		t.Fatalf("raised-cap group sentence missing from description: %q", in.Presentation.Description)
	}
	// The wording promise, executable: the cap-1 twin of the same decision
	// still renders the historical sentence byte-identically.
	if got := targetSetSentences(cap1Twin(d)); got != "Options that share a group are mutually exclusive." {
		t.Fatalf("cap-1 twin description = %q, want the historical sentence unchanged", got)
	}
	if got := prefixRefIDs(in, 2); got[0] != "o70" || got[1] != "o71" {
		t.Fatalf("prefix candidates = %v, want the two group-g options first", got)
	}
	// Round trip: the mock's prefix answer validates under the decision's own
	// validator and maps back to indices [0 1] despite the reordering.
	p := pendingFor(d, battleView())
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
			{Kind: mb.RefCard, ID: "o70"}, {Kind: mb.RefCard, ID: "o71"},
		}}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("prefix answer rejected by the decision's own validator: %v", err)
	}
	if intent.Choices[0] != 0 || intent.Choices[1] != 1 {
		t.Fatalf("intent choices = %v, want the group-g options' indices [0 1]", intent.Choices)
	}
}

// TestConstrainedTargetPrefixGroupLimitsPerGroup: a per-Group GroupLimits
// entry raises ONLY group "g"'s cap; group "h" stays at the default 1. The
// prefix must fill Min out of "g" alone, and the cap must still refuse a
// THIRD option of "g".
func TestConstrainedTargetPrefixGroupLimitsPerGroup(t *testing.T) {
	d := &decision.Decision{Seq: 12, Player: 1, Kind: decision.KTarget, Min: 2, Max: 3,
		Prompt: "Choose at least two", GroupLimits: map[string]int{"g": 2},
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "G one", Obj: 74, Group: "g"},
			{Index: 1, Kind: "permanent", Label: "G two", Obj: 75, Group: "g"},
			{Index: 2, Kind: "permanent", Label: "H", Obj: 76, Group: "h"},
			{Index: 3, Kind: "permanent", Label: "G three", Obj: 77, Group: "g"},
		}}
	// Precondition: with the GroupLimits entry removed, the same first two
	// options are an illegal set (mutual exclusivity), so the fixture is
	// genuinely driven by the per-group cap and not by option order.
	twin := cap1Twin(d)
	if err := twin.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err == nil {
		t.Fatal("precondition: the cap-1 twin accepted the same-group pair {0,1}")
	}
	// The cap still binds at 2: a third "g" option is refused even though the
	// count (3) is inside Min..Max.
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1, 3}}); err == nil {
		t.Fatal("expected three group-g choices to exceed the per-group limit of 2")
	}
	in := targetPrefix(t, d)
	if got := prefixRefIDs(in, 2); got[0] != "o74" || got[1] != "o75" {
		t.Fatalf("prefix candidates = %v, want the two group-g options first", got)
	}
	// Render assertion: g is capped at 2, h stays at the default 1, so the
	// description names the SPREAD, not uniform exclusivity nor a uniform cap.
	if !strings.Contains(in.Presentation.Description, "at most 1 for some groups, at most 2 for others") {
		t.Fatalf("mixed-cap spread sentence missing from description: %q", in.Presentation.Description)
	}
	p := pendingFor(d, battleView())
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
			{Kind: mb.RefCard, ID: "o74"}, {Kind: mb.RefCard, ID: "o75"},
		}}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("prefix answer rejected by the decision's own validator: %v", err)
	}
}

// TestConstrainedTargetPrefixGroupCapSearchFallback: with
// TargetsWithSameController raised, the LARGEST controller group cannot fill
// Min even at the raised cap (its three options share group "g" capped at
// 2), so the greedy walk fails and orderTargetOptions falls through to the
// controller search -- which must carry the per-group COUNTS (not booleans)
// through its backtracking walk. The legal prefix lies in the smaller
// controller group. The raised cap is still real: the decision admits a
// two-from-g pair the cap-1 twin refuses.
func TestConstrainedTargetPrefixGroupCapSearchFallback(t *testing.T) {
	d := &decision.Decision{Seq: 13, Player: 1, Kind: decision.KTarget, Min: 3, Max: 3,
		Prompt: "Choose three", TargetsWithSameController: true,
		GroupLimits: map[string]int{"g": 2},
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "G one", Obj: 80, Controller: 1, Group: "g"},
			{Index: 1, Kind: "permanent", Label: "G two", Obj: 81, Controller: 1, Group: "g"},
			{Index: 2, Kind: "permanent", Label: "G three", Obj: 82, Controller: 1, Group: "g"},
			{Index: 3, Kind: "permanent", Label: "H", Obj: 83, Controller: 0, Group: "h"},
			{Index: 4, Kind: "permanent", Label: "I", Obj: 84, Controller: 0, Group: "i"},
			{Index: 5, Kind: "permanent", Label: "J", Obj: 85, Controller: 0, Group: "j"},
		}}
	// Precondition: the greedy walk alone cannot fill Min -- the hoisted p1
	// group yields only two options at the cap and the controller constraint
	// forbids crossing to p0 -- so the answer below genuinely comes from the
	// search fallback.
	ordered := hoistByController(append([]decision.Option(nil), d.Options...))
	if got := greedyPrefix(ordered, d); got != nil {
		t.Fatalf("precondition: greedyPrefix filled Min (%d options), the search fallback is not what ran", len(got))
	}
	// Precondition: the cap-1 twin refuses the two-from-g pair {0,1} that the
	// raised-cap decision admits (asserted on a Min 2..2 variant of the same
	// options, since d itself demands exactly 3 choices).
	pair := *d
	pair.Min, pair.Max = 2, 2
	twinPair := cap1Twin(&pair)
	if err := twinPair.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err == nil {
		t.Fatal("precondition: the cap-1 twin accepted the same-group pair {0,1}")
	}
	if err := pair.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err != nil {
		t.Fatalf("the raised-cap decision refused the pair it must admit: %v", err)
	}
	in := targetPrefix(t, d)
	if got := prefixRefIDs(in, 3); got[0] != "o83" || got[1] != "o84" || got[2] != "o85" {
		t.Fatalf("prefix candidates = %v, want the p0 distinct-group triple [o83 o84 o85]", got)
	}
	// Render assertion: same mixed-cap spread as the PerGroup test (g at 2,
	// h/i/j at the default 1), alongside the controller sentence.
	if !strings.Contains(in.Presentation.Description, "at most 1 for some groups, at most 2 for others") {
		t.Fatalf("mixed-cap spread sentence missing from description: %q", in.Presentation.Description)
	}
	p := pendingFor(d, battleView())
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
			{Kind: mb.RefCard, ID: "o83"}, {Kind: mb.RefCard, ID: "o84"}, {Kind: mb.RefCard, ID: "o85"},
		}}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("prefix answer rejected by the decision's own validator: %v", err)
	}
}
