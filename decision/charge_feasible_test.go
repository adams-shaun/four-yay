package decision

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestRequiredQuotaMaximizesFeasibleSetAcrossLifeAndMana is the review-r2
// MAJOR's reproduction at the shared rule's home: the required quota must
// select a MAXIMUM-CARDINALITY set that is feasible across BOTH the life
// (CostLife plus each pip) and the mana (MaxSum) constraints, not greedily
// take the first option that fits one dimension. Three required options at
// four life: a 3-life option and two one-pip (2-life) options, all Value 0.
// The ascending-Value greedy takes the 3-life option first, then cannot fit
// either pip -- quota 1 -- although the two pips fit together (4 life), so
// CR 508.1d's "attack with as many as possible" demands quota 2.
func TestRequiredQuotaMaximizesFeasibleSetAcrossLifeAndMana(t *testing.T) {
	d := &Decision{Kind: KAttackers, Min: 0, Max: 3, PayerLife: 4, Options: []Option{
		{Index: 0, Kind: "attacker", Obj: 1, Required: true, CostLife: 3},
		{Index: 1, Kind: "attacker", Obj: 2, Required: true, CostPhyrexian: 1},
		{Index: 2, Kind: "attacker", Obj: 3, Required: true, CostPhyrexian: 1},
	}}
	// Preconditions: every option is individually affordable, the two pips
	// fit together, and all three do not -- so the maximum-cardinality answer
	// is exactly the two pips and a greedy first-fit is wrong.
	for _, o := range d.Options {
		if int(o.chargeLifeCost()) > int(d.PayerLife) {
			t.Fatalf("fixture wrong: option %d costs %d, over the %d-life bound on its own", o.Index, o.chargeLifeCost(), d.PayerLife)
		}
	}
	if !d.ChargeOptionsFit([]int{1, 2}) {
		t.Fatal("precondition: the two pips (4 life) must fit")
	}
	if d.ChargeOptionsFit([]int{0, 1, 2}) {
		t.Fatal("precondition: 3 + 2 + 2 life must not fit a 4-life bound")
	}
	if q := d.RequiredQuota(); q != 2 {
		t.Fatalf("RequiredQuota = %d, want 2 (the two pips fit together; the 3-life option is the greedy trap)", q)
	}
	got := d.FitRequired([]int{0, 1, 2})
	if !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("FitRequired([0 1 2]) = %v, want [1 2] (maximum-cardinality feasible set)", got)
	}
	if err := d.Validate(Intent{Choices: got}); err != nil {
		t.Fatalf("repaired answer %v failed Validate: %v", got, err)
	}
}

// TestRequiredQuotaLargeChargedSetScalesExactly pins that the maximum-
// cardinality search stays exact on a larger charged set (and finishes well
// inside its node budget): twenty one-pip required options at twenty life
// must yield quota ten, not the greedy's first-fit count.
func TestRequiredQuotaLargeChargedSetScalesExactly(t *testing.T) {
	var opts []Option
	for i := 0; i < 20; i++ {
		opts = append(opts, Option{Index: i, Kind: "attacker", Obj: state.ObjID(100 + i), Required: true, CostPhyrexian: 1})
	}
	d := &Decision{Kind: KAttackers, Min: 0, Max: 20, PayerLife: 20, Options: opts}
	if !d.ChargeOptionsFit([]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}) {
		t.Fatal("precondition: ten pips (20 life) must fit")
	}
	all := make([]int, 20)
	for i := range all {
		all[i] = i
	}
	if d.ChargeOptionsFit(all) {
		t.Fatal("precondition: twenty pips (40 life) must not fit")
	}
	if q := d.RequiredQuota(); q != 10 {
		t.Fatalf("RequiredQuota = %d, want 10 (twenty pips at two life against twenty life)", q)
	}
}

// half: one required creature may attack different defenders at different
// prices, and the maximum-cardinality set must choose the option that lets
// the other required creature in. Obj 1's 3-life pair and Obj 2's 2-life pair
// fit (5 <= 5) while Obj 1's 4-life pair would not; the search must take
// Obj 1's cheaper pair so both Objs are covered.
func TestRequiredQuotaPicksCheapestFeasibleOptionPerObj(t *testing.T) {
	d := &Decision{Kind: KAttackers, Min: 0, Max: 2, PayerLife: 5, Options: []Option{
		{Index: 0, Kind: "attacker", Obj: 1, Required: true, CostLife: 3},
		{Index: 1, Kind: "attacker", Obj: 1, Required: true, CostLife: 4},
		{Index: 2, Kind: "attacker", Obj: 2, Required: true, CostPhyrexian: 1},
	}}
	if q := d.RequiredQuota(); q != 2 {
		t.Fatalf("RequiredQuota = %d, want 2 (Obj 1's 3-life pair plus Obj 2's pip = 5 life)", q)
	}
	got := d.FitRequired([]int{0, 2})
	if !reflect.DeepEqual(got, []int{0, 2}) {
		t.Fatalf("FitRequired([0 2]) = %v, want [0 2]", got)
	}
	if !d.ChargeOptionsFit(got) {
		t.Fatalf("repaired answer %v is not charge-feasible (life %d)", got, d.PayerLife)
	}
	if err := d.Validate(Intent{Choices: got}); err != nil {
		t.Fatalf("repaired answer %v failed Validate: %v", got, err)
	}
}

