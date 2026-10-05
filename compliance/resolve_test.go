package compliance

import "testing"

func TestCorpusName(t *testing.T) {
	corpus := map[string]bool{"Lightning Bolt": true, "Fire // Ice": true, "Delver of Secrets": true}
	folded := map[string]string{
		FoldName("Lightning Bolt"):         "Lightning Bolt",
		FoldName("Fire // Ice"):            "Fire // Ice",
		FoldName("Delver of Secrets"):      "Delver of Secrets",
		FoldName("Dáin Ironfoot"):          "Dáin Ironfoot",
		FoldName("With Great Power . . ."): "With Great Power . . .",
	}
	has := func(n string) bool { return corpus[n] }
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"Lightning Bolt", "Lightning Bolt", true},
		{"Fire // Ice", "Fire // Ice", true},
		{"Delver of Secrets // Insectile Aberration", "Delver of Secrets", true},
		{"Academic Ascent", "", false},
		// Folded matches: XMage's ASCII spelling finds the corpus's accented
		// name, and vice versa.
		{"Dain Ironfoot", "Dáin Ironfoot", true},
		{"With Great Power...", "With Great Power . . .", true},
	} {
		got, ok := CorpusNameFold(has, folded, tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("CorpusNameFold(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestFoldName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Dáin Ironfoot", "dainironfoot"},
		{"Dain Ironfoot", "dainironfoot"},
		{"With Great Power . . .", "withgreatpower"},
		{"With Great Power...", "withgreatpower"},
		{"Bespoke Bō", "bespokebo"},
		{"Araña, Heart of the Spider", "aranaheartofthespider"},
		{"Óin the Brave", "ointhebrave"},
		{"Mjölnir, Hammer of Thor", "mjolnirhammerofthor"},
		{"Bartolomé del Presidio", "bartolomedelpresidio"},
		{"Urza's Tower", "urzastower"},
		{"", ""},
	} {
		if got := FoldName(tc.in); got != tc.want {
			t.Errorf("FoldName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
