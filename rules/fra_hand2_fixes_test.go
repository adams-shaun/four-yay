package rules

// Engine divergences found by the second FRA hand-written Oracle audit
// (rules/testdata/oracle, branch wt/xo-hand2). Each test was written RED
// against the unfixed engine.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// TestAbilityCastValidSALoyaltyTerms pins the activation arm's ValidSA$
// grammar for the loyalty family: `Activated.Loyalty` (14 corpus lines, the
// Keral Keep Disciples / Chandra's Regulator / Jaya's Phoenix "whenever you
// activate a loyalty ability" class) used to fall through every case and fail
// closed, so none of those triggers ever fired. A `+` joins terms that must
// all hold (Activated.Loyalty+OppCtrl), CountersRemovedToPay<OP><N> reads the
// counters the activation removed as its cost (Way of the Mind Sculptor), and
// an unknown term still fails closed.
func TestAbilityCastValidSALoyaltyTerms(t *testing.T) {
	t.Parallel()
	minus3 := &cards.SA{Kind: "AB", API: "Draw", Params: map[string]string{"Cost": "SubCounter<3/LOYALTY>", "Planeswalker": "True"}}
	plus1 := &cards.SA{Kind: "AB", API: "GainLife", Params: map[string]string{"Cost": "AddCounter<1/LOYALTY>", "Planeswalker": "True"}}
	tap := &cards.SA{Kind: "AB", API: "Draw", Params: map[string]string{"Cost": "T"}}
	cases := []struct {
		name    string
		ab      *cards.SA
		valid   string
		removed int32
		abCtrl  state.PlayerID
		want    bool
	}{
		{"loyalty minus", minus3, "Activated.Loyalty", 3, 0, true},
		{"loyalty plus", plus1, "Activated.Loyalty", 0, 0, true},
		{"non-loyalty", tap, "Activated.Loyalty", 0, 0, false},
		{"not loyalty", tap, "Activated.!Loyalty", 0, 0, true},
		{"removed three GE2", minus3, "Activated.Loyalty+CountersRemovedToPayGE2", 3, 0, true},
		{"removed one GE2", minus3, "Activated.Loyalty+CountersRemovedToPayGE2", 1, 0, false},
		{"removed none GE2", plus1, "Activated.Loyalty+CountersRemovedToPayGE2", 0, 0, false},
		{"removed unknown fails closed", minus3, "Activated.Loyalty+CountersRemovedToPayGE2", -1, 0, false},
		{"loyalty opp ctrl", minus3, "Activated.Loyalty+OppCtrl", 3, 1, true},
		{"loyalty own ctrl vs OppCtrl", minus3, "Activated.Loyalty+OppCtrl", 3, 0, false},
		{"unknown term fails closed", minus3, "Activated.Loyalty+Bogus", 3, 0, false},
		{"crew vehicle still unread", tap, "Activated.Crew+Vehicle", 0, 0, false},
	}
	// The classifier reads only IsLoyaltyAbility/cards.IsManaAbilityAPI, which
	// touch no engine state.
	b := boardOf(&Engine{})
	for _, c := range cases {
		if got := trigmatch.AbilityCastValidSA(b, c.ab, c.valid, c.abCtrl, 0, c.removed); got != c.want {
			t.Errorf("%s: abilityCastValidSA(%q, removed %d) = %v, want %v", c.name, c.valid, c.removed, got, c.want)
		}
	}
}

