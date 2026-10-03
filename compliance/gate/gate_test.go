package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
)

// TestDeclaredSetsCompliant is the compliance claim (spec section 8): every
// set in compliance/declared.json meets its level at this head. It runs
// only declared sets, so its cost grows with the claim, not the corpus. A
// missing corpus fails rather than skips: a declared set cannot be checked
// without it.
func TestDeclaredSetsCompliant(t *testing.T) {
	root := filepath.Join("..", "..")
	declared, err := compliance.LoadDeclared(filepath.Join(root, "compliance", "declared.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(declared) == 0 {
		t.Log("no declared sets")
		return
	}
	reg, err := cards.LoadRegistry(cards.CachePath(filepath.Join(root, ".cards")))
	if err != nil {
		t.Fatalf("declared sets need the corpus (make fetch-cards compile-cards): %v", err)
	}
	for set, level := range declared {
		probs, err := Check(reg, root, set, level)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		for _, p := range probs {
			t.Errorf("%s:%s %s: %s", set, level, p.Card, p.Reason)
		}
	}
}

func TestDeclaredFileIsWellFormed(t *testing.T) {
	d, err := compliance.LoadDeclared(filepath.Join("..", "..", "compliance", "declared.json"))
	if err != nil {
		t.Fatal(err)
	}
	for set := range d {
		if _, err := os.Stat(filepath.Join("..", "..", "compliance", "manifests", compliance.ManifestFileName(set))); err != nil {
			t.Errorf("declared set %s has no manifest", set)
		}
		if strings.ToUpper(set) != set {
			t.Errorf("declared set %q: codes are upper case", set)
		}
	}
}
