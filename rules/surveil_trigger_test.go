package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The trig:Surveil half (task trig-surveil, "whenever you surveil"): the
// events.Surveil record api:Surveil's effSurveil emits beside each surveil
// instruction is what the mode matches. The corpus population is 12 files /
// 12 raw `T:Mode$ Surveil` lines (Mirko, Obsessive Theorist; Dimir Spybug;
// Thoughtbound Phantasm; Whispering Snitch; Copy Catchers; Disinformation
// Campaign; Blood Operative; and the five Secondary$ lines paired with a
// T:Mode$ Scry sibling -- Matoya, Archon Elder; Planetarium of Wan Shi Tong
// (its Remembered present-clause gate is covered by
// tla-trigger-present-defined-remembered); Prudent Fateseer; River Song; and
// Val, Marooned Surveyor, whose scry/discover/seek halves are other tickets'
// scopes).
//
// The harness is the investigate/token-replacement one: corpus cards by
// name, a 2-seat engine, real activations driving real effSurveil bodies
// (Wretched Doll's `{B}, {T}: Surveil 1` is the activator; Otherworldly
// Gaze's `SP$ Surveil | Amount$ 3` is the spell shape), logged MoveZone
// moves to fire the ETBs, and replayCheck on every leaf.

// TestSurveilTriggerIsRegistered: the coverage census (make report) reads
// effects.Supported(), so a missing entry would silently keep every carrier
// unplayable -- six of the twelve carriers are the Revenant Recon (mkc)
// census deck's cards, Mirko its commander.
func TestSurveilTriggerIsRegistered(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["trig:Surveil"] {
		t.Fatal("effects.Supported() is missing trig:Surveil")
	}
}

// countSurveilMarkers reports how many events.Surveil records the log
// carries (the once-per-instruction observable).
func countSurveilMarkers(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Surveil {
			n++
		}
	}
	return n
}

// surveilArrangeAnswer waits for the KArrange ask a surveil instruction
// poses (passing whatever priority stands between here and it), asserts its
// shape (the library owner is asked, Min 0 -- any number of the looked-at
// cards may go to the graveyard -- and one option per looked-at card) and
// answers the EMPTY set, so the looked-at cards move to the graveyard: the
// ask's answer is part of the observable, not a silent default.
func surveilArrangeAnswer(t *testing.T, e *Engine, wantOptions int) {
	t.Helper()
	d := passUntilResolved(t, e, 20)
	if d == nil || d.Kind != decision.KArrange {
		t.Fatalf("expected a pending KArrange from the surveil, got %+v", d)
	}
	if d.Player != 0 {
		t.Fatalf("surveil arrange player = %d, want 0 (the library owner)", d.Player)
	}
	if d.Min != 0 {
		t.Fatalf("surveil arrange Min = %d, want 0 (any number may go to the graveyard)", d.Min)
	}
	if len(d.Options) != wantOptions {
		t.Fatalf("surveil arrange offered %d options, want %d", len(d.Options), wantOptions)
	}
	submitChoices(t, e)  // the empty answer: all looked-at cards go to the graveyard
	addMana(t, e, 0, "") // a priority round flushes the queued surveil trigger
}

// surveilDrain drains the stack after a surveil resolution: answers any
// trigger_order ask with the offered order (CR 603.3b -- the two Mirko
// triggers two simultaneous surveils queue), any target ask with option 0,
// and otherwise passes priority, until the stack is empty.
func surveilDrain(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 60 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		switch d.Kind {
		case decision.KTriggerOrder:
			choices := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				choices = append(choices, o.Index)
			}
			submitChoices(t, e, choices...)
		case decision.KTarget:
			submitChoices(t, e, 0)
		case decision.KPriority:
			if len(e.G.Stack) == 0 {
				return
			}
			passPriorityOnce(t, e)
		}
	}
}

