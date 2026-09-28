// Task cli-20260923T060000Z-equip-reduce (the eqcm1 row's Targeted$ sub-shape):
// an equip cost reduction whose body reads a ROOT target ref used to resolve
// to 0 at the OFFER gate, so the ability was withheld at full price unless the
// controller happened to have the undiscounted amount available. The charge
// became target-aware earlier (alltargeted1's repriceForTargets), but the
// offer gate and beginActivation still folded a nil-target read of 0, so a
// `{10}` equip against a 4-power creature cost `{6}` yet was not offered from
// a `{6}` pool. ownReduceCostOffer now resolves the body against the best
// legal root target for the offer price (and beginActivation folds the same
// amount), while repriceForTargets still charges the amount for the target
// actually chosen at CR 601.2c. These leaves pin both halves on the real
// corpus Belt of Giant Strength (`K:Equip:10:::ReduceCost$ X` with
// `SVar:X:Targeted$CardPower`), the card the row names.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const (
	// equipReduceBruteSrc is a 4/4 target: power 4, so Belt's reduction is {4}.
	equipReduceBruteSrc = "Name:Brute\nManaCost:3 R\nTypes:Creature Ogre\nPT:4/4\nOracle:x\n"
	// equipReduceSmallSrc is a 2/2 target: power 2, reduction {2}.
	equipReduceSmallSrc = "Name:Small\nManaCost:G\nTypes:Creature Elf\nPT:2/2\nOracle:x\n"
)

// beltGame builds a 2-seat game with the real corpus Belt of Giant Strength and
// the two fixture creatures on seat 0's battlefield, driven to turn 3's Main1
// with a fresh priority decision. It returns the engine, config, the belt and
// the two creature ids (brute 4/4, small 2/2).
func beltGame(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	beltCard, ok := reg.Lookup("Belt of Giant Strength")
	if !ok {
		t.Fatal("Belt of Giant Strength not found in the compiled corpus registry")
	}
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{beltCard, card(t, equipReduceBruteSrc), card(t, equipReduceSmallSrc)}, mountainDeck(t, 37)...),
			mountainDeck(t, 40),
		},
		Tokens: reg.Tokens,
	}
	e := New(seatZeroStart(cfg))
	e.Advance()
	beltID := findAndMoveToHand(t, e, 0, "Belt of Giant Strength")
	moveToBattlefield(t, e, beltID)
	bruteID := moveSeeded(t, e, 0, equipReduceBruteSrc, state.ZBattlefield)
	smallID := moveSeeded(t, e, 0, equipReduceSmallSrc, state.ZBattlefield)
	// moveSeeded cleared the stale genesis priority ask; re-drive and pass one
	// full round so the board parks at turn 3 Main1 for seat 0.
	e.pending = nil
	e.Advance()
	driveToStep(t, e, 3, 0, state.StepMain1)
	return e, cfg, beltID, bruteID, smallID
}

func TestBeltOfGiantStrengthEquipOfferedAtTargetDiscountedPrice(t *testing.T) {
	t.Parallel()
	e, cfg, beltID, bruteID, _ := beltGame(t, 511)
	// Precondition: the fixture really is a 4-power creature and Belt's equip
	// really is {10}, so the reduction ({10} -> {6}) is nonzero. A vacuous
	// setup (power 0, or a printed {6}) must fail here, not pass silently.
	if got := e.Power(bruteID); got != 4 {
		t.Fatalf("fixture target power = %d, want 4 (the reduction depends on it)", got)
	}
	// {6} is exactly {10} minus the 4-power target's reduction. Before the fix
	// the offer gate read the reduction as 0 and withheld the equip.
	addMana(t, e, 0, "CCCCCC")
	opt, ok := findAbilityOption(e, beltID, 0)
	if !ok {
		t.Fatal("Belt of Giant Strength's equip not offered from a {6} pool against a 4-power creature ({10} - {4})")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, bruteID)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(beltID).AttachedTo != bruteID {
		t.Fatalf("Belt attached to %d, want the 4/4 %d", e.G.Obj(beltID).AttachedTo, bruteID)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after equipping the 4/4 = %d, want 0 (exactly {6} charged, the discounted price)", got)
	}
	replayCheck(t, e, cfg)
}

