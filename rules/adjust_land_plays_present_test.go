package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The IsPresent$ gate on an AdjustLandPlays grant (ticket
// agent-20261009T055739Z-b43f3480): Thranduil's Company's "As long as you
// control another Elf, you may play an additional land on each of your
// turns" is an intervening-if, not a fail-closed rider. The staticEffects
// walk evaluates the gate through continuousGateHolds before the grant is
// built, so the second drop exists exactly while another Elf is on the
// battlefield.

// elfSrc is the "another Elf" the IsPresent$ Elf.YouCtrl+Other gate reads.
const elfSrc = "Name:Test Elf\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n"

// TestAdjustLandPlaysIsPresentGateHoldsWithAnotherElf pins the positive
// direction: with the source AND a second Elf on the battlefield the grant
// applies (two drops, the third not offered), and when the second Elf
// leaves the intervening-if re-evaluates on the next rescan and the extra
// drop is gone. The precondition (both Elves on the battlefield, the source
// distinct from the probe) is asserted, so the test cannot pass on a board
// the gate never sees.
func TestAdjustLandPlaysIsPresentGateHoldsWithAnotherElf(t *testing.T) {
	t.Parallel()
	e := landBase(t)
	src := onBoardGrant(t, e, 0, azusaIsPresentSrc)
	elf := onBoardGrant(t, e, 0, elfSrc)
	if e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(elf).Zone != state.ZBattlefield || src == elf {
		t.Fatalf("precondition: want source and a distinct second Elf on the battlefield (src=%d elf=%d)", src, elf)
	}
	first := handCard(e, card(t, landSrc("Mountain")), 0)
	second := handCard(e, card(t, landSrc("Forest")), 0)
	third := handCard(e, card(t, landSrc("Plains")), 0)
	e.priorityRound()
	if !playOneLand(t, e, 0, first) {
		t.Fatalf("ordinary drop not offered")
	}
	if n := countPlayLand(e, second); n != 1 {
		t.Fatalf("want the second hand land offered while another Elf is on the battlefield, got %d", n)
	}
	if !playOneLand(t, e, 0, second) {
		t.Fatalf("second drop not offered")
	}
	if n := countPlayLand(e, third); n != 0 {
		t.Fatalf("want the third drop not offered beyond the +1 grant, got %d", n)
	}
	// The Elf leaves: the grant's intervening-if fails and the extra drop
	// disappears on the next static rescan.
	e.emit(events.Event{Kind: events.MoveZone, Obj: elf, From: state.ZBattlefield, To: state.ZGraveyard})
	if n := countPlayLand(e, third); n != 0 {
		t.Fatalf("want no extra drop after the other Elf left the battlefield, got %d", n)
	}
}

// TestAdjustLandPlaysIsPresentGateNeedsAnotherElf pins the negative
// direction on the same board shape: the source alone (it is an Elf, but
// Other excludes it) grants nothing, so the second drop is not offered.
func TestAdjustLandPlaysIsPresentGateNeedsAnotherElf(t *testing.T) {
	t.Parallel()
	e := landBase(t)
	src := onBoardGrant(t, e, 0, azusaIsPresentSrc)
	if e.G.Obj(src).Zone != state.ZBattlefield {
		t.Fatalf("precondition: source not on the battlefield")
	}
	first := handCard(e, card(t, landSrc("Mountain")), 0)
	second := handCard(e, card(t, landSrc("Forest")), 0)
	e.priorityRound()
	if !playOneLand(t, e, 0, first) {
		t.Fatalf("ordinary drop not offered")
	}
	if n := countPlayLand(e, second); n != 0 {
		t.Fatalf("want no extra drop while the source is the only Elf, got %d", n)
	}
}
