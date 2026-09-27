package main

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// boardCapDecks is a deck pair whose games must mint tokens: 30 Dragon
// Fodder (SP$ Token | TokenAmount$ 2 -- two 1/1 red Goblins minted inside
// ONE resolution) and 30 Mountains, so a bot seat casts a token engine
// without depending on any card the generation pass might not pick.
func boardCapDecks() []genDeck {
	cards := make([]string, 0, 60)
	for i := 0; i < 30; i++ {
		cards = append(cards, "Dragon Fodder", "Mountain")
	}
	return []genDeck{{Colour: "R", Cards: cards}, {Colour: "R", Cards: append([]string(nil), cards...)}}
}

// TestMidResolutionObjectCap pins the mid-resolution arm of the object
// budget: -max-objects is armed on the engine's own per-event watchdog
// (rules/livelock.go's LoopGuard.MaxObjs), so a budget crossing INSIDE one
// resolution is caught one event after it happens -- not at the next
// decision boundary, which may be tens of thousands of expensive events
// later and, on a wall-clock-budget harness, after the game has already
// been given up as a hang.
//
// Two 60-card decks put 120 objects in the arena at genesis, and Dragon
// Fodder's resolution mints its two Goblins in two consecutive events, so a
// cap of 121 is crossed by the SECOND of the two mints: the record must
// carry the boundary record's "bigboard" classification vocabulary AND the
// mid-resolution provenance a boundary record can never carry.
func TestMidResolutionObjectCap(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	decks := boardCapDecks()
	f, _ := playOne(reg, decks, 17, 6, 20000, 121, false, false)
	if f == nil || f.Kind != "bigboard" {
		t.Fatalf("failure = %+v, want kind bigboard", f)
	}
	// The classification vocabulary is the boundary record's.
	if !strings.HasPrefix(f.Sig, "bigboard: ") {
		t.Fatalf("sig %q, want the bigboard sig prefix", f.Sig)
	}
	if !strings.Contains(f.Diag, "exceeds -max-objects 121") {
		t.Fatalf("diag %q lacks the cap crossing", f.Diag)
	}
	// And it names the mid-resolution provenance: the abort fired ON a mint
	// event inside the resolution, which a decision-boundary record cannot
	// carry.
	if !strings.Contains(f.Diag, "-- mid-resolution object cap --") {
		t.Fatalf("diag %q lacks the mid-resolution block", f.Diag)
	}
	if !strings.Contains(f.Diag, "object cap") {
		t.Fatalf("diag %q lacks the object-cap reason", f.Diag)
	}
	if !strings.Contains(f.Diag, "kind token_create") {
		t.Fatalf("diag %q lacks the aborting event kind (mid-resolution proof)", f.Diag)
	}
}

// TestMidResolutionObjectCapInertUnderBudget pins the other half of the
// budget: a cap no game in the pair can reach is armed but never fires, so
// the 3-turn cap ends the game as a plain stall and no failure record
// exists at all.
func TestMidResolutionObjectCapInertUnderBudget(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if f, _ := playOne(reg, boardCapDecks(), 17, 3, 20000, 100000, false, false); f != nil {
		t.Fatalf("game under the object budget recorded %+v", f)
	}
}

// TestMidResolutionObjectCapDisabled pins max-objects 0: no cap is armed,
// the budget is off, and the game plays to its turn cap with no failure.
func TestMidResolutionObjectCapDisabled(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if f, _ := playOne(reg, boardCapDecks(), 17, 2, 20000, 0, false, false); f != nil {
		t.Fatalf("disabled budget recorded %+v", f)
	}
}
