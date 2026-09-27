package decision

import (
	"reflect"
	"strings"
	"testing"
)

// TestChargeTapPoolFitIsTheSharedRule pins the one home for the
// declaration-dependent tap rule: the published pool (ChargeTapPool) minus the
// candidates the chosen options consume (each Option.TapPoolCost) must still
// cover the obligations the chosen options owe (each Option.CostTaps). The
// three-vs-two shape is Hollow Warrior plus two uncharged creatures: the
// two-creature declaration leaves the pool payable, the all-three declaration
// does not.
func TestChargeTapPoolFitIsTheSharedRule(t *testing.T) {
	d := &Decision{Kind: KAttackers, Min: 0, Max: 3, ChargeTapPool: 3, Options: []Option{
		{Index: 0, Kind: "attacker", Obj: 1, CostTaps: 1, TapPoolCost: 1},
		{Index: 1, Kind: "attacker", Obj: 2, TapPoolCost: 1},
		{Index: 2, Kind: "attacker", Obj: 3, TapPoolCost: 1},
	}}
	// PRECONDITION: the compared sets really differ -- all three consumes the
	// whole pool, two leaves one -- and the option metadata matches.
	if len(d.Options) != 3 || d.Options[0].CostTaps != 1 || d.Options[1].TapPoolCost != 1 {
		t.Fatalf("fixture wrong: %+v", d.Options)
	}
	if !d.ChargeTapPoolFit([]int{0, 1}) {
		t.Fatal("two-creature declaration must leave the pool payable")
	}
	if d.ChargeTapPoolFit([]int{0, 1, 2}) {
		t.Fatal("all-three declaration exhausts the pool and must not fit")
	}
	// The predicate folds into ChargeOptionsFit, so the repair never restores
	// a pool-exhausting set.
	if !d.ChargeOptionsFit([]int{0, 1}) || d.ChargeOptionsFit([]int{0, 1, 2}) {
		t.Fatal("ChargeOptionsFit must carry the tap-pool rule")
	}
	// Validate is the wire half; the rejection names the obligation.
	err := d.Validate(Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1, 2}})
	if err == nil || !strings.Contains(err.Error(), "tap obligation") {
		t.Fatalf("Validate error = %v, want a tap-obligation rejection", err)
	}
	if err := d.Validate(Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err != nil {
		t.Fatalf("Validate rejected the legal declaration: %v", err)
	}
}

// TestChargeTapPoolFitIsInertWithoutAPublishedPool pins the byte-identical
// ordinary case: no published pool leaves the rule inert even when options
// carry CostTaps, so a hand-built decision that publishes nothing is unchanged
// (the historical drop still happens in ChargeOptionConstraints, below).
func TestChargeTapPoolFitIsInertWithoutAPublishedPool(t *testing.T) {
	d := &Decision{Kind: KAttackers, Min: 0, Max: 2, Options: []Option{
		{Index: 0, Kind: "attacker", Obj: 1, CostTaps: 1},
		{Index: 1, Kind: "attacker", Obj: 2},
	}}
	if !d.ChargeTapPoolFit([]int{0, 1}) {
		t.Fatal("an unpublished pool must leave ChargeTapPoolFit inert")
	}
	if err := d.Validate(Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err != nil {
		t.Fatalf("an unpublished pool must not change Validate: %v", err)
	}
}

// TestChargeOptionConstraintsVerifiesPublishedTapPool pins the bot filter's
// half: with a published pool it KEEPS a tap-costed pick while the pool stays
// payable and drops the pick that would exhaust it -- so the guard's answer is
// exactly what Decision.Validate accepts. With no pool published it drops the
// tap-costed pick outright, the historical conservative direction.
func TestChargeOptionConstraintsVerifiesPublishedTapPool(t *testing.T) {
	d := &Decision{Kind: KAttackers, Min: 0, Max: 3, ChargeTapPool: 3, Options: []Option{
		{Index: 0, Kind: "attacker", Obj: 1, CostTaps: 1, TapPoolCost: 1},
		{Index: 1, Kind: "attacker", Obj: 2, TapPoolCost: 1},
		{Index: 2, Kind: "attacker", Obj: 3, TapPoolCost: 1},
	}}
	got := ChargeOptionConstraints(d, []int{0, 1, 2}, d.PayerLifeBound(), 0)
	if !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("ChargeOptionConstraints = %v, want [0 1] (third pick exhausts the pool)", got)
	}
	if err := d.Validate(Intent{Seq: d.Seq, Player: d.Player, Choices: got}); err != nil {
		t.Fatalf("the kept answer failed Validate: %v", err)
	}

	// Unpublished pool: the tap-costed pick is dropped as before.
	no := &Decision{Kind: KAttackers, Min: 0, Max: 2, Options: []Option{
		{Index: 0, Kind: "attacker", Obj: 1},
		{Index: 1, Kind: "attacker", Obj: 2, CostTaps: 1},
	}}
	if got := ChargeOptionConstraints(no, []int{0, 1}, no.PayerLifeBound(), 0); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("ChargeOptionConstraints without a pool = %v, want [0]", got)
	}
}
