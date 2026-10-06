package templates

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestTemplatesAreDistinctAndVersioned(t *testing.T) {
	seen := map[string]bool{}
	for _, tp := range All {
		if tp.ID == "" || tp.Version < 1 || seen[tp.ID] {
			t.Errorf("template %+v: want a unique id and a version >= 1", tp)
		}
		seen[tp.ID] = true
	}
}

// TestGenerateNamesTheTemplateVersion: each template's own version is in
// its scenarios' ids and names, so a bump stales only that template.
func TestGenerateNamesTheTemplateVersion(t *testing.T) {
	reg, err := cards.SharedCorpus(filepath.Join("..", "..", "..", ".cards"))
	if err != nil {
		t.Fatalf("the generator needs the corpus (make fetch-cards compile-cards): %v", err)
	}
	for card, tp := range map[string]Template{"Forest": PlayLand, "Shock": CastResolve, "Counterspell": CounterSpell} {
		it, skip := Generate(reg, card)
		if skip != nil {
			t.Fatalf("%s: %s", card, skip.Reason)
		}
		if it.Template != tp.ID || it.ID != fmt.Sprintf("%s/%s/v%d", card, tp.ID, tp.Version) ||
			!strings.HasPrefix(it.Name, fmt.Sprintf("gen%d-", tp.Version)) {
			t.Errorf("%s: %s %s %s, want template %+v", card, it.Template, it.ID, it.Name, tp)
		}
	}
}
