package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// trig:Exerted ("Whenever you exert a creature, ...") pinned end to end on
// real corpus cards. Task exert1 built the attack-time exert ELECTION
// (S:Mode$ OptionalAttackCost, events.Exert, the untap-step skip) and the
// static's own Trigger$ rider; this mode is the separate listener a different
// script line carries. Vizier of the True drives the main flow: its Exerted
// line queues a DB$ Tap whose target is a creature an opponent controls.
// Trueheart Twins pins the no-target half (DB$ PumpAll). Both are the corpus's
// real carriers (5 files carry T:Mode$ Exerted, all ValidCard$ Creature.YouCtrl).

// exertedEngine builds a two-seat Mountain-deck game with the exert carrier
// on seat 0 and a target creature on seat 1, at Main1 of turn 1.
func exertedEngine(t *testing.T, reg *cards.Registry, carrier *cards.Card) (*Engine, Config) {
	t.Helper()
	e, cfg := addPhaseEngine(t, reg, []*cards.Card{carrier}, []*cards.Card{card(t, exertBearSrc)})
	return e, cfg
}

// exertedTriggerCount counts Exert events that reached the printed Exerted
// trigger, i.e. positive Exert events on which a trigger was queued. It reads
// the positive Exert event count -- the trigger's own queueing is asserted by
// the callers through pendingTriggers/stack, so this is just the event side.
func exertedTriggerCount(e *Engine) int { return exertCount(e, true) }

// driveToExertTurn passes priority (declining every attackers and blockers
// declaration, and answering a cleanup discard naively) until it is turn 3
// seat 0's Main1 -- the first turn the test's exert carrier has been under
// its controller's control long enough to attack, without an intervening
// combat killing it. A plain driveToTurn would answer the intervening
// attackers decisions with their first option and trade creatures off.
func driveToExertTurn(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn >= 3 && e.G.Active == 0 && e.G.Step == state.StepMain1 {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before turn 3 seat 0 main1 (turn %d seat %d step %s)", e.G.Turn, e.G.Active, e.G.Step)
		}
		d := e.Pending()
		if d == nil {
			continue
		}
		switch d.Kind {
		case decision.KPriority:
			passOnce(t, e)
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		default:
			if answerIfDiscard(t, e) {
				continue
			}
			if len(d.Options) == 0 {
				t.Fatalf("empty decision %+v while driving to the exert turn", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit: %v", err)
			}
		}
	}
	t.Fatal("did not reach turn 3 seat 0 main1")
}

// TestVizierOfTheTrueExertedTriggerTapsAnOpponentCreature is the leaf: Vizier
// of the True attacks, its exert election is accepted, the Exert event fires
// its T:Mode$ Exerted line, the queued DB$ Tap asks for a creature an
// opponent controls, and the answered target is tapped. The whole game
// replays byte-identically.
func TestVizierOfTheTrueExertedTriggerTapsAnOpponentCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := exertedEngine(t, reg, lookup(t, reg, "Vizier of the True"))
	viz := moveByName(t, e, 0, "Vizier of the True", state.ZBattlefield)
	bear := moveByName(t, e, 1, "Bear", state.ZBattlefield)
	driveToExertTurn(t, e)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, viz)

	// The exert election is pending; accept it (option 1).
	exertPending(t, e, viz)
	submitChoices(t, e, 1)
	if got := exertedTriggerCount(e); got != 1 {
		t.Fatalf("%d positive Exert events, want 1", got)
	}
	// The Exerted trigger queued: drive the resolution and answer the tap's
	// target ask with the opponent's Bear.
	var sawTarget bool
	for i := 0; i < 40; i++ {
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
			continue
		}
		d := e.Pending()
		if d != nil && d.Kind == decision.KTriggerOptional {
			submitChoices(t, e, 0)
			continue
		}
		if d != nil && d.Kind == decision.KTarget {
			sawTarget = true
			idx := -1
			for i, o := range d.Options {
				if o.Obj == bear {
					idx = i
				}
			}
			if idx < 0 {
				t.Fatalf("the tap's target ask did not offer the opponent's Bear: %+v", d.Options)
			}
			submitChoices(t, e, idx)
			continue
		}
		if len(e.G.Stack) > 0 {
			e.resolveTop()
			continue
		}
		break
	}
	if !sawTarget {
		t.Fatal("the Exerted trigger never asked to tap a creature an opponent controls")
	}
	if !e.G.Obj(bear).Tapped {
		t.Fatalf("the exerted trigger did not tap the opponent's Bear (tapped=%v)", e.G.Obj(bear).Tapped)
	}
	replayCheck(t, e, cfg)
}

