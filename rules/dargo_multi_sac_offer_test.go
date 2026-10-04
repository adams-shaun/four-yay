package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// Both announced sacrifice parts use the same X. The broad Dargo part can
// pay X=3 (making {1}{R} enough), but the added artifact-only part can pay
// only X=1. An offer based on the broad part's capacity would abort when the
// narrower part is settled, because neither X=0 nor X=1 reduces enough.
func TestDargoMultipleSacXPartsWithholdUnsettleableOffer(t *testing.T) {
	t.Parallel()
	e, spell, ids := dargoEngine(t, []string{
		"Name:Anvil\nTypes:Artifact\nOracle:x\n",
		"Name:Crab\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:Boar\nTypes:Creature\nPT:2/2\nOracle:x\n",
	}, "1R")
	for _, id := range ids {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: sacrifice candidate %d is not on battlefield", id)
		}
	}
	base := pay.WithSpellAbilityExtras(e.G.Obj(spell).Face(), e.castOfferBase(0, spell))
	if len(base.Sac) != 1 || !base.Sac[0].Announced {
		t.Fatalf("precondition: Dargo cost does not carry one announced Sac part: %+v", base.Sac)
	}
	extra := ParseCost("Sac<X/Artifact>")
	if len(extra.Sac) != 1 || !extra.Sac[0].Announced {
		t.Fatalf("precondition: extra cost is not an announced Sac: %+v", extra.Sac)
	}
	if got := len(pay.SacrificeCostCandidates(asPayer(e), 0, spell, base.Sac[0], false)); got != 3 {
		t.Fatalf("precondition: broad sacrifice pool=%d, want 3", got)
	}
	if got := len(pay.SacrificeCostCandidates(asPayer(e), 0, spell, extra.Sac[0], false)); got != 1 {
		t.Fatalf("precondition: narrow sacrifice pool=%d, want 1", got)
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 2 || pool[state.MR] != 1 {
		t.Fatalf("precondition: pool=%+v, want {1}{R} (cannot pay even X=1's {4}{R})", pool)
	}
	if !e.offerCastable(0, spell, base, spellScope(""), false) {
		t.Fatal("precondition: Dargo with just the broad sacrifice cost must be offerable at X=3")
	}
	if e.offerCastable(0, spell, base.Plus(extra), spellScope(""), false) {
		t.Fatal("offered cost requiring two shared-X sacrifices when artifact-only part caps X at 1")
	}
}
