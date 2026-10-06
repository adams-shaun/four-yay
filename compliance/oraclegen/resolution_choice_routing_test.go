package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// Pin resolution-time answer shapes that the Standard scenarios leave to
// XMage's AI: a choose-a-creature Choice ask, Discover's optional cast, and
// divided damage with fewer targets than the spell allows.
func TestResolutionTimeChoiceAnswerShapes(t *testing.T) {
	tests := []struct {
		name string
		ds   []rules.OracleDecision
		want []XAnswer
	}{
		{
			name: "choose a creature for each player (Unstable Glyphbridge)",
			ds: []rules.OracleDecision{{Step: 1, Seat: 0, Kind: "choose_n", Resume: "choice", Options: 1,
				Picks: []string{""}, PickRefs: []string{"p1:Grizzly Bears"}, PickKinds: []string{"card"}, Min: 1, Max: 1}},
			want: []XAnswer{{0, "choice", "Grizzly Bears"}},
		},
		{
			name: "decline discover cast",
			ds:   []rules.OracleDecision{{Step: 1, Seat: 1, Kind: "mode", Resume: "play", Options: 1, Min: 0, Max: 1}},
			want: []XAnswer{{1, "choice", "no"}},
		},
		{
			name: "accept discover cast",
			ds:   []rules.OracleDecision{{Step: 1, Seat: 1, Kind: "mode", Resume: "play", Options: 1, Picks: []string{"Cast Jace Beleren"}, PickIdx: []int{0}, Min: 0, Max: 1}},
			want: []XAnswer{{1, "choice", "yes"}},
		},
		{
			name: "short divided damage target allocation uses one recipient",
			ds: []rules.OracleDecision{
				{Step: 1, Seat: 0, Kind: "target", Options: 2, Min: 1, Max: 2, Picks: []string{"Grizzly Bears"}, PickRefs: []string{"p1:Grizzly Bears"}, Divided: 2},
				{Step: 1, Seat: 0, Kind: "choose_n", Resume: "damage_split", Options: 2, Min: 2, Max: 2,
					Picks: []string{"Grizzly Bears", "Grizzly Bears"}, PickIdx: []int{0, 0}, PickRefs: []string{"p1:Grizzly Bears", "p1:Grizzly Bears"}},
			},
			want: []XAnswer{{0, "target", "p1:Grizzly Bears^X=2"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.ds) == 0 || tt.ds[0].Options == 0 {
				t.Fatal("fixture must present a real resolution-time ask")
			}
			if tt.name == "short divided damage target allocation uses one recipient" &&
				(len(tt.ds[0].PickRefs) != 1 || len(tt.ds[0].PickRefs) >= tt.ds[0].Max) {
				t.Fatalf("fixture must leave an optional target slot open: %+v", tt.ds[0])
			}
			got := routed(t, tt.ds, 2)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("answers = %#v, want %#v", got, tt.want)
			}
		})
	}
}
