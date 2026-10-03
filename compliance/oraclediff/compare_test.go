package oraclediff

import (
	"errors"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func snap(cp string) rules.OracleSnapshot {
	return rules.OracleSnapshot{
		Checkpoint: cp, Turn: 1, Step: "main1",
		Players: []rules.OracleSnapPlayer{
			{Seat: 0, Life: 20, Hand: []string{}, Graveyard: []string{}, LibraryCount: 39, LibraryTop: []string{"Wastes"}},
			{Seat: 1, Life: 20, Hand: []string{}, Graveyard: []string{}, LibraryCount: 38, LibraryTop: []string{"Shock"}},
		},
		Permanents: []rules.OracleSnapPerm{
			{Ref: "p1:Grizzly Bears", Name: "Grizzly Bears", Controller: 1, Owner: 1, PT: "2/2", Types: []string{"Bear", "Creature"}, Colors: "G"},
		},
	}
}

func xsnap(cp string) rules.OracleSnapshot {
	s := snap(cp)
	s.Step = "PRECOMBAT_MAIN"
	s.Permanents = []rules.OracleSnapPerm{
		{Name: "Grizzly Bears", Controller: 1, Owner: 1, PT: "2/2", Types: []string{"Creature", "Bear"}, Colors: "G"},
	}
	return s
}

func TestCompareAgreesAcrossVocabularies(t *testing.T) {
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{snap("setup")}}
	x := XResult{Snapshots: []rules.OracleSnapshot{xsnap("setup")}}
	if v := Compare(g, nil, x); v.Status != Agree {
		t.Fatalf("%+v", v)
	}
}

func TestCompareReportsFirstDifference(t *testing.T) {
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{snap("setup"), snap("step 0 (cast)")}}
	x := XResult{Snapshots: []rules.OracleSnapshot{xsnap("setup"), xsnap("step 0 (cast)")}}
	x.Snapshots[1].Players[1].Life = 17
	x.Snapshots[1].Permanents[0].Damage = 1
	v := Compare(g, nil, x)
	if v.Status != Diverge || v.Checkpoint != "step 0 (cast)" || v.Field != "p1.life" || v.Gorge != "20" || v.XMage != "17" {
		t.Fatalf("%+v", v)
	}
}

func TestCompareMatchesTokensByCharacteristics(t *testing.T) {
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{snap("setup")}}
	g.Snapshots[0].Permanents = append(g.Snapshots[0].Permanents, rules.OracleSnapPerm{
		Ref: "p0:token:Soldier Token", Name: "Soldier Token", Token: true, PT: "1/1", Types: []string{"Creature", "Soldier"}, Colors: "W"})
	x := XResult{Snapshots: []rules.OracleSnapshot{xsnap("setup")}}
	x.Snapshots[0].Permanents = append(x.Snapshots[0].Permanents, rules.OracleSnapPerm{
		Name: "Soldier", Token: true, PT: "1/1", Types: []string{"Soldier", "Creature"}, Colors: "W"})
	if v := Compare(g, nil, x); v.Status != Agree {
		t.Fatalf("%+v", v)
	}
	x.Snapshots[0].Permanents[1].PT = "2/2"
	if v := Compare(g, nil, x); v.Status != Diverge || v.Field != "permanents" {
		t.Fatalf("%+v", v)
	}
}

func TestCompareSeparatesHarnessFromEngineFailures(t *testing.T) {
	ok := XResult{Snapshots: []rules.OracleSnapshot{xsnap("setup")}}
	for _, tc := range []struct {
		fails  []string
		gerr   error
		x      XResult
		status Status
		field  string
	}{
		{nil, errors.New("bad json"), ok, Harness, ""},
		{nil, nil, XResult{Harness: "op x unsupported"}, Harness, ""},
		{[]string{"step 0 (cast): harness: card \"X\" not in the corpus"}, nil, ok, Harness, ""},
		{[]string{"step 0 (cast): no legal cast of Shock"}, nil, ok, Diverge, "action"},
		{[]string{"step 0 (cast): unconsumed answer(s) for this step: [...]"}, nil, ok, Diverge, "decisions"},
	} {
		g := rules.OracleResult{Fails: tc.fails, Snapshots: []rules.OracleSnapshot{snap("setup")}}
		if v := Compare(g, tc.gerr, tc.x); v.Status != tc.status || v.Field != tc.field {
			t.Errorf("fails %q: %+v, want %s %s", tc.fails, v, tc.status, tc.field)
		}
	}
}

func TestRefName(t *testing.T) {
	for in, want := range map[string]string{
		"p1:Grizzly Bears#2": "Grizzly Bears", "p0:token:Soldier Token": "Soldier Token",
		"Fire // Ice": "Fire // Ice", "p0:Borrowing 100,000 Arrows": "Borrowing 100,000 Arrows",
	} {
		if got := RefName(in); got != want {
			t.Errorf("RefName(%q) = %q, want %q", in, got, want)
		}
	}
}
