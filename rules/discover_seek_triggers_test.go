package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The trig:Discover / trig:SeekAll halves (task trigdisc1): the
// events.Discover / events.Seek records are what the modes match. The
// emitting primitives (api:Discover, api:Seek) are still unimplemented, so
// like the pre-api matcher probes this file drives the markers directly
// through e.emit -- the same checkTriggers walk a real discover/seek action's
// marker will ride -- and pins the matchers, the queue-time gates and the
// shared bodies on the REAL compiled corpus SAs. Replay-verified throughout
// (the markers are Apply no-ops, so a replay re-derives every trigger push
// from the logged markers byte-identically).
//
// Corpus carriers (2 Discover files, 3 SeekAll files): Val, Marooned Surveyor
// (primary Discover + secondary SeekAll sharing one Execute$ TrigDmg body),
// Curator of Sun's Creation (Discover, ActivationLimit$ 1), Vexyr, Ich-Tekik's
// Heir (SeekAll -> Golem token), Lurker in the Deep (SeekAll, PlayerTurn$ True;
// body needs api:Conjure/MakeCard -- out of scope, not pinned here).

// discoverSeekMarker emits one marker record for p (the acting seat) from
// source src and clears the engine's pending decision, the moveSeededCard
// discipline: a marker emit pushes its triggers synchronously inside emit, and
// the harness re-establishes a fresh priority round through addMana before
// draining.
func discoverSeekMarker(t *testing.T, e *Engine, kind events.Kind, p state.PlayerID, src state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: kind, Player: p, Obj: src})
	e.pending = nil
}

// triggerPushesFor counts the trigger pushes in the log naming source src --
// the queue-time observable for a trigger whose body does not yet move state
// (Curator's DB$ Discover needs api:Discover, so its firing is visible only
// as the push, not as a state change).
func triggerPushesFor(e *Engine, src state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == src {
			n++
		}
	}
	return n
}

// TestDiscoverSupported and TestSeekAllSupported: the coverage census (make
// report) reads effects.Supported(), so a missing entry would silently keep
// every carrier unplayable.
func TestDiscoverSupported(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["trig:Discover"] {
		t.Fatalf("effects.Supported() is missing trig:Discover")
	}
}

func TestSeekAllSupported(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["trig:SeekAll"] {
		t.Fatalf("effects.Supported() is missing trig:SeekAll")
	}
}

// TestDiscoverSeekTriggersAreRegistered pins both support declarations in one
// place (the TestNumLoyaltyActPrimitiveIsRegistered shape).
func TestDiscoverSeekTriggersAreRegistered(t *testing.T) {
	t.Parallel()
	supported := effects.Supported()
	if !supported["trig:Discover"] || !supported["trig:SeekAll"] {
		t.Fatalf("effects.Supported() is missing trig:Discover (%v) or trig:SeekAll (%v)",
			supported["trig:Discover"], supported["trig:SeekAll"])
	}
}

// TestValMaroonedSurveyorDiscoverMarkerDealsDamageAndGainsLife: Val on the
// battlefield, one Discover marker fires the PRIMARY half -- 2 damage to each
// opponent, 2 life to Val's controller -- replay-verified.
func TestValMaroonedSurveyorDiscoverMarkerDealsDamageAndGainsLife(t *testing.T) {
	t.Parallel()
	val := tokenReplCorpusCard(t, "Val, Marooned Surveyor")
	e, cfg := tokenReplGame(t, 91, val)
	valID := moveSeededCard(t, e, 0, val, state.ZBattlefield)
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	discoverSeekMarker(t, e, events.Discover, 0, valID)
	addMana(t, e, 0, "")
	investigateDrain(t, e)
	if e.G.Players[1].Life != life1-2 {
		t.Fatalf("Discover marker left opponent at %d life, want %d (2 damage)",
			e.G.Players[1].Life, life1-2)
	}
	if e.G.Players[0].Life != life0+2 {
		t.Fatalf("Discover marker left Val's controller at %d life, want %d (+2)",
			e.G.Players[0].Life, life0+2)
	}
	replayCheck(t, e, cfg)
}

