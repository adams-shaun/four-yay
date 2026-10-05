package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const sorceressSchemesShape = "Name:Sorceress's Schemes\nManaCost:1 R\nTypes:Sorcery\n" +
	"A:SP$ ChangeZone | Origin$ Graveyard,Exile | Destination$ Hand | " +
	"TgtPrompt$ Choose target instant or sorcery card in your graveyard or exiled card with flashback you own | " +
	"ValidTgts$ Instant.inZoneGraveyard+YouCtrl,Sorcery.inZoneGraveyard+YouCtrl,Card.withFlashback+inZoneExile+YouCtrl | " +
	"SubAbility$ DBMana | SpellDescription$ Return the target card to your hand. Add {R}.\n" +
	"SVar:DBMana:DB$ Mana | Produced$ R\nOracle:x\n"

// TestSorceressSchemesMultiZoneTargets covers the real target-alternative
// shape inline: each inZone alternative contributes only its named Origin$ zone.
func TestSorceressSchemesMultiZoneTargets(t *testing.T) {
	t.Parallel()
	scheme := card(t, sorceressSchemesShape)
	shock := card(t, "Name:Shock\nManaCost:R\nTypes:Instant\nOracle:x\n")
	flashback := card(t, "Name:Flashback Card\nManaCost:1 R\nTypes:Instant\nK:Flashback:2 R\nOracle:x\n")
	ineligible := card(t, "Name:Exiled Instant\nManaCost:R\nTypes:Instant\nOracle:x\n")
	e := handEngine(t, scheme)

	grave := e.G.AddObject(shock, 0)
	grave.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{grave.ID})
	exiled := e.G.AddObject(flashback, 0)
	exiled.Zone = state.ZExile
	e.G.SetZone(state.ZExile, 0, []state.ObjID{exiled.ID})
	wrongExile := e.G.AddObject(ineligible, 0)
	wrongExile.Zone = state.ZExile
	e.G.SetZone(state.ZExile, 0, []state.ObjID{exiled.ID, wrongExile.ID})

	if grave.Zone != state.ZGraveyard || exiled.Zone != state.ZExile || wrongExile.Zone != state.ZExile {
		t.Fatalf("fixture zones: grave=%s flashback=%s ineligible=%s", grave.Zone, exiled.Zone, wrongExile.Zone)
	}
	if grave.Zone == exiled.Zone {
		t.Fatalf("fixture must exercise distinct target zones: both are %s", grave.Zone)
	}
	ability := scheme.Faces[0].Abilities[0]
	gotZones := targetZones(ability)
	if len(gotZones) != 2 || gotZones[0] != state.ZGraveyard || gotZones[1] != state.ZExile {
		t.Fatalf("multi-zone target census = %v, want graveyard and exile", gotZones)
	}
	e.G.Players[0].Pool[state.MR] = 5
	e.G.Players[0].Pool[state.MC] = 5
	e.askPriority(0)

	var schemeID state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(id).Face().Name == "Sorceress's Schemes" {
			schemeID = id
		}
	}
	if schemeID == 0 {
		t.Fatal("Sorceress's Schemes missing from hand")
	}
	submitChoices(t, e, passToCast(t, e, schemeID))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected target decision, got %+v", d)
	}
	graveIndex, exileIndex := -1, -1
	for _, opt := range d.Options {
		switch opt.Obj {
		case grave.ID:
			graveIndex = opt.Index
		case exiled.ID:
			exileIndex = opt.Index
		case wrongExile.ID:
			t.Fatalf("exiled instant without flashback was offered: %+v", d.Options)
		}
	}
	if graveIndex < 0 || exileIndex < 0 {
		t.Fatalf("eligible alternatives not both offered: graveyard index=%d exile index=%d options=%+v", graveIndex, exileIndex, d.Options)
	}
	submitChoices(t, e, graveIndex)
	passUntilStackEmpty(t, e, 8)
	if z := e.G.Obj(grave.ID).Zone; z != state.ZHand {
		t.Fatalf("selected graveyard Shock ended in %s, want hand", z)
	}
}
