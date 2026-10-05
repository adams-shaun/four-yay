package rules

import (
	"os"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestSorceressSchemesCorpusMultiZoneTargets exercises the compiled corpus
// card, rather than a hand-authored approximation of its ChangeZone ability.
func TestSorceressSchemesCorpusMultiZoneTargets(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("../.cards/cardsfolder/s/sorceresss_schemes.txt"); err != nil {
		t.Fatalf("required Forge corpus card is unavailable: %v", err)
	}
	reg := searchTestRegistry(t)
	scheme := searchCorpusCard(t, reg, "Sorceress's Schemes")
	shock := searchCorpusCard(t, reg, "Shock")
	flashback := searchCorpusCard(t, reg, "Deep Analysis")
	ineligible := searchCorpusCard(t, reg, "Lightning Bolt")

	if len(scheme.Faces) == 0 || scheme.Faces[0].SpellAbility() == nil {
		t.Fatal("corpus Sorceress's Schemes has no spell ability")
	}
	if !shock.Faces[0].IsInstant() || !flashback.Faces[0].IsSorcery() || !flashback.Faces[0].HasKeyword("Flashback") || !ineligible.Faces[0].IsInstant() || ineligible.Faces[0].HasKeyword("Flashback") {
		t.Fatalf("unexpected corpus target characteristics: Shock=%+v Deep Analysis=%+v Lightning Bolt=%+v", shock.Faces[0], flashback.Faces[0], ineligible.Faces[0])
	}

	e := handEngine(t, scheme)
	grave := e.G.AddObject(shock, 0)
	grave.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{grave.ID})
	exiled := e.G.AddObject(flashback, 0)
	exiled.Zone = state.ZExile
	wrongExile := e.G.AddObject(ineligible, 0)
	wrongExile.Zone = state.ZExile
	e.G.SetZone(state.ZExile, 0, []state.ObjID{exiled.ID, wrongExile.ID})

	if grave.Zone != state.ZGraveyard || exiled.Zone != state.ZExile || wrongExile.Zone != state.ZExile {
		t.Fatalf("fixture zones: grave=%s eligible exile=%s ineligible exile=%s", grave.Zone, exiled.Zone, wrongExile.Zone)
	}
	if grave.Zone == exiled.Zone {
		t.Fatalf("fixture must compare distinct target zones: grave=%s eligible exile=%s", grave.Zone, exiled.Zone)
	}
	if !containsID(e.G.Zone(state.ZGraveyard, 0), grave.ID) || !containsID(e.G.Zone(state.ZExile, 0), exiled.ID) || !containsID(e.G.Zone(state.ZExile, 0), wrongExile.ID) {
		t.Fatal("fixture objects are not present in the zones read by target legality")
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
		t.Fatal("corpus Sorceress's Schemes missing from hand")
	}
	submitChoices(t, e, passToCast(t, e, schemeID))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected target decision, got %+v", d)
	}
	graveOffered, exileOffered := false, false
	for _, opt := range d.Options {
		switch opt.Obj {
		case grave.ID:
			graveOffered = true
		case exiled.ID:
			exileOffered = true
		case wrongExile.ID:
			t.Fatalf("exiled instant without flashback was offered: %+v", d.Options)
		}
	}
	if !graveOffered || !exileOffered {
		t.Fatalf("eligible alternatives not both offered: graveyard=%t exile=%t options=%+v", graveOffered, exileOffered, d.Options)
	}
}
