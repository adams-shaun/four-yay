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
	want := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "Joo Dee, One of Many [only copy]"}}}
	if len(d.OptionRefs) != 2 || d.PickIdx[0] == 0 {
		t.Fatalf("fixture must select the token from two same-named offered objects: %+v", d)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("same-name copy answer = %#v, want %#v", got, want)
	}
}
