package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestKarnAnimateUsesQueuedExtraTurns pins the real Karn +1 while an extra
// turn is already scheduled.  UntilYourNextTurn ends when Karn's next actual
// turn starts, including the pending extra-turn queue (most-recent first),
// rather than when ordinary rotation would next reach Karn.
func TestKarnAnimateUsesQueuedExtraTurns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		extraSeat  state.PlayerID
		wantExpiry int32
	}{
		{name: "Karn extra turn", extraSeat: 0, wantExpiry: 1},
		{name: "opponent extra turn", extraSeat: 1, wantExpiry: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := testutil.CorpusRegistry(t)
			e := corpusEngine(t, reg, []*cards.Card{
				lookup(t, reg, "Karn, the Great Creator"), lookup(t, reg, "Sol Ring"),
			}, nil)
			karn := moveByName(t, e, 0, "Karn, the Great Creator", state.ZBattlefield)
			ring := moveByName(t, e, 0, "Sol Ring", state.ZBattlefield)
			if e.IsCreature(ring) || e.G.Obj(karn).Counter("LOYALTY") <= 0 {
				t.Fatal("precondition: Karn must have loyalty and Sol Ring must be a noncreature")
			}
			e.emit(events.Event{Kind: events.ExtraTurn, Player: tc.extraSeat, Amount: 1})
			e.pending = nil
			e.priorityRound()
			submitChoices(t, e, abilityOption(t, e, karn, 0).Index)
			submitTarget(t, e, ring)
			passUntilStackEmpty(t, e, 40)
			if !e.IsCreature(ring) {
				t.Fatal("precondition: Karn's +1 did not animate Sol Ring")
			}

			found := false
			for _, ce := range e.continuous {
				if ce.Source == ring && ce.Controller == 0 && ce.Duration == "UntilYourNextTurn" {
					found = true
					if ce.UntilTurn != tc.wantExpiry {
						t.Fatalf("Karn animation expiry = turn %d, want %d", ce.UntilTurn, tc.wantExpiry)
					}
				}
			}
			if !found {
				t.Fatal("precondition: Karn's +1 registered no UntilYourNextTurn effect")
			}
		})
	}
}
