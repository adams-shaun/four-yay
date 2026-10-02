//go:build manabrew

package manabrew

// Tests for the RAISED-CAP wording of targetSetSentences' group sentence
// (groupcap-wording): since the group-cap merge the underlying rule is
// "at most GroupCapFor(g) options of one group", so a description that still
// said "mutually exclusive" at a raised cap would LIE -- a client honouring
// it would refuse a legal answer. The sentence is derived from
// Decision.GroupCapFor (the one home) over the distinct groups present:
// byte-identical at every cap-1 group, a uniform "At most N ..." at one
// uniform raised cap, and a spread sentence at mixed caps. Group ids are
// opaque and never rendered.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestTargetSetSentencesGroupCapUniformMap: a decision whose GroupLimits map
// uniformly raises one group to 3 renders "At most 3 options of each group
// may be chosen together." -- exactly that sentence, and nothing else, in
// the description. The cap-1 twin of the same decision still renders the
// historical sentence byte-identically.
func TestTargetSetSentencesGroupCapUniformMap(t *testing.T) {
	d := &decision.Decision{Seq: 31, Player: 1, Kind: decision.KTarget, Min: 2, Max: 3,
		Prompt: "Choose", GroupLimits: map[string]int{"g": 3},
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "G one", Obj: 90, Group: "g"},
			{Index: 1, Kind: "permanent", Label: "G two", Obj: 91, Group: "g"},
			{Index: 2, Kind: "permanent", Label: "G three", Obj: 92, Group: "g"},
		}}
	// Precondition: the caps the wording is derived from really are 3 for
	// every present group (and the two descriptions differ).
	if got := d.GroupCapFor("g"); got != 3 {
		t.Fatalf("precondition: GroupCapFor(g) = %d, want 3", got)
	}
	in := targetPrefix(t, d)
	if in.Presentation.Description != "At most 3 options of each group may be chosen together." {
		t.Fatalf("description = %q, want exactly the raised-cap sentence", in.Presentation.Description)
	}
	twin := cap1Twin(d)
	if got := targetSetSentences(twin); got != "Options that share a group are mutually exclusive." {
		t.Fatalf("cap-1 twin description = %q, want the historical sentence unchanged", got)
	}
}

// TestTargetSetSentencesGroupCapMixed: a decision whose groups carry
// DIFFERENT caps ({g: 2, h: 3}) renders the spread sentence exactly:
// ascending, naming the cap values, without exposing the opaque group ids.
// The cap-1 twin still renders the historical sentence.
func TestTargetSetSentencesGroupCapMixed(t *testing.T) {
	d := &decision.Decision{Seq: 32, Player: 1, Kind: decision.KTarget, Min: 2, Max: 3,
		Prompt: "Choose", GroupLimits: map[string]int{"g": 2, "h": 3},
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "G", Obj: 93, Group: "g"},
			{Index: 1, Kind: "permanent", Label: "G two", Obj: 94, Group: "g"},
			{Index: 2, Kind: "permanent", Label: "H", Obj: 95, Group: "h"},
			{Index: 3, Kind: "permanent", Label: "H two", Obj: 96, Group: "h"},
		}}
	// Precondition: the caps really differ, and ascending order matters.
	if d.GroupCapFor("g") != 2 || d.GroupCapFor("h") != 3 || d.GroupCapFor("g") == d.GroupCapFor("h") {
		t.Fatalf("precondition: caps g=%d h=%d, want 2 and 3 (distinct)", d.GroupCapFor("g"), d.GroupCapFor("h"))
	}
	in := targetPrefix(t, d)
	if in.Presentation.Description != "Options of a group may be chosen together up to the group's cap: at most 2 for some groups, at most 3 for others." {
		t.Fatalf("description = %q, want exactly the mixed-cap spread sentence", in.Presentation.Description)
	}
	// The exact equality above already proves the opaque group ids (g, h)
	// are never rendered as such.
	twin := cap1Twin(d)
	if got := targetSetSentences(twin); got != "Options that share a group are mutually exclusive." {
		t.Fatalf("cap-1 twin description = %q, want the historical sentence unchanged", got)
	}
}

// TestTargetSetSentencesMixedWithDefaultCap: a raised group beside a
// group at the default cap 1 names BOTH values (at most 1 for some groups,
// at most 2 for others) -- the historical sentence is reserved for the
// all-caps-1 universe only, where it is the whole truth.
func TestTargetSetSentencesMixedWithDefaultCap(t *testing.T) {
	d := &decision.Decision{Seq: 33, Player: 1, Kind: decision.KTarget, Min: 2, Max: 2,
		Prompt: "Choose", GroupLimits: map[string]int{"g": 2},
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "G", Obj: 97, Group: "g"},
			{Index: 1, Kind: "permanent", Label: "G two", Obj: 98, Group: "g"},
			{Index: 2, Kind: "permanent", Label: "H", Obj: 99, Group: "h"},
		}}
	// Precondition: the default cap is real -- h reads 1 through the one
	// home even though GroupLimits names only g.
	if d.GroupCapFor("g") != 2 || d.GroupCapFor("h") != 1 {
		t.Fatalf("precondition: caps g=%d h=%d, want 2 and 1", d.GroupCapFor("g"), d.GroupCapFor("h"))
	}
	if got := targetSetSentences(d); got != "Options of a group may be chosen together up to the group's cap: at most 1 for some groups, at most 2 for others." {
		t.Fatalf("description = %q, want the spread sentence including the default cap", got)
	}
}
