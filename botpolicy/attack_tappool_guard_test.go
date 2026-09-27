package botpolicy

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestLegalAttackChoicesVerifiesPublishedTapPool pins the botpolicy KAttackers
// arm against the shared wire rule: with the engine's tap-candidate pool
// published, the guard keeps a tap-costed pair while the declaration still
// leaves the pool payable, and drops the pair that would exhaust it -- so the
// answer it hands to Clamp is exactly what Decision.Validate accepts. This is
// the botpolicy half of Hollow Warrior's declaration-dependent tap pool; the
// guard and the validator derive from the one ChargeTapPoolFit helper, so the
// bot cannot re-submit an answer the engine rejects.
func TestLegalAttackChoicesVerifiesPublishedTapPool(t *testing.T) {
	d := &decision.Decision{Kind: decision.KAttackers, Player: 0, Min: 0, Max: 3, ChargeTapPool: 3, Options: []decision.Option{
		{Index: 0, Kind: "attacker", Obj: 1, CostTaps: 1, TapPoolCost: 1},
		{Index: 1, Kind: "attacker", Obj: 2, TapPoolCost: 1},
		{Index: 2, Kind: "attacker", Obj: 3, TapPoolCost: 1},
	}}
	b := Board{Life: map[state.PlayerID]int32{0: 20}}
	// PRECONDITION: the unfiltered answer is one the validator rejects, so the
	// guard has real work to do.
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1, 2}}); err == nil {
		t.Fatal("precondition: the pool-exhausting answer must fail Validate")
	}
	got := LegalAttackChoices(b, d, []int{0, 1, 2})
	if !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("LegalAttackChoices = %v, want [0 1] (third pair exhausts the pool)", got)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: got}); err != nil {
		t.Fatalf("the guard's answer failed Validate: %v", err)
	}
}

// TestLegalAttackChoicesDropsTapWithoutPublishedPool pins the conservative
// fallback: no published pool means no way to verify a tap obligation, so the
// guard still drops the pair (and the validator, with no pool published, does
// not tighten -- the engine's board-aware check remains the backstop).
func TestLegalAttackChoicesDropsTapWithoutPublishedPool(t *testing.T) {
	d := &decision.Decision{Kind: decision.KAttackers, Player: 0, Max: 2, Options: []decision.Option{
		{Index: 0, Kind: "attacker", Obj: 1},
		{Index: 1, Kind: "attacker", Obj: 2, CostTaps: 1},
	}}
	b := Board{Life: map[state.PlayerID]int32{0: 20}}
	if got := LegalAttackChoices(b, d, []int{0, 1}); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("LegalAttackChoices = %v, want [0] (no published pool)", got)
	}
}
