package effects

import "testing"

// TestKeywordPredicatesReadPublishedDerivedKeywords pins the board-wide
// derived keyword table (SpecContext.Layers.DerivedKeywords, rules'
// EffectiveKeywords): a resolving effect's with<X>/without<X> filter sees a
// granted keyword and a lost one, through both the interpreted and the
// compiled matcher, while ExtraKeywords stays authoritative when bound and an
// object with no entry keeps the printed read.
func TestKeywordPredicatesReadPublishedDerivedKeywords(t *testing.T) {
	g, ids := board(t)
	bear, flier := ids["myBear"], ids["myFlier"]
	sc := SpecContext{You: 0, Layers: LayerTables{DerivedKeywords: []ObjectKeywords{
		{ID: bear, Keywords: []string{"Flying", "Double Strike"}}, // granted flying (Ajani's -3)
		{ID: flier, Keywords: nil},                                // lost all abilities
	}}}
	for _, tc := range []struct {
		spec string
		id   string
		want bool
	}{
		{"Creature.withoutFlying", "myBear", false},
		{"Creature.withFlying", "myBear", true},
		{"Creature.withoutFlying", "myFlier", true},
		{"Creature.withFlying", "myFlier", false},
		{"Creature.withoutFlying", "theirBig", true},
		{"Creature.withoutFlying+YouCtrl", "myBear", false},
	} {
		if got := MatchesSpecCtx(g, tc.spec, ids[tc.id], sc); got != tc.want {
			t.Errorf("%s on %s = %v, want %v", tc.spec, tc.id, got, tc.want)
		}
	}
	// Unbound: the printed read.
	if !MatchesSpecCtx(g, "Creature.withoutFlying", bear, SpecContext{You: 0}) {
		t.Error("without a table the printed (flightless) Bear must match withoutFlying")
	}
	// A bound per-candidate list outranks the table.
	sc.ExtraKeywords, sc.ExtraKeywordsOwner = []string{}, bear
	if !MatchesSpecCtx(g, "Creature.withoutFlying", bear, sc) {
		t.Error("a bound empty ExtraKeywords must outrank the table entry")
	}
}