// TestBlockRequiredTeamChargeBoundKeepsTeamMinimum is the review-r2 MAJOR's
// block-side reproduction: the combined charge bound must be solved WITH the
// team minima inside the block-team search, never by filtering a finished
// team pair by pair. Two blockers of a MinBlockers=2 attacker, each carrying
// one pip, at two life: each pair is individually affordable (2 life) and a
// lone blocker is not a legal team, but the pair (4 life) overruns the
// bound, so no legal team can satisfy the requirement and the quota must be
// 0 -- not a pruned single-blocker "team" the engine rejects.
func TestBlockRequiredTeamChargeBoundKeepsTeamMinimum(t *testing.T) {
	d := &Decision{Kind: KBlockers, Min: 0, Max: 2, PayerLife: 2, Options: []Option{
		{Index: 0, Kind: "block", Obj: 10, Attacker: 100, BlockMust: true, Required: true, MinBlockers: 2, CostPhyrexian: 1},
		{Index: 1, Kind: "block", Obj: 11, Attacker: 100, MinBlockers: 2, CostPhyrexian: 1},
	}}
	// Preconditions: the Min$ attacker means a lone blocker is illegal, and
	// the two-pip team overruns the bound -- so the pair-by-pair filter's
	// [0] is an illegal team and the correct maximum is empty.
	if d.blockCountLegal([]int{0}) {
		t.Fatal("fixture wrong: a lone blocker of a MinBlockers=2 attacker must be illegal")
	}
	if !d.blockCountLegal([]int{0, 1}) {
		t.Fatal("fixture wrong: the two-blocker team must be legal")
	}
	if d.ChargeOptionsFit([]int{0, 1}) {
		t.Fatal("fixture wrong: two pips (4 life) must not fit a 2-life bound")
	}
	team := d.BlockRequiredTeam()
	if !d.blockCountLegal(team) {
		t.Fatalf("BlockRequiredTeam = %v is not a legal team", team)
	}
	if !d.ChargeOptionsFit(team) {
		t.Fatalf("BlockRequiredTeam = %v is not charge-feasible (life %d)", team, d.PayerLife)
	}
	if len(team) != 0 {
		t.Fatalf("BlockRequiredTeam = %v, want [] (no legal team is payable)", team)
	}
	if q := d.RequiredQuota(); q != 0 {
		t.Fatalf("RequiredQuota = %d, want 0 (no payable legal team satisfies the requirement)", q)
	}
	repaired := d.FitRequired([]int{0, 1})
	if !d.blockCountLegal(repaired) {
		t.Fatalf("FitRequired([0 1]) = %v is not a legal team", repaired)
	}
	if !d.ChargeOptionsFit(repaired) {
		t.Fatalf("FitRequired([0 1]) = %v is not charge-feasible (life %d)", repaired, d.PayerLife)
	}
	if len(repaired) != 0 {
		t.Fatalf("FitRequired([0 1]) = %v, want [] (the illegal/pruned singleton must never be returned)", repaired)
	}
}

// TestBlockRequiredTeamChargeBoundKeepsPayableTeam pins the other side: when
// the bound is large enough for the whole team, the charge rule must not
// prune a member the Min$ attacker needed -- the result is the full legal
// team and the requirement counts.
func TestBlockRequiredTeamChargeBoundKeepsPayableTeam(t *testing.T) {
	d := &Decision{Kind: KBlockers, Min: 0, Max: 2, PayerLife: 4, Options: []Option{
		{Index: 0, Kind: "block", Obj: 10, Attacker: 100, BlockMust: true, Required: true, MinBlockers: 2, CostPhyrexian: 1},
		{Index: 1, Kind: "block", Obj: 11, Attacker: 100, MinBlockers: 2, CostPhyrexian: 1},
	}}
	if !d.ChargeOptionsFit([]int{0, 1}) {
		t.Fatal("fixture wrong: two pips (4 life) must fit a 4-life bound")
	}
	team := d.BlockRequiredTeam()
	if !reflect.DeepEqual(team, []int{0, 1}) {
		t.Fatalf("BlockRequiredTeam = %v, want [0 1] (the Min$ team is payable)", team)
	}
	if q := d.RequiredQuota(); q != 1 {
		t.Fatalf("RequiredQuota = %d, want 1 (one required blocker, helper counts nothing)", q)
	}
}
