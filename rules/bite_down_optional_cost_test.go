// bite_down_optional_cost_test.go — the optional-cost cast variant's OFFER
// pricing on the real corpus carrier whose ReduceCost amount reads the cast's
// own election: Bite Down on Crime ("As an additional cost to cast this
// spell, you may collect evidence 6. This spell costs {2} less to cast if
// evidence was collected.", SVar Z = Count$OptionalGenericCostPaid.2.0).
//
// CR 601.2g determines the total cost, including the reduction the elected
// optional cost enables, BEFORE payment: with a pool of exactly the reduced
// price ({1}{G}) the optional-cost variant must be OFFERED, the evidence ask
// posed, and the charge must be the reduced price — while the plain cast
// variant keeps the FULL price (the election seed must never leak into it).
// Before the seed, the amount evaluated its UNPAID branch at offer time (the
// card sits in hand; the pay-time provenance flag is not folded until
// CR 601.2a's push) and the variant was only ever offered at the unreduced
// price. Bite Down on Crime is in no repo deck, so no chain head depends on
// this file.

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// biteDownEngine builds the board the evidence cost needs: p0 holds
// Bite Down on Crime in hand with Colossal Dreadmaw (mana value 6, the exact
// evidence 6) in its graveyard, both seats control a Bear.
func biteDownEngine(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	e, cfg, _ := altCostEngine(t, seed, []string{"Bite Down on Crime", "Colossal Dreadmaw"},
		[]string{altBearSrc}, []string{altBearSrc})
	spell := findCardObj(t, e, 0, "Bite Down on Crime", state.ZHand)
	colossal := moveToGraveyardFromLibrary(t, e, "Colossal Dreadmaw")
	mine := findCardObj(t, e, 0, "Bear", state.ZBattlefield)
	foe := findCardObj(t, e, 1, "Bear", state.ZBattlefield)
	return e, cfg, spell, colossal, mine, foe
}

// answerBiteDownAsks walks the pending cast's asks: the evidence KChoose
// (answered with Colossal Dreadmaw, the exact-evidence card) and the two
// KTarget asks in cast order — the spell's YouCtrl pump target first (p0's
// Bear), the fight's YouDontCtrl target second (p1's Bear) — failing on
// anything else. It returns only when the asks are exhausted, leaving the
// resolution (or a fresh priority) for the caller.
func answerBiteDownAsks(t *testing.T, e *Engine, colossal, mine, foe state.ObjID) {
	t.Helper()
	asked := 0
	for i := 0; ; i++ {
		if i > 6 {
			t.Fatal("cast asks did not settle")
		}
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending mid-cast")
		}
		switch d.Kind {
		case decision.KChoose:
			choosePendingObject(t, e, colossal)
		case decision.KTarget:
			want := foe
			if asked == 0 {
				want = mine
			}
			asked++
			pick := -1
			for _, o := range d.Options {
				if o.Obj == want {
					pick = o.Index
					break
				}
			}
			if pick < 0 {
				t.Fatalf("target ask did not offer the wanted Bear: %+v", d.Options)
			}
			submitChoices(t, e, pick)
		default:
			return
		}
	}
}

// biteDownCollectedEvidence asserts the evidence cost's trace: Colossal
// Dreadmaw left the graveyard for exile.
func biteDownCollectedEvidence(t *testing.T, e *Engine, colossal state.ObjID) {
	t.Helper()
	if o := e.G.Obj(colossal); o == nil || o.Zone != state.ZExile {
		if o == nil {
			t.Fatal("evidence card gone from the game")
		}
		t.Fatalf("evidence card in %v, want exile (the collected evidence)", o.Zone)
	}
}

