package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func TestXAnswersDisambiguateSameNamedCopyChoice(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1,
		Picks: []string{"Joo Dee, One of Many"}, PickRefs: []string{"p0:token:Joo Dee, One of Many#1"},
		PickIdx: []int{1}, PickKinds: []string{"card"},
		OptionRefs: []string{"p0:Joo Dee, One of Many", "p0:token:Joo Dee, One of Many#1"},
	}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)
	want := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "Joo Dee, One of Many[only copy]"}}}
	if len(d.OptionRefs) != 2 || d.PickIdx[0] == 0 {
		t.Fatalf("fixture must select the token from two same-named offered objects: %+v", d)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("same-name copy answer = %#v, want %#v", got, want)
	}
}

// Two same-named cards have no copy-filter discriminator, so the answer is
// the pick's exact scenario ref (carried to XMage as its @alias), which names
// exactly one object; a bare name would leave two candidates.
func TestXAnswersSameKindSameNameIsExactRef(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1,
		Picks: []string{"Forest"}, PickRefs: []string{"p0:Forest#1"},
		PickIdx: []int{1}, PickKinds: []string{"card"},
		OptionRefs: []string{"p0:Forest", "p0:Forest#1"},
	}
	amb, marker := SameNameAmbiguity(d, 0)
	if !amb || marker != "" {
		t.Fatalf("two same-named cards: ambiguous=%v marker=%q, want true and no copy marker", amb, marker)
	}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)
	want := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "@p0:Forest#1"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("same-kind answer = %#v, want %#v", got, want)
	}
}

// The original among a same-named token copy is "[no copy]", the mirror of
// the token's "[only copy]".
func TestXAnswersOriginalAmongTokenCopyIsNoCopy(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1,
		Picks: []string{"Joo Dee, One of Many"}, PickRefs: []string{"p0:Joo Dee, One of Many"},
		PickIdx: []int{0}, PickKinds: []string{"card"},
		OptionRefs: []string{"p0:Joo Dee, One of Many", "p0:token:Joo Dee, One of Many#1"},
	}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)
	want := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "Joo Dee, One of Many[no copy]"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("original answer = %#v, want %#v", got, want)
	}
}

// A same-name pick on the TARGET queue carries the discriminator too: XMage's
// chooseTarget parses the same marker and filters on isCopy() (Extravagant
// Replication targets a token copy of a card on the battlefield). The choice
// path is not the only emission site.
func TestXAnswersDisambiguateSameNamedCopyTarget(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "target", GorgeKind: "target", Options: 2, Min: 1, Max: 1,
		Picks: []string{"Grizzly Bears (a)"}, PickRefs: []string{"p0:Grizzly Bears"},
		PickIdx: []int{0}, PickKinds: []string{"card"},
		OptionRefs: []string{"p0:Grizzly Bears", "p0:token:Grizzly Bears"},
	}
	if len(d.OptionRefs) != 2 {
		t.Fatalf("fixture must offer two same-named objects: %+v", d)
	}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)
	want := [][]XAnswer{{{Seat: 0, Kind: "target", Value: "Grizzly Bears[no copy]"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("same-name target answer = %#v, want %#v", got, want)
	}
}
