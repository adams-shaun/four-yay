package templates_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/gate"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
)

func corpusReg(t *testing.T) *cards.Registry {
	t.Helper()
	reg, err := cards.LoadRegistry(cards.CachePath(filepath.Join("..", "..", "..", ".cards")))
	if err != nil {
		t.Fatalf("the generator needs the corpus (make fetch-cards compile-cards): %v", err)
	}
	return reg
}

// TestItemForLevelAMatchesGenerate: a level-A template string returns exactly
// the item Generate makes, hash included, so rule/triage/refreeze score the
// same scenario they did before ItemFor existed.
func TestItemForLevelAMatchesGenerate(t *testing.T) {
	reg := corpusReg(t)
	want, skip := templates.Generate(reg, "Shock")
	if skip != nil {
		t.Fatalf("Shock: %s", skip.Reason)
	}
	got, skip := templates.ItemFor(reg, "Shock", "cast-resolve")
	if skip != nil {
		t.Fatalf("ItemFor(Shock, cast-resolve): %s", skip.Reason)
	}
	if got.ID != want.ID || got.Template != want.Template {
		t.Fatalf("ItemFor = %s/%s, Generate = %s/%s", got.ID, got.Template, want.ID, want.Template)
	}
	if gate.ItemSHA(got) != gate.ItemSHA(want) {
		t.Errorf("item shas differ: %s vs %s", gate.ItemSHA(got), gate.ItemSHA(want))
	}
}

// TestItemForLevelBKeyReachesGenerateB: a real card's requirement key
// resolves through levelb.Requirements and returns exactly what GenerateB
// makes for it -- a served scenario once its template lands, a skip
// otherwise. The card is asserted to carry the requirement first, so a card
// with none would not make the key lookup fail for the wrong reason.
func TestItemForLevelBKeyReachesGenerateB(t *testing.T) {
	reg := corpusReg(t)
	const card = "Prodigal Sorcerer"
	c, ok := reg.Lookup(card)
	if !ok {
		t.Fatalf("%s is not in the corpus", card)
	}
	reqs := levelb.Requirements(c)
	if len(reqs) == 0 {
		t.Fatalf("%s has no level-B requirement, so no key can address it", card)
	}
	key := reqs[0].Key
	if key != "activate#0.0" {
		t.Fatalf("%s first requirement is %s, want activate#0.0", card, key)
	}
	got, skip := templates.ItemFor(reg, card, key)
	want, wantSkip := templates.GenerateB(reg, card, reqs[0])
	if (skip == nil) != (wantSkip == nil) {
		t.Fatalf("ItemFor and GenerateB disagree on serving %s: skip=%v wantSkip=%v", key, skip, wantSkip)
	}
	if skip != nil {
		if skip.Reason != wantSkip.Reason {
			t.Errorf("ItemFor skip %q != GenerateB skip %q", skip.Reason, wantSkip.Reason)
		}
		if strings.Contains(skip.Reason, "no template for activate") {
			t.Errorf("activate template did not land: %q", skip.Reason)
		}
		return
	}
	if got.ID != want.ID || got.Template != want.Template {
		t.Errorf("ItemFor = %s/%s, GenerateB = %s/%s", got.ID, got.Template, want.ID, want.Template)
	}
}

// TestItemForUnknownKeyNamesIt: a level-B string that names no requirement of
// the card is a skip that names the string, not a silent empty item.
func TestItemForUnknownKeyNamesIt(t *testing.T) {
	reg := corpusReg(t)
	_, skip := templates.ItemFor(reg, "Shock", "activate#9.9")
	if skip == nil {
		t.Fatal("an unknown level-B key generated a scenario")
	}
	if !strings.Contains(skip.Reason, "activate#9.9") {
		t.Errorf("skip reason %q does not name the key", skip.Reason)
	}
}
