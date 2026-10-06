package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestPatientNaturalistImprintsMilledLand pins the corpus trigger's own
// compiled chain, rather than an equivalent hand-built Mill/ChangeZone pair.
func TestPatientNaturalistImprintsMilledLand(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Patient Naturalist")
	if !ok || len(card.Faces) == 0 {
		t.Fatal("Patient Naturalist missing from corpus")
	}
	face := card.Faces[0]
	var mill *cards.SA
	for _, tr := range face.Triggers {
		if tr.Params["Execute"] == "TrigMill" {
			mill = tr.Effect
			break
		}
	}
	resolved := cards.ResolveSVar(face.SVars, "TrigMill")
	if mill == nil || resolved == nil || mill.API != "Mill" ||
		mill.ParamStr(cards.PKImprint) != "True" || mill.Params["NumCards"] != "3" ||
		mill.Params["Defined"] != "You" || mill.Params["SubAbility"] != "DBChangeZone" {
		t.Fatalf("precondition: corpus trigger must execute the three-card imprint mill: trigger=%+v resolved=%+v", mill, resolved)
	}
	change := cards.ResolveSVar(face.SVars, "DBChangeZone")
	if change == nil || mill.Sub == nil || mill.Sub.API != "ChangeZone" ||
		mill.Sub.Params["ChangeType"] != "Land.YouOwn+IsImprinted" ||
		mill.Sub.Params["Origin"] != "Graveyard,Exile" || mill.Sub.Params["Destination"] != "Hand" ||
		mill.Sub.Params["RememberChanged"] != "True" || mill.Sub.Params["Hidden"] != "True" ||
		mill.Sub.Params["Mandatory"] != "True" || change.API != mill.Sub.API ||
		change.Params["ChangeType"] != mill.Sub.Params["ChangeType"] ||
		mill.Sub.Params["SubAbility"] != "DBTreasure" || mill.Sub.Sub == nil ||
		mill.Sub.Sub.API != "Token" || mill.Sub.Sub.Params["ConditionDefined"] != "Remembered" ||
		mill.Sub.Sub.Params["ConditionCompare"] != "EQ0" {
		t.Fatalf("precondition: corpus mill must chain through imprinted-land pickup to conditional Treasure: mill=%+v pickup=%+v", mill, change)
	}

	h := &askHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(card, 0).ID
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{src})
	// Establish the trigger's battlefield source via the same event fold as play.
	events.Apply(h.g, events.Event{Kind: events.MoveZone, Obj: src, From: state.ZLibrary, To: state.ZBattlefield})
	ids := []state.ObjID{
		h.g.AddObject(mkCard(t, riderBear), 0).ID,
		h.g.AddObject(mkCard(t, riderLand), 0).ID,
		h.g.AddObject(mkCard(t, riderHalo), 0).ID,
	}
	h.g.SetZone(state.ZLibrary, 0, ids)
	if h.g.Obj(src).Zone != state.ZBattlefield || len(h.g.Zone(state.ZLibrary, 0)) != 3 ||
		len(h.g.Zone(state.ZGraveyard, 0)) != 0 || len(h.g.Zone(state.ZHand, 0)) != 0 ||
		!h.g.Obj(ids[1]).Face().IsLand() || h.g.Obj(ids[0]).Face().IsLand() || h.g.Obj(ids[2]).Face().IsLand() {
		t.Fatal("precondition: battlefield source and three top-of-library cards (only middle card a land) required")
	}
	ctx := &Ctx{Source: src, Controller: 0, SVars: face.SVars}
	Resolve(h, ctx, mill)
	// All three must actually be milled; otherwise an empty pickup could
	// look like a correct no-Treasure result without running the chain.
	milled := make(map[state.ObjID]bool)
	pickedUp := false
	for _, e := range h.log {
		if e.Kind == events.MoveZone && e.Obj == ids[1] && e.From == state.ZGraveyard && e.To == state.ZHand {
			pickedUp = true
		}
		if e.Kind == events.MoveZone && e.From == state.ZLibrary && e.To == state.ZGraveyard {
			milled[e.Obj] = true
		}
		if e.Kind == events.TokenCreate || (e.Kind == events.Note && strings.Contains(e.Text, "unknown token script")) {
			t.Errorf("land pickup should suppress Treasure fallback: %+v", e)
		}
	}
	for _, id := range ids {
		if !milled[id] {
			t.Fatalf("precondition: card %d was not milled; log=%+v", id, h.log)
		}
	}
	if !pickedUp || h.g.Obj(ids[1]).Zone != state.ZHand {
		t.Errorf("corpus chain did not pick up milled land (pickedUp=%v, zone=%v); log=%+v", pickedUp, h.g.Obj(ids[1]).Zone, h.log)
	}
	for _, id := range []state.ObjID{ids[0], ids[2]} {
		if h.g.Obj(id).Zone != state.ZGraveyard {
			t.Errorf("milled nonland %d left graveyard (zone %v)", id, h.g.Obj(id).Zone)
		}
	}
}
