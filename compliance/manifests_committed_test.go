package compliance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The committed manifests are the compliance denominator: each must sit at
// ManifestFileName(code) (a path Go's module zip accepts) and list at least
// one card.
func TestCommittedManifestsAreWellFormed(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("manifests", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 500 {
		t.Fatalf("only %d committed manifests; the glob is not reaching compliance/manifests", len(paths))
	}
	for _, p := range paths {
		base := filepath.Base(p)
		if reservedWindowsName(strings.TrimSuffix(base, ".json")) {
			t.Errorf("%s: a Windows reserved name, which Go's module zip refuses", base)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var m Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			t.Errorf("%s: %v", base, err)
			continue
		}
		if want := ManifestFileName(m.Code); base != want {
			t.Errorf("%s holds set %q, want file %s", base, m.Code, want)
		}
		if len(m.Cards) == 0 {
			t.Errorf("%s: no cards", base)
		}
		// Spot checks at XMAGE_REF 6b602a1c: Bloomburrow.java comments
		// Heirloom Epic out; HasCon2017.java's two cards use the qualified
		// constructor.
		for _, c := range m.Cards {
			if m.Code == "BLB" && c.Name == "Heirloom Epic" {
				t.Errorf("BLB lists Heirloom Epic, which its set class comments out")
			}
		}
		if m.Code == "H17" && len(m.Cards) != 2 {
			t.Errorf("H17 has %d cards, want 2", len(m.Cards))
		}
	}
}
