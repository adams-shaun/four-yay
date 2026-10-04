package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCipherEncodedCombatDamageOffersCopyCastKernel: Paranoid Delusions
// resolves and encodes on a creature; that creature's combat damage to a
// player offers the copy cast, accepting it puts a copy on the stack which
// resolves (mills three), and the original stays exiled and encoded.
func TestCipherEncodedCombatDamageOffersCopyCastKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	delusions := mustCorpusCard(t, reg, "Paranoid Delusions")
	if !cipherHasResolutionTail(delusions) {
		t.Fatalf("Paranoid Delusions' K:Cipher did not expand into a trigger")
	}
	e, cfg, cipherID, creatures := cipherBoard(t, reg, delusions, cipherBeanSrc)
	creature := creatures[0]
	castCipherDelusions(t, e, cipherID, []int{0})
	if got := cipherEncodedOn(e, creature); len(got) != 1 || got[0] != cipherID {
		t.Fatalf("precondition: encoded cards = %v, want [%d]", got, cipherID)
	}
	base := len(e.L.Events)
	// The combat damage step holds no decision when damage is dealt: drop
	// the fixture's held priority so the trigger drain does not mistake it
	// for an outstanding ask (drainAwaitsTarget).
	e.pending = nil
	dealCipherCombatDamage(t, e, creature, 1, 2)
	if !drainCipher(t, e, 40, nil, []int{0}) {
		t.Fatalf("combat damage by the encoded creature posed no copy-cast offer")
	}
	if n := countPutOnStackFor(e, base, "Paranoid Delusions"); n < 1 {
		t.Fatalf("%d copies of the encoded card reached the stack, want >= 1", n)
	}
	copyID := putOnStackObjFor(e, base, "Paranoid Delusions")
	copyObj := e.G.Obj(copyID)
	if copyID == 0 || copyObj == nil || copyObj.Zone == state.ZStack || !copyObj.IsCopy {
		t.Fatalf("the copy %d did not resolve off the stack as a copy: %+v", copyID, copyObj)
	}
	milled := 0
	for i := base; i < len(e.L.Events); i++ {
		if events.IsMill(e.L.Events[i]) {
			milled++
		}
	}
	if milled != 3 {
		t.Fatalf("the resolved copy milled %d cards, want 3", milled)
	}
	if co := e.G.Obj(cipherID); co == nil || co.Zone != state.ZExile {
		t.Fatalf("after the copy cast the original zone = %v, want exile", zoneOf(co))
	}
	if got := cipherEncodedOn(e, creature); len(got) != 1 || got[0] != cipherID {
		t.Fatalf("after the copy cast the encoded cards = %v, want [%d]", got, cipherID)
	}
	replayCheck(t, e, cfg)
}
