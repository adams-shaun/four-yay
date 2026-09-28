package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// mustCorpusCard pulls a real card out of the compiled .cards corpus, so the
// engine tests below resolve the exact real script parameters (UnlessCost$,
// SubAbility$, no UnlessPayer$) rather than a hand-trimmed fixture.
func mustCorpusCard(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus has no %q", name)
	}
	return c
}

func countDraw(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw {
			n++
		}
	}
	return n
}

// handIDsByFace maps the names of seat 0's hand cards to their object IDs.
func handIDsByFace(e *Engine) map[string]state.ObjID {
	out := map[string]state.ObjID{}
	for _, id := range e.G.Zone(state.ZHand, 0) {
		out[e.G.Obj(id).Face().Name] = id
	}
	return out
}

// drainUntilUnlessPay submits "pass" on every priority decision until a
// mid-resolution KModes (the unless_pay ask) becomes pending, and returns
// it. This is the resolution-tests' passUntilStackEmpty for the case where
// the very thing we came to see is a mid-resolution decision, not a pass.
func drainUntilUnlessPay(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			return nil
		}
		switch d.Kind {
		case decision.KModes:
			return d
		case decision.KPriority:
			castFirst(t, e, "pass")
		default:
			t.Fatalf("unexpected decision %+v while seeking the unless_pay ask", d)
		}
	}
	return nil
}

// counterFixture casts a creature spell from seat 0's hand, then a real
// corpus counterspell at it, targets the creature spell, and returns the
// engine plus the two card IDs. Used by test 4 (empty pool) and test 7
// (SubAbility chain-once) so both run through cast/askTarget/resolve with
// real card data.
func counterFixture(t *testing.T, reg *cards.Registry, counter, creature string) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	e := handEngine(t, mustCorpusCard(t, reg, counter), mustCorpusCard(t, reg, creature))
	ids := handIDsByFace(e)
	casterID, creatureID := ids[counter], ids[creature]
	if casterID == 0 || creatureID == 0 {
		t.Fatalf("hand missing %q or %q", counter, creature)
	}
	e.G.Players[0].Pool[state.MU] = 8
	e.G.Players[0].Pool[state.MG] = 8
	e.G.Players[0].Pool[state.MR] = 8
	e.askPriority(0)

	// Cast the creature onto the stack (no targets).
	submitChoices(t, e, passToCast(t, e, creatureID))
	// Cast the counterspell in response and name the creature spell as target.
	submitChoices(t, e, passToCast(t, e, casterID))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision after casting the counterspell, got %+v", d)
	}
	spellIdx := -1
	for _, o := range d.Options {
		if o.Obj == creatureID {
			spellIdx = o.Index
		}
	}
	if spellIdx < 0 {
		t.Fatalf("creature spell not offered as a counter target: %+v", d.Options)
	}
	submitChoices(t, e, spellIdx)
	return e, casterID, creatureID
}

// TestCounterUnlessCostEmptyPoolCannotPayAndCounters is test 4 of the brief:
// payMana (rules/stack.go) spends only the payer's FLOATING mana pool
// (state.Player.Pool); there is no tap-lands-to-pay flow mid-resolution. So
// a payer who said "pay" but has an empty pool cannot actually pay, and the
// engine records the decline: the spell IS countered. This is the common
// case in real games (a counterspell taxes a spell its controller already
// spent everything casting). On the unmodified tree effCounter never poses
// the unless_pay ask at all, so drainUntilUnlessPay returns nil and the test
// fails before any payment is attempted.
func TestCounterUnlessCostEmptyPoolCannotPayAndCounters(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, bearID := counterFixture(t, reg, "Mana Leak", "Grizzly Bears")

	// Empty the pool so the {3} unless-pay tax cannot be covered.
	e.G.Players[0].Pool = state.Mana{}

	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask posed for Mana Leak")
	}
	// The payer is the CONTROLLER OF THE COUNTERED SPELL (the Bear spell,
	// seat 0), which here is also the counterspell's caster -- asserted so
	// the pay decision is routed to the tax-payer and not, say, the
	// opponent.
	if pay.Player != 0 {
		t.Fatalf("unless_pay payer = seat %d, want the countered spell's controller (seat 0)", pay.Player)
	}
	// The payer AGREES to pay (option 0), but the pool is empty, so
	// resumeResolution's payMana records a decline and the Bear is countered.
	submitChoices(t, e, pay.Options[0].Index)
	passUntilStackEmpty(t, e, 20)

	if z := e.G.Obj(bearID).Zone; z != state.ZGraveyard {
		t.Fatalf("unpayable 'pay' should counter the spell: Bear zone = %s, want Graveyard", z)
	}
	// The counterspell itself also resolves off the stack to the graveyard,
	// so the count is not asserted -- only the countered spell's zone is.
	if !hasEventKind(e, events.ModeChosen) {
		t.Fatal("no ModeChosen event recorded the unless-pay answer")
	}
}

// TestCounterWithSubAbilityRunsChainExactlyOnce is test 7 of the brief and
// the chain-suspension guarantee we now depend on: an ASKING counterspell
// that also carries a SubAbility$ (Runeboggle: Counter | UnlessCost$ 1 |
// SubAbility$ DBDraw) must run that sub-ability EXACTLY ONCE, on the resume
// pass, not on the first (suspended) pass. Because effCounter asks through
// h.Ask, effects.Resolve's Suspended() guard stops the chain at the ask; the
// answer re-enters and the chain continues exactly once. On the unmodified
// tree effCounter ignores UnlessCost$, never suspends, so no unless_pay ask
// is posed and the test fails at that check.
func TestCounterWithSubAbilityRunsChainExactlyOnce(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, branch := range []string{"decline", "pay"} {
		t.Run(branch, func(t *testing.T) {
			e, _, bearID := counterFixture(t, reg, "Runeboggle", "Grizzly Bears")
			// Capture BEFORE resolution, not after the suspended pass.
			pre := countDraw(e)
			pay := drainUntilUnlessPay(t, e, 30)
			if pay == nil {
				t.Fatal("no unless_pay ask posed for Runeboggle")
			}
			suspended := countDraw(e)
			if suspended != pre {
				t.Fatalf("suspended draw count = %d, want %d", suspended, pre)
			}
			choice, wantZone := 1, state.ZGraveyard
			if branch == "pay" {
				choice, wantZone = 0, state.ZBattlefield
			}
			submitChoices(t, e, pay.Options[choice].Index)
			passUntilStackEmpty(t, e, 20)
			after := countDraw(e)
			t.Logf("draws pre=%d suspended=%d resumed=%d", pre, suspended, after)
			if after != pre+1 {
				t.Fatalf("resumed draw count = %d, want %d", after, pre+1)
			}
			if z := e.G.Obj(bearID).Zone; z != wantZone {
				t.Fatalf("%s spell zone = %s, want %s", branch, z, wantZone)
			}
		})
	}
}
