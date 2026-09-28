package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Round-2 regressions for the UntilYourNextTurn / UntilTheEndOfYourNextTurn
// next-turn boundary scheduler, all on REAL corpus cards:
//
//   - a pending extra turn an R:Event$ BeginTurn | ExtraTurn$ True | Skip$
//     True replacement makes the seat SKIP produces no turn, so nextTurnFor
//     must apply the same skip decision rules/turn.go's consumer does; the
//     carrier here is Ugin's Nexus ("if a player would begin an extra turn,
//     that player skips that turn instead"), a real corpus card.
//   - GainControl's LoseControl$ UntilTheEndOfYourNextTurn carries the same
//     boundary in controlGrant.untilTurn; the carrier here is the real corpus
//     Power of Persuasion d20=20 SVar (DB$ GainControl | Defined$ Targeted |
//     LoseControl$ UntilTheEndOfYourNextTurn), resolved through the ordinary
//     effects.Resolve path exactly as the card's own resolution would.
//
// Every test asserts its precondition (the effect registered, the skip
// carrier is on the battlefield) so a vacuous setup fails loudly.

// skipCorpusEngine builds a 2-seat corpus game with Ugin's Nexus on seat 0's
// battlefield (it skips ANY player's extra turn, its controller's included,
// so one carrier serves both directions) and Karn + Sol Ring in seat 0's
// deck.
func skipCorpusEngine(t *testing.T) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{
			lookup(t, reg, "Karn, the Great Creator"),
			lookup(t, reg, "Sol Ring"),
			lookup(t, reg, "Ugin's Nexus"),
		},
		nil)
	nexus := moveByName(t, e, 0, "Ugin's Nexus", state.ZBattlefield)
	if e.G.Obj(nexus).Zone != state.ZBattlefield {
		t.Fatal("precondition: Ugin's Nexus must be on the battlefield to skip extra turns")
	}
	return e, cfg
}

// animateKarnRing animates a real corpus Sol Ring with a real corpus Karn's
// +1 and returns the ring. It fails loudly if the animation did not happen.
func animateKarnRing(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	karn := moveByName(t, e, 0, "Karn, the Great Creator", state.ZBattlefield)
	ring := moveByName(t, e, 0, "Sol Ring", state.ZBattlefield)
	if e.IsCreature(ring) || e.G.Obj(karn).Counter("LOYALTY") <= 0 {
		t.Fatal("precondition: Karn must have loyalty and Sol Ring must be a noncreature")
	}
	e.pending = nil
	e.priorityRound()
	submitChoices(t, e, abilityOption(t, e, karn, 0).Index)
	submitTarget(t, e, ring)
	passUntilStackEmpty(t, e, 40)
	if !e.IsCreature(ring) {
		t.Fatal("precondition: Karn's +1 did not animate Sol Ring")
	}
	return ring
}

// TestNextTurnForIgnoresAPendingSkippedGrant is the opponent-direction
// regression for finding 1. It is a DIRECT unit test of nextTurnFor, not an
// end-to-end drive, because the per-cleanup reschedule self-corrects an
// over-long boundary the moment the skipped grant drains from the queue --
// so a skipped OPPONENT grant has no observable end-to-end effect (only the
// controller's own skip can drop the effect on the grant's own cleanup; see
// TestKarnAnimateSurvivesASkippedOwnExtraTurn). The function the finding
// names must still ignore the pending entry, and this pins that.
//
// Seat 0 is active on turn 1 with Ugin's Nexus live; seat 1's pending extra
// turn is skipped. Premise: seat 0's next turn is turn 3 either way, so a
// pending skipped grant must NOT move nextTurnFor(0) at all.
func TestNextTurnForIgnoresAPendingSkippedGrant(t *testing.T) {
	t.Parallel()
	e, _ := skipCorpusEngine(t)
	baseline := e.nextTurnFor(0)
	e.emit(events.Event{Kind: events.ExtraTurn, Player: 1, Amount: 1})
	if len(e.G.ExtraTurnQueue) == 0 || e.G.ExtraTurnQueue[len(e.G.ExtraTurnQueue)-1].Player != 1 {
		t.Fatalf("precondition: seat 1's extra turn is not pending: %v", e.G.ExtraTurnQueue)
	}
	if skip, _ := e.extraTurnSkipped(1); !skip {
		t.Fatal("precondition: Ugin's Nexus did not name seat 1's extra turn as skipped")
	}
	if got := e.nextTurnFor(0); got != baseline {
		t.Fatalf("a pending SKIPPED grant moved seat 0's next turn: got %d, want %d (the no-grant value)",
			got, baseline)
	}
}

