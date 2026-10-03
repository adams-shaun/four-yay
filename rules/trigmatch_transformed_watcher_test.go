package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// transformedTriggerFor returns the first Transformed trigger on the named
// corpus object's current face, or fails. Reading the real T: line keeps the
// watcher tests pinned to the corpus shape rather than a fabricated Trigger.
func transformedTriggerFor(t *testing.T, e *Engine, id state.ObjID) cards.Trigger {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("transformedTriggerFor: object %d missing or faceless: %+v", id, o)
	}
	for _, tr := range o.Face().Triggers {
		if tr.Mode == "Transformed" {
			return tr
		}
	}
	t.Fatalf("object %d face %q carries no Mode$ Transformed trigger", id, o.Face().Name)
	return cards.Trigger{}
}

// TestTransformedWatcherMatchesAnotherPermanent is the finding's exact probe:
// Cult of the Waxing Moon's real "Whenever a permanent you control transforms
// into a non-Human creature" line must admit ANOTHER permanent's transform
// event. Before the fix the matcher required ev.Obj == source, so only Card.Self
// bodies could ever fire and the watcher family (Cult of the Waxing Moon,
// Corruption of Towashi, Norn's Inquisitor, Neglected Heirloom) stayed dead.
func TestTransformedWatcherMatchesAnotherPermanent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	cult := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Cult of the Waxing Moon"))
	brigid := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Brigid, Clachan's Heart"))
	if o := e.G.Obj(cult); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Cult of the Waxing Moon must be seat 0's battlefield permanent: %+v", o)
	}
	if o := e.G.Obj(brigid); o == nil || o.Zone != state.ZBattlefield || o.FaceIdx != 0 {
		t.Fatalf("precondition: Brigid must be front-face on the battlefield: %+v", o)
	}
	tr := transformedTriggerFor(t, e, cult)
	if tr.Params["ValidCard"] == "Card.Self" {
		t.Fatalf("precondition: Cult's Transformed line is not a watcher: %+v", tr.Params)
	}

	// The real marker for a transform INTO Brigid's back (Kithkin, non-Human)
	// face: the events.FlipFace the SetState Mode$ Transform primitive emits.
	marker := events.Event{Kind: events.FlipFace, Obj: brigid, Amount: 1, Text: "Transformed"}
	if !trigmatch.TransformedMatches(boardOf(e), tr, cult, marker, nil) {
		t.Fatal("Cult of the Waxing Moon did not match another permanent's transform")
	}

	// A self-source Body (Brigid's own Card.Self line) must still match its own
	// transform, so the watcher fix did not lose the self case.
	selfTr := transformedTriggerFor(t, e, brigid)
	if !trigmatch.TransformedMatches(boardOf(e), selfTr, brigid, marker, nil) {
		t.Fatal("Brigid's Card.Self Transformed trigger did not match its own transform")
	}
	// ...and the self trigger must NOT fire on an unrelated permanent's
	// transform: Card.Self is what scopes it.
	if trigmatch.TransformedMatches(boardOf(e), selfTr, cult, marker, nil) {
		t.Fatal("Card.Self Transformed trigger matched a different permanent")
	}

	// Negative precondition: without the marker this is not a transform, and
	// the watcher must not match a bare face change (Flip, alternate-face cast).
	if trigmatch.TransformedMatches(boardOf(e), tr, cult, events.Event{Kind: events.FlipFace, Obj: brigid, Amount: 1}, nil) {
		t.Fatal("watcher matched a FlipFace with no Transformed marker")
	}
}

// TestTransformedWatcherQueuesOnAnotherPermanentsTransform drives the real
// trigger-queue path end to end: Cult of the Waxing Moon watching a seat-0
// Brigid transform into its non-Human back face queues exactly one trigger
// whose source is the watcher.
func TestTransformedWatcherQueuesOnAnotherPermanentsTransform(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	cult := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Cult of the Waxing Moon"))
	brigid := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Brigid, Clachan's Heart"))
	if o := e.G.Obj(brigid); o == nil || o.FaceIdx != 0 || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Brigid must start front-face on the battlefield: %+v", o)
	}
	// Drop any state-based or entry bookkeeping before the transform so the
	// queue count below is attributable to the transform alone.
	e.pendingTriggers = nil

	o := e.G.Obj(brigid)
	sa := cards.ResolveSVar(o.Face().SVars, "TrigTransform")
	if sa == nil || sa.Params["Mode"] != "Transform" {
		t.Fatalf("precondition: Brigid front face TrigTransform = %+v", sa)
	}
	e.resolveAbility(brigid, 0, nil, sa, o.Face().SVars)
	after := e.G.Obj(brigid)
	if after == nil || after.FaceIdx != 1 {
		t.Fatalf("precondition: Brigid did not transform to its back face: %+v", after)
	}
	if after.Zone != state.ZBattlefield {
		t.Fatal("precondition: transformed Brigid left the battlefield")
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("Cult of the Waxing Moon queued %d triggers on another permanent's transform, want 1", len(e.pendingTriggers))
	}
	if got := e.pendingTriggers[0].Source; got != cult {
		t.Fatalf("queued trigger source = %d, want the watcher %d", got, cult)
	}
}
