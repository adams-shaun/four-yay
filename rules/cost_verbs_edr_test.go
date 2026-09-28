package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file closes the AGENTS.md "Known approximations" row that claimed
// "Exile/Discard/Return and other cost verbs remain unsupported": the three
// verbs are modelled by ParseCost, priced by nonManaCastable and paid by the
// cast/activation flow, so a CARDNAME-bearing cost of each verb IS offered
// and IS paid. Each test below drives a REAL corpus card whose whole cost is
// that verb and asserts the source actually moves (the payment), not just
// that an option appeared.

// costAbilityIndex returns the index of the first activated ability of card
// whose Cost$ carries the given token, fatal if none does -- the anchor the
// real-corpus activation tests use (exile_any_grave_cost_test.go's pattern).
func costAbilityIndex(t *testing.T, c *cards.Card, token string) int {
	t.Helper()
	for i, sa := range c.Faces[0].Abilities {
		if sa.Kind == "AB" && strings.Contains(sa.Params["Cost"], token) {
			return i
		}
	}
	t.Fatalf("card %q carries no ability with cost token %q", c.Faces[0].Name, token)
	return -1
}

// edrBoard builds a two-seat game over real corpus cards, moving each named
// seat-0 card to its target zone through logged events (so replayCheck holds)
// and advancing to seat 0's first main phase. ids maps every named card to
// its object.
func edrBoard(t *testing.T, reg *cards.Registry, seed uint64, moves map[string]state.Zone) (*Engine, Config, map[string]state.ObjID) {
	t.Helper()
	names := make([]string, 0, len(moves))
	byName := map[string]*cards.Card{}
	for name := range moves {
		c := mustCorpusCard(t, reg, name)
		byName[name] = c
		names = append(names, name)
	}
	// Deterministic deck order so the replay rebuild sees the same deck.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	var deck []*cards.Card
	for _, name := range names {
		deck = append(deck, byName[name])
	}
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append(deck, mountainDeck(t, 40-len(deck))...), mountainDeck(t, 40)}}
	e := New(cfg)
	ids := map[string]state.ObjID{}
	for _, name := range names {
		ids[name] = moveByName(t, e, 0, name, moves[name])
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.Advance()
	edrSeatZeroPriority(t, e)
	return e, cfg, ids
}

// edrSeatZeroPriority passes priority until seat 0 (the caller's
// protagonist) holds the pending decision, so an ability option and a submit
// target the same seat.
func edrSeatZeroPriority(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d != nil && d.Kind == decision.KPriority && d.Player != 0 {
			passPriority(t, e)
			continue
		}
		break
	}
}

// TestCostVerbExileDiscardReturnParsePrecisely asserts (the precondition for
// every behaviour test below) that ParseCost models each verb as its own real
// part, with no phantom generic and no Unknown census entry: before the
// verbs were modelled each token fell through to the unrecognised-symbol
// fallback and priced one generic mana, which is exactly the deviation the
// deleted row described.
func TestCostVerbExileDiscardReturnParsePrecisely(t *testing.T) {
	t.Parallel()
	exile := ParseCost("Exile<1/CARDNAME>")
	if len(exile.Exile) != 1 || exile.Exile[0].N != 1 || exile.Exile[0].Spec != "CARDNAME" {
		t.Fatalf("Exile<1/CARDNAME> part = %+v", exile.Exile)
	}
	if exile.Generic != 0 || len(exile.Unknown) != 0 {
		t.Fatalf("Exile priced a phantom generic or reported unknown: generic=%d unknown=%v", exile.Generic, exile.Unknown)
	}

	discard := ParseCost("U Discard<1/CARDNAME>")
	if len(discard.Discard) != 1 || discard.Discard[0].N != 1 || discard.Discard[0].Spec != "CARDNAME" {
		t.Fatalf("Discard<1/CARDNAME> part = %+v", discard.Discard)
	}
	if discard.Generic != 0 || discard.Colored[state.ManaIndex('U')] != 1 || len(discard.Unknown) != 0 {
		t.Fatalf("Discard parse wrong: %+v", discard)
	}

	ret := ParseCost("Return<1/CARDNAME>")
	if len(ret.Return) != 1 || ret.Return[0].N != 1 || ret.Return[0].Spec != "CARDNAME" {
		t.Fatalf("Return<1/CARDNAME> part = %+v", ret.Return)
	}
	if ret.Generic != 0 || len(ret.Unknown) != 0 {
		t.Fatalf("Return priced a phantom generic or reported unknown: generic=%d unknown=%v", ret.Generic, ret.Unknown)
	}
}

