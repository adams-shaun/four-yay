package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestCloneKnownKeysSorted: the unread lookup binary-searches the table.
func TestCloneKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(cloneKnownKeys[:]) || len(slices.Compact(slices.Clone(cloneKnownKeys[:]))) != len(cloneKnownKeys) {
		t.Fatal("cloneKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileClone pins the compiled shapes the resolution reads: the source
// and become selectors, the ChoiceZone$ classification, the modifier lists
// and flags, the SetColor$ parse, the ETB whitelist's text half and the
// unread report.
func TestCompileClone(t *testing.T) {
	sa := &cards.SA{API: "Clone", Params: map[string]string{
		"Defined": " Targeted ", "Choices": " Creature.Other ", "ChoiceZone": " Graveyard ", "ChoiceOptional": "True",
		"ChoiceTitle": " Pick ", "CloneTarget": " Self ", "ExcludeChosen": "true", "CloneZone": " Battlefield ",
		"Optional": "True", "AddTypes": "Legendary & Spirit", "SetCreatureTypes": "Frog", "NonLegendary": "True",
		"RemoveSubTypes": "True", "AddSVars": "A, B", "AddTriggers": "TrigA,TrigB", "AddStaticAbilities": "S1, S2",
		"AddKeywords": "Flying & Haste", "PumpKeywords": "Trample", "PumpDuration": " EOT ", "NewName": " Kiki ",
		"GainThisAbility": "True", "SetPower": "X", "SetColor": "Blue", "IntoPlayTapped": " True",
		"Duration": " UntilEndOfTurn ", "AttachedTo": " Remembered ", "FaceDown": "True", "KeepFacedown": "False",
		"Hidden": "True",
	}}
	p := CloneOf(sa)
	if p.Defined != "Targeted" || p.Choices != "Creature.Other" || p.ChoicesRaw != " Creature.Other " ||
		p.ChoiceZone != "Graveyard" || p.ChoiceZoneKind != state.ZGraveyard || !p.ChoiceZoneOK ||
		!p.ChoiceOptional || p.ChoiceTitle != "Pick" || p.CloneTarget != "Self" || !p.ExcludeChosen ||
		p.CloneZone != "Battlefield" || !p.Optional || p.CopyFromChosenName {
		t.Fatalf("selectors = %+v", p)
	}
	if !slices.Equal(p.TypeAdds, []string{"Legendary", "Spirit", "Frog"}) || !p.SetCreatureTypes || !p.NonLegendary ||
		!p.RemoveSubTypes || p.RemoveCardTypes || !slices.Equal(p.AddSVars, []string{"A", "B"}) ||
		!slices.Equal(p.AddTriggers, []string{"TrigA", "TrigB"}) || !slices.Equal(p.StaticNames, []string{"S1", "S2"}) ||
		!slices.Equal(p.AddKeywords, []string{"Flying", "Haste"}) || !slices.Equal(p.PumpKeywords, []string{"Trample"}) ||
		p.PumpDuration != "EOT" || p.NewName != "Kiki" || !p.GainThisAbility {
		t.Fatalf("modifiers = %+v", p)
	}
	if p.SetPower != (ParamText{Text: "X", Present: true}) || p.SetToughness.Present ||
		p.SetColor != "Blue" || !p.SetColorOK || !slices.Equal(p.SetColors, []string{"U"}) {
		t.Fatalf("P/T and colour = %+v", p)
	}
	if p.IntoPlayTapped != " True" || !p.IntoPlayTappedSet || p.IntoPlayTappedTrue || p.Duration != "UntilEndOfTurn" ||
		p.AttachedTo != "Remembered" || !p.FaceDown || !p.KeepFacedownFalse || p.ETBShapeOK {
		t.Fatalf("riders = %+v", p)
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if CloneOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	etb := CloneOf(&cards.SA{API: "Clone", Params: map[string]string{
		"Choices": "Creature.Other", "AddTypes": "Spirit", "AddKeywords": "Flying", "IntoPlayTapped": "True",
		"SpellDescription": "x"}})
	if !etb.ETBShapeOK || !etb.ChoiceZoneOK || etb.ChoiceZoneKind != state.ZBattlefield {
		t.Fatalf("ETB shape = %+v", etb)
	}
	// ETBShapeOK checks the body's text; the caller also checks that the
	// source face binds Y to Count$CastTotalManaSpent before offering it.
	if !CloneOf(&cards.SA{API: "Clone", Params: map[string]string{"Choices": "Creature.Other+cmcLEY"}}).ETBShapeOK {
		t.Fatal("Mockingbird's cast-spend selector must pass the text-only ETB gate")
	}
	for _, params := range []map[string]string{
		{"Choices": "Creature.Other+cmcLEX"},
		{"Choices": "Creature", "AddKeywords": "IfNew Vanishing:3"},
		{"Choices": "Creature", "IntoPlayTapped": "False"},
		{"Choices": "Creature", "ChoiceTitle": "Pick"},
	} {
		if CloneOf(&cards.SA{API: "Clone", Params: params}).ETBShapeOK {
			t.Errorf("ETB shape admitted %v", params)
		}
	}
	bad := CloneOf(&cards.SA{API: "Clone", Params: map[string]string{"ChoiceZone": "Library", "SetColor": "Plaid"}})
	if bad.ChoiceZoneOK || bad.SetColorOK {
		t.Fatalf("fail-closed shapes = %+v", bad)
	}
}

// TestCloneOfIsAllocationFree: a configured record or a front-cache hit
// allocates nothing.
func TestCloneOfIsAllocationFree(t *testing.T) {
	bound := slottedSA(t, "Clone", map[string]string{"Defined": "Targeted", "Duration": "UntilEndOfTurn"})
	f := NewSAFacts(bound)
	f.Publish()
	if LoadSAFacts(bound) != f {
		t.Fatal("precondition: the configured record is not published on bound's facts slot")
	}
	cached := &cards.SA{API: "Clone", Params: map[string]string{"Choices": "Creature.Other"}}
	if n := allocsPerRun(100, func() { _ = CloneOf(bound) }); n != 0 {
		t.Fatalf("configured CloneOf allocated %v objects per run; want 0", n)
	}
	// The front cache is direct-mapped and shared with parallel corpus tests.
	// Prime this slot immediately before each measured lookup: an eviction
	// between separate iterations is not an allocation on a cache HIT.
	p := CloneOf(cached)
	slot := &cloneFront[paramMapSlot(cached.Params)]
	if n := allocsPerRun(100, func() {
		slot.Store(p)
		_ = CloneOf(cached)
	}); n != 0 {
		t.Fatalf("front-cache hit allocated %v objects per run; want 0", n)
	}
	if f.Clone == nil || !f.Clone.boundTo(bound.Params) {
		t.Fatal("NewSAFacts did not compile the Clone half")
	}
}
