package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestPlayerCountOtherAmountCountsOtherPlayers pins Kaya, Spirits' Justice's
// `SVar:OneEach:PlayerCountOther$Amount` -- Forge's PlayerCountOther$ names
// every living player other than the resolving controller (the group the
// "-2: exile target creature you control. For each other player, exile up to
// one target creature that player controls" ability bounds its per-player
// TargetMax$ with). It is the same living-opponent set PlayerCountOpponents$
// counts, read through the shared selector table.
func TestPlayerCountOtherAmountCountsOtherPlayers(t *testing.T) {
	h := newHost(t, 3)
	c := &Ctx{Controller: 0}
	// Preconditions the assertion depends on: three live seats and exactly
	// two of them are not the resolving controller.
	if len(h.g.AliveFrom(0)) != 3 {
		t.Fatalf("fixture: %d living players, want 3", len(h.g.AliveFrom(0)))
	}
	if got := opponentGroup(h.g, c); len(got) != 2 {
		t.Fatalf("fixture: %d opponents of seat 0, want 2", len(got))
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountOther$Amount"); !ok || got != 2 {
		t.Fatalf("PlayerCountOther$Amount = (%d, %v), want (2, true) for two other players", got, ok)
	}
	// Seat 1 concedes: PlayerCountOther$ -- like every living group -- drops
	// to the one remaining other player.
	h.g.Players[1].Lost = true
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountOther$Amount"); !ok || got != 1 {
		t.Fatalf("PlayerCountOther$Amount after a concession = (%d, %v), want (1, true)", got, ok)
	}
}

// TestPlayerCountDefinedRememberedOwnerSumZoneSizes pins Deadly Cover-Up's
// three heads -- `PlayerCountDefinedRememberedOwner$CardsInGraveyard`,
// `$CardsInHand` and `$CardsInLibrary` -- the summed zone sizes of the OWNERS
// of the resolution's remembered objects. Deadly Cover-Up exiles an
// opponent's graveyard card with RememberChanged$ True, then searches "its
// owner's graveyard, hand, and library" with ChangeNum$ sized by these heads;
// the owner group must therefore read the remembered card's OWNER, distinct
// owners count once, and an unresolvable remembered id contributes nothing.
func TestPlayerCountDefinedRememberedOwnerSumZoneSizes(t *testing.T) {
	h := newHost(t, 4)
	// Two remembered cards owned by seat 1 and one owned by seat 2; seat 3
	// is untouched and must not be summed in.
	rem := func(owner state.PlayerID) state.Target {
		o := h.g.AddObject(mkCard(t, pcsMountain), owner)
		return state.Target{Obj: o.ID}
	}
	c := &Ctx{Controller: 0, Remembered: []state.Target{rem(1), rem(1), rem(2)}}
	// Every remembered object must resolve and carry its owner; otherwise the
	// owner walk below would "pass" by counting nothing.
	for i, tg := range c.Remembered {
		o := h.g.Obj(tg.Obj)
		if o == nil {
			t.Fatalf("fixture: remembered object %d does not resolve", tg.Obj)
		}
		if tg.IsPlayer {
			t.Fatalf("fixture: remembered entry %d is a player, want an object", i)
		}
	}

	// Seat 1 (two remembered owners, one seat): graveyard 2, hand 1, library 5.
	pcsZoneObjs(t, h, 1, 2, pcsMountain, state.ZGraveyard)
	pcsZoneObjs(t, h, 1, 1, pcsMountain, state.ZHand)
	pcsZoneObjs(t, h, 1, 5, pcsMountain, state.ZLibrary)
	// Seat 2 (one remembered object): graveyard 3, hand 4, library 0.
	pcsZoneObjs(t, h, 2, 3, pcsMountain, state.ZGraveyard)
	pcsZoneObjs(t, h, 2, 4, pcsMountain, state.ZHand)
	// Seat 3 holds cards too, but owns no remembered card -- a distractor.
	pcsZoneObjs(t, h, 3, 9, pcsMountain, state.ZGraveyard)

	// Precondition: the three zones really hold the fixture's counts, so the
	// sums below cannot be satisfied by an empty board.
	if got := len(h.g.Zone(state.ZGraveyard, 1)) + len(h.g.Zone(state.ZGraveyard, 2)) + len(h.g.Zone(state.ZGraveyard, 3)); got != 14 {
		t.Fatalf("fixture: %d total graveyard cards, want 14", got)
	}

	for _, tc := range []struct {
		body string
		want int32
	}{
		// seat 1 (2) + seat 2 (3), seat 3's 9 excluded.
		{"Count$PlayerCountDefinedRememberedOwner$CardsInGraveyard", 5},
		// seat 1 (1) + seat 2 (4).
		{"Count$PlayerCountDefinedRememberedOwner$CardsInHand", 5},
		// seat 1 (5) + seat 2 (0).
		{"Count$PlayerCountDefinedRememberedOwner$CardsInLibrary", 5},
	} {
		if got, ok := EvalCountOK(h, c, tc.body); !ok || got != tc.want {
			t.Errorf("%s = (%d, %v), want (%d, true)", tc.body, got, ok, tc.want)
		}
	}

	// A remembered id that no longer resolves contributes no owner: the
	// library sum drops from seat 1's five to zero.
	c.Remembered = append(c.Remembered, state.Target{Obj: 1 << 20})
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountDefinedRememberedOwner$CardsInLibrary"); !ok || got != 5 {
		t.Fatalf("with an unresolvable remembered id = (%d, %v), want (5, true)", got, ok)
	}
}

