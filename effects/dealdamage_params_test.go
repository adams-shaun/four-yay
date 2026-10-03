package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestDealDamageKnownKeysSorted: the unread lookup binary-searches the table.
func TestDealDamageKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(dealDamageKnownKeys[:]) || len(slices.Compact(slices.Clone(dealDamageKnownKeys[:]))) != len(dealDamageKnownKeys) {
		t.Fatal("dealDamageKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileDealDamage pins the compiled shapes the target ask, the payment
// planner and the resolution share: the amount, the division, the riders and
// the unread report.
func TestCompileDealDamage(t *testing.T) {
	sa := &cards.SA{API: "DealDamage", Params: map[string]string{
		"NumDmg": "X", "DividedAsYouChoose": "4", "RememberDamaged": "True",
		"ExcessSVar": " Excess ", "ExcessSVarCondition": "Creature", "DamageSource": " Targeted ",
		"DamageMap": "True", "RelativeTarget": "True", "ReplaceDyingDefined": "Remembered.Creature",
		"Hidden": "True",
	}}
	p := DealDamageOf(sa)
	if p.NumDmg != (ParamText{Text: "X", Present: true}) || !p.Divided || p.DividedTotal != (ParamText{Text: "4", Present: true}) {
		t.Fatalf("amounts = %+v", p)
	}
	if !p.RememberDamaged || p.ExcessSVar != "Excess" || p.ExcessSVarCondition != "Creature" ||
		p.DamageSource != "Targeted" || !p.DamageMap || !p.RelativeTarget || p.ReplaceDyingDefined != "Remembered.Creature" {
		t.Fatalf("riders = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if DealDamageOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	q := DealDamageOf(&cards.SA{API: "DealDamage", Params: map[string]string{"DividedAsYouChoose": " ", "DamageMap": "False"}})
	if q.NumDmg.Present || q.Divided || q.DamageMap || q.RememberDamaged || q.Unread != nil {
		t.Fatalf("absent shapes = %+v", q)
	}
	if got := DamageAmount(&cards.SA{API: "DamageAll", Params: map[string]string{"NumDmg": "2"}}); got != (ParamText{Text: "2", Present: true}) {
		t.Fatalf("DamageAll amount = %+v", got)
	}
}

// TestDealDamageOfIsAllocationFree: the resolution's read of its compiled
// parameters (a configured record or a front-cache hit) allocates nothing --
// DealDamage is a hot path in the random-burn bench rows.
func TestDealDamageOfIsAllocationFree(t *testing.T) {
	bound := &cards.SA{API: "DealDamage", Params: map[string]string{"NumDmg": "3", "Defined": "Targeted"}}
	f := NewSAFacts(bound)
	f.Publish()
	cached := &cards.SA{API: "DealDamage", Params: map[string]string{"NumDmg": "2"}}
	DealDamageOf(cached)
	if n := testing.AllocsPerRun(100, func() {
		_ = DealDamageOf(bound)
		_ = DealDamageOf(cached)
		_ = DamageAmount(bound)
	}); n != 0 {
		t.Fatalf("DealDamageOf allocated %v objects per run; want 0", n)
	}
	if f.DealDamage == nil || !f.DealDamage.boundTo(bound.Params) {
		t.Fatal("NewSAFacts did not compile the DealDamage half")
	}
}
