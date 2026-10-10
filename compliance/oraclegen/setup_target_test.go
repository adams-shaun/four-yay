package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// Only an OPTIONAL target ask posed during the setup drive that gorge
// declined is scripted: a mandatory ask, one that took a pick, or a
// gameplay-step ask keeps today's routing.
func TestIsSetupTargetDecline(t *testing.T) {
	base := rules.OracleDecision{Step: -1, Seat: 0, Kind: "target", Options: 2, Min: 0}
	if !IsSetupTargetDecline(base) {
		t.Fatalf("precondition: the base decision must be a setup target decline: %+v", base)
	}
	for name, mut := range map[string]func(*rules.OracleDecision){
		"mandatory":      func(d *rules.OracleDecision) { d.Min = 1 },
		"picked":         func(d *rules.OracleDecision) { d.Picks = []string{"Grizzly Bears (b)"} },
		"gameplay step":  func(d *rules.OracleDecision) { d.Step = 0 },
		"not a target":   func(d *rules.OracleDecision) { d.Kind = "yesno" },
		"nothing to ask": func(d *rules.OracleDecision) { d.Options = 0 },
	} {
		d := base
		mut(&d)
		if IsSetupTargetDecline(d) {
			t.Errorf("%s: %+v must not be scripted as a setup target decline", name, d)
		}
	}
}

func TestSetupTargetDeclinesAndXAnswers(t *testing.T) {
	ds := []rules.OracleDecision{
		{Step: -1, Seat: 0, Kind: "target", Options: 2, Min: 0},
		{Step: -1, Seat: 0, Kind: "target", Options: 1, Min: 1, Picks: []string{"x"}},
		{Step: -1, Seat: 0, Kind: "target", Options: 2, Min: 0},
	}
	want := []Answer{{Kind: "target", Pick: []string{}}, {Kind: "target", Pick: []string{}}}
	if got := SetupTargetDeclines(ds); !reflect.DeepEqual(got, want) {
		t.Fatalf("SetupTargetDeclines = %#v, want %#v", got, want)
	}
	x := xanswers(ds, 1, nil, nil)
	skip := XAnswer{Seat: 0, Kind: "setup_target", Value: "[target_skip]"}
	if len(x) != 1 || !reflect.DeepEqual(x[0], []XAnswer{skip, skip}) {
		t.Fatalf("xanswers = %#v, want two setup_target skips leading step 0", x)
	}
}
