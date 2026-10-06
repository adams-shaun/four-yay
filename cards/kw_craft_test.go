package cards

import (
	"strings"
	"testing"
)

func TestCraftShapeAndExpansion(t *testing.T) {
	uniform := "5 G ExileCtrlOrGrave<1/Cave.Other>"
	if !CraftShapeSupported(uniform) {
		t.Fatalf("uniform corpus form rejected: %q", uniform)
	}
	if !CraftShapeSupported("5 XMin1 ExileCtrlOrGrave<X/Permanent.Other/permanent>") {
		t.Fatal("uniform material with Forge's display description rejected")
	}
	// The two shapes this slice adds: the X-minimum material (The Enigma
	// Jewel, "four or more nonlands with activated abilities") and the
	// multi-slot material (Throne of the Grim Captain, one card per typed
	// slot), whose parameter carries Forge's trailing display prose.
	for _, supported := range []string{
		"8 U XMin4 ExileCtrlOrGrave<X/Permanent.Other+nonLand+hasAbility Activated/nonlands with activated abilities>",
		"4 ExileCtrlOrGrave<1/Dinosaur.Other> ExileCtrlOrGrave<1/Merfolk.Other>",
		"4 ExileCtrlOrGrave<1/Dinosaur.Other> ExileCtrlOrGrave<1/Merfolk.Other> ExileCtrlOrGrave<1/Pirate.Other> ExileCtrlOrGrave<1/Vampire.Other>:a Dinosaur, a Merfolk, a Pirate, and a Vampire:the four",
	} {
		if !CraftShapeSupported(supported) {
			t.Errorf("supported Craft shape rejected: %q", supported)
		}
	}
	for _, exotic := range []string{
		"3 R R XMin4 ExileFromGrave<X/Instant.Red+Other;Sorcery.Red+Other>",
		"6 ExileCtrlOrGrave<2/Permanent.Other+withSharedCardType>",
	} {
		if CraftShapeSupported(exotic) {
			t.Errorf("exotic Craft shape accepted: %q", exotic)
		}
	}

	f := &Face{Keywords: []string{"Craft:" + uniform}}
	f.expandKeywords()
	if len(f.Abilities) != 1 || f.Abilities[0].API != "ChangeZone" {
		t.Fatalf("Craft abilities = %+v, want one ChangeZone ability", f.Abilities)
	}
	if !strings.Contains(f.Abilities[0].ParamStr(PKCost), "ExileCtrlOrGrave<1/Cave.Other>") ||
		!strings.Contains(f.Abilities[0].ParamStr(PKCost), "Exile<1/CARDNAME>") {
		t.Fatalf("Craft cost = %q", f.Abilities[0].ParamStr(PKCost))
	}
	if got := f.Primitives(); containsPrimitive(got, "api:Craft.OtherShape") {
		t.Fatalf("uniform carrier received exotic marker: %v", got)
	}

	// X-minimum: the announced-X exile count and its floor must reach the
	// installed ability verbatim, and the marker must be gone.
	xmin := &Face{Keywords: []string{"Craft:8 U XMin4 ExileCtrlOrGrave<X/Permanent.Other+nonLand+hasAbility Activated/nonlands with activated abilities>"}}
	xmin.expandKeywords()
	if len(xmin.Abilities) != 1 || !strings.Contains(xmin.Abilities[0].ParamStr(PKCost), "XMin4 ExileCtrlOrGrave<X/Permanent.Other+nonLand+hasAbility Activated/nonlands with activated abilities>") {
		t.Fatalf("Xmin Craft cost = %+v", xmin.Abilities)
	}
	if got := xmin.Primitives(); containsPrimitive(got, "api:Craft.OtherShape") {
		t.Fatalf("Xmin carrier received exotic marker: %v", got)
	}

	// Multi-slot: every slot reaches the cost and the trailing description
	// prose is stripped, never parsed as a cost token.
	multi := &Face{Keywords: []string{"Craft:4 ExileCtrlOrGrave<1/Dinosaur.Other> ExileCtrlOrGrave<1/Merfolk.Other> ExileCtrlOrGrave<1/Pirate.Other> ExileCtrlOrGrave<1/Vampire.Other>:a Dinosaur, a Merfolk, a Pirate, and a Vampire:the four"}}
	multi.expandKeywords()
	if len(multi.Abilities) != 1 {
		t.Fatalf("multi-slot Craft abilities = %+v, want one", multi.Abilities)
	}
	cost := multi.Abilities[0].ParamStr(PKCost)
	if strings.Contains(cost, "Dinosaur, a Merfolk") || strings.Count(cost, "ExileCtrlOrGrave<") != 4 {
		t.Fatalf("multi-slot Craft cost = %q, want four slots and no display prose", cost)
	}
	if got := multi.Primitives(); containsPrimitive(got, "api:Craft.OtherShape") {
		t.Fatalf("multi-slot carrier received exotic marker: %v", got)
	}

	bad := &Face{Keywords: []string{"Craft:6 ExileCtrlOrGrave<2/Permanent.Other+withSharedCardType>"}}
	if !containsPrimitive(bad.Primitives(), "api:Craft.OtherShape") {
		t.Fatal("group-predicate carrier lacks fail-closed Craft shape marker")
	}
}

func containsPrimitive(primitives []string, want string) bool {
	for _, p := range primitives {
		if p == want {
			return true
		}
	}
	return false
}
