package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// flickerwispSrc is an inline fixture mirroring the real Flickerwisp's
// delayed-trigger shape (Mode$ Phase | Phase$ End of Turn, the exiled
// permanent returned at the beginning of the next end step). It is written
// inline, never a .txt from .cards/ (GPL). The ChangeZone's RememberChanged$
// True is what captures the exiled permanent into the ability's Remembered,
// which DelTrig's DelayedTrigger then carries into its registration; when the
// delayed trigger fires, TrigBounce's Defined$ DelayTriggerRememberedLKI
// resolves against that Remembered and returns the right permanent.
const flickerwispSrc = "Name:Flickerwisp\nManaCost:1 W W\nTypes:Creature Elemental\nPT:3/1\nK:Flying\n" +
	"T:Mode$ ChangesZone | ValidCard$ Card.Self | Origin$ Any | Destination$ Battlefield | Execute$ TrigExile\n" +
	"SVar:TrigExile:DB$ ChangeZone | ValidTgts$ Permanent.Other | Mandatory$ True | Origin$ Battlefield | Destination$ Exile | RememberChanged$ True | SubAbility$ DelTrig\n" +
	"SVar:DelTrig:DB$ DelayedTrigger | Mode$ Phase | Phase$ End of Turn | Execute$ TrigBounce | SubAbility$ DBCleanup\n" +
	"SVar:TrigBounce:DB$ ChangeZone | Origin$ Exile | Destination$ Battlefield | Defined$ DelayTriggerRememberedLKI\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\nOracle:x\n"

// delayedFixture builds a 2-seat game, puts a target creature on seat 0's
// battlefield, casts its Flickerwisp, resolves the ETB (answering the exiled
// target) and returns the engine with the delayed trigger registered and the
// exiled target's id.
func delayedFixture(t *testing.T) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, find := etbConfig(t, 81,
		[]string{flickerwispSrc, "Name:TargetGuy\nTypes:Creature\nPT:2/2\nOracle:x\n"}, nil)
	tg := find("TargetGuy", 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: tg,
		From: e.G.Obj(tg).Zone, To: state.ZBattlefield})
	e.pending = nil
	e.Advance()
	addMana(t, e, 0, "WWW")
	castFirst(t, e, "cast")
	// Pass priority until Flickerwisp resolves onto the battlefield; its ETB
	// then triggers, and the trigger drain asks for the exile target.
	passUntilKind(t, e, decision.KTarget, 40)
	// Choose TargetGuy as the exile target, then let the ETB resolve.
	submitTarget(t, e, tg)
	passUntilStackEmpty(t, e, 40)
	if len(e.G.Delayed) != 1 {
		t.Fatalf("expected one delayed-trigger registration, got %d", len(e.G.Delayed))
	}
	if o := e.G.Obj(tg); o == nil || o.Zone != state.ZExile {
		t.Fatalf("target %d zone = %v, want exile after the ETB resolved", tg, zoneName(o))
	}
	return e, cfg, tg
}

func zoneName(o *state.Object) state.Zone {
	if o == nil {
		return 0
	}
	return o.Zone
}

// passUntilKind answers "pass" priority decisions until the pending decision
// has the given kind (or a non-priority decision appears), bounded.
func passUntilKind(t *testing.T, e *Engine, kind decision.Kind, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		if d.Kind == kind {
			return
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("expected priority decision on the way to %s, got %+v", kind, d)
		}
		submitPass(t, e)
	}
	t.Fatalf("did not reach a %s decision within %d passes", kind, limit)
}

func submitPass(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at a priority decision: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "pass" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("priority decision with no pass option: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit pass: %v", err)
	}
}

// submitTarget answers a KTarget decision with the option carrying obj.
func submitTarget(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == obj {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit target: %v", err)
			}
			return
		}
	}
	t.Fatalf("target %d not offered: %+v", obj, d.Options)
}

// driveToEndStep passes priority from the current main phase through to the
// end step of the current turn.
func driveToEndStep(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 200 && !e.G.Over; i++ {
		if e.G.Step == state.StepEnd {
			return
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KTarget:
			// A target ask (e.g. the exile target) resolved.
			t.Fatalf("unexpected target decision while driving to end step: %+v", d)
		default:
			t.Fatalf("unexpected decision %+v while driving to end step", d)
		}
	}
	if e.G.Step != state.StepEnd {
		t.Fatalf("did not reach the end step: step=%s", e.G.Step)
	}
}

