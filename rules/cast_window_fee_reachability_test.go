package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const (
	cwFeeGreenSource = "Name:FeeGreen\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ 1 T | Produced$ G | Amount$ 2 | SpellDescription$ Add {G}{G}.\nOracle:x\n"
	cwFreeColorlessSource = "Name:FreeColorless\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\nOracle:x\n"
	cwTwoFeeGreenSource = "Name:TwoFeeGreen\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ 2 T | Produced$ G | SpellDescription$ Add {G}.\nOracle:x\n"
)

// TestCastWindowGenericFeeConsumesTheRealPool ensures an activation fee is
// deducted before checking the spell: {C} pays the {1} fee, leaving {G}{G},
// which cannot pay the spell's required {C}{G}.
func TestCastWindowGenericFeeConsumesTheRealPool(t *testing.T) {
	b := equipWindowGame(t, 718, cwFeeGreenSource)
	e := b.engine
	source := b.byName["FeeGreen"]
	if source == 0 || e.G.Obj(source).Zone != state.ZBattlefield || e.G.Obj(source).Tapped {
		t.Fatalf("FeeGreen must be an untapped battlefield source: %+v", e.G.Obj(source))
	}
	addMana(t, e, 0, "C")
	if got := e.G.Players[0].Pool; got[state.MC] != 1 || got.Total() != 1 {
		t.Fatalf("precondition pool = %v, want exactly {C}", got)
	}
	units := e.castWindowUnitsForSourceTest(source, "G", 1)
	units[0].alts[0].amt = 2
	if e.castWindowReachable(0, ParseCost("C G"), e.G.Players[0].Pool, state.Mana{}, [7]state.Mana{}, 20, nil, units) {
		t.Fatal("probe claims {C} pays a {1}: add {G}{G} activation followed by a {C}{G} spell")
	}
	if !e.untappedManaSource(0, source) {
		t.Fatal("precondition: the real cast window does not offer FeeGreen")
	}
}

// TestCastWindowFeeFundingMayExceedSpellPipCount ensures a free mana ability
// can fund a later {2} activation even when the spell itself has only one pip.
func TestCastWindowFeeFundingMayExceedSpellPipCount(t *testing.T) {
	b := equipWindowGame(t, 719, cwFreeColorlessSource, cwTwoFeeGreenSource)
	e := b.engine
	freeID, paidID := b.byName["FreeColorless"], b.byName["TwoFeeGreen"]
	for _, id := range []state.ObjID{freeID, paidID} {
		if id == 0 || e.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(id).Tapped {
			t.Fatalf("source %d must be untapped on battlefield: %+v", id, e.G.Obj(id))
		}
	}
	addMana(t, e, 0, "C")
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("precondition pool total = %d, want 1", got)
	}
	units := append(e.castWindowUnitsForSourceTest(freeID, "C", 0), e.castWindowUnitsForSourceTest(paidID, "G", 2)...)
	if !e.castWindowReachable(0, ParseCost("G"), e.G.Players[0].Pool, state.Mana{}, [7]state.Mana{}, 20, nil, units) {
		t.Fatal("probe missed free {C}, then pay {2}, then produce {G}; search cannot be bounded by the spell's one pip")
	}
	if !e.untappedManaSource(0, freeID) {
		t.Fatal("precondition: the real cast window does not initially offer the free source")
	}
	// This is the pool state after the free source's {C} has resolved. The
	// live payment gate must then expose the paid source for the {2} fee.
	addMana(t, e, 0, "C")
	if !e.untappedManaSource(0, paidID) {
		t.Fatal("real cast-window payment gate does not expose the {2} source after the free {C}")
	}
}