func TestBeltOfGiantStrengthEquipChargeFollowsChosenTarget(t *testing.T) {
	t.Parallel()
	e, cfg, beltID, bruteID, smallID := beltGame(t, 512)
	if got := e.Power(bruteID); got != 4 {
		t.Fatalf("brute fixture power = %d, want 4", got)
	}
	if got := e.Power(smallID); got != 2 {
		t.Fatalf("small fixture power = %d, want 2 (the two targets must differ)", got)
	}
	// {8} is {10} minus the SMALL target's reduction. The offer gate prices the
	// ability against the best legal target ({4} -> {6}), so it is offered; the
	// charge must still be the price for the target ACTUALLY chosen ({8}), not
	// the best-case {6}. Without the offer-side fix the equip is withheld at its
	// undiscounted {10} and this fails at the offer assertion.
	addMana(t, e, 0, "CCCCCCCC")
	opt, ok := findAbilityOption(e, beltID, 0)
	if !ok {
		t.Fatal("Belt of Giant Strength's equip not offered from an {8} pool")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, smallID)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(beltID).AttachedTo != smallID {
		t.Fatalf("Belt attached to %d, want the 2/2 %d", e.G.Obj(beltID).AttachedTo, smallID)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after equipping the 2/2 from {8} = %d, want 0 ({8} charged, the chosen target's power; {6} would mean the best-case offer price was charged)", got)
	}
	replayCheck(t, e, cfg)
}

func TestBeltOfGiantStrengthEquipWithheldWhenBestTargetStillTooSmall(t *testing.T) {
	t.Parallel()
	e, _, beltID, bruteID, _ := beltGame(t, 513)
	if got := e.Power(bruteID); got != 4 {
		t.Fatalf("fixture target power = %d, want 4 (the best legal reduction is {4})", got)
	}
	// The feature's handler must have RUN, or a withheld option proves nothing
	// (a reverted helper/registration would also withhold). Read the offer-side
	// reduction directly: it must be the 4-power target's {4}, while the
	// nil-target read it replaced is 0. This fails if the target-binding fix is
	// reverted even though the option's withholding below is the same either way.
	o := e.G.Obj(beltID)
	pa, ok := o.PileAbilityAt(0)
	if !ok {
		t.Fatal("Belt of Giant Strength's equip ability not found at index 0")
	}
	if got := e.ownReduceCost(0, beltID, pa.SA, nil, nil, pa.Merged); got != 0 {
		t.Fatalf("nil-target reduction = %d, want 0 (targets are absent at offer time)", got)
	}
	if got := e.ownReduceCostOffer(0, beltID, pa.SA, pa.Merged); got != 4 {
		t.Fatalf("offer-side reduction against the potential 4-power target = %d, want 4", got)
	}
	// {5} is one short of the best legal price {10} - {4} = {6}: no target can
	// make the equip affordable, so the offer gate must hold it out (fail
	// closed) rather than offer an activation that cannot complete.
	addMana(t, e, 0, "CCCCC")
	if opt, ok := findAbilityOption(e, beltID, 0); ok {
		t.Fatalf("Belt of Giant Strength's equip offered from {5} against a 4-power creature, but the best price is {6}: %+v", opt)
	}
}

// TestBeltOfGiantStrengthEquipWeakTargetNotOfferedAtBestOnlyPool pins the
// review-r2 MAJOR: the offer gate prices the activation at the BEST legal
// target's reduction, so from a {6} pool only the 4-power target's price is
// payable. The CR 601.2c target ask must then withhold the 2-power target
// (affordableTargetCandidates reprices it to {8} against a {6} pool with no
// untapped mana source -- driveToStep passes priority, so the fixture has no
// land in play and the 601.2g window escape cannot mask the prune), or the
// player can select an unpayable target and the transaction aborts at
// CR 601.2h after an apparently-legal choice. The 4-power target stays
// offered and completing the activation still charges exactly {6}.
func TestBeltOfGiantStrengthEquipWeakTargetNotOfferedAtBestOnlyPool(t *testing.T) {
	t.Parallel()
	e, cfg, beltID, bruteID, smallID := beltGame(t, 514)
	// Preconditions: the two targets' powers really differ (so their repriced
	// prices {6} and {8} really differ) and the board really has no untapped
	// mana source, or the withhold would be masked by the window escape.
	if got := e.Power(bruteID); got != 4 {
		t.Fatalf("brute fixture power = %d, want 4", got)
	}
	if got := e.Power(smallID); got != 2 {
		t.Fatalf("small fixture power = %d, want 2 (the repriced prices must differ)", got)
	}
	if e.hasUntappedManaSource(0) {
		t.Fatal("fixture seat 0 has an untapped mana source; the 601.2g escape would keep the unpayable target on the menu")
	}
	// {6} is exactly the best-target price ({10} - {4}); the activation is
	// offered, but only the target it was priced against is payable.
	addMana(t, e, 0, "CCCCCC")
	opt, ok := findAbilityOption(e, beltID, 0)
	if !ok {
		t.Fatal("Belt of Giant Strength's equip not offered from a {6} pool against a 4-power creature (the best-target price)")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == smallID {
			t.Fatalf("2-power target offered at a {6} pool although its repriced price is {8}: %+v", d.Options)
		}
	}
	found := false
	for _, o := range d.Options {
		if o.Obj == bruteID {
			found = true
		}
	}
	if !found {
		t.Fatalf("4-power target not offered alongside the withheld 2/2: %+v", d.Options)
	}
	targetObject(t, e, bruteID)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(beltID).AttachedTo != bruteID {
		t.Fatalf("Belt attached to %d, want the 4/4 %d", e.G.Obj(beltID).AttachedTo, bruteID)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after equipping the 4/4 from {6} = %d, want 0 (exactly the best-target price charged)", got)
	}
	replayCheck(t, e, cfg)
}
