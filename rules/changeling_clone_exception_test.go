package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 707.9b: Omni-Changeling's "except it has changeling" is part of the
// copiable values, so the copy is every creature type (layer 4) -- and a
// later creature-type strip still overrides it (CR 613.7).
func TestCloneChangelingExceptionIsAllCreatureTypesUntilStripped(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Omni-Changeling"))
	bear := e.G.AddObject(corpusAlternativeCard(t, "Grizzly Bears"), 0)
	bear.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{bear.ID})
	id := e.G.Zone(state.ZHand, 0)[0]
	if bear.Zone != state.ZBattlefield || e.G.Obj(id).Face().Name == bear.Face().Name {
		t.Fatal("precondition: distinct Omni-Changeling in hand and Grizzly Bears on the battlefield")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" {
		t.Fatalf("Omni-Changeling copy election = %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == bear.ID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Grizzly Bears absent from election: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if !hasEvent(e, events.ClonePermanent, id) {
		t.Fatal("copy was elected but the Clone handler never ran")
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Face().Name != bear.Face().Name {
		t.Fatalf("Omni-Changeling entry = %+v, want a Grizzly Bears copy", o)
	}
	if !e.HasKeyword(id, "Changeling") || !e.Derived(id).AllCreatureTypes {
		t.Fatalf("copy with changeling exception: keyword %v, all creature types %v; want both",
			e.HasKeyword(id, "Changeling"), e.Derived(id).AllCreatureTypes)
	}
	e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LType, RemoveCreatureTypes: true})
	if got := e.Derived(id); got.AllCreatureTypes || !slices.Equal(got.Types, []string{"Creature"}) {
		t.Errorf("stripped copy = types %v, all creature types %v; want [Creature], false", got.Types, got.AllCreatureTypes)
	}
}
