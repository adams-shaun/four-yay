package manabrew

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// Tests for prompt_target_set.go: a target ask that carries a set-level
// constraint (TargetsWithSameController, SetPropMode, Option.Group) must
// (a) say so in presentation.description in the wording mbtest's MockClient
// parses, and (b) offer the candidates so the FIRST MinTargets form one set
// the decision's own Validate accepts -- the orderTargetOptions prefix the
// mock answers with. Each test asserts the precondition the real assertion
// depends on (the prefix really is constraint-bound: the offered order
// alone would NOT be a legal set) so a fixture drift cannot pass vacuously.

func targetPrefix(t *testing.T, d *decision.Decision) mb.ChooseBoardTargetsInput {
	t.Helper()
	msg := pendingMustBuild(t, d, battleView())
	in, ok := msg.Input.Value.(mb.ChooseBoardTargetsInput)
	if !ok {
		t.Fatalf("prompt type = %s, want chooseBoardTargets", msg.Input.Value.PromptType())
	}
	return in
}

// prefixRefIDs returns the candidate ids of the offered prefix of n
// candidates, so the tests can name which options the prefix selects.
func prefixRefIDs(in mb.ChooseBoardTargetsInput, n int) []string {
	if n > len(in.Candidates) {
		n = len(in.Candidates)
	}
	ids := make([]string, 0, n)
	for _, cand := range in.Candidates[:n] {
		ids = append(ids, cand.ID)
	}
	return ids
}

func TestConstrainedTargetPrefixSameController(t *testing.T) {
	// Offered order: o1 (p0-controlled) sits between two p1-controlled
	// options -- the offered prefix would mix controllers, so the reordering
	// (largest p1 group hoisted to the front) is what makes the prefix legal.
	d := &decision.Decision{Seq: 5, Player: 1, Kind: decision.KTarget, Min: 2, Max: 2,
		Prompt: "Choose two", TargetsWithSameController: true,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "A", Obj: 10, Controller: 1},
			{Index: 1, Kind: "permanent", Label: "B", Obj: 11, Controller: 0},
			{Index: 2, Kind: "permanent", Label: "C", Obj: 12, Controller: 1},
			{Index: 3, Kind: "permanent", Label: "D", Obj: 13, Controller: 1},
		}}
	in := targetPrefix(t, d)
	if !strings.Contains(in.Presentation.Description, "All chosen targets must share one controller.") {
		t.Fatalf("constraint sentence missing from description: %q", in.Presentation.Description)
	}
	if got := prefixRefIDs(in, 2); got[0] != "o10" || got[1] != "o12" {
		t.Fatalf("prefix candidates = %v, want the two p1-controlled options first", got)
	}
	// Precondition: the fixtures resolve (ids are minted from the Obj).
	if len(in.Candidates) != 4 {
		t.Fatalf("candidates = %d, want one per option", len(in.Candidates))
	}
	// Round trip: the mock's prefix answer validates under the decision's own
	// validator, and maps back to the right option indices despite the
	// reordering (parseBoardTargets matches by kind+id, not by position).
	p := pendingFor(d, battleView())
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
			{Kind: mb.RefCard, ID: "o10"}, {Kind: mb.RefCard, ID: "o12"},
		}}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("prefix answer rejected by the decision's own validator: %v", err)
	}
	if intent.Choices[0] != 0 || intent.Choices[1] != 2 {
		t.Fatalf("intent choices = %v, want the p1 options' indices [0 2]", intent.Choices)
	}
	// A mixed-controller set is still fenced by the validator: the engine
	// stays the one home of the rule.
	bad := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}
	if err := d.Validate(bad); err == nil {
		t.Fatal("expected the mixed-controller set to be rejected")
	}
}

func TestConstrainedTargetPrefixSharedProperty(t *testing.T) {
	d := &decision.Decision{Seq: 6, Player: 1, Kind: decision.KTarget, Min: 2, Max: 2,
		Prompt: "Choose two sharing a type", SetPropMode: decision.SetPropShared,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "Elf 1", Obj: 20, SetProps: []string{"Elf"}},
			{Index: 1, Kind: "permanent", Label: "Goblin", Obj: 21, SetProps: []string{"Goblin"}},
			{Index: 2, Kind: "permanent", Label: "Elf 2", Obj: 22, SetProps: []string{"Elf"}},
		}}
	in := targetPrefix(t, d)
	if !strings.Contains(in.Presentation.Description, "All chosen targets must share a property.") {
		t.Fatalf("constraint sentence missing from description: %q", in.Presentation.Description)
	}
	if got := prefixRefIDs(in, 2); got[0] != "o20" || got[1] != "o22" {
		t.Fatalf("prefix candidates = %v, want the two Elf options first", got)
	}
	p := pendingFor(d, battleView())
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
			{Kind: mb.RefCard, ID: "o20"}, {Kind: mb.RefCard, ID: "o22"},
		}}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("prefix answer rejected by the decision's own validator: %v", err)
	}
}

func TestConstrainedTargetPrefixDistinctProperty(t *testing.T) {
	// Offered order: o0 and o2 share "A", so the offered prefix would not be
	// pairwise disjoint; the greedy walk must skip o2.
	d := &decision.Decision{Seq: 7, Player: 1, Kind: decision.KTarget, Min: 2, Max: 2,
		Prompt: "Choose two differing", SetPropMode: decision.SetPropDistinct,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "A one", Obj: 30, SetProps: []string{"A"}},
			{Index: 1, Kind: "permanent", Label: "B", Obj: 31, SetProps: []string{"B"}},
			{Index: 2, Kind: "permanent", Label: "A two", Obj: 32, SetProps: []string{"A"}},
		}}
	in := targetPrefix(t, d)
	if !strings.Contains(in.Presentation.Description, "No two chosen targets may share a property.") {
		t.Fatalf("constraint sentence missing from description: %q", in.Presentation.Description)
	}
	if got := prefixRefIDs(in, 2); got[0] != "o30" || got[1] != "o31" {
		t.Fatalf("prefix candidates = %v, want the disjoint pair first", got)
	}
	p := pendingFor(d, battleView())
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
			{Kind: mb.RefCard, ID: "o30"}, {Kind: mb.RefCard, ID: "o31"},
		}}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("prefix answer rejected by the decision's own validator: %v", err)
	}
}

