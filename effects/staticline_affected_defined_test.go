package effects

import "testing"

// A DB$ Effect's StaticAbilities$ SVar is parsed here, not by cards, so it
// needs the same AffectedDefined$ rewrite the printed S: lines get (44 such
// SVar bodies at FORGE_REF fb4d809).
func TestParseStaticLineNormalizesAffectedDefined(t *testing.T) {
	svars := map[string]string{"STPump": "Mode$ Continuous | AffectedDefined$ Enchanted | Affected$ Creature | AddPower$ 2 | Description$ Fixture."}
	mode, params := parseStaticLine(svars, "STPump")
	if mode != "Continuous" || params["Affected"] != "Creature.EnchantedBy" {
		t.Fatalf("mode %q Affected %q, want Continuous Creature.EnchantedBy", mode, params["Affected"])
	}
	if _, ok := params["AffectedDefined"]; ok {
		t.Fatal("AffectedDefined still present")
	}
}