// TestMirkoSurveilPutsOneCounterPerSurveil: Mirko on the battlefield, two
// Wretched Doll activations (each `{B}, {T}: Surveil 1`) -- each activation
// emits exactly ONE events.Surveil marker and fires Mirko's "whenever you
// surveil" exactly once (one +1/+1 counter per surveil, never per arranged
// card), replay-verified.
func TestMirkoSurveilPutsOneCounterPerSurveil(t *testing.T) {
	t.Parallel()
	mirko := tokenReplCorpusCard(t, "Mirko, Obsessive Theorist")
	doll := tokenReplCorpusCard(t, "Wretched Doll")
	e, cfg := tokenReplGame(t, 83, mirko, doll, doll)
	mirkoID := moveSeededCard(t, e, 0, mirko, state.ZBattlefield)
	doll1 := moveSeededCard(t, e, 0, doll, state.ZBattlefield)
	doll2 := moveSeededCard(t, e, 0, doll, state.ZBattlefield)
	// The Dolls' {B}, {T} costs need them past summoning sickness (CR 302.6)
	// and untapped: drive real turns to turn 3's Main1 (both cards entered on
	// turn 1), so every activation is replay-log clean -- no harness state
	// write a log-only replay could not reproduce.
	driveToStepAll(t, e, 3, 0, state.StepMain1)
	if got := counterOn(t, e, mirkoID, "P1P1"); got != 0 {
		t.Fatalf("setup: Mirko already carries %d P1P1 counters, want 0", got)
	}

	// First surveil: one marker, one counter.
	addMana(t, e, 0, "B")
	submitChoices(t, e, abilityOption(t, e, doll1, 0).Index)
	surveilArrangeAnswer(t, e, 1)
	surveilDrain(t, e)
	if got := counterOn(t, e, mirkoID, "P1P1"); got != 1 {
		t.Fatalf("after the first surveil Mirko carries %d P1P1 counters, want 1 "+
			"(0 = trig:Surveil never fired)", got)
	}
	if got := countSurveilMarkers(e); got != 1 {
		t.Fatalf("log carries %d Surveil markers after one surveil, want 1", got)
	}

	// Second surveil (the second Doll): one more marker, one more counter --
	// once per INSTRUCTION, not once per resolution or per card.
	addMana(t, e, 0, "B")
	submitChoices(t, e, abilityOption(t, e, doll2, 0).Index)
	surveilArrangeAnswer(t, e, 1)
	surveilDrain(t, e)
	if got := counterOn(t, e, mirkoID, "P1P1"); got != 2 {
		t.Fatalf("after the second surveil Mirko carries %d P1P1 counters, want 2", got)
	}
	if got := countSurveilMarkers(e); got != 2 {
		t.Fatalf("log carries %d Surveil markers after two surveils, want 2", got)
	}
	replayCheck(t, e, cfg)
}