// TestValMaroonedSurveyorSeekMarkerFiresTheSecondaryHalfOnItsOwn: the SeekAll
// line is Secondary$ True paired with the Discover primary (same Execute$
// TrigDmg). A Seek event does not match the primary (wrong Kind), so the
// secondary fires on its own -- the same body, 2 damage to each opponent + 2
// life -- replay-verified.
func TestValMaroonedSurveyorSeekMarkerFiresTheSecondaryHalfOnItsOwn(t *testing.T) {
	t.Parallel()
	val := tokenReplCorpusCard(t, "Val, Marooned Surveyor")
	e, cfg := tokenReplGame(t, 92, val)
	valID := moveSeededCard(t, e, 0, val, state.ZBattlefield)
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	discoverSeekMarker(t, e, events.Seek, 0, valID)
	addMana(t, e, 0, "")
	investigateDrain(t, e)
	if e.G.Players[1].Life != life1-2 || e.G.Players[0].Life != life0+2 {
		t.Fatalf("Seek marker did not fire the SeekAll secondary on its own: life %d/%d, want %d/%d",
			e.G.Players[0].Life, e.G.Players[1].Life, life0+2, life1-2)
	}
	replayCheck(t, e, cfg)
}

// TestValMaroonedSurveyorMarkerFromAnotherPlayerDoesNotFire: ValidPlayer$ You
// is matched against the marker's acting seat, so a Discover record named by
// another player leaves Val silent (the non-matching-player pin).
func TestValMaroonedSurveyorMarkerFromAnotherPlayerDoesNotFire(t *testing.T) {
	t.Parallel()
	val := tokenReplCorpusCard(t, "Val, Marooned Surveyor")
	e, cfg := tokenReplGame(t, 93, val)
	valID := moveSeededCard(t, e, 0, val, state.ZBattlefield)
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	discoverSeekMarker(t, e, events.Discover, 1, valID)
	addMana(t, e, 0, "")
	investigateDrain(t, e)
	if e.G.Players[0].Life != life0 || e.G.Players[1].Life != life1 {
		t.Fatalf("another player's Discover marker fired Val: life %d/%d, want %d/%d",
			e.G.Players[0].Life, e.G.Players[1].Life, life0, life1)
	}
	replayCheck(t, e, cfg)
}

// TestCuratorOfSunsCreationDiscoverFiresOnceEachTurn: the trigger fires on the
// first Discover marker of the turn (visible as one TriggerPush; the body's
// DB$ Discover needs the still-unimplemented api:Discover, so the firing is
// the observable) and a SECOND Discover marker the same turn is withheld by
// ActivationLimit$ 1 ("This ability triggers only once each turn") -- the
// queue-time gate the actionTriggerModes membership makes apply.
func TestCuratorOfSunsCreationDiscoverFiresOnceEachTurn(t *testing.T) {
	t.Parallel()
	curator := tokenReplCorpusCard(t, "Curator of Sun's Creation")
	e, cfg := tokenReplGame(t, 94, curator)
	cID := moveSeededCard(t, e, 0, curator, state.ZBattlefield)
	discoverSeekMarker(t, e, events.Discover, 0, cID)
	addMana(t, e, 0, "")
	investigateDrain(t, e)
	if got := triggerPushesFor(e, cID); got != 1 {
		t.Fatalf("first Discover marker left %d trigger pushes, want 1", got)
	}
	discoverSeekMarker(t, e, events.Discover, 0, cID)
	addMana(t, e, 0, "")
	investigateDrain(t, e)
	if got := triggerPushesFor(e, cID); got != 1 {
		t.Fatalf("second Discover marker the same turn fired again: %d pushes, want 1 (ActivationLimit$ 1 unread)", got)
	}
	replayCheck(t, e, cfg)
}

// TestVexyrSeekMarkerCreatesTheGolemToken: one Seek marker is ONE seek action,
// so Vexyr's "Whenever you seek one or more cards" fires once and creates
// exactly one 3/3 colorless Phyrexian Golem artifact creature token (the real
// c_3_3_a_phyrexian_golem token script), replay-verified.
func TestVexyrSeekMarkerCreatesTheGolemToken(t *testing.T) {
	t.Parallel()
	vexyr := tokenReplCorpusCard(t, "Vexyr, Ich-Tekik's Heir")
	e, cfg := tokenReplGame(t, 95, vexyr)
	vID := moveSeededCard(t, e, 0, vexyr, state.ZBattlefield)
	discoverSeekMarker(t, e, events.Seek, 0, vID)
	addMana(t, e, 0, "")
	investigateDrain(t, e)
	if got := countTokensNamedOnSeat(t, e, 0, "Phyrexian Golem Token"); got != 1 {
		t.Fatalf("one Seek marker created %d Golem tokens, want 1", got)
	}
	replayCheck(t, e, cfg)
}
