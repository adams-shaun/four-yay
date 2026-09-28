// Task agent-20260923T105645Z-f1e41416 (the cr6012g-window row):
// affordableTargetCandidates proves a repriced weak target fundable through
// the CR 601.2g window, but the window probe (castWindowUnits ->
// windowManaUnits) priced only free-cost abilities with LITERAL Amount$ and
// dropped every dynamic-amount or paid-cost source. So a Belt of Giant
// Strength equip against the 2-power target ({10} - {2} = {8}) was withheld
// from the target menu even though a Viridian-Joiner-shaped source could
// produce the difference while paying. These leaves pin the repro on the
// real corpus Belt plus an inline funding source, and the superset/anti-abort
// invariant: every source the probe promises is one manaWindowAsk offers.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const (
	// equipWindowJoinerSrc is the report's dynamically-evaluated producer:
	// "{T}: Add {G} equal to its power" as Amount$ X / SVar:X:Count$CardPower,
	// power 2 -> two green.
	equipWindowJoinerSrc = "Name:Joiner\nManaCost:2 G\nTypes:Creature Elf Druid\nPT:2/2\n" +
		"A:AB$ Mana | Cost$ T | Produced$ G | Amount$ X | SpellDescription$ Add an amount of {G} equal to CARDNAME's power.\n" +
		"SVar:X:Count$CardPower\nOracle:x\n"
	// equipWindowPayLifeSrc pays life for its mana (a non-free activation
	// cost), producing one red.
	equipWindowPayLifeSrc = "Name:PayLifeRock\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ T PayLife<1> | Produced$ R | SpellDescription$ Add {R}.\nOracle:x\n"
	// equipWindowGenericSrc pays a literal generic for its mana, producing two
	// red (net one red once the generic is paid).
	equipWindowGenericSrc = "Name:GenericRock\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ 1 T | Produced$ R | Amount$ 2 | SpellDescription$ Add {R}{R}.\nOracle:x\n"
	// equipWindowSelfSacSrc sacrifices itself for its mana: the deterministic
	// single-candidate batch.
	equipWindowSelfSacSrc = "Name:SelfSacRock\nTypes:Artifact\n" +
		"A:AB$ Mana | Cost$ T Sac<1/CARDNAME> | Produced$ C | SpellDescription$ Add {C}.\nOracle:x\n"
	// equipWindowInstantSrc is an InstantSpeed$ True producer: never offered
	// inside a payment window, so the probe must not promise it either.
	equipWindowInstantSrc = "Name:InstantRock\nTypes:Artifact\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C | InstantSpeed$ True | SpellDescription$ Add {C}. Activate only as an instant.\nOracle:x\n"
)

// equipWindowBoard is the driven board the two leaves share.
type equipWindowBoard struct {
	engine                     *Engine
	cfg                        Config
	belt, brute, small, joiner state.ObjID
	byName                     map[string]state.ObjID
}

// equipWindowGame builds the beltGame board (real corpus Belt plus the 4/4 and
// 2/2 fixtures at turn 3 Main1) and additionally seeds the caller's inline
// mana-source fixtures onto seat 0's battlefield. Every seeded permanent is
// untapped and the pool is empty when it returns.
func equipWindowGame(t *testing.T, seed uint64, extra ...string) equipWindowBoard {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	beltCard, ok := reg.Lookup("Belt of Giant Strength")
	if !ok {
		t.Fatal("Belt of Giant Strength not found in the compiled corpus registry")
	}
	deck0 := []*cards.Card{beltCard, card(t, equipReduceBruteSrc), card(t, equipReduceSmallSrc)}
	for _, src := range extra {
		deck0 = append(deck0, card(t, src))
	}
	deck0 = append(deck0, mountainDeck(t, 37)...)
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck0, mountainDeck(t, 40)},
		Tokens: reg.Tokens,
	}
	e := New(seatZeroStart(cfg))
	e.Advance()
	beltID := findAndMoveToHand(t, e, 0, "Belt of Giant Strength")
	moveToBattlefield(t, e, beltID)
	bruteID := moveSeeded(t, e, 0, equipReduceBruteSrc, state.ZBattlefield)
	smallID := moveSeeded(t, e, 0, equipReduceSmallSrc, state.ZBattlefield)
	byName := map[string]state.ObjID{}
	for _, src := range extra {
		id := moveSeeded(t, e, 0, src, state.ZBattlefield)
		byName[card(t, src).Faces[0].Name] = id
	}
	e.pending = nil
	e.Advance()
	driveToStep(t, e, 3, 0, state.StepMain1)
	joiner, ok := byName["Joiner"]
	if !ok {
		joiner = 0
	}
	return equipWindowBoard{engine: e, cfg: cfg, belt: beltID, brute: bruteID, small: smallID, joiner: joiner, byName: byName}
}

