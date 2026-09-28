// Task agent-20260923T100454Z-377fd23a: the end-to-end pin for the
// `Targeted$CardNumColors` equip-cost reduction, on the real corpus
// Dragonfire Blade (`K:Equip:4:::ReduceCost$ X` with
// `SVar:X:Targeted$CardNumColors` -- "This ability costs {1} less to activate
// for each color of the creature it targets").
//
// The target-binding half (offer-gate pricing via ownReduceCostOffer +
// repriceForTargets charging the chosen target) landed in 0043f0e0/a1e46ad1/
// 14ce2fbf; the `<Ref>$<Property>` CardNumColors count head landed in
// a62d152c (effects/count.go evalRefProperty). Before both, the reduction
// resolved to 0: the equip charged the full {4} even against a monocolored
// creature with {3} available, and was withheld entirely from a {3} pool.
//
// rules/equip_targeted_reducecost_test.go pins the FULL flow only for
// Targeted$CardPower (Belt of Giant Strength); effects/count_ref_property_rv2b_test.go
// pins the count head only at unit level. No test on main drove a real
// Targeted$CardNumColors reduction end to end. These leaves close that gap.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const (
	// dragonfireMonoSrc is a monocolored target: green, so CardNumColors = 1
	// and Dragonfire Blade's reduction is {1} (equip {4} -> {3}).
	dragonfireMonoSrc = "Name:MonoTarget\nManaCost:G\nTypes:Creature Elf\nPT:2/2\nOracle:x\n"
	// dragonfireMultiSrc is a two-color target (green + white), so
	// CardNumColors = 2 and the reduction is {2} (equip {4} -> {2}).
	dragonfireMultiSrc = "Name:MultiTarget\nManaCost:G W\nTypes:Creature Elf\nPT:2/2\nOracle:x\n"
	// dragonfireColorlessSrc is a colorless target: CardNumColors = 0, so no
	// reduction at all and the equip stays at its printed {4}.
	dragonfireColorlessSrc = "Name:ColorlessTarget\nManaCost:3\nTypes:Creature Golem\nPT:2/2\nOracle:x\n"
)

// dragonfireGame builds a 2-seat game with the real corpus Dragonfire Blade and
// one fixture creature (extra) on seat 0's battlefield, driven to turn 3's
// Main1 with a fresh priority decision. It returns the engine, config, the
// blade and the creature id.
func dragonfireGame(t *testing.T, seed uint64, extra string) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	bladeCard, ok := reg.Lookup("Dragonfire Blade")
	if !ok {
		t.Fatal("Dragonfire Blade not found in the compiled corpus registry")
	}
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{bladeCard, card(t, extra)}, mountainDeck(t, 37)...),
			mountainDeck(t, 40),
		},
		Tokens: reg.Tokens,
	}
	e := New(seatZeroStart(cfg))
	e.Advance()
	bladeID := findAndMoveToHand(t, e, 0, "Dragonfire Blade")
	moveToBattlefield(t, e, bladeID)
	id := moveSeeded(t, e, 0, extra, state.ZBattlefield)
	// moveSeeded cleared the stale genesis priority ask; re-drive and pass one
	// full round so the board parks at turn 3 Main1 for seat 0.
	e.pending = nil
	e.Advance()
	driveToStep(t, e, 3, 0, state.StepMain1)
	return e, cfg, bladeID, id
}

// TestDragonfireBladeEquipOfferedAtColorDiscountedPrice pins the reported
// symptom's fix: with {3} floated and a MONOcolored target, the equip IS
// offered and charges exactly {3} (pool 0 after), the blade attaches to the
// chosen target, and replayCheck passes. Before a62d152c the
// Targeted$CardNumColors head read 0, so the equip priced at the full {4} and
// was withheld from the {3} pool.
func TestDragonfireBladeEquipOfferedAtColorDiscountedPrice(t *testing.T) {
	t.Parallel()
	e, cfg, bladeID, monoID := dragonfireGame(t, 611, dragonfireMonoSrc)
	// Precondition: the target really is monocolored, so its CardNumColors
	// read is 1 -- the {1} reduction is nonzero. A vacuous setup (colorless,
	// or a printed {3} equip) must fail here, not pass silently.
	if got := len(e.ObjectColors(e.G.Obj(monoID))); got != 1 {
		t.Fatalf("mono target color count = %d, want 1 (the reduction depends on it)", got)
	}
	// {3} is exactly Dragonfire Blade's printed {4} minus the 1-color target's
	// reduction. Before the fix the head read 0 and withheld the equip.
	addMana(t, e, 0, "CCC")
	opt, ok := findAbilityOption(e, bladeID, 0)
	if !ok {
		t.Fatal("Dragonfire Blade's equip not offered from a {3} pool against a 1-color creature ({4} - {1})")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, monoID)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(bladeID).AttachedTo != monoID {
		t.Fatalf("Dragonfire Blade attached to %d, want the monocolored %d", e.G.Obj(bladeID).AttachedTo, monoID)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after equipping the 1-color creature from {3} = %d, want 0 (exactly {3} charged, the discounted price)", got)
	}
	replayCheck(t, e, cfg)
}

