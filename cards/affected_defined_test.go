package cards

import "testing"

// Upstream Forge (between FORGE_REF 95f04e8 and fb4d809) moved ~2,600 static
// lines from `Affected$ <Type>.EnchantedBy` (EquippedBy, Self, AttachedBy) to
// `AffectedDefined$ Enchanted` plus an optional `Affected$ <Type>` filter.
// Every case below is a shape measured in the corpus at fb4d809, with the
// legacy Affected$ the same card carried at 95f04e8.
func TestNormalizeAffectedDefined(t *testing.T) {
	for _, tc := range []struct {
		defined, affected string
		want              string
	}{
		{"Self", "", "Card.Self"},
		{"Enchanted", "Creature", "Creature.EnchantedBy"},
		{"Enchanted", "", "Card.EnchantedBy"},
		{"Equipped", "Creature", "Creature.EquippedBy"},
		{"Equipped", "", "Card.EquippedBy"},
		{"Self", "Card.counters_GE8_CHARGE", "Card.Self+counters_GE8_CHARGE"},
		{"Enchanted", "Creature.White", "Creature.EnchantedBy+White"},
		{"Equipped", "Card.Legendary", "Card.EquippedBy+Legendary"},
		{"AttachedBy Self", "Land", "Land.AttachedBy"},
		{"AttachedBy Self", "", "Card.AttachedBy"},
		{"Self", "Card.AttachedTo Creature", "Card.Self+AttachedTo Creature"},
	} {
		p := map[string]string{"Mode": "Continuous", "AffectedDefined": tc.defined}
		if tc.affected != "" {
			p["Affected"] = tc.affected
		}
		NormalizeAffectedDefined(p)
		if p["Affected"] != tc.want {
			t.Errorf("AffectedDefined$ %q + Affected$ %q -> Affected$ %q, want %q", tc.defined, tc.affected, p["Affected"], tc.want)
		}
		if _, ok := p["AffectedDefined"]; ok {
			t.Errorf("AffectedDefined$ %q: key still present after normalizing", tc.defined)
		}
	}
}

// A value the rewrite does not know stays exactly as written, so the param
// census keeps reporting it instead of the static silently changing scope.
func TestNormalizeAffectedDefinedLeavesUnknownShapesLoud(t *testing.T) {
	for _, p := range []map[string]string{
		{"AffectedDefined": "EffectSource"},
		{"AffectedDefined": "Enchanted", "Affected": "Creature,Planeswalker"},
	} {
		before := map[string]string{}
		for k, v := range p {
			before[k] = v
		}
		NormalizeAffectedDefined(p)
		for k, v := range before {
			if p[k] != v {
				t.Errorf("%v: %s changed to %q", before, k, p[k])
			}
		}
	}
	p := map[string]string{"Affected": "Creature.YouCtrl"}
	NormalizeAffectedDefined(p)
	if p["Affected"] != "Creature.YouCtrl" || len(p) != 1 {
		t.Errorf("a line with no AffectedDefined$ was changed: %v", p)
	}
}

func TestStaticParsersNormalizeAffectedDefined(t *testing.T) {
	// A synthetic fixture in the corpus shape, not any card's script.
	body := "Mode$ Continuous | AffectedDefined$ Equipped | Affected$ Creature | AddKeyword$ Haste | Description$ Fixture."
	st, ok := ParseStaticLine(body)
	if !ok || st.Params["Affected"] != "Creature.EquippedBy" {
		t.Errorf("ParseStaticLine: ok=%v Affected=%q", ok, st.Params["Affected"])
	}
	sts, ok := ParseStaticLines(body)
	if !ok || len(sts) != 1 || sts[0].Params["Affected"] != "Creature.EquippedBy" {
		t.Errorf("ParseStaticLines: ok=%v %+v", ok, sts)
	}
	f, _ := ParseBytes("fixture.txt", []byte("Name:Fixture Equipment\nManaCost:2\nTypes:Artifact Equipment\nS:"+body+"\n"))
	if f == nil || len(f.Faces) == 0 || len(f.Faces[0].Statics) != 1 || f.Faces[0].Statics[0].Params["Affected"] != "Creature.EquippedBy" {
		t.Fatalf("printed S: line not normalized: %+v", f)
	}
}
