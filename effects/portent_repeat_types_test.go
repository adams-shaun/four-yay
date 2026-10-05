package effects

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestPortentOfCalamityRepeatTypesFrom exercises Portent's compiled
// RepeatEach/ChooseCard chain against the same imprinted-library shape its
// spell creates. The selected cards make each ChosenType binding observable
// in the body's chosen-card events.
func TestPortentOfCalamityRepeatTypesFrom(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	portent, ok := reg.Lookup("Portent of Calamity")
	if !ok {
		t.Fatal("corpus has no Portent of Calamity")
	}
	var repeat *cards.SA
	var svars map[string]string
	for _, face := range portent.Faces {
		if sa := cards.ResolveSVar(face.SVars, "DBRepeatTypes"); sa != nil {
			repeat, svars = sa, face.SVars
			break
		}
	}
	if repeat == nil {
		t.Fatal("Portent of Calamity has no compiled DBRepeatTypes SVar")
	}
	if repeat.API != "RepeatEach" || repeat.Params["RepeatTypesFrom"] != "ValidLibrary Card.IsImprinted" || repeat.Params["RepeatSubAbility"] != "ChooseCard" {
		t.Fatalf("Portent DBRepeatTypes linkage changed: API=%q params=%v", repeat.API, repeat.Params)
	}
	body := cards.ResolveSVar(svars, "ChooseCard")
	if body == nil || body.API != "ChooseCard" || body.Params["Choices"] != "Card.ChosenType+YouOwn+IsImprinted" || body.Params["ChoiceZone"] != "Library" || body.Params["RememberChosen"] != "True" {
		t.Fatalf("Portent ChooseCard body linkage changed: body=%+v", body)
	}

	g := state.NewGame([]string{"you", "them"})
	h := &portentTapeHost{askHost: &askHost{}}
	h.g = g
	source := g.AddObject(portent, 0)
	g.SetZone(state.ZBattlefield, 0, []state.ObjID{source.ID})
	library := []struct {
		name string
		typ  string
	}{
		{"Sol Ring", "Artifact"},
		{"Grizzly Bears", "Creature"},
		{"Oblivion Ring", "Enchantment"},
		{"Kozilek's Command", "Kindred"},
		{"Plains", "Land"},
	}
	var ids []state.ObjID
	wantByType := map[string]state.ObjID{}
	for _, item := range library {
		card, found := reg.Lookup(item.name)
		if !found {
			t.Fatalf("corpus has no %s", item.name)
		}
		o := g.AddObject(card, 0)
		ids = append(ids, o.ID)
		if o.Face() == nil || !containsString(o.Face().Types, item.typ) {
			t.Fatalf("precondition: %s face types %v do not include %s", item.name, o.Face().Types, item.typ)
		}
		for _, typ := range []string{"Artifact", "Creature", "Enchantment", "Instant", "Kindred", "Land", "Sorcery"} {
			if containsString(o.Face().Types, typ) {
				wantByType[typ] = o.ID
			}
		}
	}
	g.SetZone(state.ZLibrary, 0, ids)
	h.Emit(events.Event{Kind: events.Imprint, Obj: source.ID, IDs: append([]state.ObjID(nil), ids...), Text: "seek-found"})
	for _, id := range ids {
		o := g.Obj(id)
		if o == nil || o.Zone != state.ZLibrary {
			t.Fatalf("precondition: imprinted object %d is not in the scanned library zone: %+v", id, o)
		}
		if !containsID(g.Obj(source.ID).SeekFound, id) {
			t.Fatalf("precondition: library object %d is not associated as imprinted: %v", id, g.Obj(source.ID).SeekFound)
		}
	}
	if got, ok := repeatEachTypesFrom(h, &Ctx{Source: source.ID, Controller: 0}, repeat.Params["RepeatTypesFrom"]); !ok || !reflect.DeepEqual(got, []string{"Artifact", "Creature", "Enchantment", "Instant", "Kindred", "Land"}) {
		t.Fatalf("precondition: real Portent selector types = %v (ok=%v), want [Artifact Creature Enchantment Instant Kindred Land]", got, ok)
	}

	// Prove the actual ChooseCard selector admits each expected library object
	// under its corresponding source ChosenType before running the chain.
	for _, typ := range []string{"Artifact", "Creature", "Enchantment", "Instant", "Kindred", "Land"} {
		h.Emit(events.Event{Kind: events.Choose, Obj: source.ID, Counter: "type", Text: typ})
		matched := cardChoices(h, &Ctx{Source: source.ID, Controller: 0}, body, 0)
		if !containsID(choiceIDs(matched), wantByType[typ]) {
			t.Fatalf("precondition: Portent ChooseCard selector did not admit %s card %d; candidates=%v", typ, wantByType[typ], choiceIDs(matched))
		}
	}
	start := len(h.log)
	Resolve(h, &Ctx{Source: source.ID, Controller: 0, SVars: svars}, repeat)
	var observed []string
	var eventsByType []string
	activeType := ""
	for _, event := range h.log[start:] {
		if event.Kind != events.Choose || event.Obj != source.ID {
			continue
		}
		switch event.Counter {
		case "type":
			activeType = event.Text
			observed = append(observed, event.Text)
			eventsByType = append(eventsByType, "type:"+event.Text)
		case "chosen":
			if len(event.IDs) != 1 {
				t.Fatalf("ChooseCard body selected %d cards, want one per type: event=%+v log=%+v", len(event.IDs), event, h.log)
			}
			if wantByType[activeType] != event.IDs[0] {
				t.Fatalf("ChooseCard body for ChosenType %s selected %d, want %d", activeType, event.IDs[0], wantByType[activeType])
			}
			eventsByType = append(eventsByType, "chosen:"+activeType)
		}
	}
	wantTypes := []string{"Artifact", "Creature", "Enchantment", "Instant", "Kindred", "Land"}
	if !reflect.DeepEqual(observed, wantTypes) {
		t.Fatalf("Portent type bindings = %v, want ordered %v; log=%+v", observed, wantTypes, h.log)
	}
	wantEvents := []string{"type:Artifact", "chosen:Artifact", "type:Creature", "chosen:Creature", "type:Enchantment", "chosen:Enchantment", "type:Instant", "chosen:Instant", "type:Kindred", "chosen:Kindred", "type:Land", "chosen:Land"}
	if !reflect.DeepEqual(eventsByType, wantEvents) {
		t.Fatalf("Portent binding/body event sequence = %v, want %v; log=%+v", eventsByType, wantEvents, h.log)
	}
}

type portentTapeHost struct {
	*askHost
	asks uint64
}

func (h *portentTapeHost) AskCount() uint64 { return h.asks }

func (h *portentTapeHost) TapeAnswer(d *decision.Decision) (decision.Intent, bool) {
	h.asks++
	if len(d.Options) == 0 {
		return decision.Intent{}, false
	}
	return decision.Intent{Choices: []int{0}}, true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
