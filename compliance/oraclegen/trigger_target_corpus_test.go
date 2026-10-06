package oraclegen_test

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// The generated ETB-trigger answers of the cards whose targets reached XMage
// in a form it rejects. Step 1 is the resolve step that poses the trigger's
// targets (step 0 is the cast).
func TestTriggerTargetAnswersOfRealCards(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	skip := oraclegen.XAnswer{Seat: 0, Kind: "target", Value: "[target_skip]"}
	for _, tc := range []struct {
		card string
		want []oraclegen.XAnswer
	}{
		// Divided counters: one TargetAmount answer with the whole total.
		{"Armament Dragon", []oraclegen.XAnswer{{Seat: 0, Kind: "target", Value: "p0:Armament Dragon^X=3"}}},
		{"Glint Weaver", []oraclegen.XAnswer{{Seat: 0, Kind: "target", Value: "p0:Glint Weaver^X=3"}}},
		// Divided damage with a player recipient.
		{"Gandalf, Spark Starter", []oraclegen.XAnswer{{Seat: 0, Kind: "target", Value: "p0^X=3"}}},
		// Target opponent, then an "up to one creature" slot nobody filled.
		{"Sting, Bilbo's Sword", []oraclegen.XAnswer{{Seat: 0, Kind: "target", Value: "p1"}, skip}},
		{"Cloak and Dagger, Entwined", []oraclegen.XAnswer{{Seat: 0, Kind: "target", Value: "p1"}, skip}},
		{"The Spot, Living Portal", []oraclegen.XAnswer{{Seat: 0, Kind: "target", Value: "The Spot, Living Portal"}, skip}},
	} {
		t.Run(tc.card, func(t *testing.T) {
			item, sk := templates.Generate(reg, tc.card)
			if sk != nil {
				t.Fatalf("generator skipped the fixture: %s", sk.Reason)
			}
			if len(item.XAnswers) < 2 {
				t.Fatalf("no resolve-step answers scripted: %v", item.XAnswers)
			}
			if item.XAnswers[0] != nil {
				t.Fatalf("the cast step must script nothing: %v", item.XAnswers[0])
			}
			if got := item.XAnswers[1]; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resolve answers\n got %v\nwant %v", got, tc.want)
			}
		})
	}
}

// Jeskai Revelation's two spell slots (target spell or permanent, any target)
// are bound through the cast's own targets; nothing is scripted on the
// resolve step. Its permanent half is the ScriptedChoicePlayer.chooseTarget
// override's job (driver_spell_or_permanent_test.go), not an answer shape.
func TestJeskaiRevelationCastCarriesBothSlots(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	item, sk := templates.Generate(reg, "Jeskai Revelation")
	if sk != nil {
		t.Fatalf("generator skipped the fixture: %s", sk.Reason)
	}
	if len(item.Steps) == 0 || len(item.Steps[0].Targets) != 2 {
		t.Fatalf("cast step must carry both slots' targets: %+v", item.Steps)
	}
	if item.XAnswers != nil {
		t.Fatalf("a spell's slots reach XMage through castSpell, not scripted answers: %v", item.XAnswers)
	}
}
