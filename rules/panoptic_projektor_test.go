// stat:Panharmonicon.ValidTurned end-to-end: Panoptic Projektor's
//
//	S:Mode$ Panharmonicon | ValidMode$ TurnFaceUp | ValidCard$ Permanent.YouCtrl |
//	ValidTurned$ Permanent
//
// ("If turning a face-down permanent face up causes a triggered ability of a
// permanent you control to trigger, that ability triggers an additional
// time") against a real trig:TurnFaceUp ability.
//
// The card's own semantics couple two scopes: ValidCard$ scopes the trigger's
// SOURCE permanent, ValidTurned$ scopes the permanent that was turned face up
// (the turn-up event's object). For a self-turn-up trigger the two are the
// same object, so Panoptic Projektor alone cannot prove ValidTurned$ is read.
// The scope-probe static below pins that half with a ValidTurned$ the turned
// creature never satisfies: with the read the trigger fires once, without it
// the doubling over-applies.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// faceUpProbeSrc is a freely-authored creature whose only ability is the
// turned-face-up self-trigger: "When CARDNAME is turned face up, you gain 1
// life." GainLife is the cheapest observable -- one folded number, no token
// or stack machinery -- and the trigger is mandatory (no OptionalDecider$),
// so the test never has to answer an ask to see the doubling.
const faceUpProbeSrc = "Name:Faceup Probe\nManaCost:1\nTypes:Creature Beast\nPT:2/2\n" +
	"T:Mode$ TurnFaceUp | ValidCard$ Card.Self | Execute$ ProbeLife | TriggerZones$ Battlefield | TriggerDescription$ When CARDNAME is turned face up, you gain 1 life.\n" +
	"SVar:ProbeLife:DB$ GainLife | LifeAmount$ 1\n"

// turnedScopeProbeSrc is a Panharmonicon static scoped to a LAND's turn-up.
// ValidCard$ still matches the creature whose trigger fired (Permanent.YouCtrl
// scopes the trigger source), but ValidTurned$ Land can never match a turned
// creature -- so a correct engine leaves the one trigger alone.
//
// This is a test-authored static, not a corpus card: no corpus carrier pairs
// ValidTurned$ with a mismatching scope (Panoptic Projektor is the corpus's
// only ValidTurned$ line, and Permanent is the widest possible scope). Without
// it the ValidTurned$ read is unprovable, because Panoptic Projektor's own
// spec matches every turned permanent.
const turnedScopeProbeSrc = "Name:Turned Scope Probe\nManaCost:2\nTypes:Artifact\n" +
	"S:Mode$ Panharmonicon | ValidMode$ TurnFaceUp | ValidCard$ Permanent.YouCtrl | ValidTurned$ Land | Description$ If turning a face-down LAND face up causes a triggered ability of a permanent you control to trigger, that ability triggers an additional time.\n"

// turnUpScenario builds a two-seat engine, puts each already-compiled extra on
// seat 0's battlefield (Panoptic Projektor or the scope probe), places a probe
// there too, marks the probe face down, and returns the engine, the probe id
// and seat 0's life total at the start. The probe's FaceDown precondition is
// asserted here: a probe that was never face down would make the turn-up a
// no-op and the whole test vacuous.
func turnUpScenario(t *testing.T, extras ...*cards.Card) (*Engine, state.ObjID, int32) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, nil, nil)
	for _, c := range extras {
		onBoardCard(t, e, 0, c)
	}
	probe := onBoardCard(t, e, 0, card(t, faceUpProbeSrc))
	o := e.G.Obj(probe)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: probe not on seat 0's battlefield (%+v)", o)
	}
	o.FaceDown = true
	return e, probe, e.G.Players[0].Life
}

// turnUp emits the turn-up and asserts the events.Apply half ran (the face-down
// marker is gone) before draining the trigger queue. A turn-up that left the
// marker in place would silently prove nothing about the feature.
func turnUp(t *testing.T, e *Engine, probe state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.TurnFaceUp, Obj: probe})
	if e.G.Obj(probe).FaceDown {
		t.Fatal("the turn-up event did not clear the probe's FaceDown marker")
	}
	answerQuiet(t, e, 60)
}

// TestPanopticProjektorDoublesTurnFaceUpTrigger is the brief's positive: with
// Panoptic Projektor on the battlefield, a permanent you control's
// Mode$ TurnFaceUp trigger fires an additional time.
func TestPanopticProjektorDoublesTurnFaceUpTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, probe, before := turnUpScenario(t, lookup(t, reg, "Panoptic Projektor"))

	turnUp(t, e, probe)
	if got := e.G.Players[0].Life - before; got != 2 {
		t.Fatalf("Panoptic Projektor turned up a probe for %d life, want 2 (the TurnFaceUp trigger doubled)", got)
	}
}

// TestTurnFaceUpTriggerFiresOnceWithoutPanharmonicon is the baseline the
// doubling is measured against: the same turn-up with no Panharmonicon
// static gains 1 life, so the test above is comparing a real +1, not a
// no-op.
func TestTurnFaceUpTriggerFiresOnceWithoutPanharmonicon(t *testing.T) {
	t.Parallel()
	e, probe, before := turnUpScenario(t)

	turnUp(t, e, probe)
	if got := e.G.Players[0].Life - before; got != 1 {
		t.Fatalf("the lone TurnFaceUp trigger gained %d life, want 1", got)
	}
}

// TestPanharmoniconValidTurnedScopesTheTurnedPermanent is the half Panoptic
// Projektor cannot prove: a Panharmonicon static whose ValidTurned$ does NOT
// match the turned permanent must not double. Without the ValidTurned$ read
// the doubling over-applies and this reads 2 life.
func TestPanharmoniconValidTurnedScopesTheTurnedPermanent(t *testing.T) {
	t.Parallel()
	e, probe, before := turnUpScenario(t, card(t, turnedScopeProbeSrc))

	turnUp(t, e, probe)
	if got := e.G.Players[0].Life - before; got != 1 {
		t.Fatalf("ValidTurned$ Land doubled a turned CREATURE's trigger (%d life, want 1): the ValidTurned$ scope was not read", got)
	}
}

// TestPanharmoniconTurnFaceUpModeDoesNotDoubleOtherTriggers is the brief's
// negative: the static's ValidMode$ TurnFaceUp restricts it to turn-up
// triggers, so an unrelated ChangesZone trigger on the same board is never
// doubled.
func TestPanharmoniconTurnFaceUpModeDoesNotDoubleOtherTriggers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{card(t, watcherGolemSrc)}, nil)
	onBoardCard(t, e, 0, lookup(t, reg, "Panoptic Projektor"))
	// Hand -> battlefield so the ChangesZone Hand-origin trigger fires; a
	// direct library move would not match its Origin$ and the counter
	// precondition below would fail loudly.
	moveByName(t, e, 0, "Watcher Golem", state.ZHand)
	gid := moveByName(t, e, 0, "Watcher Golem", state.ZBattlefield)
	g := e.G.Obj(gid)
	if g == nil || g.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Watcher Golem not on the battlefield (%+v)", g)
	}
	answerQuiet(t, e, 60)
	if c := e.G.Obj(gid).Counter("P1P1"); c != 1 {
		t.Fatalf("Panoptic Projektor doubled a ChangesZone trigger to %d counters, want 1", c)
	}
}
