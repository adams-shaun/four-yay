package oraclegen_test

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func levelBItem(t *testing.T, card, key string) oraclegen.Item {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(card)
	if !ok {
		t.Fatalf("precondition: %s is not in the corpus", card)
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key != key {
			continue
		}
		it, skip := templates.GenerateB(reg, card, req)
		if skip != nil {
			t.Fatalf("GenerateB(%s %s): %s", card, key, skip.Reason)
		}
		return it
	}
	t.Fatalf("precondition: %s has no level-B requirement %s", card, key)
	return oraclegen.Item{}
}

// Swordsman, Sharp Scoundrel: the root's "up to one Equipment" slot has no
// candidate, gorge settles it empty, and XMage asks it FIRST -- so the skip
// must lead the creature pick instead of the pick being spent on the slot.
func TestLeadingUnposedLinkSkipsFirst(t *testing.T) {
	it := levelBItem(t, "Swordsman, Sharp Scoundrel", "trigger#0.0")
	want := []oraclegen.XAnswer{
		{Seat: 0, Kind: "target", Value: "[target_skip]"},
		{Seat: 0, Kind: "target", Value: "Swordsman, Sharp Scoundrel"},
	}
	var got []oraclegen.XAnswer
	for _, step := range it.XAnswers {
		got = append(got, step...)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xmage_answers = %+v, want %+v", got, want)
	}
}

// Jade Seedstones: XMage's Craft material is a TargetCardInGraveyardBattlefieldOr
// Stack, answered from the target queue by plain card name -- never a choice,
// never a "^" joined selection.
func TestCraftMaterialOnTargetQueue(t *testing.T) {
	it := levelBItem(t, "Jade Seedstones", "activate#0.0")
	if len(it.XAnswers) == 0 || len(it.XAnswers[0]) == 0 {
		t.Fatalf("precondition: the activate step scripts no answers: %+v", it.XAnswers)
	}
	first := it.XAnswers[0][0]
	want := oraclegen.XAnswer{Seat: 0, Kind: "target", Value: "Colossal Dreadmaw"}
	if first != want {
		t.Fatalf("craft material answer = %+v, want %+v", first, want)
	}
	for _, step := range it.XAnswers {
		for _, a := range step {
			if a.Kind == "choice" {
				t.Fatalf("a craft activation scripts no choice answer, got %+v", a)
			}
		}
	}
}
