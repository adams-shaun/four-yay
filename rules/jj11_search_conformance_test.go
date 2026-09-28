package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A stated-quality search may fail to find (701.23b); a quantity-only
// search may not (701.23d). The Evolving Wilds control cannot catch this.
func TestCR701QuantityOnlyTutorMustFindAvailableCard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Vampiric Tutor")
	_, d := castSearchSpell(t, e, "Vampiric Tutor")
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" || len(d.Options) == 0 {
		t.Fatalf("CR 701.23d: real Vampiric Tutor must reach a nonempty search, got %+v", d)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("CR 701.23d: quantity-only search with %d available cards offers %d..%d; must find one, not fail to find", len(d.Options), d.Min, d.Max)
	}
	// The prompt must not offer a choice the decision refuses: a mandatory
	// search says "choose 1", never "choose up to 1".
	if strings.Contains(d.Prompt, "up to") {
		t.Errorf("CR 701.23d: mandatory search prompt offers a choice it will refuse: %q", d.Prompt)
	}
}

// The 701.23b side of the same split, pinned so the two directions stay
// discriminated: a search for a stated QUALITY (Evolving Wilds names a basic
// land) keeps the fail-to-find allowance (Min 0) even when matching cards are
// present, and declining is honoured without moving anything but still
// shuffles. Forcing Min up to Max for every search makes this leaf red.
func TestCR701StatedQualitySearchMayFailToFind(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Evolving Wilds")
	wilds := searchMoveByName(t, e, "Evolving Wilds", state.ZBattlefield)
	d := activateSearch(t, e, wilds)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("CR 701.23b: real Evolving Wilds must reach a search, got %+v", d)
	}
	if len(d.Options) == 0 {
		t.Fatalf("CR 701.23b: fixture has basic lands, options must be nonempty")
	}
	if d.Min != 0 || d.Max != 1 {
		t.Fatalf("CR 701.23b: stated-quality search with %d available cards offers %d..%d; keep Min 0 so the player may fail to find", len(d.Options), d.Min, d.Max)
	}
	// The other direction: an optional search must still SAY it is optional.
	if !strings.Contains(d.Prompt, "up to") {
		t.Errorf("CR 701.23b: optional search prompt hides the fail-to-find allowance: %q", d.Prompt)
	}
	// Declining (choosing no cards) must be honoured: nothing moves, but the
	// unconditional shuffle still happens.
	start := len(e.L.Events)
	submitChoices(t, e)
	moves, shuffles := 0, 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary {
			moves++
		}
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles++
		}
	}
	if moves != 0 || shuffles != 1 {
		t.Fatalf("CR 701.23b: fail-to-find emitted %d library moves and %d shuffles, want 0/1", moves, shuffles)
	}
}

// The bare-base half of the CR 701.23b classification, which the Evolving
// Wilds leaf cannot reach. Evolving Wilds is `Land.Basic`: its `Basic`
// predicate alone is enough to state a quality, so deleting the base-type
// check in SearchStatesQuality leaves that leaf green. Worldly Tutor is
// `ChangeType$ Creature` -- a bare type with NO predicate -- so ONLY the base
// check makes it a stated-quality search. Measured over `.cards/cardsfolder`
// with /usr/bin/grep, ~150 `Origin$ Library` lines carry a bare non-Card base
// (Land 35, Creature 31, Forest 19, Artifact 19, Plains 8, Equipment 7,
// Enchantment 7, Dragon 6, ...); without this leaf every one of them would
// silently become a mandatory search.
func TestCR701BareTypeSearchStatesAQuality(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Worldly Tutor")
	_, d := castSearchSpell(t, e, "Worldly Tutor")
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("CR 701.23b: real Worldly Tutor must reach a search, got %+v", d)
	}
	if len(d.Options) == 0 {
		t.Fatalf("CR 701.23b: fixture library holds creatures, options must be nonempty")
	}
	if d.Min != 0 {
		t.Fatalf("CR 701.23b: bare-type search %q with %d available cards offers %d..%d; a named card TYPE is a stated quality, so the player may fail to find", "Creature", len(d.Options), d.Min, d.Max)
	}
	if !strings.Contains(d.Prompt, "up to") {
		t.Errorf("CR 701.23b: optional search prompt hides the fail-to-find allowance: %q", d.Prompt)
	}
}
