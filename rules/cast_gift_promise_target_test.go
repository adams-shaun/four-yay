package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestIntoTheFloodMawPromisedGiftCastsAndBounces pins the promised-gift half
// of the offer/ask target-feasibility agreement (r2 review MINOR). With the
// gift promised, the root creature ask resolves to Min 0 / Max 0 and is
// skipped, the opponent receives the promised tapped Fish token, and the
// mid-resolution ChangeZone sub-ability asks for a nonland permanent an
// opponent controls. Answering that ask completes the cast with no reversal.
func TestIntoTheFloodMawPromisedGiftCastsAndBounces(t *testing.T) {
	e, spell := corpusTargetFeasibleCard(t, 7206, "i/into_the_flood_maw.txt", "Creature.OppCtrl")
	theirBear := bearPermanent(t, e, 1)
	if o := e.G.Obj(theirBear); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: opponent creature is not a battlefield permanent: %+v", o)
	}
	addMana(t, e, 0, "U")
	if !castOffered(e, spell) {
		t.Fatal("Into the Flood Maw was withheld despite an opponent creature target")
	}
	submitChoices(t, e, castCardOption(t, e, spell).Index)
	answerGift(t, e, true)

	// With the gift promised the root bound resolves to zero: no creature
	// ask may be posed (CR 601.2c, TargetMin$ X with X = 0).
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("promised cast must skip the root creature ask, got: %+v", d)
	}

	// Drive priority until the mid-resolution ChangeZone ask is posed.
	var ask *decision.Decision
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision while resolving the promised cast (iteration %d)", i)
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "choice" {
			ask = d
			break
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision while resolving: %+v", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d.Options)
		}
		submitChoices(t, e, idx)
	}
	if ask == nil {
		t.Fatal("the promised-gift cast never posed the ChangeZone target ask")
	}
	idx := targetOptionIndex(ask, theirBear)
	if idx < 0 {
		t.Fatalf("the opponent's nonland permanent is not offered: %+v", ask.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if hasNote(e, "cast aborted") {
		t.Fatal("the promised-gift cast was reversed despite a legal target")
	}
	if z := e.G.Obj(theirBear).Zone; z != state.ZHand {
		t.Fatalf("the promised cast did not bounce the opponent's permanent to hand: %s", z)
	}
}