// TestPlayerCountBareHasPropertyHasCardsInHand pins Aclazotz, Deepest
// Betrayal's and Naktamun Lorespinner's bare-group head
// `PlayerCount$HasPropertyHasCardsInHand_Card_LE1` ("for each player who has
// one or fewer cards in hand"). The bare PlayerCount group is every living
// player, and the HasProperty family is the same read the group-prefixed
// arms use, so the controller is counted here.
func TestPlayerCountBareHasPropertyHasCardsInHand(t *testing.T) {
	h := newHost(t, 3)
	c := &Ctx{Controller: 0}
	// Controller 0: one card (qualifies at LE1). Opponent 1: none (qualifies).
	// Opponent 2: two (does not).
	pcsZoneObjs(t, h, 0, 1, pcsMountain, state.ZHand)
	pcsZoneObjs(t, h, 2, 2, pcsMountain, state.ZHand)
	// Preconditions: all three living seats and the exact hand sizes the
	// comparison turns on.
	if len(h.g.AliveFrom(0)) != 3 {
		t.Fatalf("fixture: %d living players, want 3", len(h.g.AliveFrom(0)))
	}
	if got := len(h.g.Zone(state.ZHand, 2)); got != 2 {
		t.Fatalf("fixture: seat 2 hand = %d cards, want 2 (must fail LE1)", got)
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCount$HasPropertyHasCardsInHand_Card_LE1"); !ok || got != 2 {
		t.Fatalf("PlayerCount$HasPropertyHasCardsInHand_Card_LE1 = (%d, %v), want (2, true) "+
			"(controller 1 card + opponent 0 cards, not the 2-card seat)", got, ok)
	}
	// Hand size is read per living seat: give the 2-card opponent's second
	// card to seat 0 and nobody is at two, so the count returns to 3... but
	// only after making seat 2 qualify (move a card out).
	pcsMoveZone(h, 2, state.ZHand, state.ZGraveyard)
	if got, ok := EvalCountOK(h, c, "Count$PlayerCount$HasPropertyHasCardsInHand_Card_LE1"); !ok || got != 3 {
		t.Fatalf("after all three at <=1 = (%d, %v), want (3, true)", got, ok)
	}
	// Unknown property stays fail-closed.
	if got, ok := EvalCountOK(h, c, "Count$PlayerCount$HasPropertyNoSuchProperty"); ok {
		t.Fatalf("PlayerCount$HasPropertyNoSuchProperty = (%d, true), want unresolvable", got)
	}
}
