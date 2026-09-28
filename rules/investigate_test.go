package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The Investigate primitive (api:Investigate, CR 701.36a) pinned end to end
// on real corpus cards: Martha Jones (the ETB default-count shape),
// Wavesifter (Num$ 2) and Mirkwood Bats (the mint rides a real TokenCreate
// event, so "whenever you create a token" triggers fire). The Clue's own
// activated "{2}, Sacrifice this token: Draw a card" is pinned too, proving
// the mint is the real corpus token script (c_a_clue_draw), not a blank.
//
// The harness is the token-replacement/token-created one: corpus cards by
// name, a 2-seat engine with the real corpus token registry, and logged
// MoveZone moves to fire ETB triggers.

// investigateDrain drains the stack, answering any mid-resolution target ask
// with option 0 (the engine only offers legal targets) — the shape Martha
// Jones's sacrificed-Clue pump trigger poses while a Clue's draw resolves.
func investigateDrain(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 60 && !e.G.Over; i++ {
		d := e.Pending()
		if d != nil && d.Kind == decision.KTarget {
			submitChoices(t, e, 0)
			continue
		}
		if len(e.G.Stack) == 0 {
			return
		}
		passPriorityOnce(t, e)
	}
}

// investigateClueIDs returns seat p's battlefield objects named "Clue Token".
func investigateClueIDs(t *testing.T, e *Engine, p state.PlayerID) []state.ObjID {
	t.Helper()
	var ids []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Clue Token" {
			ids = append(ids, id)
		}
	}
	return ids
}

// TestMarthaJonesInvestigatesCreatesClue: Martha Jones's ETB (a bare
// `DB$ Investigate`, Num$ defaulting to 1) creates exactly one Clue token
// owned by the controller.
func TestMarthaJonesInvestigatesCreatesClue(t *testing.T) {
	t.Parallel()
	martha := tokenReplCorpusCard(t, "Martha Jones")
	e, cfg := tokenReplGame(t, 73, martha)
	moveSeededCard(t, e, 0, martha, state.ZBattlefield)
	addMana(t, e, 0, "") // a priority round flushes the queued ETB trigger
	investigateDrain(t, e)
	if got := countTokensNamedOnSeat(t, e, 0, "Clue Token"); got != 1 {
		t.Fatalf("Martha Jones ETB left %d Clue tokens on seat 0, want 1", got)
	}
	if got := countTokensNamedOnSeat(t, e, 1, "Clue Token"); got != 0 {
		t.Fatalf("seat 1 has %d Clue tokens; the token owner must be the controller", got)
	}
	replayCheck(t, e, cfg)
}

// TestWavesifterInvestigatesTwice: Wavesifter's `DB$ Investigate | Num$ 2`
// creates two Clue tokens.
func TestWavesifterInvestigatesTwice(t *testing.T) {
	t.Parallel()
	wave := tokenReplCorpusCard(t, "Wavesifter")
	e, cfg := tokenReplGame(t, 74, wave)
	moveSeededCard(t, e, 0, wave, state.ZBattlefield)
	addMana(t, e, 0, "") // a priority round flushes the queued ETB trigger
	investigateDrain(t, e)
	if got := countTokensNamedOnSeat(t, e, 0, "Clue Token"); got != 2 {
		t.Fatalf("Wavesifter ETB left %d Clue tokens on seat 0, want 2 (Num$ 2)", got)
	}
	replayCheck(t, e, cfg)
}

// TestInvestigateFiresTokenCreatedTrigger: the mint is a real TokenCreate
// event, so Mirkwood Bats ("whenever you create a token, each opponent
// loses 1 life") responds to an investigate with exactly one life loss.
func TestInvestigateFiresTokenCreatedTrigger(t *testing.T) {
	t.Parallel()
	bats := tokenReplCorpusCard(t, "Mirkwood Bats")
	martha := tokenReplCorpusCard(t, "Martha Jones")
	e, cfg := tokenReplGame(t, 75, bats, martha)
	moveSeededCard(t, e, 0, bats, state.ZBattlefield)
	before := e.G.Players[1].Life // Bats has no ETB; no loss yet
	moveSeededCard(t, e, 0, martha, state.ZBattlefield)
	addMana(t, e, 0, "") // a priority round flushes the queued ETB trigger
	investigateDrain(t, e)
	if got := countTokensNamedOnSeat(t, e, 0, "Clue Token"); got != 1 {
		t.Fatalf("expected 1 Clue token from Martha's ETB, got %d", got)
	}
	if got := e.G.Players[1].Life; got != before-1 {
		t.Fatalf("seat 1 life = %d, want %d (one loss per created Clue token)", got, before-1)
	}
	replayCheck(t, e, cfg)
}

// TestClueSacDraws: the minted Clue is the real corpus script — activate its
// "{2}, Sacrifice this token: Draw a card" ability, assert a card was drawn
// and the Clue left the battlefield.
func TestClueSacDraws(t *testing.T) {
	t.Parallel()
	martha := tokenReplCorpusCard(t, "Martha Jones")
	e, cfg := tokenReplGame(t, 76, martha)
	moveSeededCard(t, e, 0, martha, state.ZBattlefield)
	addMana(t, e, 0, "") // a priority round flushes the queued ETB trigger
	investigateDrain(t, e)
	clues := investigateClueIDs(t, e, 0)
	if len(clues) != 1 {
		t.Fatalf("setup: expected 1 Clue token, got %d", len(clues))
	}
	clue := clues[0]

	handBefore := len(e.G.Zone(state.ZHand, 0))
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	addMana(t, e, 0, "CC")
	submitChoices(t, e, abilityOption(t, e, clue, 0).Index)
	investigateDrain(t, e)

	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("after sacrificing the Clue, seat 0 hand = %d, want %d (one card drawn)", got, handBefore+1)
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != libBefore-1 {
		t.Fatalf("after sacrificing the Clue, seat 0 library = %d, want %d", got, libBefore-1)
	}
	if o := e.G.Obj(clue); o != nil && o.Zone == state.ZBattlefield {
		t.Fatal("the Clue is still on the battlefield; the Sac<1/CARDNAME> cost was not charged")
	}
	replayCheck(t, e, cfg)
}
