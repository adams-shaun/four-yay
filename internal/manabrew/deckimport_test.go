//go:build manabrew

package manabrew

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"

	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// deckCard builds one ManaBrew DeckCard whose only thing that matters to
// ImportDeck is its identity.name; every other field is a value the
// protocol requires but ImportDeck ignores.
func deckCard(name string) mb.DeckCard {
	return mb.DeckCard{Identity: mb.DeckCardIdentity{ID: "inst-" + name, Name: name}}
}

func TestDeckImport(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()

	t.Run("name match", func(t *testing.T) {
		d := mb.Deck{
			Name: "bears deck",
			Cards: []mb.DeckCard{
				deckCard("Grizzly Bears"),
				deckCard("Grizzly Bears"),
				deckCard("Mountain"),
				deckCard("Mountain"),
				deckCard("Mountain"),
			},
		}
		f, rep, err := ImportDeck(reg, d, supported)
		if err != nil {
			t.Fatalf("ImportDeck: %v", err)
		}
		if len(rep.Unresolved) != 0 {
			t.Fatalf("Unresolved = %v, want none", rep.Unresolved)
		}
		if f.Name != "bears deck" {
			t.Errorf("File.Name = %q", f.Name)
		}
		got := map[string]int{}
		for _, e := range f.Cards {
			got[e.Name] += e.Count
		}
		want := map[string]int{"Grizzly Bears": 2, "Mountain": 3}
		if len(got) != len(want) || got["Grizzly Bears"] != 2 || got["Mountain"] != 3 {
			t.Fatalf("resolved entries = %v, want %v", got, want)
		}
		if len(f.Cards) != 2 {
			t.Fatalf("f.Cards has %d distinct entries, want 2 (one per distinct name, grouped)", len(f.Cards))
		}
	})

	t.Run("front face fallback for a split card name", func(t *testing.T) {
		d := mb.Deck{
			Name:  "split test",
			Cards: []mb.DeckCard{deckCard("Fire // Ice")},
		}
		f, rep, err := ImportDeck(reg, d, supported)
		if err != nil {
			t.Fatalf("ImportDeck: %v", err)
		}
		if len(rep.Unresolved) != 0 {
			t.Fatalf("Unresolved = %v, want none (the front-face fallback should have resolved it)", rep.Unresolved)
		}
		if len(f.Cards) != 1 || f.Cards[0].Name != "Fire" || f.Cards[0].Count != 1 {
			t.Fatalf("f.Cards = %+v, want [{Fire 1}] (the registry's own front-face spelling)", f.Cards)
		}
	})

	t.Run("diacritic fold fallback", func(t *testing.T) {
		// The corpus's own spelling is "Lim-Dûl's Vault" (with the accent);
		// a client sending the plain-ASCII spelling must still resolve, per
		// spec §6.1's borrowed diacritic fold.
		d := mb.Deck{
			Name:  "diacritic test",
			Cards: []mb.DeckCard{deckCard("Lim-Dul's Vault")},
		}
		f, rep, err := ImportDeck(reg, d, supported)
		if err != nil {
			t.Fatalf("ImportDeck: %v", err)
		}
		if len(rep.Unresolved) != 0 {
			t.Fatalf("Unresolved = %v, want none", rep.Unresolved)
		}
		if len(f.Cards) != 1 || f.Cards[0].Name != "Lim-Dûl's Vault" {
			t.Fatalf("f.Cards = %+v, want the corpus's own accented spelling", f.Cards)
		}
	})

	t.Run("unresolved name is reported and errors", func(t *testing.T) {
		d := mb.Deck{
			Name:  "bogus",
			Cards: []mb.DeckCard{deckCard("Grizzly Bears"), deckCard("Not A Real Card Whatsoever")},
		}
		f, rep, err := ImportDeck(reg, d, supported)
		if err == nil {
			t.Fatalf("ImportDeck: want an error naming the unresolved card, got nil")
		}
		if !strings.Contains(err.Error(), "Not A Real Card Whatsoever") {
			t.Errorf("error %q does not name the unresolved card", err)
		}
		if len(rep.Unresolved) != 1 || rep.Unresolved[0] != "Not A Real Card Whatsoever" {
			t.Fatalf("Unresolved = %v, want [\"Not A Real Card Whatsoever\"]", rep.Unresolved)
		}
		// The card that DID resolve is still in the returned File.
		found := false
		for _, e := range f.Cards {
			if e.Name == "Grizzly Bears" {
				found = true
			}
		}
		if !found {
			t.Errorf("f.Cards = %+v, want Grizzly Bears still present despite the sibling error", f.Cards)
		}
	})

	t.Run("ignored fields are dropped but counted", func(t *testing.T) {
		bears := deckCard("Grizzly Bears")
		bears.Identity.SetCode = "M10"
		bears.Identity.CardNumber = "168"
		bears.Identity.OracleID = "abc-123"
		bears.Identity.TokenScript = "some_token"
		bears.Identity.Foil = true

		d := mb.Deck{
			Name:         "ignored fields",
			Cards:        []mb.DeckCard{bears},
			Tokens:       []mb.DeckCard{deckCard("Goblin")},
			Attractions:  []mb.DeckCard{deckCard("Some Attraction")},
			Contraptions: []mb.DeckCard{deckCard("Some Contraption")},
			Schemes:      []mb.DeckCard{deckCard("Some Scheme")},
			Planes:       []mb.DeckCard{deckCard("Some Plane")},
			Maybeboard:   []mb.DeckCard{deckCard("Some Maybe")},
			Labels:       []mb.DeckLabel{{Name: "aggro"}},
			CustomTags:   []string{"pauper"},
			CardTags:     map[string][]string{"Grizzly Bears": {"creature"}},
			Companion:    &mb.DeckCard{Identity: mb.DeckCardIdentity{Name: "Some Companion"}},
			Draft:        []byte(`{"picked":true}`),
			PlaymatURL:   "https://example.invalid/playmat.png",
		}
		_, rep, err := ImportDeck(reg, d, supported)
		if err != nil {
			t.Fatalf("ImportDeck: %v", err)
		}
		wantAtLeast := map[string]int{
			"setCode":      1,
			"cardNumber":   1,
			"oracleId":     1,
			"tokenScript":  1,
			"foil":         1,
			"tokens":       1,
			"attractions":  1,
			"contraptions": 1,
			"schemes":      1,
			"planes":       1,
			"maybeboard":   1,
			"labels":       1,
			"customTags":   1,
			"cardTags":     1,
			"companion":    1,
			"draft":        1,
			"playmat":      1,
		}
		for k, want := range wantAtLeast {
			if got := rep.Ignored[k]; got < want {
				t.Errorf("Ignored[%q] = %d, want at least %d (report: %v)", k, got, want, rep.Ignored)
			}
		}
	})

	t.Run("unsupported card reported via reg.Unsupported", func(t *testing.T) {
		// Incinerate is on the ratchet's knownUnsupported list (AGENTS.md,
		// stat:CantRegenerate): it resolves fine by name but the engine
		// cannot fully play it yet.
		d := mb.Deck{
			Name:  "unsupported",
			Cards: []mb.DeckCard{deckCard("Grizzly Bears"), deckCard("Incinerate")},
		}
		_, rep, err := ImportDeck(reg, d, supported)
		if err != nil {
			t.Fatalf("ImportDeck: %v (Incinerate resolves by name; only its primitives are unsupported)", err)
		}
		missing, ok := rep.Unsupported["Incinerate"]
		if !ok || len(missing) == 0 {
			t.Fatalf("Unsupported[%q] = %v, want a non-empty missing-primitive list (report: %+v)", "Incinerate", missing, rep.Unsupported)
		}
		if _, ok := rep.Unsupported["Grizzly Bears"]; ok {
			t.Errorf("Grizzly Bears should not be reported unsupported")
		}
	})

	t.Run("sideboard resolves independently", func(t *testing.T) {
		d := mb.Deck{
			Name:      "sideboard test",
			Cards:     []mb.DeckCard{deckCard("Grizzly Bears")},
			Sideboard: []mb.DeckCard{deckCard("Mountain"), deckCard("Mountain")},
		}
		f, rep, err := ImportDeck(reg, d, supported)
		if err != nil {
			t.Fatalf("ImportDeck: %v", err)
		}
		if len(rep.Unresolved) != 0 {
			t.Fatalf("Unresolved = %v", rep.Unresolved)
		}
		if len(f.Sideboard) != 1 || f.Sideboard[0].Name != "Mountain" || f.Sideboard[0].Count != 2 {
			t.Fatalf("f.Sideboard = %+v, want [{Mountain 2}]", f.Sideboard)
		}
		if len(f.Cards) != 1 || f.Cards[0].Name != "Grizzly Bears" {
			t.Fatalf("f.Cards = %+v, sideboard must not leak into the main deck", f.Cards)
		}
	})

	t.Run("commander is folded into the main list when the builder kept it separate", func(t *testing.T) {
		// A commander deck-builder convention: the commander's DeckCard
		// lives only in `commanders`, not duplicated into `cards`.
		d := mb.Deck{
			Name: "commander test",
			Cards: []mb.DeckCard{
				deckCard("Grizzly Bears"),
				deckCard("Mountain"),
			},
			Commanders: []mb.DeckCard{deckCard("Prosper, Tome-Bound")},
		}
		f, rep, err := ImportDeck(reg, d, supported)
		if err != nil {
			t.Fatalf("ImportDeck: %v", err)
		}
		if len(rep.Unresolved) != 0 {
			t.Fatalf("Unresolved = %v", rep.Unresolved)
		}
		if len(f.Commanders) != 1 || f.Commanders[0] != "Prosper, Tome-Bound" {
			t.Fatalf("f.Commanders = %v, want [\"Prosper, Tome-Bound\"]", f.Commanders)
		}
		names := map[string]bool{}
		for _, e := range f.Cards {
			names[e.Name] = true
		}
		if !names["Prosper, Tome-Bound"] {
			t.Fatalf("f.Cards = %+v, the commander must be folded into the main list so File.CommanderIndices can find it", f.Cards)
		}
		if len(f.CommanderIndices()) != 1 {
			t.Errorf("f.CommanderIndices() = %v, want exactly one index", f.CommanderIndices())
		}
	})

	t.Run("commander already present in cards is not duplicated", func(t *testing.T) {
		d := mb.Deck{
			Name: "commander already listed",
			Cards: []mb.DeckCard{
				deckCard("Prosper, Tome-Bound"),
				deckCard("Mountain"),
			},
			Commanders: []mb.DeckCard{deckCard("Prosper, Tome-Bound")},
		}
		f, _, err := ImportDeck(reg, d, supported)
		if err != nil {
			t.Fatalf("ImportDeck: %v", err)
		}
		n := 0
		for _, e := range f.Cards {
			if e.Name == "Prosper, Tome-Bound" {
				n += e.Count
			}
		}
		if n != 1 {
			t.Fatalf("Prosper, Tome-Bound total count = %d, want 1 (not duplicated)", n)
		}
	})
}

