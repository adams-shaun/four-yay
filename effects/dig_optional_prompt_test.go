package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestExplorersScopeDigPosesOptionalTake(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Explorer's Scope")
	if !ok {
		t.Fatal("corpus is missing Explorer's Scope")
	}
	var effect *cards.SA
	var svars map[string]string
	for _, face := range card.Faces {
		if _, ok := face.SVars["TrigDig"]; ok {
			effect = cards.ResolveSVar(face.SVars, "TrigDig")
			svars = face.SVars
			break
		}
	}
	if effect == nil || effect.API != "Dig" || effect.Params["OptionalAbilityPrompt"] == "" {
		t.Fatalf("precondition: Explorer's Scope TrigDig is not the expected Dig shape: %+v", effect)
	}
	h := &askHost{}
	h.g = state.NewGame(names(2))
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	id := h.g.AddObject(land, 0).ID
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{id})
	Resolve(h, &Ctx{Controller: 0, SVars: svars}, effect)
	if h.asked == nil || h.asked.Kind != decision.KChoose || h.asked.Min != 0 {
		t.Fatalf("Explorer's Scope decision = %+v, want optional take election", h.asked)
	}
}
