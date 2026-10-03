package compliance

import "testing"

func TestCorpusName(t *testing.T) {
	corpus := map[string]bool{"Lightning Bolt": true, "Fire // Ice": true, "Delver of Secrets": true}
	has := func(n string) bool { return corpus[n] }
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"Lightning Bolt", "Lightning Bolt", true},
		{"Fire // Ice", "Fire // Ice", true},
		{"Delver of Secrets // Insectile Aberration", "Delver of Secrets", true},
		{"Academic Ascent", "", false},
	} {
		got, ok := CorpusName(has, tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("CorpusName(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
