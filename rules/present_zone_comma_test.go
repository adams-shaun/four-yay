package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestPresentZoneCommaListDesertCarriers(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	desert := inZoneCard(t, e, 0, state.ZGraveyard, "Name:Triage Desert\nTypes:Land Desert\nOracle:x\n")
	if o := e.G.Obj(desert); o == nil || o.Zone != state.ZGraveyard {
		t.Fatal("precondition: Desert fixture is not in the controller's graveyard")
	}
	if got := e.presentZoneCount(state.ZGraveyard, "Desert", 0, 0); got != 1 {
		t.Fatalf("precondition: graveyard Desert count=%d, want 1", got)
	}

	camel := onBoardCard(t, e, 0, corpusCard(t, "Solitary Camel"))
	if o := e.G.Obj(camel); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Solitary Camel is not on the battlefield")
	}
	if !e.HasKeyword(camel, "Lifelink") {
		t.Fatal("Solitary Camel lacks lifelink with a Desert in its controller's graveyard")
	}

	wall := onBoardCard(t, e, 0, corpusCard(t, "Wall of Forgotten Pharaohs"))
	var wallAbility *cards.SA
	for _, ab := range e.G.Obj(wall).Face().Abilities {
		if ab != nil && strings.TrimSpace(ab.ParamStr(cards.PKIsPresent)) != "" {
			wallAbility = ab
			break
		}
	}
	if wallAbility == nil {
		t.Fatal("precondition: Wall of Forgotten Pharaohs has no IsPresent ability")
	}
	if !e.abilityPresentHolds(0, wall, wallAbility) {
		t.Fatal("Wall of Forgotten Pharaohs' IsPresent ability gate rejected the graveyard Desert")
	}

	hold := onBoardCard(t, e, 0, corpusCard(t, "Deserts Hold"))
	var desertTrigger *cards.Trigger
	for i := range e.G.Obj(hold).Face().Triggers {
		tr := &e.G.Obj(hold).Face().Triggers[i]
		if strings.TrimSpace(tr.Params["IsPresent"]) != "" && strings.TrimSpace(tr.Params["PresentZone"]) == "Battlefield,Graveyard" {
			desertTrigger = tr
			break
		}
	}
	if desertTrigger == nil {
		t.Fatal("precondition: Deserts Hold has no Battlefield,Graveyard IsPresent trigger")
	}
	if !e.presentClauseHolds(*desertTrigger, hold, 0, nil, "IsPresent", "PresentCompare", "PresentDefined", "PresentZone") {
		t.Fatal("Deserts Hold's IsPresent trigger clause rejected the graveyard Desert")
	}
}
