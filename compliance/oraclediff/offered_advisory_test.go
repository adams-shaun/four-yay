package oraclediff

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func TestCompareFreezeMeetsOfferedAdvisoryLabels(t *testing.T) {
	offer := func(source, kind, label string) rules.OracleSnapOffered {
		return rules.OracleSnapOffered{Source: source, Kind: kind, Label: label}
	}
	gMana := []rules.OracleSnapOffered{offer("p0:Verge", "activate", "Add B"), offer("p0:Verge", "activate", "Add R")}
	for _, tc := range []struct {
		name                 string
		g, x                 []rules.OracleSnapOffered
		agree, sameCanonical bool
	}{
		{"braces and case", []rules.OracleSnapOffered{offer("p0:Mountain", "activate", "Add R")}, []rules.OracleSnapOffered{offer("p0:Mountain", "activate", "Add {r}")}, true, true},
		{"display prefix", []rules.OracleSnapOffered{offer("p0:Bolt", "cast", "Cast Lightning Bolt")}, []rules.OracleSnapOffered{offer("p0:Bolt", "cast", "Lightning Bolt")}, true, true},
		{"singleton advisory", []rules.OracleSnapOffered{offer("p0:A", "activate", "Activate A")}, []rules.OracleSnapOffered{offer("p0:A", "activate", "Sacrifice A: Deal 1 damage")}, true, true},
		{"multiple named costs and ordering", gMana, []rules.OracleSnapOffered{offer("p0:Verge", "activate", "{T}: Add {R}"), offer("p0:Verge", "activate", "{T}: Add {B}")}, true, true},
		{"normalized rule prefix", gMana, []rules.OracleSnapOffered{offer("p0:Verge", "activate", "{T}: Add {R}. Activate only if you control a Swamp."), offer("p0:Verge", "activate", "{T}: Add {B}.")}, true, false},
		{"ambiguous prefix needs one-to-one matching", []rules.OracleSnapOffered{offer("p0:A", "activate", "Add"), offer("p0:A", "activate", "Add B")}, gManaForSource("p0:A"), true, false},
		{"missing source", gMana, []rules.OracleSnapOffered{offer("p1:Verge", "activate", "Add B"), offer("p0:Verge", "activate", "Add R")}, false, false},
		{"missing kind", gMana, []rules.OracleSnapOffered{offer("p0:Verge", "cast", "Add B"), offer("p0:Verge", "activate", "Add R")}, false, false},
		{"missing ability", gMana, gMana[:1], false, false},
		{"different ability on same source", gMana, []rules.OracleSnapOffered{offer("p0:Verge", "activate", "Add B"), offer("p0:Verge", "activate", "Add G")}, false, false},
		{"cannot reuse one matching ability", gMana, []rules.OracleSnapOffered{offer("p0:Verge", "activate", "Add B"), offer("p0:Verge", "activate", "Add B")}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if reflect.DeepEqual(tc.g, tc.x) {
				t.Fatal("precondition: engine vocabularies must differ")
			}
			res := func(offers []rules.OracleSnapOffered) rules.OracleResult {
				return rules.OracleResult{Snapshots: []rules.OracleSnapshot{{Checkpoint: "setup"}, {Checkpoint: "priority", Offered: offers}}}
			}
			g, x := res(tc.g), res(tc.x)
			compare := []string{CompareOffered}
			v := CompareOpts(g, nil, XResult{Snapshots: x.Snapshots}, compare)
			if (v.Status == Agree) != tc.agree || (!tc.agree && v.Field != "offered") {
				t.Errorf("Compare: %+v, want agree=%v", v, tc.agree)
			}
			if got := CanonicalOpts(g.Snapshots, compare) == CanonicalOpts(x.Snapshots, compare); got != tc.sameCanonical {
				t.Errorf("canonical normalized equality=%v, want %v", got, tc.sameCanonical)
			}
			for _, pair := range [][2]rules.OracleResult{{g, x}, {x, g}} {
				frozen := FreezeOpts(pair[0], compare)
				hasOffered := false
				for _, f := range frozen {
					hasOffered = hasOffered || f.Field == "offered"
				}
				if !hasOffered {
					t.Fatal("precondition: frozen observation must include offered")
				}
				if ok, why := MeetsOpts(frozen, pair[1], compare); ok != tc.agree {
					t.Errorf("Meets=%v (%s), want %v", ok, why, tc.agree)
				}
			}
		})
	}
}

func gManaForSource(source string) []rules.OracleSnapOffered {
	return []rules.OracleSnapOffered{{Source: source, Kind: "activate", Label: "Add B"}, {Source: source, Kind: "activate", Label: "Add R"}}
}