// TestDeckImportReportDeterministic guards against a map-range leak into the
// Unresolved slice ordering: two decks with the SAME unresolved names in a
// different encounter order must not spuriously "sort themselves" into an
// order that hides a real regression, but the encoding must otherwise be
// stable across repeated runs (AGENTS.md's no-nondeterminism rule).
func TestDeckImportReportDeterministic(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	d := mb.Deck{
		Name: "det",
		Cards: []mb.DeckCard{
			deckCard("Nonexistent Card A"),
			deckCard("Nonexistent Card B"),
			deckCard("Nonexistent Card A"),
		},
	}
	var runs [][]string
	for i := 0; i < 3; i++ {
		_, rep, _ := ImportDeck(reg, d, supported)
		got := append([]string(nil), rep.Unresolved...)
		runs = append(runs, got)
	}
	for i := 1; i < len(runs); i++ {
		if len(runs[i]) != len(runs[0]) {
			t.Fatalf("run %d Unresolved = %v, run 0 = %v", i, runs[i], runs[0])
		}
		for j := range runs[0] {
			if runs[i][j] != runs[0][j] {
				t.Fatalf("run %d Unresolved = %v, run 0 = %v (order must be stable)", i, runs[i], runs[0])
			}
		}
	}
	want := []string{"Nonexistent Card A", "Nonexistent Card B"}
	sort.Strings(want)
	got := append([]string(nil), runs[0]...)
	sort.Strings(got)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Unresolved (sorted) = %v, want %v", got, want)
	}
}