// TestCastWindowGenericFeeDebitsSnowProvenance ensures a generic activation
// fee cannot leave a snow pip payable after spending the only snow unit.
func TestCastWindowGenericFeeDebitsSnowProvenance(t *testing.T) {
	b := equipWindowGame(t, 720, cwTwoFeeGreenSource)
	e := b.engine
	source := b.byName["TwoFeeGreen"]
	if source == 0 || e.G.Obj(source).Zone != state.ZBattlefield || e.G.Obj(source).Tapped {
		t.Fatalf("precondition: fee source must be untapped on battlefield: %+v", e.G.Obj(source))
	}
	e.G.Players[0].Pool = state.Mana{}
	addMana(t, e, 0, "CW")
	e.G.Players[0].Snow[state.MC] = 1
	if got := e.G.Players[0].Pool; got[state.MC] != 1 || got[state.MW] != 1 || e.G.Players[0].Snow[state.MC] != 1 {
		t.Fatalf("precondition pool/snow = %v/%v, want {C}{W} with one snow {C}", got, e.G.Players[0].Snow)
	}
	units := e.castWindowUnitsForSourceTest(source, "G", 2)
	if e.castWindowReachable(0, ParseCost("S G"), e.G.Players[0].Pool, e.G.Players[0].Snow,
		[7]state.Mana{}, 20, nil, units) {
		t.Fatal("probe claims the {2} fee can spend the only snow {C} and still pay {S}{G}")
	}

	// A one-mana fee deterministically spends {C} before snow {W}; that
	// remaining snow provenance must still be available to the spell.
	e.G.Players[0].Pool = state.Mana{}
	addMana(t, e, 0, "CW")
	e.G.Players[0].Snow = state.Mana{}
	e.G.Players[0].Snow[state.MW] = 1
	if e.G.Players[0].Pool[state.MC] != 1 || e.G.Players[0].Pool[state.MW] != 1 || e.G.Players[0].Snow[state.MW] != 1 {
		t.Fatalf("precondition preserving-snow pool/snow = %v/%v", e.G.Players[0].Pool, e.G.Players[0].Snow)
	}
	units = e.castWindowUnitsForSourceTest(source, "G", 1)
	if !e.castWindowReachable(0, ParseCost("S G"), e.G.Players[0].Pool, e.G.Players[0].Snow,
		[7]state.Mana{}, 20, nil, units) {
		t.Fatal("probe failed to preserve snow {W} while the {1} fee spends {C}")
	}
}

// TestCastWindowPaidSourceOrderCanReverseCostSort proves an initially more
// expensive activation may need to run before a cheaper fee-consuming source.
func TestCastWindowPaidSourceOrderCanReverseCostSort(t *testing.T) {
	large := `Name:LargeFee
Types:Land
A:AB$ Mana | Cost$ 2 T | Produced$ C | Amount$ 3 | SpellDescription$ Add {C}{C}{C}.
Oracle:x
`
	small := `Name:SmallFee
Types:Land
A:AB$ Mana | Cost$ 1 T | Produced$ G | SpellDescription$ Add {G}.
Oracle:x
`
	b := equipWindowGame(t, 721, large, small)
	e := b.engine
	largeID, smallID := b.byName["LargeFee"], b.byName["SmallFee"]
	for _, id := range []state.ObjID{largeID, smallID} {
		if id == 0 || e.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(id).Tapped {
			t.Fatalf("precondition: source %d must be untapped on battlefield: %+v", id, e.G.Obj(id))
		}
	}
	e.G.Players[0].Pool = state.Mana{}
	addMana(t, e, 0, "CC")
	if got := e.G.Players[0].Pool; got[state.MC] != 2 || got.Total() != 2 {
		t.Fatalf("precondition pool = %v, want exactly {C}{C}", got)
	}
	units := append(e.castWindowUnitsForSourceTest(largeID, "C", 2), e.castWindowUnitsForSourceTest(smallID, "G", 1)...)
	units[0].alts[0].amt = 3
	if !e.castWindowReachable(0, ParseCost("C C G"), e.G.Players[0].Pool, state.Mana{},
		[7]state.Mana{}, 20, nil, units) {
		t.Fatal("probe missed {C}{C} -> pay {2}, add {C}{C}{C} -> pay {1}, add {G}")
	}
	if !e.untappedManaSource(0, largeID) || !e.untappedManaSource(0, smallID) {
		t.Fatal("precondition: both activations must be offered initially")
	}
	addMana(t, e, 0, "CCC")
	if !e.untappedManaSource(0, smallID) {
		t.Fatal("live mana window does not offer the {1} activation after the {2} activation")
	}
}

// castWindowUnitsForSourceTest supplies a single known production alternative
// to isolate activation-fee reachability from the corpus census walk.
func (e *Engine) castWindowUnitsForSourceTest(id state.ObjID, symbol string, fee int32) []windowManaUnit {
	counts := [6]int32{}
	for i, c := range []byte("WUBRGC") {
		if len(symbol) == 1 && symbol[0] == c {
			counts[i] = 1
		}
	}
	return []windowManaUnit{{id: id, alts: []windowManaAlt{{counts: counts, amt: 1, costGeneric: fee}}}}
}
