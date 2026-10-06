package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 707.9b: Omni-Changeling's "except it has changeling" is part of the
// copiable values, so the copy is every creature type in layer 4. That
// characteristic-defining ability applies BEFORE every ordinary layer-4
// effect (CR 613.2/613.3), so an OLDER creature-type strip already on the
// battlefield cannot be undone by the copy entering -- the strip's timestamp
// is earlier than the copy's, but a CDA is not ordered by timestamp at all.
func TestCloneChangelingCDAAfterEarlierStrip(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Omni-Changeling"))
	bear := e.G.AddObject(corpusAlternativeCard(t, "Grizzly Bears"), 0)
	bear.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{bear.ID})
	id := e.G.Zone(state.ZHand, 0)[0]
	if bear.Zone != state.ZBattlefield || e.G.Obj(id).Face().Name == bear.Face().Name {
		t.Fatal("precondition: distinct Omni-Changeling in hand and Grizzly Bears on the battlefield")
	}
	// An OLDER strip that removes every creature type from every creature.
	e.AddContinuous(state.ContinuousEffect{Source: bear.ID, Affects: "Creature", Layer: state.LType, RemoveCreatureTypes: true})
	if !slices.Equal(e.Derived(bear.ID).Types, []string{"Creature"}) || e.Derived(bear.ID).AllCreatureTypes {
		t.Fatalf("precondition: older strip must clear Grizzly Bears' subtypes: %+v", e.Derived(bear.ID))
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
	if !e.HasKeyword(id, "Changeling") {
		t.Fatal("copy must retain the Changeling keyword")
	}
	if got := e.Derived(id); got.AllCreatureTypes || !slices.Equal(got.Types, []string{"Creature"}) {
		t.Errorf("earlier type strip must apply AFTER copied Changeling CDA: types %v all=%v", got.Types, got.AllCreatureTypes)
	}
	assertChangelingTypePredicate(t, e, id, "Creature.Surrakar", false)
}

// The setter shape of the same ordering rule: an OLDER SetCreatureTypes
// effect (strip all creature subtypes, then add Human) is applied before the
// copied Changeling CDA and therefore survives it.
func TestCloneChangelingCDAAfterEarlierSetter(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Omni-Changeling"))
	bear := e.G.AddObject(corpusAlternativeCard(t, "Grizzly Bears"), 0)
	bear.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{bear.ID})
	id := e.G.Zone(state.ZHand, 0)[0]
	if bear.Zone != state.ZBattlefield || e.G.Obj(id).Face().Name == bear.Face().Name {
		t.Fatal("precondition: distinct Omni-Changeling in hand and Grizzly Bears on the battlefield")
	}
	e.AddContinuous(state.ContinuousEffect{Source: bear.ID, Affects: "Creature", Layer: state.LType, SetCreatureTypes: true, AddTypes: []string{"Human"}})
	if got := e.Derived(bear.ID); got.AllCreatureTypes || !slices.Equal(got.Types, []string{"Creature", "Human"}) {
		t.Fatalf("precondition: older setter must leave Grizzly Bears as [Creature Human]: %+v", got)
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
	if !e.HasKeyword(id, "Changeling") {
		t.Fatal("copy must retain the Changeling keyword")
	}
	if got := e.Derived(id); got.AllCreatureTypes || !slices.Equal(got.Types, []string{"Creature", "Human"}) {
		t.Errorf("earlier type setter must apply AFTER copied Changeling CDA: types %v all=%v", got.Types, got.AllCreatureTypes)
	}
	assertChangelingTypePredicate(t, e, id, "Creature.Surrakar", false)
}