// TestWayOfTheMindSculptorDrawsWhenTwoCountersRemoved drives the card end to
// end: its ETB empowers Jace 5, the Jace token's [-3] removes three loyalty
// counters, so "whenever you activate a loyalty ability, if you removed two
// or more loyalty counters to activate it, draw a card" draws one card on top
// of the ability's own draw. The [-1] the next turn (one counter) draws
// nothing extra.
func TestWayOfTheMindSculptorDrawsWhenTwoCountersRemoved(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Way of the Mind Sculptor")}, nil)
	moveByName(t, e, 0, "Way of the Mind Sculptor", state.ZHand)
	addMana(t, e, 0, "UUUUU")
	castNamed(t, e, "Way of the Mind Sculptor")
	passUntilStackEmpty(t, e, 40)
	toks := jaceTokens(e, 0)
	if len(toks) != 1 || e.G.Obj(toks[0]).Counter("LOYALTY") != 5 {
		t.Fatalf("precondition: Way of the Mind Sculptor's ETB did not make a 5-loyalty Jace token: %v", toks)
	}
	tok := toks[0]
	hand := len(e.G.Zone(state.ZHand, 0))
	opt, ok := findAbilityOptionByLabel(e, tok, "Draw a card")
	if !ok {
		t.Fatalf("the Jace token's [-3] is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(tok).Counter("LOYALTY"); got != 2 {
		t.Fatalf("Jace token loyalty after [-3] = %d, want 2", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand+2 {
		t.Fatalf("hand after [-3] with Way out = %d, want %d (the ability's draw plus Way's)", got, hand+2)
	}
	replayCheck(t, e, cfg)
}

// TestPreparedCopyPaysItsManaCost pins that casting a prepared permanent's
// spell copy pays that spell's mana cost. The reminder text is "you may cast
// a copy of its spell" -- no "without paying its mana cost" -- so CR 601.2f
// applies; Forge's prepared effect is likewise a plain `MayPlay$ True` grant
// with no MayPlayWithoutManaCost$. The engine used to price and charge the
// copy as free (Cost{}), so Whiplash Wordsmith's Vicious Verse ({B/R}) was
// castable from an empty pool and left floating mana untouched.
func TestPreparedCopyPaysItsManaCost(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Whiplash Wordsmith")}, nil)
	ww := moveByName(t, e, 0, "Whiplash Wordsmith", state.ZHand)
	addMana(t, e, 0, "BBBB")
	castNamed(t, e, "Whiplash Wordsmith")
	answerHybridPayment(t, e)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(ww); o == nil || o.Zone != state.ZBattlefield || !o.Prepared {
		t.Fatalf("precondition: Whiplash Wordsmith should be on the battlefield prepared, got %+v", o)
	}
	copyID := sosPreparedCopy(e, "Vicious Verse")
	if copyID == 0 {
		t.Fatal("precondition: no Vicious Verse prepared copy in exile")
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("precondition: pool not empty: %v", e.G.Players[0].Pool)
	}
	if _, ok := sosPreparedCastOption(e, copyID); ok {
		t.Fatal("the prepared Vicious Verse copy ({B/R}) is offered with an empty pool and no mana sources")
	}
	addMana(t, e, 0, "B")
	opt, ok := sosPreparedCastOption(e, copyID)
	if !ok {
		t.Fatalf("the prepared copy is not offered with {B} floating: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	answerHybridPayment(t, e)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("casting the prepared copy left %d mana floating; its {B/R} was not paid", got)
	}
	if got := e.G.Players[1].Life; got != 19 {
		t.Fatalf("Vicious Verse copy: opponent life %d, want 19", got)
	}
	replayCheck(t, e, cfg)
}

// answerHybridPayment answers the cast's target ask and its "how to pay
// this hybrid symbol" asks with their first option until none is pending.
func answerHybridPayment(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 8; i++ {
		d := e.Pending()
		if d == nil || len(d.Options) == 0 ||
			(d.Kind != decision.KTarget && (d.Kind != decision.KChoose || !strings.HasPrefix(d.Options[0].Kind, "pay_"))) {
			return
		}
		submitChoices(t, e, d.Options[0].Index)
	}
}

// TestJacesMachinationsLoyaltyAtInstantSpeed pins the "until end of turn,
// you may activate loyalty abilities of Jace planeswalkers you control on any
// player's turn any time you could cast an instant" half of Jace's
// Machinations: an Effect-delivered `Mode$ CastWithFlash | ValidSA$
// Activated.Loyalty` static. It used to be a bare "unimplemented" Note and the
// CR 606.3 loyalty gate demanded sorcery timing unconditionally, so the Jace
// token's abilities were never offered with a spell on the stack. The
// permission changes timing only: the once-per-permanent-per-turn limit
// stands, and it ends with the turn.
func TestJacesMachinationsLoyaltyAtInstantSpeed(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Jace's Machinations"), lookup(t, reg, "Lightning Bolt")}, nil)
	moveByName(t, e, 0, "Jace's Machinations", state.ZHand)
	moveByName(t, e, 0, "Lightning Bolt", state.ZHand)
	addMana(t, e, 0, "UUU")
	castNamed(t, e, "Jace's Machinations")
	passUntilStackEmpty(t, e, 40)
	toks := jaceTokens(e, 0)
	if len(toks) != 1 || e.G.Obj(toks[0]).Counter("LOYALTY") != 8 {
		t.Fatalf("precondition: Jace's Machinations did not make an 8-loyalty Jace token: %v", toks)
	}
	tok := toks[0]
	addMana(t, e, 0, "R")
	castNamed(t, e, "Lightning Bolt")
	answerHybridPayment(t, e)
	if len(e.G.Stack) == 0 {
		t.Fatal("precondition: Lightning Bolt is not on the stack")
	}
	opt, ok := findAbilityOptionByLabel(e, tok, "Draw a card")
	if !ok {
		t.Fatalf("with Lightning Bolt on the stack the Jace token's [-3] is not offered under Jace's Machinations: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	if _, again := findAbilityOptionByLabel(e, tok, "Surveil"); again {
		t.Fatal("a second loyalty ability of the same Jace was offered in the same turn (CR 606.3)")
	}
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(tok).Counter("LOYALTY"); got != 5 {
		t.Fatalf("Jace token loyalty after [-3] = %d, want 5", got)
	}
	replayCheck(t, e, cfg)
}

// TestEmpowerTokenLoyaltyOncePerTurn pins CR 606.3 for a planeswalker TOKEN
// created this turn: "only if no player has previously activated a loyalty
// ability of that permanent that turn". A token is minted straight onto the
// battlefield (TokenCreate folds AddObject + Move, no MoveZone event), so the
// per-turn fold never saw it enter and counted none of its activations: Way
// of the Mind Sculptor's fresh Jace token could [-1], let it resolve, and
// [-3] again in the same main phase.
func TestEmpowerTokenLoyaltyOncePerTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Way of the Mind Sculptor")}, nil)
	moveByName(t, e, 0, "Way of the Mind Sculptor", state.ZHand)
	addMana(t, e, 0, "UUUUU")
	castNamed(t, e, "Way of the Mind Sculptor")
	passUntilStackEmpty(t, e, 40)
	toks := jaceTokens(e, 0)
	if len(toks) != 1 {
		t.Fatalf("precondition: no Jace token: %v", toks)
	}
	tok := toks[0]
	opt, ok := findAbilityOptionByLabel(e, tok, "Surveil")
	if !ok {
		t.Fatalf("the Jace token's [-1] is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	drainKeepTop(t, e, "the Jace token's [-1]")
	if len(e.G.Stack) != 0 || e.G.Obj(tok).Counter("LOYALTY") != 4 {
		t.Fatalf("precondition: [-1] did not resolve to loyalty 4 (stack %v)", e.G.Stack)
	}
	if got := e.loyaltyActivationsThisTurn(tok); got != 1 {
		t.Errorf("loyaltyActivationsThisTurn(new Jace token) = %d after one activation, want 1", got)
	}
	if _, again := findAbilityOptionByLabel(e, tok, "Draw a card"); again {
		t.Fatal("the Jace token's [-3] is offered after its [-1] resolved in the same turn (CR 606.3)")
	}
	replayCheck(t, e, cfg)
}
