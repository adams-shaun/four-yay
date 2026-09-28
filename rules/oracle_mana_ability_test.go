package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestOracleManaAbilityOffered(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	run := func(t *testing.T, swamp bool, activate bool) *oracleRun {
		t.Helper()
		battlefield := []string{"Blazemire Verge"}
		if swamp {
			battlefield = append(battlefield, "Swamp")
		}
		sc := oracleScenario{
			Setup: map[string]oracleSeat{"p0": {Battlefield: battlefield}},
		}
		if activate {
			sc.Steps = []oracleStep{{Op: "activate", Seat: 0, Card: "p0:Blazemire Verge", Ability: "Add {R}"}}
			sc.Expect = []oracleExpect{{Pool: map[string]string{"p0": "R"}}}
		}
		fails, _, r := runOracleScenario(reg, sc)
		if len(fails) != 0 {
			t.Fatalf("scenario failed: %v", fails)
		}
		id, err := r.resolve("p0:Blazemire Verge")
		if err != nil {
			t.Fatal(err)
		}
		if r.e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatalf("precondition: Verge zone = %s", r.e.G.Obj(id).Zone)
		}
		return r
	}

	t.Run("conditional ability presence and absence", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			swamp bool
			want  bool
		}{{"without Swamp", false, false}, {"with Swamp", true, true}} {
			t.Run(tc.name, func(t *testing.T) {
				r := run(t, tc.swamp, false)
				id, _ := r.resolve("p0:Blazemire Verge")
				labels := r.manaAbilityLabels(0, id)
				if !oracleHasFold(labels, "Add B") {
					t.Fatalf("precondition: unconditional Add B absent from %v", labels)
				}
				wantRed := tc.want
				if oracleHasFold(labels, "Add R") != wantRed {
					t.Fatalf("Add R available in %v = %v, want %v", labels, oracleHasFold(labels, "Add R"), wantRed)
				}
				x := oracleExpect{Offered: &oracleOffered{Seat: 0, Kind: "activate", Card: "p0:Blazemire Verge", Label: "Add {R}"}, Want: &wantRed}
				if bad := r.check(x); len(bad) > 0 {
					t.Fatal(bad)
				}
			})
		}
	})

	t.Run("activate selects named second-stage ability", func(t *testing.T) {
		r := run(t, true, true)
		if got := poolString(r.e.G.Players[0].Pool); got != "R" {
			t.Fatalf("pool %q, want R (not the Add B fallback)", got)
		}
	})

	// The generic "Activate <card> for mana" priority option must never stand
	// in for an ability the source cannot currently produce: the step has to
	// fail loudly, not silently activate the one available ability instead.
	t.Run("activate of a gated-out ability fails loudly", func(t *testing.T) {
		r := run(t, false, false)
		id, _ := r.resolve("p0:Blazemire Verge")
		labels := r.manaAbilityLabels(0, id)
		if !oracleHasFold(labels, "Add B") {
			t.Fatalf("precondition: source has no mana ability at all in %v -- the failure below would not be about the gate", labels)
		}
		sc := oracleScenario{
			Setup: map[string]oracleSeat{"p0": {Battlefield: []string{"Blazemire Verge"}}},
			Steps: []oracleStep{{Op: "activate", Seat: 0, Card: "p0:Blazemire Verge", Ability: "Add {R}"}},
		}
		fails, _, _ := runOracleScenario(reg, sc)
		if len(fails) == 0 {
			t.Fatal("activate Add {R} with no Swamp succeeded: the harness fell back to a different mana ability")
		}
		named := false
		for _, f := range fails {
			if strings.Contains(f, "not offered") && strings.Contains(f, "Add {R}") {
				named = true
			}
		}
		if !named {
			t.Fatalf("expected a not-offered failure naming Add {R}, got %v", fails)
		}
	})

	// A non-mana ability label must never fall back to the generic mana
	// activation either (Mistveil Plains' graveyard ability is not a mana
	// ability; without white mana in the pool it is not even offered).
	t.Run("activate with a non-mana label never activates mana", func(t *testing.T) {
		sc := oracleScenario{
			Setup: map[string]oracleSeat{"p0": {Battlefield: []string{"Mistveil Plains", "Savannah Lions", "Savannah Lions"}, Graveyard: []string{"Grizzly Bears"}}},
			Steps: []oracleStep{{Op: "activate", Seat: 0, Card: "p0:Mistveil Plains", Ability: "Put"}},
		}
		fails, _, _ := runOracleScenario(reg, sc)
		if len(fails) == 0 {
			t.Fatal("activate ability Put on Mistveil Plains succeeded: the harness silently activated the mana ability")
		}
		named := false
		for _, f := range fails {
			if strings.Contains(f, "not offered") {
				named = true
			}
		}
		if !named {
			t.Fatalf("expected a not-offered failure, got %v", fails)
		}
	})
}
