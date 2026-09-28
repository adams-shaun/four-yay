package rules

// compound-statics1: the compound `S:Mode$ CantAttack,CantBlock` split landed
// as commit f81f996e (cards/parse.go splitStaticModes), so each mode reaches
// the runtime as its OWN static sharing one Params map. That made a
// condition-bearing compound line reachable to attackBlocked: Bast, Panther
// Goddess prints
//
//	S:Mode$ CantAttack,CantBlock | ValidCard$ Card.Self |
//	    IsPresent$ Creature.YouCtrl | PresentCompare$ LE2
//
// The block half already bound at runtime (blockRestricted's CantBlock loop
// runs continuousGateHolds with no whitelist); the attack half was skipped
// WHOLE because CantAttackParamsReadableForRules (effects/misc.go) never
// admitted IsPresent$/IsPresent2$/PresentCompare$. This test is the
// end-to-end pin on the real corpus card: both halves restrict at two
// creatures and release at three.
//
// Every fixture mutation goes through e.emit, so the test ends replay-verified.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestBastCompoundModeStaticGates(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bast, ok := reg.Lookup("Bast, Panther Goddess")
	if !ok {
		t.Fatal("corpus missing Bast, Panther Goddess")
	}
	// Precondition: the corpus card still prints the compound shape this
	// ticket splits -- both halves present after the parse-time split, and
	// the CantAttack half carries the present gate this ticket makes readable.
	var hasAttack, hasBlock, attackHasGate bool
	for _, f := range bast.Faces {
		for _, st := range f.Statics {
			switch st.Mode {
			case "CantAttack":
				hasAttack = true
				_, hasPresent := st.Params["IsPresent"]
				_, hasCompare := st.Params["PresentCompare"]
				attackHasGate = hasPresent && hasCompare
			case "CantBlock":
				hasBlock = true
			}
		}
	}
	if !hasAttack || !hasBlock {
		t.Fatalf("corpus Bast lacks the split CantAttack/CantBlock statics (hasAttack=%v hasBlock=%v): %+v",
			hasAttack, hasBlock, bast.Faces[0].Statics)
	}
	if !attackHasGate {
		t.Fatalf("corpus Bast's CantAttack half carries no IsPresent$/PresentCompare$ gate: %+v",
			bast.Faces[0].Statics)
	}

	// Seat 1 controls Bast + one bear (two creatures); a third bear waits in
	// hand to raise the count to three. Distinct card() pointers so
	// restrictionGame's pointer-matched board move never confuses the hand
	// copy with a board one.
	filler := card(t, staticBearFixture)
	bear := card(t, staticBearFixture)  // the third creature, dealt to hand
	other := card(t, staticBearFixture) // seat 2's lone creature, for a defender pair
	e, cfg := restrictionGame(t, 6201,
		[][]*cards.Card{{}, {bear}, {}},
		[][]*cards.Card{{}, {bast, filler}, {other}})
	bastID := bearOnBoard(t, e, 1, bast)
	bearBoard := bearOnBoard(t, e, 1, filler)
	otherID := bearOnBoard(t, e, 2, other)

	// Precondition: exactly two creatures on seat 1's battlefield (Bast + the
	// filler bear), and the two values under test differ from the three-creature
	// state we move to below.
	if n := len(e.G.Zone(state.ZBattlefield, 1)); n != 2 {
		t.Fatalf("seat 1 has %d battlefield objects, want exactly 2 (Bast + bear)", n)
	}
	if !e.G.Obj(bastID).EffectiveIsCreature() {
		t.Fatal("precondition: Bast is not a creature on the battlefield")
	}

	// Two creatures: BOTH halves bind on Bast; the plain bear is unrestricted
	// in both directions (Card.Self scoping precondition).
	if !e.attackBlocked(bastID, 2, 0) {
		t.Fatal("Bast NOT attack-restricted with 2 creatures: the CantAttack half's IsPresent gate never bound")
	}
	if !e.blockRestricted(bastID, otherID) {
		t.Fatal("Bast NOT block-restricted with 2 creatures")
	}
	if e.attackBlocked(bearBoard, 2, 0) || e.blockRestricted(bearBoard, otherID) {
		t.Fatal("the plain bear (precondition) is restricted — the fixture does not isolate Bast")
	}

	// Third creature arrives (emitted MoveZone, replay-verified).
	thirdID := findAndMoveToHand(t, e, 1, "Grizzly Bears")
	e.emit(events.Event{Kind: events.MoveZone, Obj: thirdID, From: state.ZHand, To: state.ZBattlefield})
	if n := len(e.G.Zone(state.ZBattlefield, 1)); n != 3 {
		t.Fatalf("seat 1 has %d battlefield objects after the move, want 3", n)
	}

	// Three creatures: both halves release.
	if e.attackBlocked(bastID, 2, 0) {
		t.Fatal("Bast still attack-restricted with 3 creatures: PresentCompare LE2 did not release")
	}
	if e.blockRestricted(bastID, otherID) {
		t.Fatal("Bast still block-restricted with 3 creatures: PresentCompare LE2 did not release")
	}
	if e.attackBlocked(bearBoard, 2, 0) || e.blockRestricted(bearBoard, otherID) {
		t.Fatal("the plain bear (Card.Self control) became restricted after the third creature arrived")
	}
	replayCheck(t, e, cfg)
}