// TestDelayedTriggerFlickerwispReturnsPermanent is the brief's test 1: the
// ETB exiles a permanent and the delayed trigger returns it at the beginning
// of the next end step.
func TestDelayedTriggerFlickerwispReturnsPermanent(t *testing.T) {
	t.Parallel()
	e, _, tg := delayedFixture(t)
	// Still exiled before the end step fires.
	if o := e.G.Obj(tg); o == nil || o.Zone != state.ZExile {
		t.Fatalf("target not exiled before the end step: %v", zoneName(o))
	}
	driveToEndStep(t, e)
	// The delayed trigger fires at the beginning of the end step, on the
	// stack, and its resolution returns the exiled permanent.
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(tg); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("target not returned by the end step: %v", zoneName(o))
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("delayed trigger should be gone after firing, got %d registrations", len(e.G.Delayed))
	}
}

// driveToStepAll is driveToStep extended over the combat decisions that a
// drive across a whole turn encounters: it declares no attackers and no
// blocks (CR 506/509 -- the fixture's creatures need not attack), answers a
// cleanup discard naively, and otherwise passes priority.
func driveToStepAll(t *testing.T, e *Engine, turn int32, active state.PlayerID, step state.Step) {
	t.Helper()
	for i := 0; i < 8000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before reaching turn %d seat %d step %s", turn, active, step)
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatal("no decision pending")
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		case decision.KTarget:
			// A target ask (e.g. an unanswered exile target) is unexpected
			// here; fail loudly rather than guessing.
			t.Fatalf("unexpected target decision %+v while driving", d)
		case decision.KChoose:
			// A cleanup-step discard down to the hand-size limit (CR 514.1):
			// discard the first offered card and keep driving.
			if len(d.Options) == 0 {
				t.Fatalf("KChoose with no options: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		default:
			t.Fatalf("unexpected decision %+v while driving", d)
		}
	}
	t.Fatalf("did not reach turn %d seat %d step %s", turn, active, step)
}

// TestDelayedTriggerFiresOnce is the brief's test 2: the delayed trigger
// fires once, not at every subsequent end step. One-shot is structural --
// events.Apply's DelayedPush case removes the registration, so there is
// nothing left to fire on a later occurrence of the phase. This pins both
// halves: exactly one DelayedPush in the whole log, and no registration left
// to fire again.
func TestDelayedTriggerFiresOnce(t *testing.T) {
	t.Parallel()
	e, _, tg := delayedFixture(t)
	driveToEndStep(t, e)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(tg); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("target not returned: %v", zoneName(o))
	}
	delayedPushes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedPush {
			delayedPushes++
		}
	}
	if delayedPushes != 1 {
		t.Fatalf("expected exactly one DelayedPush, got %d", delayedPushes)
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("no delayed registration should survive a firing, got %d", len(e.G.Delayed))
	}
}

// TestDelayedTriggerDoesNotRefireNextTurn is the brief's test 2 companion: on
// a later occurrence of the phase (the next turn's end step) the delayed
// trigger does not fire again -- the target stays put and no further
// DelayedPush is recorded.
func TestDelayedTriggerDoesNotRefireNextTurn(t *testing.T) {
	t.Parallel()
	e, _, tg := delayedFixture(t)
	driveToEndStep(t, e)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(tg); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("target not returned: %v", zoneName(o))
	}
	delayedPushes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedPush {
			delayedPushes++
		}
	}
	// The next end step belongs to seat 1 (the turn after seat 0's): a
	// registration that had survived would fire there, so reaching it and
	// observing no new DelayedPush is the no-refire proof.
	driveToStepAll(t, e, e.G.Turn+1, e.G.NextAlive(0), state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	after := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedPush {
			after++
		}
	}
	if after != delayedPushes {
		t.Fatalf("a second DelayedPush fired at the next end step: before=%d after=%d", delayedPushes, after)
	}
	if o := e.G.Obj(tg); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("target should remain on the battlefield next turn, got %v", zoneName(o))
	}
}

// TestDelayedTriggerFiresAfterSourceLeaves is the brief's test 3: the delayed
// trigger still fires even when its source has left the battlefield.
func TestDelayedTriggerFiresAfterSourceLeaves(t *testing.T) {
	t.Parallel()
	e, _, tg := delayedFixture(t)
	// Destroy Flickerwisp (its source) before the end step.
	fw := findByName(e, "Flickerwisp", 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: fw,
		From: state.ZBattlefield, To: state.ZGraveyard, Text: "destroyed"})
	if o := e.G.Obj(tg); o == nil || o.Zone != state.ZExile {
		t.Fatalf("target should still be exiled: %v", zoneName(o))
	}
	driveToEndStep(t, e)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(tg); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("delayed trigger should still fire with its source gone: %v", zoneName(o))
	}
}

