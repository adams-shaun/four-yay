package cards

import (
	"strings"
	"testing"
)

func TestCraftShapeAndExpansion(t *testing.T) {
	uniform := "5 G ExileCtrlOrGrave<1/Cave.Other>"
	if !CraftUniformShape(uniform) {
		t.Fatalf("uniform corpus form rejected: %q", uniform)
	}
	if !CraftUniformShape("5 XMin1 ExileCtrlOrGrave<X/Permanent.Other/permanent>") {
		t.Fatal("uniform material with Forge's display description rejected")
	}
	for _, exotic := range []string{
		"3 R R XMin4 ExileFromGrave<X/Instant.Red+Other;Sorcery.Red+Other>",
		"4 ExileCtrlOrGrave<1/Dinosaur.Other> ExileCtrlOrGrave<1/Merfolk.Other>",
		"6 ExileCtrlOrGrave<2/Permanent.Other+withSharedCardType>",
		"8 U XMin4 ExileCtrlOrGrave<X/Permanent.Other+nonLand+hasAbility Activated>",
	} {
		if CraftUniformShape(exotic) {
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
	bad := &Face{Keywords: []string{"Craft:4 ExileCtrlOrGrave<1/Dinosaur.Other> ExileCtrlOrGrave<1/Merfolk.Other>"}}
	if !containsPrimitive(bad.Primitives(), "api:Craft.OtherShape") {
		t.Fatal("multi-material carrier lacks fail-closed Craft shape marker")
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