// TestBiteDownOnCrimeOfferedAtReducedPrice is the defect pin: with a pool of
// EXACTLY the reduced price {1}{G} the optional-cost cast variant IS offered,
// the evidence ask is posed, and the charge empties the pool.
func TestBiteDownOnCrimeOfferedAtReducedPrice(t *testing.T) {
	t.Parallel()
	e, cfg, spell, colossal, mine, foe := biteDownEngine(t, 701)
	// PRECONDITION: the corpus card really carries the applicable
	// OptionalCost static — without it no optional-cost variant could ever be
	// offered and every assertion below would be vacuous.
	if len(e.optionalCostViews(e.collectCostStatics(), 0, spell)) != 1 {
		t.Fatalf("precondition: Bite Down on Crime carries no applicable OptionalCost static")
	}
	// PRECONDITION: the evidence card is really in the graveyard at mana
	// value 6, so the cost is payable and its paid branch is the live one.
	if o := e.G.Obj(colossal); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: evidence card not in graveyard")
	}
	addMana(t, e, 0, "CG")
	paid := castModeOption(t, e, spell, "optionalcost")
	submitChoices(t, e, paid)
	answerBiteDownAsks(t, e, colossal, mine, foe)
	if got := poolTotal(e.G.Players[0].Pool); got != 0 {
		t.Fatalf("pool holds %d after paying the reduced price, want 0", got)
	}
	passUntilStackEmpty(t, e, 40)
	if !optionalCostCastInfo(e, spell) {
		t.Fatal("no pay-time CastInfo carrying optionalcostpaid")
	}
	biteDownCollectedEvidence(t, e, colossal)
	if o := e.G.Obj(foe); o == nil || o.Zone != state.ZGraveyard {
		if o == nil {
			t.Fatal("fight victim gone from the game")
		}
		t.Fatalf("fight victim in %v, want graveyard (took %d damage of 4)", o.Zone, int(o.Damage))
	}
	if o := e.G.Obj(mine); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("pumped creature left the battlefield")
	}
	replayCheck(t, e, cfg)
}

// TestBiteDownOnCrimeFullPoolChargesReduced pins the charge side at a full
// pool: the optional-cost variant charges the REDUCED price, leaving two
// generic pips in the pool.
func TestBiteDownOnCrimeFullPoolChargesReduced(t *testing.T) {
	t.Parallel()
	e, cfg, spell, colossal, mine, foe := biteDownEngine(t, 702)
	// PRECONDITION: both variants are offered at the full price.
	addMana(t, e, 0, "CCCG")
	castModeOption(t, e, spell, "")
	paid := castModeOption(t, e, spell, "optionalcost")
	submitChoices(t, e, paid)
	answerBiteDownAsks(t, e, colossal, mine, foe)
	if got := poolTotal(e.G.Players[0].Pool); got != 2 {
		t.Fatalf("pool holds %d after the reduced charge, want 2 left over", got)
	}
	biteDownCollectedEvidence(t, e, colossal)
	if optionalCostCastInfo(e, spell) == false {
		t.Fatal("no pay-time CastInfo carrying optionalcostpaid")
	}
	replayCheck(t, e, cfg)
}

// TestBiteDownOnCrimePlainCastChargesFull pins the no-leak direction: the
// plain cast variant, chosen at the full pool, charges the FULL price and
// stamps no optional-cost provenance — the election seed must ride the
// variant's own scope only.
func TestBiteDownOnCrimePlainCastChargesFull(t *testing.T) {
	t.Parallel()
	e, cfg, spell, colossal, mine, foe := biteDownEngine(t, 703)
	addMana(t, e, 0, "CCCG")
	castModeOption(t, e, spell, "optionalcost") // offered beside it
	plain := castModeOption(t, e, spell, "")
	submitChoices(t, e, plain)
	answerBiteDownAsks(t, e, colossal, mine, foe)
	if got := poolTotal(e.G.Players[0].Pool); got != 0 {
		t.Fatalf("pool holds %d after the full-price plain cast, want 0", got)
	}
	if o := e.G.Obj(colossal); o != nil && o.Zone == state.ZExile {
		t.Fatal("plain cast collected evidence")
	}
	if optionalCostCastInfo(e, spell) {
		t.Fatal("plain cast stamped optionalcostpaid provenance")
	}
	replayCheck(t, e, cfg)
}

// TestBiteDownOnCrimeReducedPoolPlainNotOffered pins the pricing SYMMETRY the
// offer gate keeps: at the reduced pool the plain variant (full price) must
// NOT be offered, so no under- or over-priced variant slips through the same
// gate the fixed variant passes.
func TestBiteDownOnCrimeReducedPoolPlainNotOffered(t *testing.T) {
	t.Parallel()
	e, _, spell, _, _, _ := biteDownEngine(t, 704)
	addMana(t, e, 0, "CG")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell && o.Mode == "" {
			t.Fatalf("plain cast offered at the reduced pool: %+v", d.Options)
		}
	}
}
