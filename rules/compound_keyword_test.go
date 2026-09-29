package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func TestCantAttackOrBlockPrintedKeywordClassificationAndFlags(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	card := searchCorpusCard(t, reg, "Vigean Hydropon")
	var printed bool
	for _, face := range card.Faces {
		for _, keyword := range face.Keywords {
			if strings.EqualFold(cards.KeywordHead(keyword), "CantAttackOrBlock") {
				printed = true
			}
		}
	}
	if !printed {
		t.Fatalf("precondition: %s has no canonical compound printed keyword", card.Faces[0].Name)
	}
	if !effects.Supported()["kw:CantAttackOrBlock"] {
		t.Fatal(`effects.Supported() is missing "kw:CantAttackOrBlock"`)
	}
	for _, name := range []string{"Vigean Hydropon", "Grakk, the Pacifist"} {
		carrier := searchCorpusCard(t, reg, name)
		var hasCompound bool
		for _, face := range carrier.Faces {
			for _, keyword := range face.Keywords {
				hasCompound = hasCompound || strings.EqualFold(cards.KeywordHead(keyword), "CantAttackOrBlock")
			}
		}
		if !hasCompound {
			t.Fatalf("precondition: %s has no canonical compound printed keyword", name)
		}
		missing := reg.Unsupported(carrier, effects.Supported())
		if hasString(missing, "kw:CARDNAME can't attack or block.") || hasString(missing, "kw:CantAttackOrBlock") {
			t.Errorf("%s remains unsupported by compound keyword: %v", name, missing)
		}
	}

	e := layerEngine(t)
	o := e.G.AddObject(card, 0)
	o.Zone = state.ZBattlefield
	e.G.Clock++
	o.Timestamp = e.G.Clock
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))
	e.staticEpoch, e.activeEpoch, e.typesEpoch = -1, -1, -1
	flags := e.derivedHiddenFlags(o.ID)
	if !flags.cantAttack || !flags.cantBlock {
		t.Fatalf("derived flags for printed %s = %+v, want cantAttack and cantBlock", card.Faces[0].Name, flags)
	}
}

func hasString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
