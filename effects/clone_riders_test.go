package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The real corpus's named riders are resolved against the printed grantor,
// never against the face it copies over.
func TestCloneNamedStaticFromCephalidFacetaker(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	h := &fakeHost{g: state.NewGame(names(2))}
	facetaker, _ := reg.Lookup("Cephalid Facetaker")
	bear, _ := reg.Lookup("Grizzly Bears")
	if facetaker == nil || bear == nil {
		t.Fatal("missing corpus cards")
	}
	a, b := h.g.AddObject(facetaker, 0).ID, h.g.AddObject(bear, 0).ID
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{a, b})
	h.g.Obj(a).Zone, h.g.Obj(b).Zone = state.ZBattlefield, state.ZBattlefield
	original := h.g.Obj(a).Face()
	body := cards.ResolveSVar(original.SVars, "TrigClone")
	if body == nil || body.Params["AddStaticAbilities"] != "Unblockable" || len(original.Statics) == len(h.g.Obj(b).Face().Statics) && original.Name == bear.Faces[0].Name {
		t.Fatal("precondition: missing distinct clone and static")
	}
	Resolve(h, &Ctx{Source: a, Controller: 0, Targets: []state.Target{{Obj: b}}, SVars: original.SVars}, body)
	f := h.g.Obj(a).Face()
	if f == nil || f.Name != bear.Faces[0].Name || len(f.Statics) <= len(bear.Faces[0].Statics) {
		t.Fatalf("named static not granted: face %v", f)
	}
	found := false
	for _, st := range f.Statics {
		if st.Mode == "CantBlockBy" && st.Params["ValidAttacker"] == "Creature.Self" {
			found = true
		}
	}
	if !found {
		t.Fatalf("named Unblockable static absent: %+v", f.Statics)
	}
}

func TestCloneKillerCosplayCopiesNamedCard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	h := &fakeHost{g: state.NewGame(names(2))}
	cosplay, _ := reg.Lookup("Killer Cosplay")
	bear, _ := reg.Lookup("Grizzly Bears")
	forest, _ := reg.Lookup("Forest")
	if cosplay == nil || bear == nil || forest == nil {
		t.Fatal("missing corpus cards")
	}
	h.g.NameUniverse = reg.Cards
	a, b := h.g.AddObject(cosplay, 0).ID, h.g.AddObject(forest, 0).ID
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{a, b})
	h.g.Obj(a).Zone, h.g.Obj(b).Zone = state.ZBattlefield, state.ZBattlefield
	original := h.g.Obj(a).Face()
	body := cards.ResolveSVar(original.SVars, "DBCopy")
	if body == nil || body.Params["CopyFromChosenName"] != "True" || original.Name == bear.Faces[0].Name || h.g.Obj(b).Face().Name == bear.Faces[0].Name {
		t.Fatal("precondition: named card and clone body must differ")
	}
	h.Emit(events.Event{Kind: events.Choose, Obj: a, Counter: "name", Text: bear.Faces[0].Name})
	Resolve(h, &Ctx{Source: a, Controller: 0, Targets: []state.Target{{Obj: b}}, SVars: original.SVars}, body)
	if f := h.g.Obj(b).Face(); f == nil || f.Name != bear.Faces[0].Name {
		t.Fatalf("chosen-name copy = %v", f)
	}
}

func TestClonePermeatingMassHonorsCloneZone(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	h := &fakeHost{g: state.NewGame(names(2))}
	mass, _ := reg.Lookup("Permeating Mass")
	bear, _ := reg.Lookup("Grizzly Bears")
	if mass == nil || bear == nil {
		t.Fatal("missing corpus cards")
	}
	a, b := h.g.AddObject(mass, 0).ID, h.g.AddObject(bear, 0).ID
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{a})
	h.g.Obj(a).Zone = state.ZBattlefield
	h.g.SetZone(state.ZGraveyard, 0, []state.ObjID{b})
	h.g.Obj(b).Zone = state.ZGraveyard
	body := cards.ResolveSVar(mass.Faces[0].SVars, "TrigCopy")
	if body == nil || body.Params["CloneZone"] != "Battlefield" || mass.Faces[0].Name == bear.Faces[0].Name {
		t.Fatal("precondition: real mass clone with distinct source")
	}
	// CloneTarget names the battlefield victim, while Defined$ Self names the mass.
	// The source is moved off the prescribed zone; it must NOT be copied.
	h.g.SetZone(state.ZGraveyard, 0, []state.ObjID{a, b})
	h.g.Obj(a).Zone = state.ZGraveyard
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{b})
	h.g.Obj(b).Zone = state.ZBattlefield
	Resolve(h, &Ctx{Source: a, Controller: 0, Targets: []state.Target{{Obj: b}}, Remembered: []state.Target{{Obj: b}}, SVars: mass.Faces[0].SVars}, body)
	if h.g.Obj(b).CopyFace != nil {
		t.Fatal("CloneZone Battlefield copied an object in graveyard")
	}
}

