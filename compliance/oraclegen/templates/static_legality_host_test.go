package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestHostLegalityRowsServed pins the FDN rows the host-legality, life,
// mana-conversion, look-at and named-card templates serve (each verified
// AGREE against XMage when it landed).
func TestHostLegalityRowsServed(t *testing.T) {
	for _, c := range []struct{ name, key string }{
		{"Pacifism", "static#0.0"},
		{"Pacifism", "static#0.1"},
		{"Gate Colossus", "static#0.0"},
		{"Stromkirk Noble", "static#0.0"},
		{"Giant Cindermaw", "static#0.0"},
		{"Vizier of the Menagerie", "static#0.0"},
		{"Vizier of the Menagerie", "static#0.2"},
		{"Sorcerous Spyglass", "static#0.0"},
		{"Ball Lightning", "combat#0.block"},
	} {
		it := generateBKey(t, c.name, c.key)
		if it.ID != c.name+"/"+c.key+"/v1" {
			t.Errorf("%s %s: generated %q", c.name, c.key, it.ID)
		}
	}
}

// The Spyglass item scripts XMage's card-name answer, which gorge's
// one-name legacy NameCard never poses as a choice.
func TestNamedCardItemScriptsTheName(t *testing.T) {
	it := generateBKey(t, "Sorcerous Spyglass", "static#0.0")
	found := false
	for _, step := range it.XAnswers {
		for _, a := range step {
			if a == (oraclegen.XAnswer{Seat: 0, Kind: "choice", Value: activatedProbe}) {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no %q name answer in %+v", activatedProbe, it.XAnswers)
	}
}
