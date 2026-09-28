// Mode$ Foretell (task agent-20260923T032009Z-3b9d3432): Dream Devourer's
// "Whenever you foretell a card, CARDNAME gets +2/+0 until end of turn"
// (CR 702.126b), the corpus's sole carrier of the mode. Two provenance
// shapes must fire it -- the {2} Foretell special action's pay-time
// FlagForetold CastInfo (card still in hand) and an effect's Foretold$ True
// designation (the exile MoveZone whose counter carries the designation) --
// and one shape must NOT: the later foretell-cost cast from exile, which
// stamps the same flag on a CastInfo emitted after the card has moved to the
// stack. Pinned on the real corpus carriers Dream Devourer and Ethereal
// Valkyrie (the designation's carrier), with the same harness the foretell
// action tests use.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// dreamDevourerID returns seat 0's Dream Devourer on the battlefield, the
// trigger source every assertion below reads.
func dreamDevourerID(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Dream Devourer" {
			return id
		}
	}
	t.Fatal("precondition: Dream Devourer is not on seat 0's battlefield")
	return 0
}

func TestDreamDevourerForetellTriggerPumpsUntilEndOfTurn(t *testing.T) {
	e := handEngine(t, card(t, grantedForetellBear))
	dreamDevourerOut(t, e)
	dd := dreamDevourerID(t, e)
	bear := e.G.Zone(state.ZHand, 0)[0]
	// Preconditions the assertion rests on: Dream Devourer is a 0/3 on the
	// battlefield (so the granted +2/+0 has a different value to move to),
	// and the hand card that will be foretelled is there.
	if p, tp, _ := e.Characteristics(dd); p != 0 || tp != 3 {
		t.Fatalf("precondition: Dream Devourer is %d/%d, want 0/3", p, tp)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: bear not in seat 0's hand: %+v", o)
	}
	e.G.Players[0].Pool[state.MC] = 2
	foretellIt(t, e, bear)
	e.priorityRound()
	answerQuiet(t, e, 60)
	if p, tp, _ := e.Characteristics(dd); p != 2 || tp != 3 {
		t.Fatalf("foretelling did not grant Dream Devourer +2/+0: got %d/%d, want 2/3", p, tp)
	}
	// "until end of turn": the grant is gone once the turn it was granted in
	// has ended. driveToLaterTurnMain answers the combat asks Dream Devourer
	// (a creature on the battlefield) poses on the way with empty
	// declarations -- a real, expected ask.
	driveToLaterTurnMain(t, e)
	if p, _, _ := e.Characteristics(dd); p != 0 {
		t.Fatalf("the +2/+0 outlived the turn: power %d", p)
	}
	// Casting the foretold card for its foretell cost is NOT foretelling
	// (CR 702.126a: the special action is the exile): the same FlagForetold
	// provenance rides the later cast's CastInfo, and the trigger must stay
	// silent on it.
	e.G.Players[0].Pool[state.MC] = 0
	e.G.Players[0].Pool[state.MG] = 1
	submitOption(t, e, "foretell_cast", "Cast Grizzly Fast (foretold)")
	finishCast(t, e, bear)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("foretold cast did not put the bear on the battlefield: %+v", o)
	}
	if p, _, _ := e.Characteristics(dd); p != 0 {
		t.Fatalf("the later foretold CAST fired the foretell trigger: power %d, want 0", p)
	}
}

func TestDreamDevourerForetellTriggerFiresOnEffectDesignation(t *testing.T) {
	// Ethereal Valkyrie's ETB exiles a hand card face down with Foretold$
	// True: the effect-designation MoveZone shape (counter
	// exiled_with_face_down_foretold), which emits no CastInfo at all.
	e := handEngine(t, corpusAlternativeCard(t, "Ethereal Valkyrie"), card(t, grantedForetellBear))
	dreamDevourerOut(t, e)
	dd := dreamDevourerID(t, e)
	hand := e.G.Zone(state.ZHand, 0)
	vid, mid := hand[0], hand[1]
	if _, ok := e.G.Obj(mid).Face().KeywordParam("Foretell"); ok {
		t.Fatal("precondition: the bear must carry no printed Foretell of its own")
	}
	if p, _, _ := e.Characteristics(dd); p != 0 {
		t.Fatalf("precondition: Dream Devourer power %d, want 0", p)
	}
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MU], e.G.Players[0].Pool[state.MW] = 4, 1, 1
	castMode(t, e, vid, "")
	finishCast(t, e, vid)
	if v := e.G.Obj(vid); v.Zone != state.ZBattlefield {
		t.Fatalf("valkyrie in %s, want battlefield", v.Zone)
	}
	e.priorityRound()
	answerQuiet(t, e, 60)
	// The effect designated the bear foretold: the precondition the trigger
	// assertion reads (the card really was moved by the designation).
	if mo := e.G.Obj(mid); mo.Zone != state.ZExile || !mo.FaceDown || mo.CastFlags&state.FlagForetold == 0 {
		t.Fatalf("bear not designated foretold: zone=%s faceDown=%v flags=%#x",
			mo.Zone, mo.FaceDown, mo.CastFlags)
	}
	// The designation fired Dream Devourer's trigger: +2/+0 through end of
	// turn.
	if p, tp, _ := e.Characteristics(dd); p != 2 || tp != 3 {
		t.Fatalf("effect-designated foretelling did not grant +2/+0: got %d/%d, want 2/3", p, tp)
	}
}