// TestTrueheartTwinsExertedTriggerPumpsYourTeam pins the no-target half: the
// Exerted line's DB$ PumpAll gives creatures you control +1/+0 until end of
// turn, so the Twins' derived power rises by one.
func TestTrueheartTwinsExertedTriggerPumpsYourTeam(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := exertedEngine(t, reg, lookup(t, reg, "Trueheart Twins"))
	tt := moveByName(t, e, 0, "Trueheart Twins", state.ZBattlefield)
	driveToExertTurn(t, e)
	basePower := e.Power(tt)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, tt)

	exertPending(t, e, tt)
	submitChoices(t, e, 1)
	if got := exertedTriggerCount(e); got != 1 {
		t.Fatalf("%d positive Exert events, want 1", got)
	}
	passUntilStackEmpty(t, e, 40)
	if got := e.Power(tt); got != basePower+1 {
		t.Fatalf("power after the Exerted PumpAll = %d, want %d", got, basePower+1)
	}
	replayCheck(t, e, cfg)
}

// TestExertedMatchesRejectsTheConsumeMarker pins the matcher's Amount gate
// directly: the Exert itself (Amount >= 0) matches, the untap-step consume
// marker (Amount == -1) does not. The marker is the same events.Exert Kind,
// so no other gate distinguishes it; this is the one that must.
func TestExertedMatchesRejectsTheConsumeMarker(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := exertedEngine(t, reg, lookup(t, reg, "Vizier of the True"))
	viz := moveByName(t, e, 0, "Vizier of the True", state.ZBattlefield)
	tr := cards.Trigger{Mode: "Exerted", Params: map[string]string{"ValidCard": "Creature.YouCtrl"}}

	if !trigmatch.ExertedMatches(boardOf(e), tr, viz, events.Event{Kind: events.Exert, Obj: viz, Amount: 0}) {
		t.Fatal("the Exert itself (Amount 0) did not match trig:Exerted")
	}
	if trigmatch.ExertedMatches(boardOf(e), tr, viz, events.Event{Kind: events.Exert, Obj: viz, Amount: -1}) {
		t.Fatal("the Amount -1 consume marker must not match trig:Exerted")
	}
	if trigmatch.ExertedMatches(boardOf(e), tr, viz, events.Event{Kind: events.Damage, Obj: viz}) {
		t.Fatal("a non-Exert event must not match trig:Exerted")
	}
	// Control: an opponent's creature is not Creature.YouCtrl for a trigger
	// whose source is your Vizier.
	other := moveByName(t, e, 1, "Bear", state.ZBattlefield)
	if trigmatch.ExertedMatches(boardOf(e), tr, viz, events.Event{Kind: events.Exert, Obj: other, Amount: 0}) {
		t.Fatal("an opponent's creature matched the YouCtrl ValidCard$ filter")
	}
}

// TestExertedModeEligibilityMask pins triggerModeEvents' new row: the mode is
// eligible on events.Exert only, and not on the events other modes read (the
// compiled-interest prefilter must admit the event, but the mode's own mask
// stays exact).
func TestExertedModeEligibilityMask(t *testing.T) {
	t.Parallel()
	m := triggerModeEvents("Exerted")
	if !m.allows(events.Exert) {
		t.Fatal("triggerModeEvents(Exerted) does not allow events.Exert")
	}
	for _, k := range []events.Kind{events.Damage, events.MoveZone, events.Tap, events.PutOnStack, events.StepChange} {
		if m.allows(k) {
			t.Fatalf("triggerModeEvents(Exerted) unexpectedly allows kind %d", k)
		}
	}
}

// TestExertedPrimitiveIsDeclaredSupported pins the support declaration
// cmd/forgec's report and deck validation read (effects.Supported, populated
// by rules/trigger_match.go's init via effects.RegisterNonAPI). The matcher
// alone does not make the card playable to the report: the "trig:Exerted"
// entry is what cards.Registry.Unsupported consults, so a future mode that
// adds a manifest this list forgets is silently undercounted -- this is that
// list's own pin for Exerted.
func TestExertedPrimitiveIsDeclaredSupported(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["trig:Exerted"] {
		t.Fatal(`effects.Supported() is missing "trig:Exerted"`)
	}
}
