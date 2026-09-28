package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// The trig:Investigated half (task investtrig1, "whenever you investigate"):
// the events.Investigate record api:Investigate's effInvestigate emits beside
// each Clue mint is what the mode matches. Erdwal Illuminator is the
// FirstTime$ carrier ("Whenever you investigate for the first time each
// turn, investigate an additional time"); Wavesifter (DB$ Investigate |
// Num$ 2) is the investigator, so one ETB is two investigates and the pin
// discriminates all three behaviours at once: no trigger = 2 Clues, an
// ungated trigger = 4, the real FirstTime$-gated trigger = 3.
//
// The harness is the investigate/token-replacement one: corpus cards by
// name, a 2-seat engine with the real corpus token registry, logged MoveZone
// moves to fire the ETBs, and replayCheck on every leaf.

// TestInvestigatedSupported: the coverage census (make report) reads
// effects.Supported(), so a missing entry would silently keep every carrier
// unplayable.
func TestInvestigatedSupported(t *testing.T) {
	t.Parallel()
	supported := effects.Supported()
	if !supported["trig:Investigated"] {
		t.Fatalf("effects.Supported() is missing trig:Investigated")
	}
}

// TestErdwalIlluminatorInvestigatesAnAdditionalTimeOnTheFirstInvestigate:
// Erdwal on the battlefield, Wavesifter's ETB investigates twice. The FIRST
// investigate fires Erdwal (one extra investigate: one more Clue); the
// SECOND investigate in the same turn does not (FirstTime$ True), and
// Erdwal's own extra investigate does not chain either. Three Clue tokens
// total, replay-verified.
func TestErdwalIlluminatorInvestigatesAnAdditionalTimeOnTheFirstInvestigate(t *testing.T) {
	t.Parallel()
	erdwal := tokenReplCorpusCard(t, "Erdwal Illuminator")
	wave := tokenReplCorpusCard(t, "Wavesifter")
	e, cfg := tokenReplGame(t, 77, erdwal, wave)
	moveSeededCard(t, e, 0, erdwal, state.ZBattlefield)
	moveSeededCard(t, e, 0, wave, state.ZBattlefield)
	addMana(t, e, 0, "") // a priority round flushes the queued ETB trigger
	investigateDrain(t, e)
	if got := countTokensNamedOnSeat(t, e, 0, "Clue Token"); got != 3 {
		t.Fatalf("first investigate should fire Erdwal exactly once: got %d Clue tokens, want 3 "+
			"(2 = trig never fired, 4 = FirstTime$ gate unread)", got)
	}
	replayCheck(t, e, cfg)
}

// TestErdwalIlluminatorSecondInvestigateThisTurnDoesNotFire: the same turn's
// second investigating source is silent. Erdwal + Martha Jones (Num$ default
// 1): the first investigate chains the extra one (2 Clues). A second Martha
// entering later in the turn investigates once more and must NOT fire Erdwal
// again (still 3 Clues, not 4).
func TestErdwalIlluminatorSecondInvestigateThisTurnDoesNotFire(t *testing.T) {
	t.Parallel()
	erdwal := tokenReplCorpusCard(t, "Erdwal Illuminator")
	martha := tokenReplCorpusCard(t, "Martha Jones")
	e, cfg := tokenReplGame(t, 78, erdwal, martha, martha)
	moveSeededCard(t, e, 0, erdwal, state.ZBattlefield)
	firstMartha := moveSeededCard(t, e, 0, martha, state.ZBattlefield)
	addMana(t, e, 0, "")
	investigateDrain(t, e)
	if got := countTokensNamedOnSeat(t, e, 0, "Clue Token"); got != 2 {
		t.Fatalf("first Martha investigate + Erdwal's extra left %d Clue tokens, want 2", got)
	}
	// The second Martha copy: another investigate in the same turn. Martha
	// Jones is legendary, so answer the real CR 704.5j choice before passing
	// priority to resolve the second copy's ETB trigger.
	secondMartha := moveSeededCard(t, e, 0, martha, state.ZBattlefield)
	if firstMartha == secondMartha || e.G.Obj(firstMartha).Zone != state.ZBattlefield ||
		e.G.Obj(secondMartha).Zone != state.ZBattlefield {
		t.Fatalf("fixture: two distinct Martha Jones copies must be on the battlefield")
	}
	e.checkStateBased()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 ||
		d.Options[0].Kind != "keep" || d.Options[1].Kind != "keep" ||
		d.Options[0].Obj != firstMartha || d.Options[1].Obj != secondMartha {
		t.Fatalf("two battlefield Martha Jones copies must pose a legend choice: %+v", d)
	}
	submitChoices(t, e, 0)
	addMana(t, e, 0, "")
	investigateDrain(t, e)
	if got := countTokensNamedOnSeat(t, e, 0, "Clue Token"); got != 3 {
		t.Fatalf("second investigate this turn fired Erdwal: got %d Clue tokens, want 3", got)
	}
	replayCheck(t, e, cfg)
}

// TestPlainClueTokenCreationDoesNotFireInvestigated: the marker is emitted
// only by effInvestigate, so a plain Clue-token creation (DB$ Token |
// TokenScript$ c_a_clue_draw, no Investigate) never fires "whenever you
// investigate" — the reason the trigger keys on its own event Kind rather
// than on the mint.
func TestPlainClueTokenCreationDoesNotFireInvestigated(t *testing.T) {
	t.Parallel()
	erdwal := tokenReplCorpusCard(t, "Erdwal Illuminator")
	forge := cardByName(t, tokenForgeSrc("c_a_clue_draw"))
	e, cfg := tokenReplGame(t, 79, erdwal, forge)
	moveSeededCard(t, e, 0, erdwal, state.ZBattlefield)
	maker := moveSeededCard(t, e, 0, forge, state.ZBattlefield)
	activateTokenForge(t, e, maker)
	if got := countTokensNamedOnSeat(t, e, 0, "Clue Token"); got != 1 {
		t.Fatalf("plain Clue mint left %d Clue tokens, want 1", got)
	}
	// Erdwal's trigger must not have fired: the mint is the only Clue.
	replayCheck(t, e, cfg)
}
