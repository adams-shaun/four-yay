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

// TestAmbiguousChoiceCensus pins unresolved same-name object answers by the
// declared audit set. The generator's structural discriminator is applied
// whenever option refs contain a name collision, independent of card identity.
func TestAmbiguousChoiceCensus(t *testing.T) {
	for _, set := range censusSets {
		t.Run(set, func(t *testing.T) {
			d := rules.OracleDecision{
				Step: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1,
				Picks: []string{"Fixture"}, PickRefs: []string{"p0:token:Fixture#1"},
				PickIdx: []int{1}, PickKinds: []string{"card"},
				OptionRefs: []string{"p0:Fixture", "p0:token:Fixture#1"},
			}
			got := XAnswers([]rules.OracleDecision{d}, 1, nil)
			if len(got) != 1 || len(got[0]) != 1 || got[0][0].Value != "Fixture [only copy]" {
				t.Fatalf("unresolved same-name answer for %s: %+v", set, got)
			}
		})
	}
}
