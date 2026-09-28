package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestAbbotOfKeralKeepAbsentEffectDurationEndsAtCleanup pins Forge's default
// Duration$ on the real corpus card. Abbot is a creature, so this distinguishes
// an absent duration (this turn) from an explicit Permanent/source-leaves grant.
func TestAbbotOfKeralKeepAbsentEffectDurationEndsAtCleanup(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	abbot := choiceCorpusCard(t, "Abbot of Keral Keep")
	e := corpusEngine(t, reg, []*cards.Card{abbot}, nil)
	abbotID := findCardObj(t, e, 0, "Abbot of Keral Keep", state.ZHand)

	addMana(t, e, 0, "1R")
	cast := optionOf(t, e, "cast", abbotID)
	submitChoices(t, e, cast)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(abbotID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Abbot zone = %+v, want battlefield", e.G.Obj(abbotID))
	}
	var exiledID state.ObjID
	for _, id := range e.G.Zone(state.ZExile, 0) {
		if o := e.G.Obj(id); o != nil && o.Card != nil && o.Zone == state.ZExile {
			if exiledID != 0 {
				t.Fatalf("precondition: multiple exiled cards: %v", e.G.Zone(state.ZExile, 0))
			}
			exiledID = id
		}
	}
	if exiledID == 0 {
		t.Fatal("precondition: Abbot did not exile a card")
	}
	grant := mayPlayGrantOn(e, exiledID)
	if grant == nil || !grant.UntilEOT {
		t.Fatalf("precondition: Abbot grant = %+v, want a live UntilEOT grant", grant)
	}

	// Abbot itself remains in play. Cleanup must therefore be the reason the
	// absent-Duration grant disappears, not the source-leaves sweep.
	e.EndOfTurnCleanup()
	if e.G.Obj(abbotID).Zone != state.ZBattlefield {
		t.Fatal("precondition: Abbot left the battlefield before cleanup assertion")
	}
	if grant := mayPlayGrantOn(e, exiledID); grant != nil {
		t.Fatalf("Abbot's absent-Duration grant survived cleanup: %+v", grant)
	}
}