// TestConstrainedTargetPrefixControllerAndGroup pins the round-3 MINOR: a
// TargetsWithSameController ask whose options ALSO carry Group exclusivity.
// The controller hoist alone only guarantees the largest controller group;
// inside it group exclusivity can consume the candidates the greedy walk
// needs, so orderTargetOptions retries with each controller as the base. The
// precondition is that the largest controller group does NOT fill Min (its
// two options share one Group), while the smaller group does.
func TestConstrainedTargetPrefixControllerAndGroup(t *testing.T) {
	d := &decision.Decision{Seq: 9, Player: 1, Kind: decision.KTarget, Min: 2, Max: 2,
		Prompt: "Choose two", TargetsWithSameController: true,
		Options: []decision.Option{
			// Largest controller group (p1) but both share "g": only one may
			// be chosen, so the p1 controller cannot fill Min.
			{Index: 0, Kind: "permanent", Label: "A", Obj: 50, Controller: 1, Group: "g"},
			{Index: 1, Kind: "permanent", Label: "B", Obj: 51, Controller: 1, Group: "g"},
			// Smaller controller group (p0) with distinct groups: it fills Min.
			{Index: 2, Kind: "permanent", Label: "C", Obj: 52, Controller: 0, Group: "h"},
			{Index: 3, Kind: "permanent", Label: "D", Obj: 53, Controller: 0, Group: "i"},
		}}
	in := targetPrefix(t, d)
	if !strings.Contains(in.Presentation.Description, "All chosen targets must share one controller.") {
		t.Fatalf("controller sentence missing from description: %q", in.Presentation.Description)
	}
	if !strings.Contains(in.Presentation.Description, "Options that share a group are mutually exclusive.") {
		t.Fatalf("group sentence missing from description: %q", in.Presentation.Description)
	}
	// Precondition: the naive largest-controller hoist would put o50 and o51
	// first, and the group exclusion makes that prefix illegal, so the
	// assertion below genuinely depends on the retry.
	if got := prefixRefIDs(in, 2); got[0] == "o50" && got[1] == "o51" {
		t.Fatalf("prefix %v is the illegal same-group pair; the controller retry did not run", got)
	}
	got := prefixRefIDs(in, 2)
	if got[0] != "o52" || got[1] != "o53" {
		t.Fatalf("prefix candidates = %v, want the p0 distinct-group pair [o52 o53]", got)
	}
	// The prefix validates, and a same-controller same-group pair is fenced.
	p := pendingFor(d, battleView())
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
			{Kind: mb.RefCard, ID: "o52"}, {Kind: mb.RefCard, ID: "o53"},
		}}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("prefix answer rejected by the decision's own validator: %v", err)
	}
	bad := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}
	if err := d.Validate(bad); err == nil {
		t.Fatal("expected the same-group pair to be rejected")
	}
}

// TestParseBoardTargetsDistinguishesDuplicateRefs pins the round-3 MINOR:
// two options that mint the SAME kind+id ref must resolve to two distinct
// option indices, not the same one (which Validate rejects as a duplicate).
// The positional matcher round 1 used distinguished them; the kind+id walk
// must keep that ability by consuming each matching option at most once.
func TestParseBoardTargetsDistinguishesDuplicateRefs(t *testing.T) {
	// Two options for the same object ref (o90), distinguished only by Index.
	d := &decision.Decision{Seq: 10, Player: 1, Kind: decision.KTarget, Min: 2, Max: 2,
		Prompt: "Choose two", Repeatable: true,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "A", Obj: 90},
			{Index: 1, Kind: "permanent", Label: "B", Obj: 90},
		}}
	// Precondition: the two options really do mint the same wire ref.
	if targetRefID(d.Options[0]) != targetRefID(d.Options[1]) {
		t.Fatal("fixture does not offer a duplicate ref")
	}
	p := pendingFor(d, battleView())
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
			{Kind: mb.RefCard, ID: "o90"}, {Kind: mb.RefCard, ID: "o90"},
		}}), p, d.Player))
	if intent.Choices[0] == intent.Choices[1] {
		t.Fatalf("duplicate refs collapsed to one index: %v", intent.Choices)
	}
	if intent.Choices[0] != 0 || intent.Choices[1] != 1 {
		t.Fatalf("intent choices = %v, want the two distinct options [0 1]", intent.Choices)
	}
}

func TestUnconstrainedTargetKeepsOfferedOrder(t *testing.T) {
	d := &decision.Decision{Seq: 8, Player: 1, Kind: decision.KTarget, Min: 1, Max: 2,
		Prompt: "Choose a target",
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "A", Obj: 40, Controller: 1},
			{Index: 1, Kind: "permanent", Label: "B", Obj: 41, Controller: 0},
		}}
	in := targetPrefix(t, d)
	if in.Presentation.Description != "" {
		t.Fatalf("unconstrained ask must carry no constraint sentence, got %q", in.Presentation.Description)
	}
	if got := prefixRefIDs(in, 2); got[0] != "o40" || got[1] != "o41" {
		t.Fatalf("unconstrained candidates reordered: %v", got)
	}
}
