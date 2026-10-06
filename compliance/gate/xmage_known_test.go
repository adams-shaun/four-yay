package gate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
)

func TestXMageKnownInstalledBeforeLevelBGeneration(t *testing.T) {
	root := filepath.Join("..", "..")
	reg, err := cards.SharedCorpus(filepath.Join(root, ".cards"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadSet(reg, root, "FIN"); err != nil {
		t.Fatal(err)
	}
	if oraclegen.XMageKnown("1996 World Champion") {
		t.Fatal("gate setup left the permissive probe heuristic installed")
	}

	card := "Torgal, A Fine Hound"
	c, ok := reg.Lookup(card)
	if !ok {
		t.Fatalf("corpus precondition: %q is absent", card)
	}
	var checked bool
	for _, req := range levelb.Requirements(c) {
		it, skip := templates.GenerateB(reg, card, req)
		if skip != nil {
			continue
		}
		checked = true
		if strings.Contains(string(it.Raw()), "1996 World Champion") {
			t.Fatalf("gate-generated %s fixture still uses unknown probe %q", req.Key, "1996 World Champion")
		}
	}
	if !checked {
		t.Fatal("precondition: Torgal has no generated level-B scenario")
	}
}