// TestEnhancedSurveillanceExilesItselfAsItsCost: the corpus ability
// `A:AB$ ChangeZoneAll | Cost$ Exile<1/CARDNAME>` (Enhanced Surveillance) is
// OFFERED, and paying it moves the source to exile and resolves its body
// (the controller's graveyard is shuffled into their library). A row-17
// engine withheld the offer entirely.
func TestEnhancedSurveillanceExilesItselfAsItsCost(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	surv := mustCorpusCard(t, reg, "Enhanced Surveillance")
	idx := costAbilityIndex(t, surv, "Exile<1/CARDNAME>")
	e, cfg, ids := edrBoard(t, reg, 61, map[string]state.Zone{
		"Enhanced Surveillance": state.ZBattlefield,
		"Grizzly Bears":         state.ZGraveyard,
	})
	src, fodder := ids["Enhanced Surveillance"], ids["Grizzly Bears"]
	// Precondition: the source is where the rule reads it and the fodder
	// really is in the graveyard the body moves.
	if e.G.Obj(src).Zone != state.ZBattlefield {
		t.Fatalf("Surveillance zone = %v, want battlefield", e.G.Obj(src).Zone)
	}
	if e.G.Obj(fodder).Zone != state.ZGraveyard {
		t.Fatalf("fodder zone = %v, want graveyard", e.G.Obj(fodder).Zone)
	}
	opt := abilityOption(t, e, src, idx)
	submitChoices(t, e, opt.Index)
	if e.G.Obj(src).Zone != state.ZExile {
		t.Fatalf("paying Exile<1/CARDNAME> did not exile the source: %v", e.G.Obj(src).Zone)
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(fodder).Zone != state.ZLibrary {
		t.Fatalf("the Exile-cost ability's body did not shuffle the graveyard away: fodder=%v", e.G.Obj(fodder).Zone)
	}
	replayCheck(t, e, cfg)
}

// TestMnemonicSphereDiscardsItselfFromHandAsItsCost: the corpus Channel
// ability `A:AB$ Draw | Cost$ U Discard<1/CARDNAME> | ActivationZone$ Hand`
// (Mnemonic Sphere) is offered from the HAND, and paying it discards the
// Sphere and draws its controller a card.
func TestMnemonicSphereDiscardsItselfFromHandAsItsCost(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sphere := mustCorpusCard(t, reg, "Mnemonic Sphere")
	idx := costAbilityIndex(t, sphere, "Discard<1/CARDNAME>")
	e, cfg, ids := edrBoard(t, reg, 62, map[string]state.Zone{
		"Mnemonic Sphere": state.ZHand,
	})
	sph := ids["Mnemonic Sphere"]
	// Precondition: the card is in hand (where its ActivationZone$ lets the
	// hand ability be offered) and the pool really holds the {U} the parse
	// above priced.
	if e.G.Obj(sph).Zone != state.ZHand {
		t.Fatalf("Sphere zone = %v, want hand", e.G.Obj(sph).Zone)
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 1})
	if e.G.Players[0].Pool[state.MU] != 1 {
		t.Fatalf("pool = %+v, want one blue", e.G.Players[0].Pool)
	}
	// Re-snapshot the priority decision so the freshly funded pool is part of
	// the offer gate's price check: Advance alone keeps the stored pending
	// snapshot, which was built before the mana arrived.
	e.pending = nil
	e.priorityRound()
	edrSeatZeroPriority(t, e)
	handBefore := len(e.G.Zone(state.ZHand, 0))
	opt := abilityOption(t, e, sph, idx)
	submitChoices(t, e, opt.Index)
	// Discard<1/CARDNAME> with exactly one candidate still poses the
	// discard-cost choice: answering it is what actually pays.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		picked := -1
		for _, o := range d.Options {
			if o.Obj == sph {
				picked = o.Index
			}
		}
		if picked < 0 {
			t.Fatalf("discard ask did not offer the source to discard: %+v", d.Options)
		}
		submitChoices(t, e, picked)
	}
	if e.G.Obj(sph).Zone != state.ZGraveyard {
		t.Fatalf("paying Discard<1/CARDNAME> did not discard the source: %v", e.G.Obj(sph).Zone)
	}
	passUntilStackEmpty(t, e, 20)
	// The Sphere left the hand and one card was drawn: net hand size is
	// unchanged, but the draw is a real logged library->hand move.
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore {
		t.Fatalf("hand size = %d, want %d (discard one, draw one)", got, handBefore)
	}
	if countMoves(e.L.Events, sph, state.ZGraveyard) != 1 {
		t.Fatal("the discard is not a single logged move to the graveyard")
	}
	replayOK := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw && ev.Player == 0 {
			replayOK = true
		}
	}
	if !replayOK {
		t.Fatal("the Discard-cost ability's body did not draw a card")
	}
	replayCheck(t, e, cfg)
}

