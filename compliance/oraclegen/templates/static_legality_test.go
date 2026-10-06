package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

func TestCanAttackDefenderGhalta(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Ghalta, the Immovable", "static#0.1", "static.can-attack-defender")
	res, ok := runStatic(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 || !snapshotPermanentAttacking(res.Snapshots, "p0:"+defenderProbe) {
		t.Fatal("precondition: generated attack does not reach the attacking state")
	}
}

func TestCantBlockByTetsuko(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Tetsuko Umezawa, Fugitive", "static#0.0", "static.cant-block-by")
	want := false
	if got := it.Steps[len(it.Steps)-1].Expect[0]; got.CanBlock == nil || got.Want == nil || *got.Want != want {
		t.Fatalf("item lacks the negative block-pair assertion: %+v", got)
	}
	control := it.Scenario
	control.Setup = cloneOracleSetup(control.Setup)
	p0 := control.Setup["p0"]
	p0.Battlefield = removeFixture(p0.Battlefield, it.Card)
	control.Setup["p0"] = p0
	control.Steps = append([]oraclegen.Step(nil), control.Steps...)
	control.Steps[len(control.Steps)-1].Expect = append([]oraclegen.Expect(nil), control.Steps[len(control.Steps)-1].Expect...)
	control.Steps[len(control.Steps)-1].Expect[0].Want = boolPtr(true)
	if res, ok := runStatic(reg, control); !ok || len(res.Fails) != 0 {
		t.Fatalf("precondition: control does not offer the same block pair: %v", res.Fails)
	}
}

func TestCantBeCastProft(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Proft, Sinister Mastermind", "static#0.0", "static.cant-be-cast-threshold")
	last := it.Steps[len(it.Steps)-1]
	if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || *last.Expect[0].Want {
		t.Fatalf("item lacks offered-cast want=false assertion: %+v", last.Expect)
	}
	control := it.Scenario
	control.Setup = cloneOracleSetup(control.Setup)
	p0 := control.Setup["p0"]
	p0.Graveyard = []string{"Plains", "Mountain", "Island", "Forest", "Swamp", "Shock", "Grizzly Bears"}
	control.Setup["p0"] = p0
	control.Steps = append([]oraclegen.Step(nil), control.Steps...)
	control.Steps[len(control.Steps)-1].Expect = append([]oraclegen.Expect(nil), control.Steps[len(control.Steps)-1].Expect...)
	control.Steps[len(control.Steps)-1].Expect[0].Want = boolPtr(true)
	if res, ok := runStatic(reg, control); !ok || len(res.Fails) != 0 {
		t.Fatalf("precondition: seven-card graveyard control does not offer Proft: %v", res.Fails)
	}
}

func TestCantBeCastYuriko(t *testing.T) {
	reg := loadGenRegistry(t)
	it := staticItemFor(t, reg, "Yuriko, Blade of the Mighty", "static#0.0", "static.cant-be-cast-combat")
	last := it.Steps[len(it.Steps)-1]
	if last.Step != "begin-combat" {
		t.Fatalf("checkpoint %q, want begin-combat", last.Step)
	}
	if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || *last.Expect[0].Want {
		t.Fatalf("item lacks offered-cast want=false assertion: %+v", last.Expect)
	}
	control := it.Scenario
	control.Setup = cloneOracleSetup(control.Setup)
	p0 := control.Setup["p0"]
	p0.Battlefield = removeFixture(p0.Battlefield, it.Card)
	p0.Battlefield = append(p0.Battlefield, "Mountain")
	control.Setup["p0"] = p0
	control.Steps = append([]oraclegen.Step(nil), control.Steps...)
	control.Steps[len(control.Steps)-1] = oraclegen.Step{Op: "pass_to", Seat: 0, Decision: "priority", Expect: []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "cast", Card: "p0:Lightning Bolt"}, Want: boolPtr(true)}}}
	if res, ok := runStatic(reg, control); !ok || len(res.Fails) != 0 {
		t.Fatalf("precondition: outside-combat control does not offer cast: %v", res.Fails)
	}
}

func TestCantBeActivatedYuriko(t *testing.T) {
	reg := loadGenRegistry(t)
	staticItemFor(t, reg, "Yuriko, Blade of the Mighty", "static#0.1", "static.cant-be-activated-combat")
}

func TestCanAttackDefenderSurveillance(t *testing.T) {
	reg := loadGenRegistry(t)
	c, ok := reg.Lookup("Surveillance Phantasm")
	if !ok {
		t.Fatal("precondition: Surveillance Phantasm absent")
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key == "static#0.0" {
			if req.Sub != "static.can-attack-defender-svar" {
				t.Fatalf("classified %s", req.Sub)
			}
			it, skip := GenerateB(reg, "Surveillance Phantasm", req)
			if skip != nil {
				t.Fatalf("Surveillance: %s", skip.Reason)
			}
			res, ok := runStatic(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 || !snapshotPermanentAttacking(res.Snapshots, "p0:Surveillance Phantasm") {
				t.Fatalf("precondition: surveilled Phantasm did not reach the attacking state: %v", res.Fails)
			}
			return
		}
	}
	t.Fatal("precondition: missing static#0.0")
}

func cloneOracleSetup(src map[string]oraclegen.Seat) map[string]oraclegen.Seat {
	out := make(map[string]oraclegen.Seat, len(src))
	for seat, s := range src {
		s.Battlefield = append([]string(nil), s.Battlefield...)
		s.Tapped = append([]string(nil), s.Tapped...)
		s.Hand = append([]string(nil), s.Hand...)
		s.Graveyard = append([]string(nil), s.Graveyard...)
		s.Exile = append([]string(nil), s.Exile...)
		s.Library = append([]string(nil), s.Library...)
		s.LibraryTop = append([]string(nil), s.LibraryTop...)
		out[seat] = s
	}
	return out
}

func removeFixture(in []string, name string) []string {
	out := make([]string, 0, len(in))
	for _, got := range in {
		if got != name {
			out = append(out, got)
		}
	}
	return out
}

func snapshotPermanentAttacking(snaps []rules.OracleSnapshot, ref string) bool {
	for _, snap := range snaps {
		for _, p := range snap.Permanents {
			if p.Ref == ref && p.Attacking {
				return true
			}
		}
	}
	return false
}
