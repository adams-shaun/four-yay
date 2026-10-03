package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestNoTargetDecisionOffersAnIllegalTarget is a general invariant, not a
// regression test for one card: across EVERY target decision raised by the
// acceptance games at every seat count, every option offered must be one the
// rules actually permit.
//
// It exists because that invariant was silently false. `askTarget` (which
// builds the offer) and `legalTargets` (which rechecks at resolution) are two
// separate implementations of the same rule kept in agreement by hand, and
// they drifted: a counterspell was offered as a target of ITSELF (CR 115.5),
// the bot dutifully picked it, and the spell countered itself into its own
// graveyard. Three of the eight counters in the 8-seat game did exactly that,
// and nothing in the suite noticed -- the two existing Counter tests set
// Targets directly and never went through askTarget at all.
//
// The narrow exclusion that fixed it is in, at both sites, and this test does
// not re-test it. What it pins is the PROPERTY that fix was one instance of:
// a seat, human or bot, must never be shown an action the rules forbid. An
// engine that offers an illegal option and relies on the chooser declining it
// has moved a rules obligation onto its clients, and a bot will always take it.
//
// The check is structural rather than card-specific on purpose, so the next
// divergence between the offer and the recheck fails HERE, on whatever card
// first exposes it, instead of being found by reading a game log.
func TestNoTargetDecisionOffersAnIllegalTarget(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	for _, seats := range []int{2, 4, 6, 8} {
		seats := seats
		t.Run(seatCount(seats), func(t *testing.T) {
			t.Parallel()
			all := testutil.LegacyDeckNames()
			names := make([]string, seats)
			decks := make([][]*cards.Card, seats)
			for i := 0; i < seats; i++ {
				names[i] = all[i%len(all)]
				decks[i] = testutil.RepoDeck(t, reg, all[i%len(all)])
			}
			// Seed 42 / Mulligans 1 mirrors playAcceptance, so this walks the
			// same games the chain heads pin rather than a different sample.
			e := New(Config{Seed: 42, Names: names, Decks: decks,
				Tokens: reg.Tokens, Mulligans: 1})
			b := newTestBot(7)
			e.Advance()

			checked, decisions := 0, 0
			for n := 0; !e.G.Over && e.Pending() != nil && n < 400000; n++ {
				d := e.Pending()
				if d.Kind == decision.KTarget {
					decisions++
					for _, o := range d.Options {
						if o.Obj == 0 {
							continue // a player target carries no object
						}
						checked++
						// CR 115.5: a spell or ability on the stack is an
						// illegal target for itself. askTarget runs after the
						// object is already on the stack, so this is reachable
						// whenever the offer forgets to exclude it.
						if o.Obj == d.Source {
							t.Fatalf("%d seats, intent %d: target decision seq %d offered its own source %d (%q) — CR 115.5: a spell or ability on the stack is an illegal target for itself",
								seats, n, d.Seq, o.Obj, o.Label)
						}
						if e.G.Obj(o.Obj) == nil {
							t.Fatalf("%d seats, intent %d: target decision seq %d offered object %d (%q), which the game cannot resolve",
								seats, n, d.Seq, o.Obj, o.Label)
						}
					}
				}
				if err := e.Submit(b.answer(e, d)); err != nil {
					t.Fatalf("%d seats, intent %d: %v", seats, n, err)
				}
			}
			if !e.G.Over {
				t.Fatalf("%d-seat game did not finish (turn %d)", seats, e.G.Turn)
			}
			// A vacuous pass is the way this test most plausibly rots: if the
			// acceptance games stop raising target decisions it would go green
			// having checked nothing at all.
			if checked == 0 {
				t.Fatalf("%d seats: examined no target options — this test reached no target decision and proves nothing", seats)
			}
			t.Logf("%d seats: %d target decisions, %d offered options, all legal", seats, decisions, checked)
		})
	}
}

func seatCount(n int) string {
	switch n {
	case 2:
		return "2seats"
	case 4:
		return "4seats"
	case 6:
		return "6seats"
	}
	return "8seats"
}
