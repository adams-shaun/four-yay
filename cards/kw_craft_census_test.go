package cards

import (
	"strings"
	"testing"
)

// craftShapeName names one corpus Craft material grammar. The census below is
// the durable record of which shapes this build reads: a shape the classifier
// rejects keeps api:Craft.OtherShape and stays NAMED here, so a future corpus
// addition cannot silently flip a card from "supported" to "quietly never
// offered".
func craftShapeName(param string) string {
	body := craftMaterialBody(param)
	if strings.Contains(body, "ExileFromGrave<") {
		return "grave" // a graveyard-only material head, not ExileCtrlOrGrave
	}
	if strings.Count(body, "ExileCtrlOrGrave<") >= 2 {
		return "multi" // Throne of the Grim Captain: one distinct card per slot
	}
	for _, pred := range craftGroupPredicates {
		if strings.Contains(body, pred) {
			return "group" // a set-relation predicate the per-object matcher cannot read
		}
	}
	if strings.Contains(body, "XMin") || strings.Contains(body, "<X/") {
		return "xmin" // The Enigma Jewel: "four or more ...", an announced count with a floor
	}
	return "fixed" // the plain ExileCtrlOrGrave<N/Spec> carrier
}

func TestCraftCorpusShapeCensus(t *testing.T) {
	reg := compiledCorpus(t)
	counts := map[string]int{}
	for _, c := range reg.Cards {
		for _, face := range c.Faces {
			for _, keyword := range face.Keywords {
				if KeywordHead(keyword) != "Craft" {
					continue
				}
				param := keywordParam(keyword)
				shape := craftShapeName(param)
				counts[shape]++
				supported := CraftShapeSupported(param)
				if containsPrimitive(face.Primitives(), "api:Craft.OtherShape") == supported {
					t.Errorf("Craft card %q shape %s: marker=%v supported=%v disagree",
						face.Name, shape, !supported, supported)
				}
			}
		}
	}
	t.Logf("Craft corpus shape census: fixed=%d xmin=%d multi=%d (supported) grave=%d group=%d (named unsupported) total=%d",
		counts["fixed"], counts["xmin"], counts["multi"], counts["grave"], counts["group"],
		counts["fixed"]+counts["xmin"]+counts["multi"]+counts["grave"]+counts["group"])
	if counts["fixed"] != 16 || counts["xmin"] != 5 || counts["multi"] != 1 ||
		counts["grave"] != 1 || counts["group"] != 1 {
		t.Fatalf("Craft shape counts = %+v; want fixed=16 xmin=5 multi=1 grave=1 group=1", counts)
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
