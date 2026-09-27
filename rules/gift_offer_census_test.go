package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestGiftOfferCensusUnionsPromisedBranch pins the OFFER-feasibility wiring
// itself, not only the end-to-end offer: with only a noncreature spell on the
// stack, the plain (unpromised) branch of Long River's Pull's
// Count$PromisedGift bound is unsatisfiable, so the census must be feasible
// through the promised branch alone. Without the union in targetSAAvailable
// the census answers false and the cast option is withheld (the original
// symptom, observed here at the census layer the offer gate reads).
func TestGiftOfferCensusUnionsPromisedBranch(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pull := mustCorpusCard(t, reg, "Long River's Pull")
	zap := card(t, lrpZapSrc)
	e, _ := tokenReplGameSeats(t, 19, []*cards.Card{pull}, []*cards.Card{zap})
	pullID := moveSeededCard(t, e, 0, pull, state.ZHand)
	mainSA := pull.Faces[0].SpellAbility()

	// Preconditions: the bound the census resolves differs between the two
	// branches, and the promised branch (min 0) is the only satisfiable one.
	unpromisedMin, _ := e.resolvedTargetBounds(0, pullID, mainSA, 0)
	promised := true
	promisedMin, _ := e.resolvedTargetBoundsWithGift(0, pullID, mainSA, 0, &promised)
	if unpromisedMin != 1 || promisedMin != 0 || unpromisedMin == promisedMin {
		t.Fatalf("precondition: Long River's Pull bounds = unpromised %d, promised %d; want distinct 1 and 0", unpromisedMin, promisedMin)
	}
	if o := e.G.Obj(pullID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: pull zone = %v, want hand", o.Zone)
	}
	if n := len(e.legalTargetCandidates(0, pullID, pullID, mainSA)); n != 0 {
		t.Fatalf("precondition: candidate count = %d, want 0 (nothing creature-like on the stack yet)", n)
	}
	// With candidates 0 and the plain minimum 1, the unpromised branch alone
	// is FALSE; the union must bring in the promised branch (min 0) and make
	// the census TRUE so the cast option is not wrongly withheld.
	if !e.targetSAAvailable(0, pullID, pullID, mainSA, 0, false) {
		t.Fatal("offer census = false with only a noncreature spell on the stack; the promised branch (min 0) must make the bound feasible")
	}
}
