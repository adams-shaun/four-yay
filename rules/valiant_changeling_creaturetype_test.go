package rules

// Task diffcount2 end-to-end pin on the real corpus carrier: Valiant
// Changeling carries
//   S:Mode$ ReduceCost | ValidCard$ Card.Self | Type$ Spell | Amount$ X |
//     EffectZone$ All | Description$ This spell costs {1} less to cast for
//     each creature type among creatures you control. This effect can't
//     reduce the amount of mana this spell costs by more than {5}.
//   SVar:X:Count$Valid Creature.YouCtrl$CreatureType/LimitMax.5
// Before the CreatureType spelling existed the SVar read 0 (whole-token
// fail-closed), so Valiant Changeling always cost full price.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// vcTypeCreature builds a one-type creature of the given subtype.
func vcTypeCreature(subtype string) string {
	return "Name:VC " + subtype + "\nManaCost:1 G\nTypes:Creature " + subtype + "\nPT:1/1\nOracle:x\n"
}

// valiantChangelingGame builds a 2-seat game whose protagonist seat 0 holds
// the real corpus Valiant Changeling in hand and the given creature sources
// on its battlefield, driven to turn 3's Main1 so seat 0 can cast.
func valiantChangelingGame(t *testing.T, seed uint64, creatures []string) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	vc, ok := reg.Lookup("Valiant Changeling")
	if !ok {
		t.Fatal("Valiant Changeling not found in the compiled corpus registry")
	}
	extras := make([]*cards.Card, 0, len(creatures))
	for _, src := range creatures {
		extras = append(extras, card(t, src))
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(append([]*cards.Card{vc}, extras...), mountainDeck(t, 35)...),
			mountainDeck(t, 40),
		},
		Tokens: reg.Tokens,
	})
	e := New(cfg)
	e.Advance()
	vcID := findAndMoveToHand(t, e, 0, "Valiant Changeling")
	// The creatures move to the battlefield EVENT-BACKED (moveSeeded), not
	// through the eventless onBoard helper: the replayCheck call in
	// TestValiantChangelingCastsForTheCappedDiscount folds the recorded log,
	// and an eventless placement would diverge from its own log.
	for _, src := range creatures {
		moveSeeded(t, e, 0, src, state.ZBattlefield)
	}
	e.pending = nil
	e.Advance()
	driveToStep(t, e, 3, 0, state.StepMain1)
	return e, cfg, vcID
}

// TestValiantChangelingReducesPerCreatureTypeCapped pins both halves of the
// real card: seven distinct creature types on the battlefield read 7 but the
// /LimitMax.5 clamp holds the reduction at {5}, and three distinct types read
// 3 uncapped.
func TestValiantChangelingReducesPerCreatureTypeCapped(t *testing.T) {
	t.Parallel()
	seven := []string{
		vcTypeCreature("Bear"), vcTypeCreature("Elf"), vcTypeCreature("Goblin"),
		vcTypeCreature("Cat"), vcTypeCreature("Bird"), vcTypeCreature("Dragon"),
		vcTypeCreature("Zombie"),
	}
	e, _, vcID := valiantChangelingGame(t, 821, seven)
	// PRECONDITION: the seven creatures really are on the battlefield with
	// seven distinct subtypes; without them a zero reduction would be read
	// for the wrong reason.
	if got := len(e.G.Zone(state.ZBattlefield, 0)); got != 7 {
		t.Fatalf("seat 0 battlefield holds %d permanents, want 7", got)
	}
	if got := reduceOf(t, e, 0, vcID); got != 5 {
		t.Fatalf("Valiant Changeling reduction with 7 creature types = %d, want 5 (the LimitMax cap)", got)
	}

	three := []string{
		vcTypeCreature("Bear"), vcTypeCreature("Elf"), vcTypeCreature("Goblin"),
	}
	e2, _, vc2 := valiantChangelingGame(t, 822, three)
	if got := len(e2.G.Zone(state.ZBattlefield, 0)); got != 3 {
		t.Fatalf("seat 0 battlefield holds %d permanents, want 3", got)
	}
	if got := reduceOf(t, e2, 0, vc2); got != 3 {
		t.Fatalf("Valiant Changeling reduction with 3 creature types = %d, want 3 (uncapped)", got)
	}
}

// TestValiantChangelingCastsForTheCappedDiscount pins the discount as real
// money: with seven creature types out the capped {5} reduction makes the
// {5}{W}{W} spell castable for {W}{W} alone, and the pool empties.
func TestValiantChangelingCastsForTheCappedDiscount(t *testing.T) {
	t.Parallel()
	seven := []string{
		vcTypeCreature("Bear"), vcTypeCreature("Elf"), vcTypeCreature("Goblin"),
		vcTypeCreature("Cat"), vcTypeCreature("Bird"), vcTypeCreature("Dragon"),
		vcTypeCreature("Zombie"),
	}
	e, cfg, _ := valiantChangelingGame(t, 823, seven)
	addMana(t, e, 0, "WW")
	opt := castByName(t, e, 0, "Valiant Changeling")
	if opt == nil {
		t.Fatal("Valiant Changeling must be castable for {W}{W} with the capped {5} reduction")
	}
	submitChoices(t, e, opt.Index)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after casting Valiant Changeling = %d, want 0 ({W}{W} paid)", got)
	}
	replayCheck(t, e, cfg)
}
