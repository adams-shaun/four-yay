package cards

import (
	"strings"
	"testing"
)

func TestCraftCorpusShapeCensus(t *testing.T) {
	reg := compiledCorpus(t)
	uniform, exotic := 0, 0
	for _, c := range reg.Cards {
		for _, face := range c.Faces {
			for _, keyword := range face.Keywords {
				if KeywordHead(keyword) != "Craft" {
					continue
				}
				if CraftUniformShape(keywordParam(keyword)) {
					uniform++
					if containsPrimitive(face.Primitives(), "api:Craft.OtherShape") {
						t.Errorf("uniform Craft card %q has shape marker", face.Name)
					}
				} else {
					exotic++
					if !containsPrimitive(face.Primitives(), "api:Craft.OtherShape") {
						t.Errorf("exotic Craft card %q lacks shape marker", face.Name)
					}
				}
			}
		}
	}
	t.Logf("Craft corpus shape census: uniform=%d exotic=%d total=%d", uniform, exotic, uniform+exotic)
	if uniform != 20 || exotic != 4 {
		t.Fatalf("Craft shape counts = %d uniform, %d exotic; want 20/4", uniform, exotic)
	}
	stonetree, ok := reg.Lookup("Kaslem's Stonetree")
	if !ok || len(stonetree.Faces) == 0 {
		t.Fatal("precondition: corpus card Kaslem's Stonetree is missing")
	}
	foundCraft := false
	for _, ability := range stonetree.Faces[0].Abilities {
		if ability.API == "ChangeZone" && strings.Contains(ability.ParamStr(PKCost), "ExileCtrlOrGrave<1/Cave.Other>") {
			foundCraft = true
		}
	}
	if !foundCraft {
		t.Fatalf("Kaslem's Stonetree abilities = %+v, want expanded Cave Craft activation", stonetree.Faces[0].Abilities)
	}
}
