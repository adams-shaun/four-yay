package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestGalianBeastReturnsFrontFace pins CR 712.8a / 712.14: Vincent Valentine
// transformed into Galian Beast dies, its back-face trigger returns it to the
// battlefield tapped, and it comes back FRONT face up (Vincent Valentine 2/2),
// not as Galian Beast again.
func TestGalianBeastReturnsFrontFace(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	vincent := mustCorpusCard(t, reg, "Vincent Valentine")
	if vincent.AlternateMode != "DoubleFaced" || len(vincent.Faces) != 2 ||
		vincent.Faces[0].Name != "Vincent Valentine" || vincent.Faces[1].Name != "Galian Beast" {
		t.Fatalf("fixture is not the Vincent/Galian DFC: mode=%q faces=%d", vincent.AlternateMode, len(vincent.Faces))
	}

	e, cfg := censusEngine(t, 712, []*cards.Card{vincent}, nil)
	vid := moveOwnerCard(t, e, 0, vincent, state.ZBattlefield)
	e.emit(events.Event{Kind: events.FlipFace, Obj: vid, Amount: 1})
	e.pending = nil
	e.priorityRound()
	o := e.G.Obj(vid)
	if o == nil || o.Zone != state.ZBattlefield || o.FaceIdx != 1 || o.Face().Name != "Galian Beast" {
		t.Fatalf("precondition: want Galian Beast on the battlefield, got %+v", o)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: vid, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)

	o = e.G.Obj(vid)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Galian Beast's death trigger did not return it to the battlefield: %+v", o)
	}
	if !o.Tapped {
		t.Fatalf("returned permanent should be tapped")
	}
	if o.FaceIdx != 0 || o.Face().Name != "Vincent Valentine" {
		t.Fatalf("returned as face %d %q, want front face Vincent Valentine", o.FaceIdx, o.Face().Name)
	}
	replayCheck(t, e, cfg)
}
