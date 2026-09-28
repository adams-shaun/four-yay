package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// delverFixture loads the REAL corpus Delver of Secrets onto seat 0's
// battlefield (task fb-20260914T033246Z-3f1cc033), puts `top` on top of
// seat 0's library (zone index 0), and drives one upkeep StepChange through
// putTriggersOnStack + resolveTop, which suspends on the RevealOptional$
// ask. Returns the engine, Delver's object id and the pending decision.
func delverFixture(t *testing.T, reg *cards.Registry, top string) (*Engine, state.ObjID, *decision.Decision) {
	t.Helper()
	delver, ok := reg.Lookup("Delver of Secrets")
	if !ok {
		t.Fatal("corpus has no Delver of Secrets")
	}
	e := layerEngine(t)
	o := e.G.AddObject(delver, state.PlayerID(0))
	o.Zone = state.ZBattlefield
	e.G.Clock++
	o.Timestamp = e.G.Clock
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))
	tc, ok := reg.Lookup(top)
	if !ok {
		t.Fatalf("corpus has no %q", top)
	}
	to := e.G.AddObject(tc, state.PlayerID(0))
	to.Zone = state.ZLibrary
	e.G.SetZone(state.ZLibrary, 0, []state.ObjID{to.ID})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want exactly the Delver upkeep trigger", e.G.Stack)
	}
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pending = %+v, want the RevealOptional$ KChoose ask", d)
	}
	if d.ResumeKind != "reveal_optional" {
		t.Fatalf("ResumeKind = %q, want reveal_optional", d.ResumeKind)
	}
	return e, o.ID, d
}

// countFlips counts FlipFace events for id.
func countFlips(e *Engine, id state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.FlipFace && ev.Obj == id {
			n++
		}
	}
	return n
}

// revealNotes returns the non-secret Notes carrying ids (the reveal Notes).
func revealNotes(e *Engine) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && !ev.Secret && len(ev.IDs) > 0 {
			out = append(out, ev)
		}
	}
	return out
}

// TestDelverWithALandOnTopAndARevealDeclinedDoesNotTransform is the
// user-reported defect: the trigger used to resolve with no ask at all and
// transform unconditionally. Now the peek asks, "no" reveals nothing, the
// ConditionDefined$ Remembered gate finds nothing, and Delver stays a 1/1.
func TestDelverWithALandOnTopAndARevealDeclinedDoesNotTransform(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, id, _ := delverFixture(t, reg, "Mountain")
	submitChoices(t, e, 1) // "no"
	if got := e.G.Obj(id).FaceIdx; got != 0 {
		t.Fatalf("FaceIdx = %d, want 0 (front) after a declined reveal over a land", got)
	}
	if n := countFlips(e, id); n != 0 {
		t.Fatalf("%d FlipFace events after a declined reveal", n)
	}
	if notes := revealNotes(e); len(notes) != 0 {
		t.Fatalf("a declined reveal left reveal Notes: %+v", notes)
	}
}

// TestDelverRevealAskCarriesTheTopCardOnlyToThePeekingSeat is round-2
// finding 1's pin: the may-reveal decision must tell the deciding seat WHAT
// it is deciding over (the library is not projected to that seat, and the
// public Note exists only after "yes"), and that payload must reach no SEAT
// other than the peeking one — view.project attaches a decision to no viewer
// whose seat is not Decision.Player. (Since the spectator-decision contract,
// approx row 33, an Omniscient spectator additionally carries a read-only
// copy of the pending ask; it is not a seat.)
func TestDelverRevealAskCarriesTheTopCardOnlyToThePeekingSeat(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, d := delverFixture(t, reg, "Mountain")
	top := e.G.Zone(state.ZLibrary, 0)[0]

	own := view.Project(e.G, e, 0, d)
	if own.Decision == nil {
		t.Fatal("the peeking seat's view carries no decision")
	}
	if got := own.Decision.Options[0].Label; got != "Yes — reveal Mountain" {
		t.Fatalf("yes label = %q, want it to name the card being revealed", got)
	}
	if got := own.Decision.Options[0].Obj; got != top {
		t.Fatalf("yes option Obj = %d, want the top card %d", got, top)
	}
	if !strings.Contains(own.Decision.Prompt, "Mountain") {
		t.Fatalf("prompt = %q, want it to name the card", own.Decision.Prompt)
	}

	for _, other := range []struct {
		viewer state.PlayerID
		what   string
	}{{1, "the opponent's view"}} {
		if v := view.Project(e.G, e, other.viewer, d); v.Decision != nil {
			t.Fatalf("%s carries the reveal ask — the payload must reach the peeking seat alone", other.what)
		}
	}
	if v := view.ProjectFor(e.G, e, view.NoSeat, view.Omniscient, d); v.Decision == nil {
		t.Fatal("an Omniscient spectator's view lost the reveal ask")
	}
}