// TestEquipReduceWindowFundedTargetIsOffered is the brief's repro: with {6}
// floating and an untapped Joiner (power 2 -> {G}{G}), the 2/2's repriced {8}
// is fundable in the CR 601.2g window, so the 2/2 must be offered as a target,
// and completing the activation must charge exactly the 2/2's {8} price.
func TestEquipReduceWindowFundedTargetIsOffered(t *testing.T) {
	t.Parallel()
	b := equipWindowGame(t, 517, equipWindowJoinerSrc)
	e, cfg, beltID, bruteID, smallID, joinerID := b.engine, b.cfg, b.belt, b.brute, b.small, b.joiner

	// Preconditions: the repriced prices really differ, the pool really is
	// empty, and the funding source really is untapped and on the battlefield
	// where the window reads it.
	if got := e.Power(bruteID); got != 4 {
		t.Fatalf("brute fixture power = %d, want 4 (best-target price {6})", got)
	}
	if got := e.Power(smallID); got != 2 {
		t.Fatalf("small fixture power = %d, want 2 (repriced {8})", got)
	}
	if got := e.Power(joinerID); got != 2 {
		t.Fatalf("Joiner fixture power = %d, want 2 (its Amount$ X must resolve to 2)", got)
	}
	jo := e.G.Obj(joinerID)
	if jo == nil || jo.Zone != state.ZBattlefield || jo.Tapped {
		t.Fatalf("Joiner precondition: obj=%+v, want an untapped battlefield permanent", jo)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("precondition: pool = %d, want 0 before the fixture floats mana", got)
	}

	// {6} is the best-target offer price ({10} - {4}); the weak target's {8}
	// is payable only with the Joiner's {2}.
	addMana(t, e, 0, "CCCCCC")
	opt, ok := findAbilityOption(e, beltID, 0)
	if !ok {
		t.Fatal("Belt of Giant Strength's equip not offered from a {6} pool against a 4-power creature")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the equip's target decision, got %+v", d)
	}
	foundSmall, foundBrute := false, false
	for _, o := range d.Options {
		if o.Obj == smallID {
			foundSmall = true
		}
		if o.Obj == bruteID {
			foundBrute = true
		}
	}
	if !foundBrute {
		t.Fatalf("4-power target not offered at a {6} pool: %+v", d.Options)
	}
	if !foundSmall {
		t.Fatalf("2-power target withheld although its repriced {8} is fundable from {6} plus the untapped Joiner's {G}{G}: %+v", d.Options)
	}

	// Complete the activation on the WEAK target: the charge must be its own
	// {8} price, paid from {6} plus the Joiner's {2}. The CR 601.2g window is
	// posed before the ability reaches the stack, so answer its activate
	// option for the Joiner (its presence is itself part of the fix) and then
	// drain.
	targetObject(t, e, smallID)
	tapWindowSource(t, e, joinerID)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(beltID).AttachedTo; got != smallID {
		t.Fatalf("Belt attached to %d, want the 2/2 %d", got, smallID)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after equipping the 2/2 from {6}+{2} = %d, want 0 (exactly the weak target's {8})", got)
	}
	if !e.G.Obj(joinerID).Tapped {
		t.Fatal("the Joiner was not tapped to fund the weak target's price")
	}
	replayCheck(t, e, cfg)
}

// TestCastWindowProvableSubsetOfManaWindowOffers is the superset/anti-abort
// invariant: over a board carrying every shape the widened layer can price
// plus two shapes it must not (InstantSpeed$ True, a {1}-for-{1} net-zero
// rock), every source castWindowUnits promises is a member of the source list
// manaWindowAsk would offer (battlefield/hand/graveyard, non-InstantSpeed,
// not already committed to this cast).
func TestCastWindowProvableSubsetOfManaWindowOffers(t *testing.T) {
	t.Parallel()
	b := equipWindowGame(t, 518,
		equipWindowJoinerSrc, equipWindowPayLifeSrc, equipWindowGenericSrc,
		equipWindowSelfSacSrc, equipWindowInstantSrc)
	e := b.engine
	instantID := b.byName["InstantRock"]
	if instantID == 0 {
		t.Fatal("InstantSpeed fixture was not seeded onto the battlefield")
	}

	// Drive the Belt equip to its target ask so e.cast is a real pendingCast.
	addMana(t, e, 0, "CCCCCC")
	opt, ok := findAbilityOption(e, b.belt, 0)
	if !ok {
		t.Fatal("Belt of Giant Strength's equip not offered from a {6} pool")
	}
	submitChoices(t, e, opt.Index)
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the equip's target decision, got %+v", e.Pending())
	}
	pc := e.cast
	if pc == nil {
		t.Fatal("no pending cast while the target decision is posed")
	}

	got := e.castWindowUnits(pc)
	alts := map[state.ObjID]int{}
	for _, u := range got {
		alts[u.id] += len(u.alts)
	}
	// PRECONDITION: the widened layer actually contributed the dynamic- and
	// paid-cost sources, or this test proves nothing about the fix.
	for _, name := range []string{"Joiner", "PayLifeRock", "GenericRock", "SelfSacRock"} {
		if id := b.byName[name]; id != 0 && alts[id] == 0 {
			t.Fatalf("castWindowUnits priced no alternative for %s (%d): the widened layer is absent", name, id)
		}
	}

	// The source list manaWindowAsk enumerates (cast.go manaWindowAsk).
	offered := map[state.ObjID]bool{}
	for _, z := range []state.Zone{state.ZBattlefield, state.ZHand, state.ZGraveyard} {
		for _, id := range e.G.Zone(z, pc.player) {
			if !e.convokeCommitted(pc, id) && e.untappedManaSource(pc.player, id) {
				offered[id] = true
			}
		}
	}
	for _, u := range got {
		if !offered[u.id] {
			t.Errorf("probe promised source %d, which manaWindowAsk will not offer", u.id)
		}
	}
	// The InstantSpeed$ producer is excluded by both sides; proving it here
	// keeps the test from passing if the probe walked the priority set.
	if offered[instantID] {
		t.Fatal("precondition: InstantSpeed$ True source is in the offer set; the test board is wrong")
	}
	if alts[instantID] != 0 {
		t.Errorf("probe priced InstantSpeed$ True source %d, which no payment window offers", instantID)
	}
}

// tapWindowSource answers a CR 601.2g mana-window KChoose by activating the
// named source's tap option, fataling if the window does not offer it. After
// the tap the engine either re-enters payment (which may pose another window)
// or proceeds; this helper answers only the first window.
func tapWindowSource(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("want the CR 601.2g mana window, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == id {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("the mana window did not offer to activate source %d: %+v", id, d.Options)
}
