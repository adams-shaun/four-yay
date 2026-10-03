package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestGateToTheAetherOptionalDigAsksItsChooser uses the real corpus carrier.
// Gate to the Aether has exactly one eligible card in its one-card window,
// yet Optional$ True still requires a may decision; Choser$ TriggeredPlayer
// routes that decision to the triggered player rather than the source
// controller.
func TestGateToTheAetherOptionalDigAsksItsChooser(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	gate, ok := reg.Lookup("Gate to the Aether")
	if !ok {
		t.Fatal("Gate to the Aether missing from corpus")
	}
	sa := cards.ResolveSVar(gate.Faces[0].SVars, "TrigAetherDig")
	if sa == nil || sa.API != "Dig" || sa.Params["Optional"] != "True" || sa.Params["Choser"] != "TriggeredPlayer" {
		t.Fatalf("Gate to the Aether Dig shape drifted: %+v", sa)
	}
	h := &askHost{}
	h.g = state.NewGame(names(2))
	source := h.g.AddObject(gate, 0).ID
	forestCard, ok := reg.Lookup("Forest")
	if !ok {
		t.Fatal("Forest missing from corpus")
	}
	forest := h.g.AddObject(forestCard, 1)
	h.g.SetZone(state.ZLibrary, 1, []state.ObjID{forest.ID})
	h.g.Obj(forest.ID).Zone = state.ZLibrary
	if h.g.Obj(forest.ID).Zone != state.ZLibrary || len(h.g.Zone(state.ZLibrary, 1)) != 1 {
		t.Fatal("precondition: triggered player's one-card library was not installed")
	}
	ctx := &Ctx{Source: source, Controller: 0,
		TriggerContext: TriggerContext{TriggerPlayer: state.Target{Player: 1, IsPlayer: true}}}
	Resolve(h, ctx, sa)
	if h.asked == nil || h.asked.Kind != decision.KChoose {
		t.Fatalf("decision = %+v, want the optional one-card Dig election", h.asked)
	}
	if h.asked.Player != 1 || h.asked.Min != 0 || h.asked.Max != 1 {
		t.Fatalf("decision = %+v, want chooser seat 1 with bounds 0..1", h.asked)
	}
	if len(h.asked.Options) != 1 || h.asked.Options[0].Obj != forest.ID {
		t.Fatalf("options = %+v, want the triggered player's Forest", h.asked.Options)
	}
}

// TestDigPrimaryLibraryPositionZeroMovesTheTakenPileToTheTop pins the
// primary LibraryPosition$ independently of the remainder destination.
func TestDigPrimaryLibraryPositionZeroMovesTheTakenPileToTheTop(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	land := mkCard(t, "Name:Top Land\nTypes:Basic Land Forest\nOracle:x\n")
	bear := mkCard(t, "Name:Rest Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	below := mkCard(t, "Name:Below\nTypes:Artifact\nOracle:x\n")
	landID := h.g.AddObject(land, 0).ID
	bearID := h.g.AddObject(bear, 0).ID
	belowID := h.g.AddObject(below, 0).ID
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{landID, bearID, belowID})
	if h.g.Zone(state.ZLibrary, 0)[0] != landID || landID == belowID {
		t.Fatal("precondition: the primary card must be a distinct library-top card")
	}
	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"SP$ Dig | Defined$ You | DigNum$ 2 | ChangeNum$ 1 | ChangeValid$ Land | DestinationZone$ Library | LibraryPosition$ 0 | DestinationZone2$ Graveyard"))
	lib := h.g.Zone(state.ZLibrary, 0)
	if len(lib) != 2 || lib[0] != landID || lib[1] != belowID {
		t.Fatalf("library = %v, want primary land %d on top above %d", lib, landID, belowID)
	}
	if h.g.Obj(bearID).Zone != state.ZGraveyard {
		t.Fatalf("remainder zone = %s, want graveyard", h.g.Obj(bearID).Zone)
	}
}
