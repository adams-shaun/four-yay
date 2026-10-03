package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Vesuva's real ETB-copy replacement must apply IntoPlayTapped$ to the
// entering copy, not to a previously existing battlefield permanent.
func TestCloneVesuvaETBEntersAsTappedLand(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Vesuva"))
	forest := e.G.AddObject(corpusAlternativeCard(t, "Forest"), 1)
	forest.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{forest.ID})
	id := e.G.Zone(state.ZHand, 0)[0]
	if e.G.Obj(id).Face().Name == forest.Face().Name || forest.Zone != state.ZBattlefield || e.G.Obj(id).Tapped {
		t.Fatal("precondition: distinct untapped Vesuva and battlefield land")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" {
		t.Fatalf("Vesuva copy election missing: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == forest.ID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("battlefield land absent from election: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Face().Name != forest.Face().Name || !o.Tapped {
		t.Fatalf("Vesuva entry = %+v, want tapped Forest copy", o)
	}
	if !hasEvent(e, events.ClonePermanent, id) {
		t.Fatal("ETB choice was recorded but Clone handler never ran")
	}
}

// A non-Goad named static must use the same printed-static scanner after the
// copy. Sakashima's NoLegendRule is the corpus carrier.
func TestCloneSakashimaETBGrantsNamedIgnoreLegendRule(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Sakashima of a Thousand Faces"))
	bear := e.G.AddObject(corpusAlternativeCard(t, "Grizzly Bears"), 0)
	bear.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{bear.ID})
	id := e.G.Zone(state.ZHand, 0)[0]
	if id == 0 || e.G.Obj(id).Face().Name == bear.Face().Name || bear.Zone != state.ZBattlefield || len(e.activeStatics("IgnoreLegendRule")) != 0 {
		t.Fatal("precondition: no legend static and distinct copy template")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" {
		t.Fatalf("Sakashima copy election = %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Obj == bear.ID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("owned creature not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Face().Name != bear.Face().Name || len(o.Face().Statics) == 0 {
		t.Fatalf("Sakashima copy/static = %+v", o)
	}
	seen := false
	for _, sv := range e.activeStatics("IgnoreLegendRule") {
		if sv.Source == id {
			seen = true
		}
	}
	if !seen {
		t.Fatal("copied Sakashima did not grant its named IgnoreLegendRule to the static scanner")
	}
}