// TestDragonfireBladeEquipChargeFollowsTargetColorCount pins that the
// reduction reads the TARGET's real color count rather than a constant {1}:
// with a TWO-color target and {3} floated the equip is offered and charges
// exactly {2} (pool 1 left), whereas a constant {1} read would charge {3}
// (pool 0). The mono leaf above charges {3}; the two charges differ by the
// target's own color count.
func TestDragonfireBladeEquipChargeFollowsTargetColorCount(t *testing.T) {
	t.Parallel()
	e, cfg, bladeID, multiID := dragonfireGame(t, 612, dragonfireMultiSrc)
	// Precondition: the target really is two-colored, so its CardNumColors
	// read is 2 and the {2} reduction differs from the mono leaf's {1}. A
	// constant head would make this assertion indistinguishable from the mono
	// leaf -- so a one-color fixture must fail here, not pass silently.
	if got := len(e.ObjectColors(e.G.Obj(multiID))); got < 2 {
		t.Fatalf("multi target color count = %d, want >= 2 (the reduction must read the target's real color count)", got)
	}
	// {3} is enough for the printed {4} minus this target's {2} discount.
	addMana(t, e, 0, "CCC")
	opt, ok := findAbilityOption(e, bladeID, 0)
	if !ok {
		t.Fatal("Dragonfire Blade's equip not offered from a {3} pool against a 2-color creature ({4} - {2} = {2})")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, multiID)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(bladeID).AttachedTo != multiID {
		t.Fatalf("Dragonfire Blade attached to %d, want the multicolored %d", e.G.Obj(bladeID).AttachedTo, multiID)
	}
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool after equipping the 2-color creature from {3} = %d, want 1 (exactly {2} charged; {0} would mean a constant {1} discount)", got)
	}
	replayCheck(t, e, cfg)
}

// TestDragonfireBladeEquipWithheldAtThreeAgainstColorless pins the other end of
// the read: a COLORLESS target gives CardNumColors 0, so there is no reduction
// and the equip stays at its printed {4}. From {3} the offer gate (priced at
// {4}) must hold it out, fail closed; with a fourth mana added it is offered
// and charges exactly {4} (pool 0). A constant {1} read would offer it at {3}.
func TestDragonfireBladeEquipWithheldAtThreeAgainstColorless(t *testing.T) {
	t.Parallel()
	e, cfg, bladeID, colorlessID := dragonfireGame(t, 613, dragonfireColorlessSrc)
	// Precondition: the target really is colorless, so its CardNumColors read
	// is 0 and the reduction really is zero. A colored fixture (or a printed
	// {3} equip) must fail here, not pass silently.
	if got := len(e.ObjectColors(e.G.Obj(colorlessID))); got != 0 {
		t.Fatalf("colorless target color count = %d, want 0 (no reduction is the whole point)", got)
	}
	// {3} is one short of the printed {4}: no color means no discount, so the
	// equip is withheld.
	addMana(t, e, 0, "CCC")
	if opt, ok := findAbilityOption(e, bladeID, 0); ok {
		t.Fatalf("Dragonfire Blade's equip offered from {3} against a colorless creature (no reduction; the price is the printed {4}): %+v", opt)
	}
	// Add the fourth mana: the printed {4} is now payable and the charge must
	// be the full {4}.
	addMana(t, e, 0, "C")
	opt, ok := findAbilityOption(e, bladeID, 0)
	if !ok {
		t.Fatal("Dragonfire Blade's equip not offered from a {4} pool (the undiscounted printed cost)")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, colorlessID)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(bladeID).AttachedTo != colorlessID {
		t.Fatalf("Dragonfire Blade attached to %d, want the colorless %d", e.G.Obj(bladeID).AttachedTo, colorlessID)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after equipping the colorless creature from {4} = %d, want 0 (exactly the printed {4} charged)", got)
	}
	replayCheck(t, e, cfg)
}