// TestOtherworldlyGazeSurveilThreeFiresMirkoOnce: a `SP$ Surveil | Amount$ 3`
// spell is ONE surveil instruction -- exactly one marker and exactly one
// Mirko trigger (a per-card firing would place three counters).
func TestOtherworldlyGazeSurveilThreeFiresMirkoOnce(t *testing.T) {
	t.Parallel()
	mirko := tokenReplCorpusCard(t, "Mirko, Obsessive Theorist")
	gaze := tokenReplCorpusCard(t, "Otherworldly Gaze")
	e, cfg := tokenReplGame(t, 84, mirko, gaze)
	mirkoID := moveSeededCard(t, e, 0, mirko, state.ZBattlefield)
	if got := counterOn(t, e, mirkoID, "P1P1"); got != 0 {
		t.Fatalf("setup: Mirko already carries %d P1P1 counters, want 0", got)
	}
	d := castSpellNamed(t, e, testutil.CorpusRegistry(t), "Otherworldly Gaze", "U")
	_ = d
	surveilArrangeAnswer(t, e, 3)
	surveilDrain(t, e)
	if got := counterOn(t, e, mirkoID, "P1P1"); got != 1 {
		t.Fatalf("a surveil 3 spell left Mirko %d P1P1 counters, want 1 "+
			"(3 = the trigger fired per card; 0 = never fired)", got)
	}
	if got := countSurveilMarkers(e); got != 1 {
		t.Fatalf("a surveil 3 spell emitted %d Surveil markers, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestWhisperingSnitchFirstTimeEachTurn: Whispering Snitch's `FirstTime$
// True` gate -- the first surveil of the turn drains each opponent for 1 and
// gains 1; a SECOND surveil in the same turn emits its marker (still one per
// instruction) but fires nothing.
func TestWhisperingSnitchFirstTimeEachTurn(t *testing.T) {
	t.Parallel()
	snitch := tokenReplCorpusCard(t, "Whispering Snitch")
	doll := tokenReplCorpusCard(t, "Wretched Doll")
	e, cfg := tokenReplGame(t, 85, snitch, doll, doll)
	moveSeededCard(t, e, 0, snitch, state.ZBattlefield)
	doll1 := moveSeededCard(t, e, 0, doll, state.ZBattlefield)
	doll2 := moveSeededCard(t, e, 0, doll, state.ZBattlefield)
	driveToStepAll(t, e, 3, 0, state.StepMain1)
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life

	// First surveil of the turn: fires.
	addMana(t, e, 0, "B")
	submitChoices(t, e, abilityOption(t, e, doll1, 0).Index)
	surveilArrangeAnswer(t, e, 1)
	surveilDrain(t, e)
	if e.G.Players[1].Life != life1-1 || e.G.Players[0].Life != life0+1 {
		t.Fatalf("first surveil: life %d/%d, want %d/%d (Snitch dealt 1 and gained 1)",
			e.G.Players[0].Life, e.G.Players[1].Life, life0+1, life1-1)
	}
	if got := countSurveilMarkers(e); got != 1 {
		t.Fatalf("log carries %d Surveil markers after one surveil, want 1", got)
	}

	// Second surveil of the turn: marker emitted, trigger silent.
	addMana(t, e, 0, "B")
	submitChoices(t, e, abilityOption(t, e, doll2, 0).Index)
	surveilArrangeAnswer(t, e, 1)
	surveilDrain(t, e)
	if e.G.Players[1].Life != life1-1 || e.G.Players[0].Life != life0+1 {
		t.Fatalf("second surveil this turn fired Snitch again: life %d/%d, want %d/%d "+
			"(FirstTime$ gate unread)", e.G.Players[0].Life, e.G.Players[1].Life, life0+1, life1-1)
	}
	if got := countSurveilMarkers(e); got != 2 {
		t.Fatalf("log carries %d Surveil markers after two surveils, want 2", got)
	}
	replayCheck(t, e, cfg)
}

// TestRiverSongOpponentSurveilFires: `ValidPlayer$ Opponent` is matched
// against the marker's acting seat -- an OPPONENT's surveil fires River
// Song's trigger (the +1/+1 counter), driving the matcher through the
// non-You player spec (via the same e.emit marker walk the corpus carrier
// would ride; the body's damage half needs a target this harness does not
// drain, so the pin is the counter).
func TestRiverSongOpponentSurveilFires(t *testing.T) {
	t.Parallel()
	river := tokenReplCorpusCard(t, "River Song")
	e, cfg := tokenReplGame(t, 86, river)
	riverID := moveSeededCard(t, e, 0, river, state.ZBattlefield)
	if got := counterOn(t, e, riverID, "P1P1"); got != 0 {
		t.Fatalf("setup: River Song already carries %d P1P1 counters, want 0", got)
	}
	// Player 1 surveils: an opponent of River Song's controller.
	e.emit(events.Event{Kind: events.Surveil, Player: 1, Obj: riverID})
	e.pending = nil
	addMana(t, e, 0, "")
	surveilDrain(t, e)
	if got := counterOn(t, e, riverID, "P1P1"); got != 1 {
		t.Fatalf("an opponent's surveil left River Song %d P1P1 counters, want 1", got)
	}
	replayCheck(t, e, cfg)
}
