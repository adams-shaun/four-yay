package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// A permanent already committed to an earlier sacrifice cost cannot also be
// offered for Bargain. Candidate order remains the battlefield's stable order.
func TestBargainCandidatesExcludeEarlierSacrifices(t *testing.T) {
	t.Parallel()
	spell := searchCorpusCard(t, searchTestRegistry(t), "Candy Grapple")
	e, _, reg := bargainEngine(t, spell)
	first := seedBattlefield(t, e, reg, "Sol Ring")
	second := seedBattlefield(t, e, reg, "Propaganda")
	if e.G.Obj(first).Zone != state.ZBattlefield || e.G.Obj(second).Zone != state.ZBattlefield {
		t.Fatal("precondition: both eligible permanents are on the battlefield")
	}
	castID := searchMoveByName(t, e, "Candy Grapple", state.ZHand)
	if castID == 0 {
		t.Fatal("precondition: Bargain spell is in hand")
	}
	e.cast = &pendingCast{player: 0, card: castID, PaidCost: pay.PaidCost{Sacs: []state.ObjID{first}}}
	got := e.optionalSacrificeCandidates(&optionalSacrifices[optSacBargain], 0, castID, 0)
	if len(got) != 1 || got[0] != second {
		t.Fatalf("Bargain candidates = %v, want only uncommitted permanent %d (exclude %d)", got, second, first)
	}
}