// TestDelayedTriggerDegradesWhenReferentMoved is the brief's test 4: if the
// object it refers to has changed zones since registration, the ability does
// what it can and the rest does nothing -- no crash.
func TestDelayedTriggerDegradesWhenReferentMoved(t *testing.T) {
	t.Parallel()
	e, _, tg := delayedFixture(t)
	// Move the exiled referent out of exile before the end step (e.g. it was
	// returned another way), so the delayed trigger's TrigBounce finds nothing
	// in exile to move.
	e.emit(events.Event{Kind: events.MoveZone, Obj: tg,
		From: state.ZExile, To: state.ZGraveyard})
	driveToEndStep(t, e)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(tg); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("referent should stay in the graveyard (the return does nothing): %v", zoneName(o))
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("the delayed registration should still be consumed by firing, got %d", len(e.G.Delayed))
	}
}

// TestDelayedTriggerReplaysIdentically is the brief's test 5: a game
// containing a delayed trigger replays byte-identically from its
// (Config, Log), proving the registration and its firing both survive the
// event log.
func TestDelayedTriggerReplaysIdentically(t *testing.T) {
	t.Parallel()
	e, cfg, tg := delayedFixture(t)

	// Before the end step, a log-alone reconstruction (events.Apply, no
	// engine) of everything so far must also hold the registration -- the
	// direct proof the registration survives the log rather than living only
	// in an engine field.
	snap := replayFromLog(t, cfg, e.L.Events)
	if len(snap.Delayed) != 1 || len(e.G.Delayed) != 1 {
		t.Fatalf("registration should reconstruct from the log: live=%d, replay=%d",
			len(e.G.Delayed), len(snap.Delayed))
	}

	driveToEndStep(t, e)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(tg); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("target not returned: %v", zoneName(o))
	}

	// Ruling dt1-a: the registration and its firing must reconstruct from the
	// EVENT LOG ALONE (events.Apply, no engine re-simulation) -- a replay
	// that rebuilt the game this way is the strongest proof a registration
	// stored only in an in-memory field would fail. replayFromLog folds every
	// logged event into a fresh Game; the rebuilt one must match the live
	// game exactly (delayed registrations included).
	fresh := replayFromLog(t, cfg, e.L.Events)
	if diff := diffGames(e.G, fresh); diff != "" {
		t.Fatalf("log-alone reconstruction diverged:\n%s", diff)
	}
	// Both the live game (which fired the delayed trigger) and a log-only
	// reconstruction must agree that the registration is gone -- the one-shot
	// removal is itself reconstructible from the log.
	if len(e.G.Delayed) != 0 || len(fresh.Delayed) != 0 {
		t.Fatalf("delayed registrations differ after firing: live=%d, replay=%d",
			len(e.G.Delayed), len(fresh.Delayed))
	}
}

// TestDelayedTriggerCloneIsIndependent is the brief's test 6: a Clone taken
// between registration and firing behaves correctly and does not share
// mutable state with its original.
func TestDelayedTriggerCloneIsIndependent(t *testing.T) {
	t.Parallel()
	e, _, tg := delayedFixture(t)
	// Clone while the registration is pending (before the end step). Both
	// must behave independently.
	c := e.Clone()
	// Firing on the original should not be visible to the clone's pending
	// registration: advance the original to the end step, fire, and check the
	// clone still holds its registration and has not returned the target.
	driveToEndStep(t, e)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(tg); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("original target not returned: %v", zoneName(o))
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("original should have no registration left, got %d", len(e.G.Delayed))
	}
	if len(c.G.Delayed) != 1 {
		t.Fatalf("clone should still hold the registration, got %d", len(c.G.Delayed))
	}
	if o := c.G.Obj(tg); o == nil || o.Zone != state.ZExile {
		t.Fatalf("clone's target should still be exiled (no shared state): %v", zoneName(o))
	}
	// Firing the clone's own registration (drive it to a fresh end step) must
	// also work and return the target, independently.
	driveToEndStep(t, c)
	passUntilStackEmpty(t, c, 40)
	if o := c.G.Obj(tg); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("clone target not returned: %v", zoneName(o))
	}
}
