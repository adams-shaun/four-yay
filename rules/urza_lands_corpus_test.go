package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins Count$UrzaLands through the ACTUAL corpus card scripts, not
// an inline re-authoring of them (rules/urza_lands_test.go). The report
// (fb-20260927T094209Z-0983012f) said automan miscalculated the three Urza
// lands while playing the Eldrazi deck; the count head is fixed at current
// main, but every existing pool test authored its own copy of the scripts.
// This one loads cards/cardsfolder/u/urzas_{mine,tower,power_plant}.txt via
// the registry, so the real Types line (the hyphenated "Urza's Power-Plant"
// subtype) and the real Count$UrzaLands SVar indirection are what the
// assembled/not-assembled amounts are measured against. No script text is
// copied into the repo (the corpus is GPL-3.0).

// urzaCorpusCards loads the three real Urza land definitions. It asserts the
// preconditions the count depends on: each card is present, its Mana ability's
// Amount$ names the UrzaAmount SVar, and the SVar body is the Count$UrzaLands
// expression -- so a corpus rename or rescripting fails loudly here instead of
// silently changing what the pool assertions below measure.
func urzaCorpusCards(t *testing.T) map[string]*cards.Card {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	names := []string{"Urza's Mine", "Urza's Tower", "Urza's Power Plant"}
	out := map[string]*cards.Card{}
	for _, n := range names {
		c, ok := reg.Lookup(n)
		if !ok {
			t.Fatalf("corpus has no card %q", n)
		}
		if len(c.Faces) == 0 || len(c.Faces[0].Abilities) == 0 {
			t.Fatalf("corpus card %q has no abilities", n)
		}
		ab := c.Faces[0].Abilities[0]
		if got := ab.Params["Amount"]; got != "UrzaAmount" {
			t.Fatalf("corpus card %q Amount$ = %q, want UrzaAmount (the count indirection)", n, got)
		}
		body, ok := c.Faces[0].SVars["UrzaAmount"]
		if !ok {
			t.Fatalf("corpus card %q has no UrzaAmount SVar", n)
		}
		if !strings.Contains(body, "Count$UrzaLands.") {
			t.Fatalf("corpus card %q UrzaAmount = %q, want a Count$UrzaLands expression", n, body)
		}
		out[n] = c
	}
	return out
}

// urzaCorpusEngine builds a 2-seat game whose seat-0 deck contains the three
// real corpus Urza lands, places the ones named by `place` onto the
// battlefield with a logged MoveZone (so replayCheck reconstructs them), then
// drives to seat 0's turn-1 main phase and asks priority. It returns the
// engine, its config (for replayCheck) and the placed ids keyed by "mine",
// "tower", "plant".
func urzaCorpusEngine(t *testing.T, place map[string]bool) (*Engine, Config, map[string]state.ObjID) {
	t.Helper()
	urza := urzaCorpusCards(t)
	deck := []*cards.Card{urza["Urza's Mine"], urza["Urza's Tower"], urza["Urza's Power Plant"]}
	deck = append(deck, mountainDeck(t, 20-len(deck))...)
	cfg := seatZeroStart(Config{Seed: 44, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 20)}})
	e := New(cfg)
	e.Advance()

	ids := map[string]state.ObjID{}
	want := []struct{ key, name string }{
		{"mine", "Urza's Mine"},
		{"tower", "Urza's Tower"},
		{"plant", "Urza's Power Plant"},
	}
	for _, s := range want {
		if !place[s.key] {
			continue
		}
		id := findAndMoveToBattlefield(t, e, 0, s.name)
		// Precondition: the object the count reads is on the battlefield under
		// seat 0 with its real subtype-bearing face intact.
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 || o.Face() == nil {
			t.Fatalf("%s not on seat 0's battlefield (o=%+v)", s.name, o)
		}
		ids[s.key] = id
	}
	if len(ids) == 0 {
		t.Fatal("fixture placed no Urza lands")
	}
	driveToStep(t, e, 1, 0, state.StepMain1)
	e.priorityRound()
	return e, cfg, ids
}

// TestUrzaLandsCorpusPoolAllThree taps the three REAL corpus lands while one
// seat controls all of them: Mine 2, Tower 3, Power Plant 2 (the assembled
// amounts of each card's own Count$UrzaLands.<assembled>.<not assembled>).
func TestUrzaLandsCorpusPoolAllThree(t *testing.T) {
	t.Parallel()
	e, cfg, ids := urzaCorpusEngine(t, map[string]bool{"mine": true, "tower": true, "plant": true})
	activateMana(t, e, ids["mine"])
	if got := e.G.Players[0].Pool[state.MC]; got != 2 {
		t.Fatalf("real Mine pool = %d, want 2 colourless", got)
	}
	activateMana(t, e, ids["tower"])
	if got := e.G.Players[0].Pool.Total(); got != 5 {
		t.Fatalf("real Mine+Tower pool = %d, want 5 (2+3)", got)
	}
	activateMana(t, e, ids["plant"])
	if got := e.G.Players[0].Pool.Total(); got != 7 {
		t.Fatalf("real all three pool = %d, want 7 (2+3+2)", got)
	}
	replayCheck(t, e, cfg)
}

// TestUrzaLandsCorpusPoolUnassembled taps only real Mine+Power Plant (Tower
// missing): each of the two present must yield 1, proving the assembled
// branch above is not merely the cards' base ability. The pair is the one
// whose absence the Mine's own condition does not name, so a count that
// reported assembled for "any two Urza lands" would wrongly return 2 here.
func TestUrzaLandsCorpusPoolUnassembled(t *testing.T) {
	t.Parallel()
	e, cfg, ids := urzaCorpusEngine(t, map[string]bool{"mine": true, "plant": true})
	activateMana(t, e, ids["mine"])
	if got := e.G.Players[0].Pool[state.MC]; got != 1 {
		t.Fatalf("real Mine (Plant only) pool = %d, want 1", got)
	}
	activateMana(t, e, ids["plant"])
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Fatalf("real Mine+Plant pool = %d, want 2 (1+1)", got)
	}
	replayCheck(t, e, cfg)
}
