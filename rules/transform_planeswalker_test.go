package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestTransformEntryPlaneswalkerGetsBackFaceLoyalty pins CR 306.5b on the
// CR 711.10a transformed-entry path: when an effect returns a double-faced
// card to the battlefield transformed and the back face is a planeswalker,
// the permanent enters with loyalty counters equal to THAT face's printed
// loyalty. Liliana, Heretical Healer (a creature) exiles herself and returns
// transformed as Liliana, Defiant Necromancer (loyalty 3) whenever another
// nontoken creature you control dies.
//
// The bug this pins: the Transformed$ True flip used to be folded AFTER the
// entry MoveZone, so events.Apply's Move read the FRONT face (a creature),
// granted no loyalty, and rules/sba.go's CR 704.5i sweep then put the
// 0-loyalty walker into the graveyard. Flipping before the Move -- the order
// the modal-land play path already uses (rules/legal.go) -- makes Move grant
// the back face's loyalty.
func TestTransformEntryPlaneswalkerGetsBackFaceLoyalty(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	liliana := mustCorpusCard(t, reg, "Liliana, Heretical Healer")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")

	// Card-data precondition: the front face is a creature that is NOT a
	// planeswalker, and the back face is a planeswalker with printed loyalty
	// 3. If either drifted the assertion below would be reading a different
	// rule, so fail loudly rather than assert a coincidence.
	front, back := liliana.Faces[0], liliana.Faces[1]
	if front == nil || !front.IsCreature() || front.IsPlaneswalker() {
		t.Fatalf("front face is not a loyalty-less creature: %+v", front)
	}
	if back == nil || !back.IsPlaneswalker() || strings.TrimSpace(back.Loyalty) != "3" {
		t.Fatalf("back face is not a 3-loyalty planeswalker: %+v", back)
	}
	if liliana.AlternateMode != "DoubleFaced" || len(liliana.Faces) != 2 {
		t.Fatalf("fixture is not a transforming double-faced card: mode=%q faces=%d",
			liliana.AlternateMode, len(liliana.Faces))
	}

	e, cfg := censusEngine(t, 606, []*cards.Card{liliana, bear}, nil)
	lid := moveOwnerCard(t, e, 0, liliana, state.ZBattlefield)
	bid := moveOwnerCard(t, e, 0, bear, state.ZBattlefield)
	e.pending = nil
	e.priorityRound()

	// Board precondition: she is on the battlefield on her creature face and
	// carries no loyalty counter yet -- the value the assertion below must
	// actually change.
	o := e.G.Obj(lid)
	if o == nil || o.Zone != state.ZBattlefield || o.FaceIdx != 0 || !o.Face().IsCreature() {
		t.Fatalf("before the death: %+v", o)
	}
	if got := o.Counter("LOYALTY"); got != 0 {
		t.Fatalf("precondition: front face already has %d loyalty", got)
	}

	// The other nontoken creature dies, firing Liliana's transform trigger.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bid, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)

	o = e.G.Obj(lid)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("transformed Liliana is not on the battlefield (a 0-loyalty walker is swept by CR 704.5i): %+v", o)
	}
	if o.FaceIdx != 1 || !o.Face().IsPlaneswalker() {
		t.Fatalf("after the transform: face=%d walker=%t", o.FaceIdx, o.Face().IsPlaneswalker())
	}
	if got := o.Counter("LOYALTY"); got != 3 {
		t.Fatalf("entered-transformed walker loyalty = %d, want 3 (CR 306.5b on the CR 711.10a entry)", got)
	}
	replayCheck(t, e, cfg)
}
