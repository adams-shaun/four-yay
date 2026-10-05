package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Unravel's corpus script reads Targeted$CastTotalManaSpent after its counter
// has moved the target off the stack. Spending the target's full mana value
// must therefore skip the draw condition.
func TestUnravelCapturesTargetedCastTotalManaSpentAfterCounter(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lookup := func(name string) *cards.Card {
		t.Helper()
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus missing %q", name)
		}
		return c
	}
	unravel := lookup("Unravel")
	target := lookup("Sphinx of Uthuun") // {5}{U}{U}: mana value 7.
	e, _ := tokenReplGameSeats(t, 948, []*cards.Card{unravel, target}, nil)
	unravelID := moveSeededCard(t, e, 0, unravel, state.ZHand)
	targetID := moveSeededCard(t, e, 0, target, state.ZHand)

	if o := e.G.Obj(unravelID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Unravel = %+v, want in hand as the target is cast", e.G.Obj(unravelID))
	}
	if got := target.Faces[0].ManaValue(); got != 7 {
		t.Fatalf("precondition: %s mana value = %d, want 7", target.Faces[0].Name, got)
	}
	addMana(t, e, 0, "UUUUUUU")
	submitChoices(t, e, castOptionFor(t, e, targetID).Index)
	if o := e.G.Obj(targetID); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: target spell = %+v, want on stack", e.G.Obj(targetID))
	}
	if got := e.G.Obj(targetID).ManaSpent; got != 7 {
		t.Fatalf("precondition: target ManaSpent = %d, want 7 paid mana", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got < 1 {
		t.Fatalf("precondition: no cards in hand to cast Unravel; hand size = %d", got)
	}

	addMana(t, e, 0, "1UU")
	handBeforeUnravel := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, castModeOption(t, e, unravelID, ""))
	targetObject(t, e, targetID)
	if o := e.G.Obj(unravelID); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: Unravel = %+v, want on stack targeting the spell", e.G.Obj(unravelID))
	}
	passUntilStackEmpty(t, e, 30)

	if o := e.G.Obj(targetID); o == nil || o.Zone != state.ZGraveyard || o.ManaSpent != 0 {
		t.Fatalf("precondition: target after resolution = %+v, want countered into graveyard with live spend cleared", e.G.Obj(targetID))
	}
	if o := e.G.Obj(unravelID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: Unravel = %+v, want resolved into graveyard", e.G.Obj(unravelID))
	}
	// The cast consumed Unravel; because paid mana (7) equals mana value (7),
	// the card's actual condition must not draw. A broken capture reads zero,
	// which differs from the paid total and incorrectly adds one card.
	if got, want := len(e.G.Zone(state.ZHand, 0)), handBeforeUnravel-1; got != want {
		t.Fatalf("hand size after Unravel = %d, want %d (Unravel spent 7 for a mana-value-7 target and should not draw)", got, want)
	}
}
