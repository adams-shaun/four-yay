package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/internal/testutil"
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

// TestGateRefusesSetWithoutPrintedList is section 11.3 C7: a set with no
// compliance/printed list cannot be declared, because the XMage manifest
// the gate would fall back to omits the cards XMage lacks. Every other
// problem is still reported, so status stays useful for such a set.
func TestGateRefusesSetWithoutPrintedList(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..")
	if _, err := compliance.LoadPrinted(filepath.Join(root, "compliance", "printed"), "G00"); err == nil {
		t.Fatal("G00 has a printed list; pick a set without one")
	}
	probs, err := Check(reg, root, "G00", "A")
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) == 0 || probs[0].Card != "*" || !strings.Contains(probs[0].Reason, "no printed list") {
		t.Fatalf("G00 without a printed list: problems %v, want a leading \"*\" no-printed-list refusal", probs)
	}
	if fra, err := Check(reg, root, "FRA", "A"); err != nil {
		t.Fatal(err)
	} else {
		for _, p := range fra {
			if strings.Contains(p.Reason, "no printed list") {
				t.Errorf("FRA has a printed list but was refused: %v", p)
			}
		}
	}
}