// TestKarnAnimateSurvivesASkippedOwnExtraTurn covers the "expires too early"
// direction: a late +1 grant to the animation's OWN controller is skipped by
// live Ugin's Nexus, so the controller takes no extra turn and their next
// actual turn is the ordinary rotation's (turn 3). The animation must
// therefore still be live on the opponent's turn 2 and end only at turn 2's
// cleanup. Pre-fix, nextTurnFor counted the skipped grant as turn 2, moving
// the boundary to 1 and dropping the animation at turn 1's cleanup.
func TestKarnAnimateSurvivesASkippedOwnExtraTurn(t *testing.T) {
	t.Parallel()
	e, cfg := skipCorpusEngine(t)
	ring := animateKarnRing(t, e)

	// The late grant: seat 0's extra turn, which Ugin's Nexus skips.
	e.emit(events.Event{Kind: events.ExtraTurn, Player: 0, Amount: 1})
	// Precondition: the grant is really pending and really skipped.
	if len(e.G.ExtraTurnQueue) == 0 || e.G.ExtraTurnQueue[len(e.G.ExtraTurnQueue)-1].Player != 0 {
		t.Fatalf("precondition: seat 0's extra turn is not pending: %v", e.G.ExtraTurnQueue)
	}
	if skip, _ := e.extraTurnSkipped(0); !skip {
		t.Fatal("precondition: Ugin's Nexus did not name seat 0's extra turn as skipped")
	}

	// Turn 2 is seat 1's normal turn (seat 0's own extra was skipped). The
	// animation must still be live: seat 0's next actual turn is turn 3.
	driveToStep(t, e, 2, 1, state.StepMain1)
	if !e.IsCreature(ring) {
		t.Fatal("animation expired at turn 1's cleanup although the extra turn it was granted was SKIPPED")
	}
	// Turn 3 is seat 0's next actual turn: the animation ends as it begins.
	driveToStep(t, e, 3, 0, state.StepMain1)
	if e.IsCreature(ring) {
		t.Fatal("animation survived into seat 0's next actual turn")
	}
	replayCheck(t, e, cfg)
}

// controlBoundaryOf reads the untilTurn a live GainControl next-turn grant
// carries.
func controlBoundaryOf(t *testing.T, e *Engine, obj state.ObjID) int32 {
	t.Helper()
	for i := range e.controlGrants {
		g := &e.controlGrants[i]
		if g.Obj == obj && g.Duration.NextTurn {
			return g.untilTurn
		}
	}
	t.Fatal("precondition: no next-turn GainControl grant is registered for the object")
	return 0
}

