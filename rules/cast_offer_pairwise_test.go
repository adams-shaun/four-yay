package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// The cast-offer census and the post-push target ask share ONE feasibility
// rule: rules/legal.go's targetChoiceFeasible. A mandatory target declaration
// that cannot admit any legal selection under a cross-target set constraint is
// WITHHELD at offer time, never offered and then reversed with the CR 733.1
// "cast aborted: no legal target" note. CR 601.2c is the reason: a spell the
// engine offers must be one whose target declaration can actually be
// announced.
//
// This file pins the CROSS-CONTROLLER half of that contract. Before the shared
// rule the offer census was count-only (legal candidates >= the mandatory
// minimum), so a pairwise shape the ask would reverse was still offered; the
// resolution-path ask still owns its own fizzle (stack.go askTarget,
// TestBarrinsSpiteSameControllerCapacity).

// TestCastOfferCensusWithholdsRunAwayTogetherSingleController pins the real
// corpus card: Run Away Together's mandatory two-target
// TargetsWithDifferentControllers$ ask with exactly two legal creatures under
// ONE controller has no legal answer, so the offer must not present it at all.
func TestCastOfferCensusWithholdsRunAwayTogetherSingleController(t *testing.T) {
	t.Parallel()
	e, spell, bears := runAwayTogetherEngine(t, []state.PlayerID{1, 1})
	o := e.G.Obj(spell)
	if o == nil || o.Zone != state.ZHand || o.Face() == nil || len(bears) != 2 {
		t.Fatalf("precondition: Run Away Together must be in hand with two bears: spell %+v, bears %v", o, bears)
	}
	sa := o.Face().SpellAbility()
	if sa == nil || sa.Params["TargetMin"] != "2" || sa.Params["TargetMax"] != "2" ||
		sa.Params["TargetsWithDifferentControllers"] != "True" {
		t.Fatalf("precondition: Run Away Together lost its mandatory pairwise targets: %+v", sa)
	}
	candidates := e.legalTargetCandidates(0, spell, spell, sa)
	_, _, _, distinct := e.oneEachTargetBounds(sa, candidates, 2, 2)
	if len(candidates) != 2 || distinct != 1 || candidates[0].obj == candidates[1].obj {
		t.Fatalf("precondition: need two different candidates but only one controller: candidates %+v, distinct %d", candidates, distinct)
	}
	if castOffered(e, spell) {
		t.Fatal("Run Away Together offered with two same-controller creatures: the offer census must enforce the same different-controller capacity the ask does (CR 601.2c)")
	}
}

// pairwiseCensusSrc is a mandatory TargetMin$ 2 | TargetMax$ 2
// TargetsWithSameController$ spell. Its two legal candidates below are split
// across controllers, so the candidate COUNT reaches the minimum while the
// same-controller capacity does not: the shared feasibility rule must WITHHOLD
// the cast at offer time instead of offering it and reversing at the ask.
const pairwiseCensusSrc = "Name:Pairwise Census\nManaCost:1 W\nTypes:Instant\n" +
	"A:SP$ Draw | Defined$ You | ValidTgts$ Creature.Other | TargetMin$ 2 | " +
	"TargetMax$ 2 | TargetsWithSameController$ True | Oracle:x\n"

func TestCastOfferCensusWithholdsPairwiseConstrainedCast(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 6015, pairwiseCensusSrc)
	bearA := bearPermanent(t, e, 0)
	bearB := bearPermanent(t, e, 1)
	if a, b := e.G.Obj(bearA), e.G.Obj(bearB); a == nil || b == nil || a.Zone != state.ZBattlefield || b.Zone != state.ZBattlefield || a.Controller == b.Controller {
		t.Fatalf("precondition: bears are not battlefield permanents under distinct controllers: %+v %+v", a, b)
	}
	addMana(t, e, 0, "1W")
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand || o.Face() == nil {
		t.Fatalf("precondition: pairwise census spell is not a face-up hand card: %+v", o)
	}
	sa := o.Face().SpellAbility()
	if sa == nil || sa.Params["TargetMin"] != "2" || sa.Params["TargetMax"] != "2" ||
		sa.Params["TargetsWithSameController"] != "True" {
		t.Fatalf("precondition: fixture lost the mandatory same-controller pair shape: %+v", sa)
	}
	candidates := e.legalTargetCandidates(0, id, id, sa)
	if len(candidates) != 2 {
		t.Fatalf("precondition: target census found %d legal candidates, want the two bears", len(candidates))
	}
	// The boundary's precondition: the count reaches the minimum but the
	// same-controller capacity does not.
	_, _, capacity, constrained := e.sameControllerTargetBounds(sa, candidates, 2, 2)
	if !constrained || capacity != 1 {
		t.Fatalf("precondition: same-controller bound = (capacity %d, constrained %v), want constrained capacity 1", capacity, constrained)
	}
	if castOffered(e, id) {
		t.Fatal("pairwise-constrained cast offered with creatures split across controllers: the offer census must enforce the same-controller capacity the ask does (CR 601.2c)")
	}
}
