package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestStruggleForSanityAlternatesAndReturnsTheOpponentsPicks: Struggle for
// Sanity's target opponent reveals their hand (RevealHand RememberTargets$
// True | RememberRevealed$ True), then the OPPONENT (ChooseCard Defined$
// Player.IsRemembered) and the caster (Defined$ You) alternately exile a card
// from it until it is empty. The opponent's picks are imprinted (ChangeZone
// Imprint$ True) and return to their hand; the caster's go to the graveyard.
// With seven cards the opponent picks four (1st, 3rd, 5th, 7th) and the
// caster three. Before the fix RememberTargets$ was unread on RevealHand, so
// Player.IsRemembered named nobody: the opponent was never asked, the caster
// exiled all seven and every card went to the graveyard.
func TestStruggleForSanityAlternatesAndReturnsTheOpponentsPicks(t *testing.T) {
	t.Parallel()
	e, cfg, id, caster := corpusCardConfig(t, 7311, "Struggle for Sanity")
	addMana(t, e, caster, "BBBB") // {2}{B}{B}
	opp := 1 - caster
	hand := len(e.G.Zone(state.ZHand, opp))
	if hand != 7 {
		t.Fatalf("precondition: opponent hand %d, want 7", hand)
	}
	gyBefore := len(e.G.Zone(state.ZGraveyard, opp))
	d := castFixture(t, e, id, int(opp))
	var askers []state.PlayerID
	for i := 0; i < 60 && d != nil && len(e.G.Stack) > 0; i++ {
		if d.Kind == decision.KPriority {
			castFirst(t, e, "pass")
		} else {
			askers = append(askers, d.Player)
			submitChoices(t, e, d.Options[0].Index)
		}
		d = e.Pending()
	}
	if len(e.G.Stack) > 0 {
		t.Fatalf("the spell never finished resolving (pending %+v)", d)
	}
	if len(askers) != hand {
		t.Fatalf("%d picks were asked (%v), want %d (one per card)", len(askers), askers, hand)
	}
	for i, p := range askers {
		want := opp
		if i%2 == 1 {
			want = caster
		}
		if p != want {
			t.Fatalf("pick %d was asked of seat %d, want seat %d (opponent first, then alternating): %v", i+1, p, want, askers)
		}
	}
	if got := len(e.G.Zone(state.ZHand, opp)); got != 4 {
		t.Errorf("opponent hand after = %d, want 4 (the opponent's own picks return)", got)
	}
	if got := len(e.G.Zone(state.ZGraveyard, opp)) - gyBefore; got != 3 {
		t.Errorf("opponent graveyard grew by %d, want 3 (the caster's picks)", got)
	}
	if got := len(e.G.Zone(state.ZExile, opp)); got != 0 {
		t.Errorf("%d cards left in exile, want 0", got)
	}
	replayCheck(t, e, cfg)
}