// TestBrokenFallReturnsItselfAsItsCost: the corpus ability
// `A:AB$ Regenerate | ValidTgts$ Creature | Cost$ Return<1/CARDNAME>` is
// offered from the battlefield, and paying it returns the Aura-like
// enchantment source to its owner's hand and shields the chosen creature.
func TestBrokenFallReturnsItselfAsItsCost(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	fall := mustCorpusCard(t, reg, "Broken Fall")
	idx := costAbilityIndex(t, fall, "Return<1/CARDNAME>")
	e, cfg, ids := edrBoard(t, reg, 63, map[string]state.Zone{
		"Broken Fall":   state.ZBattlefield,
		"Grizzly Bears": state.ZBattlefield,
	})
	bf, bears := ids["Broken Fall"], ids["Grizzly Bears"]
	// Precondition: both objects are where the rule reads them, and the
	// target is a legal creature distinct from the source.
	if e.G.Obj(bf).Zone != state.ZBattlefield || e.G.Obj(bears).Zone != state.ZBattlefield {
		t.Fatalf("zones wrong: BrokenFall=%v Bears=%v", e.G.Obj(bf).Zone, e.G.Obj(bears).Zone)
	}
	if bf == bears {
		t.Fatal("source and target are the same object")
	}
	opt := abilityOption(t, e, bf, idx)
	submitChoices(t, e, opt.Index)
	// The target is announced at activation (CR 601.2c) before the cost is
	// paid (CR 601.2g): answer the creature target ask, driving any
	// intervening priority passes.
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while activating Broken Fall")
		}
		if d.Kind == decision.KTarget {
			picked := -1
			for _, o := range d.Options {
				if o.Obj == bears {
					picked = o.Index
				}
			}
			if picked < 0 {
				t.Fatalf("Broken Fall target ask lost the Bears: %+v", d.Options)
			}
			submitChoices(t, e, picked)
			break
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision activating Broken Fall: %+v", d)
		}
		e.resolveTop()
	}
	if e.G.Obj(bf).Zone != state.ZHand {
		t.Fatalf("paying Return<1/CARDNAME> did not return the source to hand: %v", e.G.Obj(bf).Zone)
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(bears).Counter("Shield") != 1 {
		t.Fatalf("the Return-cost ability's body did not shield the target: Shield=%d", e.G.Obj(bears).Counter("Shield"))
	}
	replayCheck(t, e, cfg)
}