// TestDelverWithALandOnTopAndARevealAnsweredYesDoesNotTransform pins the
// gate's other half: revealing a LAND answers "yes" — the Note goes out —
// but ConditionCompare$ EQ1 over Card.Instant,Card.Sorcery finds zero, so
// Delver does not transform (the pre-fix build transformed every upkeep
// regardless).
func TestDelverWithALandOnTopAndARevealAnsweredYesDoesNotTransform(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, id, d := delverFixture(t, reg, "Mountain")
	if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("options = %+v, want yes then no", d.Options)
	}
	submitChoices(t, e, 0) // "yes"
	if got := e.G.Obj(id).FaceIdx; got != 0 {
		t.Fatalf("FaceIdx = %d, want 0: a revealed land must not transform Delver", got)
	}
	if n := countFlips(e, id); n != 0 {
		t.Fatalf("%d FlipFace events after revealing a land", n)
	}
	notes := revealNotes(e)
	if len(notes) != 1 || len(notes[0].IDs) != 1 {
		t.Fatalf("reveal Notes = %+v, want exactly one naming the land", notes)
	}
}

// TestDelverWithAnInstantOnTopAndARevealAnsweredYesTransformsExactlyOnce is
// the card's real behaviour: an instant or sorcery revealed (and the
// reveal answered yes) transforms Delver into Insectile Aberration exactly
// once.
func TestDelverWithAnInstantOnTopAndARevealAnsweredYesTransformsExactlyOnce(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, id, _ := delverFixture(t, reg, "Lightning Bolt")
	submitChoices(t, e, 0) // "yes"
	if got := e.G.Obj(id).FaceIdx; got != 1 {
		t.Fatalf("FaceIdx = %d, want 1 (Insectile Aberration) after revealing an instant", got)
	}
	if n := countFlips(e, id); n != 1 {
		t.Fatalf("%d FlipFace events, want exactly 1", n)
	}
	notes := revealNotes(e)
	if len(notes) != 1 || len(notes[0].IDs) != 1 {
		t.Fatalf("reveal Notes = %+v, want exactly one naming the revealed instant", notes)
	}
}

// TestDelverRevealAskIsDeterministicOnRepeatUpkeeps drives two upkeeps: the
// first reveal resolves the trigger; the second upkeep poses the ask again
// (the trigger is a every-upkeep Phase trigger) — and no re-ask of the
// already-answered reveal leaks into it (the fx42 scoping of Ctx.RevealOpt).
func TestDelverRevealAskIsDeterministicOnRepeatUpkeeps(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, id, _ := delverFixture(t, reg, "Mountain")
	submitChoices(t, e, 0) // yes, land — no transform
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("second upkeep: stack = %v, want the trigger again", e.G.Stack)
	}
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.ResumeKind != "reveal_optional" {
		t.Fatalf("second upkeep pending = %+v, want a fresh reveal_optional ask", d)
	}
	submitChoices(t, e, 1) // decline this time
	if got := e.G.Obj(id).FaceIdx; got != 0 {
		t.Fatalf("FaceIdx = %d after two non-spell reveals, want 0", got)
	}
}