// steelRealArtifact resolves Power of Persuasion's real corpus d20=20 SVar
// (DB$ GainControl | Defined$ Targeted | LoseControl$ UntilTheEndOfYourNextTurn)
// against an artifact seat 1 controls, exactly as the card's own resolution
// would once the die shows 20.
func stealRealArtifact(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	pop := e.G.AddObject(choiceCorpusCard(t, "Power of Persuasion"), 0)
	stolen := e.G.AddObject(card(t, "Name:Relic\nTypes:Artifact\nOracle:x\n"), 1)
	for _, id := range []state.ObjID{pop.ID, stolen.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	}
	sa := cards.ResolveSVar(pop.Face().SVars, "DBControl")
	if sa == nil {
		t.Fatal("precondition: Power of Persuasion has no DBControl SVar")
	}
	effects.Resolve(e, &effects.Ctx{Source: pop.ID, Controller: 0,
		Targets: []state.Target{{Obj: stolen.ID}}, TargetsOffered: true, SVars: pop.Face().SVars}, sa)
	if e.G.Obj(stolen.ID).Controller != 0 {
		t.Fatalf("precondition: GainControl did not steal the artifact (controller %d)", e.G.Obj(stolen.ID).Controller)
	}
	return stolen.ID
}

// TestControlNextTurnEndsOnLateControllerExtraTurn: a GainControl's
// UntilTheEndOfYourNextTurn boundary must move EARLIER when the controller
// gets a late extra turn -- that extra turn IS their next turn, so the steal
// ends at the extra turn's cleanup. Pre-fix, rescheduleNextTurnBoundaries
// only rewrote e.continuous, leaving controlGrant.untilTurn frozen at 3 and
// the artifact stolen through seat 0's turn 3.
func TestControlNextTurnEndsOnLateControllerExtraTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := corpusEngineCfg(t, reg, nil, nil)
	stolen := stealRealArtifact(t, e)
	if b := controlBoundaryOf(t, e, stolen); b != 3 {
		t.Fatalf("registration-time control boundary = turn %d, want 3", b)
	}

	// Seat 0's late extra turn becomes their next turn (turn 2).
	e.emit(events.Event{Kind: events.ExtraTurn, Player: 0, Amount: 1})
	driveToStep(t, e, 2, 0, state.StepMain1)
	if e.G.Obj(stolen).Controller != 0 {
		t.Fatalf("steal ended before the controller's extra turn: controller %d", e.G.Obj(stolen).Controller)
	}
	driveToStep(t, e, 3, 1, state.StepMain1)
	if e.G.Obj(stolen).Controller != 1 {
		t.Fatalf("steal outlived the controller's extra next turn: controller %d, want 1", e.G.Obj(stolen).Controller)
	}
}

// TestControlNextTurnSurvivesLateOpponentExtraTurn: a GainControl's
// UntilTheEndOfYourNextTurn boundary must move LATER when another seat gets a
// late extra turn -- the inserted turns precede the controller's next turn,
// so the steal outlives them. Pre-fix the frozen untilTurn=3 ended the steal
// at turn 3's cleanup even though seat 0's next turn had become turn 4.
func TestControlNextTurnSurvivesLateOpponentExtraTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := corpusEngineCfg(t, reg, nil, nil)
	stolen := stealRealArtifact(t, e)

	e.emit(events.Event{Kind: events.ExtraTurn, Player: 1, Amount: 1})
	driveToStep(t, e, 2, 1, state.StepMain1) // opponent's extra turn
	if e.G.Obj(stolen).Controller != 0 {
		t.Fatalf("steal ended during the opponent's extra turn: controller %d", e.G.Obj(stolen).Controller)
	}
	driveToStep(t, e, 3, 1, state.StepMain1) // opponent's normal turn
	if e.G.Obj(stolen).Controller != 0 {
		t.Fatalf("steal ended before the controller's next actual turn: controller %d", e.G.Obj(stolen).Controller)
	}
	driveToStep(t, e, 4, 0, state.StepMain1) // seat 0's next actual turn
	if e.G.Obj(stolen).Controller != 0 {
		t.Fatalf("steal ended on the wrong cleanup: controller %d, want 0 on seat 0's turn 4", e.G.Obj(stolen).Controller)
	}
	driveToStep(t, e, 5, 1, state.StepMain1)
	if e.G.Obj(stolen).Controller != 1 {
		t.Fatalf("steal never ended: controller %d, want 1 after seat 0's next turn", e.G.Obj(stolen).Controller)
	}
}
