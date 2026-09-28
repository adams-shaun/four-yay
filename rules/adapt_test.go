package rules

// Task param-adapt (CR 702.35a): Adapt$ N on AB$ PutCounter was unread --
// the activation was offered without the CR 702.35a gate ("Activate only if
// this creature has no +1/+1 counters on it") and, worse, resolved placing
// the CounterNum$-default ONE counter instead of N. effPutCounter now reads
// Adapt$ N as the count (rules/effects/counters.go), the put is itself
// conditional on the recipient holding no +1/+1 counters (the effect half of
// 702.35a, which also governs Jetfire's chained `DB$ PutCounter | Adapt$ 3`
// body, where no activation gate exists), and the offer loops in
// rules/legal.go gate the activation through e.adaptGateOK -- the same
// offer-time funnel the IsPresent$/CheckSVar$/Boast$ gates share.
//
// Pteramander is the measured census carrier (`grep -rlE '^A:AB$
// PutCounter.*Adapt\$' .cards/cardsfolder` = 24 files): its ability is
//
//	A:AB$ PutCounter | Cost$ 7 U | Adapt$ 4 | ReduceCost$ X
//	SVar:X:Count$ValidGraveyard Instant.YouOwn,Sorcery.YouOwn
//
// so the leaves also exercise the accompanying ReduceCost$ discount through
// the existing ownReduceCost fold (the offer is only affordable at all when
// the graveyard discount is read -- the fixture funds less than the full
// price on purpose).
//
// Fixtures load the REAL compiled corpus card (never a copied Forge script).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// adaptFixture builds a two-seat game whose seat-0 deck is the real corpus
// Pteramander plus two authored instants (distinct names, so each can be
// moved to the graveyard by name) and Mountains. Pteramander is on seat 0's
// battlefield and both instants are in its graveyard; the caller funds the
// pool itself.
func adaptFixture(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	pter, ok := reg.Lookup("Pteramander")
	if !ok {
		t.Fatal("corpus fixture: Pteramander missing")
	}
	deck := []*cards.Card{pter,
		card(t, "Name:Fix Spark\nManaCost:R\nTypes:Instant\nOracle:x\n"),
		card(t, "Name:Fix Flask\nManaCost:R\nTypes:Instant\nOracle:x\n"),
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(deck, mountainDeck(t, 40-len(deck))...),
			mountainDeck(t, 40),
		},
		Tokens: map[string]*cards.Card{},
	})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	id := moveByName(t, e, 0, "Pteramander", state.ZBattlefield)
	moveByName(t, e, 0, "Fix Spark", state.ZGraveyard)
	moveByName(t, e, 0, "Fix Flask", state.ZGraveyard)
	return e, cfg, id
}

// TestPteramanderAdaptFourWithGraveyardDiscount: two instants in the
// graveyard discount {7}{U} to {5}{U}, which is exactly what the fixture
// funds -- a build that ignores ReduceCost$ never offers the ability at all
// (precondition), and a build that ignores Adapt$ resolves placing one
// counter instead of four.
func TestPteramanderAdaptFourWithGraveyardDiscount(t *testing.T) {
	t.Parallel()
	e, cfg, id := adaptFixture(t, 931)
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 2 {
		t.Fatalf("precondition: graveyard holds %d cards, want 2", got)
	}
	if got := e.G.Obj(id).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition: Pteramander already carries %d +1/+1 counters", got)
	}
	addMana(t, e, 0, "UUUUUU") // {5}{U} after the two-graveyard discount
	opt, ok := findAbilityOption(e, id, 0)
	if !ok {
		t.Fatalf("adapt not offered with pool {U}{U}{U}{U}{U}{U} and two graveyard instants -- the ReduceCost$ discount was not read: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Ability == nil {
		t.Fatalf("activation did not push the ability: stack %v", e.G.Stack)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying = %d, want 0 (the discounted {5}{U} exactly)", got)
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(id).Counter("P1P1"); got != 4 {
		t.Fatalf("Pteramander carries %d +1/+1 counters after resolving, want 4 (Adapt$ 4)", got)
	}
	replayCheck(t, e, cfg)
}

// TestPteramanderAdaptGateBlocksActivationWithCounters: the CR 702.35a
// activation gate withholds the ability once the creature carries a
// +1/+1 counter -- here after its own first Adapt resolved, so the gate
// blocks the SECOND activation with a fresh pool in hand.
func TestPteramanderAdaptGateBlocksActivationWithCounters(t *testing.T) {
	t.Parallel()
	e, cfg, id := adaptFixture(t, 933)
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 2 {
		t.Fatalf("precondition: graveyard holds %d cards, want 2", got)
	}
	addMana(t, e, 0, "UUUUUUUU") // full price {7}{U}, graveyard discounted
	opt, ok := findAbilityOption(e, id, 0)
	if !ok {
		t.Fatalf("adapt not offered at the discounted price: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(id).Counter("P1P1"); got != 4 {
		t.Fatalf("Pteramander carries %d +1/+1 counters after resolving, want 4", got)
	}
	addMana(t, e, 0, "UUUUUUUU")
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatal("adapt offered again while the creature carries +1/+1 counters -- the CR 702.35a activation gate was not read")
	}
	replayCheck(t, e, cfg)
}
