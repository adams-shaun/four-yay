package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func generateBKey(t *testing.T, name, key string) oraclegen.Item {
	t.Helper()
	reg := oracleHarnessCorpus(t)
	card, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s is absent from the corpus", name)
	}
	for _, req := range levelb.Requirements(card) {
		if req.Key == key {
			item, skip := GenerateB(reg, name, req)
			if skip != nil {
				t.Fatalf("GenerateB(%s %s): %s", name, key, skip.Reason)
			}
			return item
		}
	}
	t.Fatalf("%s %s requirement not classified", name, key)
	return oraclegen.Item{}
}

// A search-and-shuffle over the activate fixture's named search pool leaves an
// order XMage randomises (measured: Expedition Map, Maze's End, Wishclaw
// Talisman diverged on library_top only); the item compares counts, not order.
// The search need not be the exercised ability: a static item still casts the
// card (Prismatic Undercurrents' and Lo and Li's ETB search), a combat item
// still deals damage (Tempest Hawk's damage search), and an activate item's
// sacrifice cost fires a search trigger (Heaped Harvest). Those four flipped
// library_top between two identical XMage runs (2026-10-06).
func TestSearchShuffleOverNamedLibrarySkipsLibraryOrder(t *testing.T) {
	for _, c := range []struct{ name, key string }{
		{"Expedition Map", "activate#0.0"},
		{"Maze's End", "activate#0.1"},
		{"Wishclaw Talisman", "activate#0.0"},
		{"Heaped Harvest", "activate#0.0"},
		{"Prismatic Undercurrents", "static#0.0"},
		{"Tempest Hawk", "combat#0.attack"},
		{"Lo and Li, Twin Tutors", "static#0.1"},
	} {
		it := generateBKey(t, c.name, c.key)
		if !oraclegen.NamedLibrary(&it.Scenario) {
			t.Errorf("%s %s: precondition: fixture library holds only filler", c.name, c.key)
		}
		if !hasCompareOption(it.Compare, oraclegen.CompareNoLibraryOrder) {
			t.Errorf("%s %s: missing %q mark: %v", c.name, c.key, oraclegen.CompareNoLibraryOrder, it.Compare)
		}
	}
}

// Static scenarios carry no named library: both engines pad with the Wastes
// filler, so the setup library_top agrees (it was Plains-vs-Wastes before).
func TestStaticScenarioHasNoNamedLibrary(t *testing.T) {
	it := generateBKey(t, "Vampire Interloper", "static#0.0")
	if oraclegen.NamedLibrary(&it.Scenario) {
		t.Fatalf("static scenario names a library: %+v", it.Scenario.Setup)
	}
}

// An accepted optional sacrifice is chooseUse-then-choose in XMage
// (DesecrationDemon.java:77): the pick is preceded by a "yes".
func TestAcceptedOptionalSacrificeScriptsYesFirst(t *testing.T) {
	it := generateBKey(t, "Desecration Demon", "trigger#0.0")
	var got []oraclegen.XAnswer
	for _, step := range it.XAnswers {
		got = append(got, step...)
	}
	if len(got) != 2 || got[0] != (oraclegen.XAnswer{Seat: 1, Kind: "choice", Value: "yes"}) || got[1].Value != "Grizzly Bears" {
		t.Fatalf("xmage answers = %+v, want seat-1 yes then Grizzly Bears", got)
	}
}

// A p0 upkeep trigger whose modes depend on its own earlier choices ("choose
// one that hasn't been chosen") is cast on turn 1, so the observed firing is
// its first in both engines (setup would otherwise fire it on turn 1 on a
// fallback answer).
func TestRememberedChoiceUpkeepTriggerCastsTheCardFirst(t *testing.T) {
	it := generateBKey(t, "Demonic Pact", "trigger#0.0")
	if len(it.Steps) == 0 || it.Steps[0].Op != "cast" || it.Steps[0].Card != "p0:Demonic Pact" {
		t.Fatalf("first step = %+v, want the card's own cast", it.Steps)
	}
	if len(it.Setup["p0"].Battlefield) != 0 {
		t.Fatalf("p0 battlefield at setup = %v, want empty", it.Setup["p0"].Battlefield)
	}
}
