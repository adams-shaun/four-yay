// keyword_cost_param_test.go — the KeywordWithCost cost read.
//
// Forge's KeywordWithCost.parse (forge-game/.../keyword/KeywordWithCost.java:22)
// reads a cost-bearing keyword's cost as the FIRST colon-field and cuts an
// optional "|…" tail:
//
//	String[] allDetails = details.split(":");
//	costString = allDetails[0].split("\\|", 2)[0].trim();
//
// KeywordParam returns everything after the first colon; handing that whole
// remainder to ParseCost polluted a shaped Disguise/Flashback line with the
// trailing ReduceCost$ SVar name and its reminder text. KeywordCostParam is
// that first-field read, in one home.
package cards

import "testing"

func TestKeywordCostParamTakesTheFirstColonField(t *testing.T) {
	c, _ := ParseBytes("k.txt", []byte(
		"Name:K\nTypes:Creature\n"+
			"K:Disguise:5 R:X:This cost is reduced by {1} for each instant and sorcery card in your graveyard.\n"+
			"K:Flashback:8 U U:ReduceCost$ X:This spell costs {X} less to cast this way.\n"+
			"K:Morph:X B B\n"+
			"K:Megamorph:4 G|alt reminder\n"+
			"K:Flash\n"+
			"Oracle:x\n"))
	f := c.Faces[0]

	// The precondition that makes the assertions non-vacuous: KeywordParam
	// really does return the whole remainder for these lines, so a plain
	// KeywordParam read would differ from KeywordCostParam.
	if raw, ok := f.KeywordParam("Disguise"); !ok || raw == "5 R" {
		t.Fatalf("precondition: KeywordParam(Disguise) = %q %v, want the whole remainder", raw, ok)
	}
	if raw, ok := f.KeywordParam("Flashback"); !ok || raw == "8 U U" {
		t.Fatalf("precondition: KeywordParam(Flashback) = %q %v, want the whole remainder", raw, ok)
	}

	for _, tc := range []struct {
		head, want string
	}{
		{"Disguise", "5 R"},
		{"Flashback", "8 U U"},
		{"Morph", "X B B"},
		{"Megamorph", "4 G"},
		{"Flash", ""},
	} {
		if got, ok := f.KeywordCostParam(tc.head); !ok || got != tc.want {
			t.Errorf("KeywordCostParam(%q) = %q %v, want %q true", tc.head, got, ok, tc.want)
		}
	}
	if _, ok := f.KeywordCostParam("Delve"); ok {
		t.Error("absent keyword reported present")
	}
}

// TestKeywordCostFieldIsTheSplitOneHome pins the shared split used by a reader
// holding a derived/granted keyword line rather than a Face.
func TestKeywordCostFieldIsTheSplitOneHome(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"5 R:X:reminder", "5 R"},
		{"8 U U:ReduceCost$ X:rem", "8 U U"},
		{"X B B", "X B B"},
		{"4 G|alt", "4 G"},
		{"", ""},
	} {
		if got := KeywordCostField(tc.in); got != tc.want {
			t.Errorf("KeywordCostField(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
