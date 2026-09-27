package decision

import (
	"testing"
	"time"

	"github.com/adams-shaun/gorge/state"
)

// TestRequiredQuotaIsExactPastSearchBudget guards against returning a
// suboptimal best-so-far quota when required options trade life for Value.
func TestRequiredQuotaIsExactPastSearchBudget(t *testing.T) {
	var options []Option
	for i := 0; i < 19; i++ {
		obj := state.ObjID(100 + i)
		if i == 0 {
			options = append(options,
				Option{Index: len(options), Kind: "attacker", Obj: obj, Required: true, CostLife: 8},
				Option{Index: len(options) + 1, Kind: "attacker", Obj: obj, Required: true, Value: 1},
			)
			continue
		}
		options = append(options,
			Option{Index: len(options), Kind: "attacker", Obj: obj, Required: true, CostLife: 1},
			Option{Index: len(options) + 1, Kind: "attacker", Obj: obj, Required: true, Value: 1},
		)
	}
	d := &Decision{Kind: KAttackers, Max: 19, MaxSum: 8, PayerLife: 8, Options: options}

	// The optimum is 16: eight units of life and eight units of Value. The
	// all-19 declaration exceeds both bounds, while a concrete 16-object mix
	// proves the feasible cardinality.
	all := make([]int, len(options))
	for i := range all {
		all[i] = i
	}
	if d.ChargeOptionsFit(all) {
		t.Fatal("precondition: all 19 required creatures must exceed the charge bound")
	}
	want := []int{1}
	for i := 1; i <= 8; i++ {
		want = append(want, 2*i)
	}
	for i := 9; i <= 15; i++ {
		want = append(want, 2*i+1)
	}
	if !d.ChargeOptionsFit(want) {
		t.Fatalf("precondition: the 16-creature witness %v must fit", want)
	}
	if got := d.RequiredQuota(); got != 16 {
		t.Fatalf("RequiredQuota = %d, want exact maximum 16", got)
	}
}

// A normal token-sized declaration must stay quick even when every distinct
// required attacker has an individual life-priced obligation. The exact quota
// is half the board because each attack costs two life.
func TestRequiredQuotaChargedManyAttackersIsBounded(t *testing.T) {
	const attackers = 32
	options := make([]Option, attackers)
	for i := range options {
		options[i] = Option{
			Index: i, Kind: "attacker", Obj: state.ObjID(100 + i),
			Required: true, CostPhyrexian: 1,
		}
	}
	d := &Decision{Kind: KAttackers, Max: attackers, PayerLife: attackers, Options: options}
	if len(d.Options) != attackers || d.PayerLifeBound() != attackers {
		t.Fatalf("precondition: got %d options and life bound %d, want %d each", len(d.Options), d.PayerLifeBound(), attackers)
	}
	start := time.Now()
	got := d.RequiredQuota()
	elapsed := time.Since(start)
	if got != attackers/2 {
		t.Fatalf("RequiredQuota = %d, want %d of %d charged attackers", got, attackers/2, attackers)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("RequiredQuota took %s for %d single-option charged attackers; want under 2s", elapsed, attackers)
	}
}
