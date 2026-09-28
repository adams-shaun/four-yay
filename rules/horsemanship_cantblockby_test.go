package rules

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// horsemanshipCard parses a live corpus card at test time (GPL scripts stay
// out of the repo), so the CantBlockBy pin runs against the real Forge script
// whose ValidBlocker$ spec this ticket's predicate fix serves.
func horsemanshipCard(t *testing.T, name, path string) *cards.Card {
	t.Helper()
	c, ds := cards.Parse(filepath.Join("..", ".cards", "cardsfolder", path))
	if len(ds) != 0 {
		t.Fatalf("parse %s: %v", name, ds)
	}
	if ds = c.Link(); len(ds) != 0 {
		t.Fatalf("link %s: %v", name, ds)
	}
	return c
}

// TestCantBlockByHorsemanshipStaticRestrictsBlocking pins the rules-side
// consumer of the withHorsemanship filter predicate: the CantBlockBy static
// of the real corpus carriers (Taoist Mystic, Zuo Ci, the Mocking Sage --
// identical script lines) must now restrict blocking, where before the
// ValidBlocker$ spec failed closed and the static was inert. The Mystic
// attacks; a horsemanship creature cannot block it, a plain creature can.
func TestCantBlockByHorsemanshipStaticRestrictsBlocking(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	mystic := onBoardCard(t, e, 1, horsemanshipCard(t, "Taoist Mystic", "t/taoist_mystic.txt"))
	e.G.Obj(mystic).IsAttacking, e.G.Obj(mystic).Attacking = true, 0

	hsBlocker := onBoardCard(t, e, 0, horsemanshipCard(t, "Zhang Fei, Fierce Warrior", "z/zhang_fei_fierce_warrior.txt"))
	plainBlocker := onBoard(t, e, 0, "Name:White\nManaCost:W\nTypes:Creature\nPT:1/1\nOracle:x\n")

	if !e.blockRestricted(hsBlocker, mystic) {
		t.Error("a horsemanship creature must be restricted from blocking an attacking Taoist Mystic (CantBlockBy Creature.withHorsemanship)")
	}
	if e.blockRestricted(plainBlocker, mystic) {
		t.Error("a plain creature must not be restricted from blocking an attacking Taoist Mystic")
	}
	if e.blockRestricted(hsBlocker, plainBlocker) {
		t.Error("the static must not restrict blocking when the attacker is not its source (ValidAttacker$ Creature.Self)")
	}
	// End-to-end through canBlock, the read askBlockers and handleBlockers
	// both use: the restriction holds only against the horsemanship attacker.
	e.G.Obj(mystic).IsAttacking, e.G.Obj(mystic).Attacking = true, 0
	e.G.Obj(plainBlocker).IsAttacking, e.G.Obj(plainBlocker).Attacking = true, 0
	if e.canBlock(hsBlocker, mystic) {
		t.Error("a horsemanship creature blocked an attacking Taoist Mystic")
	}
	if !e.canBlock(hsBlocker, plainBlocker) {
		t.Error("a horsemanship creature could not block a plain attacker")
	}
}