func TestCloneVesuvaIntoPlayTapped(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	h := &fakeHost{g: state.NewGame(names(2))}
	vesuva, _ := reg.Lookup("Vesuva")
	forest, _ := reg.Lookup("Forest")
	if vesuva == nil || forest == nil {
		t.Fatal("missing corpus cards")
	}
	a, b := h.g.AddObject(vesuva, 0).ID, h.g.AddObject(forest, 0).ID
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{a, b})
	h.g.Obj(a).Zone, h.g.Obj(b).Zone = state.ZBattlefield, state.ZBattlefield
	body := cards.ResolveSVar(vesuva.Faces[0].SVars, "DBCopy")
	if body == nil || body.Params["IntoPlayTapped"] != "True" || h.g.Obj(a).Tapped || vesuva.Faces[0].Name == forest.Faces[0].Name {
		t.Fatal("precondition: untapped Vesuva, distinct land, entry rider")
	}
	Resolve(h, &Ctx{Source: a, Controller: 0, CloneETB: true, CloneChoiceValid: true, CloneChoice: b, CloneBecomeValid: true, CloneBecome: a, SVars: vesuva.Faces[0].SVars}, body)
	if !h.g.Obj(a).Tapped || h.g.Obj(a).Face().Name != forest.Faces[0].Name {
		t.Fatalf("Vesuva entry: tapped=%v face=%s", h.g.Obj(a).Tapped, h.g.Obj(a).Face().Name)
	}
}

func TestCloneAttachedToNamesBattlefieldBearer(t *testing.T) {
	h, host, bearer := cloneOptionalFixture(t)
	if h.g.Obj(host).AttachedTo != 0 || h.g.Obj(bearer).Zone != state.ZBattlefield || h.g.Obj(host).Face().Name == h.g.Obj(bearer).Face().Name {
		t.Fatal("precondition: distinct unattached battlefield permanents")
	}
	body := sa(t, "DB$ Clone | Defined$ TriggeredCardLKICopy | AttachedTo$ TriggeredTarget")
	Resolve(h, &Ctx{Source: host, Controller: 0, Remembered: []state.Target{{Obj: bearer}}, TriggerContext: TriggerContext{TriggerTarget: state.Target{Obj: bearer}}}, body)
	if got := h.g.Obj(host).AttachedTo; got != bearer {
		t.Fatalf("clone attachment = %v, want bearer %v", got, bearer)
	}
}

func TestCloneFaceDownAndKeepFacedown(t *testing.T) {
	h, host, bearer := cloneOptionalFixture(t)
	if h.g.Obj(host).FaceDown || h.g.Obj(host).Face().Name == h.g.Obj(bearer).Face().Name {
		t.Fatal("precondition: face-up distinct battlefield permanents")
	}
	Resolve(h, &Ctx{Source: host, Controller: 0, Remembered: []state.Target{{Obj: bearer}}}, sa(t, "DB$ Clone | Defined$ TriggeredCardLKICopy | FaceDown$ True"))
	if !h.g.Obj(host).FaceDown || h.g.Obj(host).CopyFace == nil {
		t.Fatal("FaceDown$ did not turn the copy face down")
	}
	Resolve(h, &Ctx{Source: host, Controller: 0, Remembered: []state.Target{{Obj: bearer}}}, sa(t, "DB$ Clone | Defined$ TriggeredCardLKICopy | KeepFacedown$ False"))
	if h.g.Obj(host).FaceDown {
		t.Fatal("KeepFacedown$ False did not turn the copy face up")
	}
}
