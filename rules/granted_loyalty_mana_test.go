package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestGrantedLoyaltyManaAbilityIsOfferedAsAnAbility pins CR 605.1a on Way of
// the Pyromancer: its granted "[+1]: Add {R}." is a LOYALTY ability, not a
// mana ability, so it is offered through the ordinary ability option on the
// planeswalker it is granted to -- labelled with the ability's own text -- and
// is NOT a member of the mana-ability set the payment window reads. Without
// the source neither appears, so the assertion is the grant's doing.
func TestGrantedLoyaltyManaAbilityIsOfferedAsAnAbility(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	// offer advances p0 to its first main phase and returns the run and
	// Ajani's id, asserting the board precondition.
	offer := func(t *testing.T, battlefield ...string) (*oracleRun, state.ObjID) {
		t.Helper()
		fails, _, r := runOracleScenario(reg, oracleScenario{
			Setup: map[string]oracleSeat{"p0": {Battlefield: battlefield}},
			Steps: []oracleStep{{Op: "pass_to", Seat: 0, Step: "main1", Decision: "priority"}},
		})
		if len(fails) != 0 {
			t.Fatalf("scenario failed: %v", fails)
		}
		id, err := r.resolve("p0:Ajani Goldmane")
		if err != nil {
			t.Fatal(err)
		}
		o := r.e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Ajani is not on the battlefield: %+v", o)
		}
		return r, id
	}
	grantedOption := func(t *testing.T, r *oracleRun, id state.ObjID) bool {
		t.Helper()
		for _, o := range r.e.Pending().Options {
			if o.Kind == "ability" && o.Obj == id && strings.Contains(strings.ToLower(o.Label), "add {r}") {
				return true
			}
		}
		return false
	}

	r, id := offer(t, "Way of the Pyromancer", "Ajani Goldmane")
	if !grantedOption(t, r, id) {
		t.Fatalf("the granted [+1]: Add {R} is not offered on Ajani: %+v", r.e.Pending().Options)
	}
	for _, l := range r.manaAbilityLabels(0, id) {
		if strings.Contains(strings.ToLower(l), "add r") {
			t.Fatalf("the granted loyalty ability is a mana-ability member %q (CR 605.1a excludes loyalty abilities)", l)
		}
	}

	ctl, cid := offer(t, "Ajani Goldmane")
	if grantedOption(t, ctl, cid) {
		t.Fatalf("without the source the option is still offered: %+v", ctl.e.Pending().Options)
	}
}
