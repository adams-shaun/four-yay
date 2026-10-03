package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// setStateDagger drives the real Dowsing Dagger damage trigger to its
// Optional$ Transform election. The source is a two-faced battlefield card;
// the effect must ask before changing it.
func setStateDagger(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := gateFixture(t, seed, "Dowsing Dagger")
	if o := e.G.Obj(id); o == nil || o.Card == nil || len(o.Card.Faces) != 2 || o.FaceIdx != 0 {
		t.Fatalf("precondition: expected front face of a two-faced Dowsing Dagger, got %+v", o)
	}
	id = gateMoveFromLibrary(t, e, "Dowsing Dagger", state.ZBattlefield)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Dagger zone = %v, want battlefield", o.Zone)
	}
	if f := e.G.Obj(id).Face(); f == nil || len(f.Triggers) < 2 || f.Triggers[1].Effect == nil || f.Triggers[1].Effect.API != "SetState" || f.Triggers[1].Effect.Params["Optional"] != "True" {
		t.Fatalf("precondition: Dagger's second trigger must be Optional$ SetState, got %+v", f)
	}
	e.emit(events.Event{Kind: events.TriggerPush, Obj: id, Player: 0, Amount: 1})
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "setstate_optional" || d.Min != 1 || d.Max != 1 {
		t.Fatalf("pending = %+v, want SetState optional election", d)
	}
	if d.Player != 0 || len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("election = %+v, want yes/no for seat 0", d)
	}
	if e.G.Obj(id).FaceIdx != 0 || hasEvent(e, events.FlipFace, id) {
		t.Fatal("Dagger transformed before the election")
	}
	return e, cfg, id
}
