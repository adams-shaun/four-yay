package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The trigger-body Cost$ ticket (trigcost1). Forge's `Cost$ <cost>` on a
// trigger body is the "you may pay <cost>. If you do, ..." idiom; before this
// ticket only the Untap / ImmediateTrigger / Draw / dyn-tap / CopySpellAbility
// shapes entered the triggered-cost pay/decline window, so e.g. Kalastria
// Highborn's `Cost$ B` executed its drain for FREE (no ask, no payment). The
// fix widens both gate sites (resolveTop's mandatory arm and resumeResolution's
// "optional" arm) through the shared triggerBodyNeedsCostWindow helper; the
// `Mandatory`-prefixed family is deliberately carved out (its real payment is
// a non-mana settle rules does not yet run, and a decline-only ask would be
// the WORSE regression).
//
// All fixtures are real compiled corpus cards; none is in any legacy golden
// deck, so TestHeads does not depend on these cards' behaviour changing.

// triggerCostWindowAsk drives the engine until a non-priority decision is
// pending and returns the pay ("trigger_cost_pay") and decline
// ("trigger_cost_decline") option indices within it.
func triggerCostWindowAsk(t *testing.T, e *Engine) (pay, decline int) {
	pay, decline = -1, -1
	t.Helper()
	d := passUntilNonPriority(t, e, 40)
	for _, o := range d.Options {
		switch o.Kind {
		case "trigger_cost_pay":
			pay = o.Index
		case "trigger_cost_decline":
			decline = o.Index
		}
	}
	if decline < 0 {
		t.Fatalf("the trigger-cost window never opened (pending %+v)", d)
	}
	return pay, decline
}

// answerPlayerTargetAsk drives until the pending target ask and submits the
// option naming the given seat (the Kalastria ask's ValidTgts$ Player options).
func answerPlayerTargetAsk(t *testing.T, e *Engine, seat state.PlayerID) {
	t.Helper()
	d := passUntilNonPriority(t, e, 20)
	if d.Kind != decision.KTarget {
		t.Fatalf("expected a target ask, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Player == seat {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("target ask has no option for seat %d: %+v", seat, d.Options)
}

// killOnBattlefield moves a battlefield permanent to the graveyard (the death
// the ChangesZone triggers key on) and re-opens priority so the trigger drains.
func killOnBattlefield(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.priorityRound()
}

// TestKalastriaTriggerCostPayChargesAndDrains pins the widened optional arm on
// the real corpus SA: answering PAY charges the {B} from the pool and the
// drain resolves (target player loses 2, controller gains 2).
func TestKalastriaTriggerCostPayChargesAndDrains(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Kalastria Highborn")
	id := searchMoveByName(t, e, "Kalastria Highborn", state.ZBattlefield)
	addMana(t, e, 0, "B")
	oppLife, myLife := e.G.Players[1].Life, e.G.Players[0].Life

	killOnBattlefield(t, e, id)
	answerPlayerTargetAsk(t, e, 1) // target the opponent

	// The optional ask: "Apply this triggered ability's effect?" -- yes.
	d := passUntilNonPriority(t, e, 20)
	if d.Kind != decision.KTriggerOptional || len(d.Options) == 0 || d.Options[0].Kind != "yes" {
		t.Fatalf("expected the trigger_optional yes ask, got %+v", d)
	}
	submitChoices(t, e, 0)

	pay, _ := triggerCostWindowAsk(t, e)
	if pay < 0 {
		t.Fatalf("the Cost$ B body was not offered as payable: %+v", e.Pending())
	}
	poolBefore := e.G.Players[0].Pool[state.MB]
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[0].Pool[state.MB]; got != poolBefore-1 {
		t.Fatalf("pool {B} = %d after paying, want %d (the cost must come OFF the pool)", got, poolBefore-1)
	}
	if got := e.G.Players[1].Life; got != oppLife-2 {
		t.Fatalf("opponent life = %d, want %d (the paid drain)", got, oppLife-2)
	}
	if got := e.G.Players[0].Life; got != myLife+2 {
		t.Fatalf("my life = %d, want %d (the paid gain)", got, myLife+2)
	}
}

// TestKalastriaTriggerCostDeclineChangesNothing pins the decline arm: a
// declined {B} body does NOT drain and does NOT gain.
func TestKalastriaTriggerCostDeclineChangesNothing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Kalastria Highborn")
	id := searchMoveByName(t, e, "Kalastria Highborn", state.ZBattlefield)
	addMana(t, e, 0, "B")
	oppLife, myLife := e.G.Players[1].Life, e.G.Players[0].Life

	killOnBattlefield(t, e, id)
	answerPlayerTargetAsk(t, e, 1)

	d := passUntilNonPriority(t, e, 20)
	if d.Kind != decision.KTriggerOptional {
		t.Fatalf("expected the trigger_optional ask, got %+v", d)
	}
	submitChoices(t, e, 0)

	_, decline := triggerCostWindowAsk(t, e)
	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)

	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.LifeChange {
			t.Fatalf("a declined trigger cost still changed life: %+v", ev)
		}
	}
	if got := e.G.Players[1].Life; got != oppLife {
		t.Fatalf("opponent life = %d after a decline, want unchanged %d", got, oppLife)
	}
	if got := e.G.Players[0].Life; got != myLife {
		t.Fatalf("my life = %d after a decline, want unchanged %d", got, myLife)
	}
	if got := e.G.Players[0].Pool[state.MB]; got != 1 {
		t.Fatalf("pool {B} = %d after a decline, want the untouched 1", got)
	}
}

// elendaEndStepCostAsk moves Elenda and Azor onto the battlefield, drives to
// the end step and returns the pending trigger-cost window ask for the
// `Cost$ PayLife<4>` body (no OptionalDecider$, so this exercises the widened
// MANDATORY arm in resolveTop).
func elendaEndStepCostAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	searchMoveByName(t, e, "Elenda and Azor", state.ZBattlefield)
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	d := passUntilNonPriority(t, e, 40)
	if d == nil {
		t.Fatal("no decision pending at the end step")
	}
	return d
}

// TestElendaAndAzorEndStepPayLifeCharges pins the mandatory arm: Elenda and
// Azor's end-step body (`AB$ Token | Cost$ PayLife<4> | TokenAmount$ Y`) is
// "you may pay 4 life. If you do, ..." -- paying charges 4 life.
func TestElendaAndAzorEndStepPayLifeCharges(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Elenda and Azor")
	myLife := e.G.Players[0].Life

	d := elendaEndStepCostAsk(t, e)
	pay, _ := triggerCostWindowAskDecision(t, d)
	if pay < 0 {
		t.Fatalf("the PayLife<4> body was not offered as payable: %+v", d.Options)
	}
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[0].Life; got != myLife-4 {
		t.Fatalf("life = %d after paying the end-step cost, want %d", got, myLife-4)
	}
}

// TestElendaAndAzorEndStepPayLifeDeclineChargesNone is the decline half.
func TestElendaAndAzorEndStepPayLifeDeclineChargesNone(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Elenda and Azor")
	myLife := e.G.Players[0].Life

	d := elendaEndStepCostAsk(t, e)
	_, decline := triggerCostWindowAskDecision(t, d)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[0].Life; got != myLife {
		t.Fatalf("life = %d after declining, want unchanged %d", got, myLife)
	}
}

// triggerCostWindowAskDecision is the decision-taking variant of
// triggerCostWindowAsk (the ask is already pending).
func triggerCostWindowAskDecision(t *testing.T, d *decision.Decision) (pay, decline int) {
	pay, decline = -1, -1
	t.Helper()
	for _, o := range d.Options {
		switch o.Kind {
		case "trigger_cost_pay":
			pay = o.Index
		case "trigger_cost_decline":
			decline = o.Index
		}
	}
	if decline < 0 {
		t.Fatalf("no trigger-cost window ask (pending %+v)", d)
	}
	return pay, decline
}

// TestPromiseOfBunreiMandatorySacrificeChargesAndTokens replaces the old
// TestMandatoryCostBodyKeepsCurrentFreeExecution, which pinned the defect: a
// `Cost$ Mandatory Sac<1/CARDNAME>` body used to execute for FREE, leaving
// Promise of Bunrei on the battlefield. trigmand1 settles the mandatory cost
// for real -- the source is sacrificed (a real events.Sacrifice), and only
// then does the body run and create its four 1/1 Spirits. No pay/decline
// election is ever posed for a mandatory cost.
func TestPromiseOfBunreiMandatorySacrificeChargesAndTokens(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Promise of Bunrei")
	promise := searchMoveByName(t, e, "Promise of Bunrei", state.ZBattlefield)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)

	killOnBattlefield(t, e, bear)
	passUntilStackEmpty(t, e, 20)

	tokens := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate && ev.Text == "c_1_1_spirit" {
			tokens++
		}
	}
	if tokens != 4 {
		t.Fatalf("token_create events for c_1_1_spirit = %d, want 4 (the paid body must run)", tokens)
	}
	if o := e.G.Obj(promise); o == nil || o.Zone == state.ZBattlefield {
		t.Fatalf("Promise of Bunrei is still on the battlefield; the Mandatory Sac cost must be charged")
	}
	if o := e.G.Obj(promise); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Promise of Bunrei zone = %v, want the graveyard after being sacrificed", o)
	}
	sawSac := false
	for _, ev := range e.L.Events {
		if events.IsSacrifice(ev) && ev.Obj == promise {
			sawSac = true
		}
	}
	if !sawSac {
		t.Fatalf("no events.Sacrifice for Promise of Bunrei; the cost was never settled")
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && (ev.Text == "trigger_cost_pay" || ev.Text == "trigger_cost_decline") {
			t.Fatalf("a mandatory cost posed a pay/decline election: %+v", ev)
		}
	}
}

// TestMandatorySacUnpayableBodyDoesNotRun is a SYNTHETIC fixture (built inline,
// no corpus script committed): a `Cost$ Mandatory Sac<1/Artifact>` trigger body
// with zero artifacts on the battlefield cannot pay its cost, so the body must
// be skipped -- the draw must not happen. This is the mandatory semantics'
// second half: if the cost cannot be paid, the effect does not run.
func TestMandatorySacUnpayableBodyDoesNotRun(t *testing.T) {
	t.Parallel()
	const script = "Name:Ingot Eater\nManaCost:1 R\nTypes:Creature Human Warrior\nPT:2/2\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigSac | TriggerDescription$ When CARDNAME enters, sacrifice an artifact. If you do, draw a card.\n" +
		"SVar:TrigSac:AB$ Draw | Cost$ Mandatory Sac<1/Artifact> | NumCards$ 1\n" +
		"Oracle:x\n"
	e := handEngine(t)
	src := e.G.AddObject(card(t, script), 0)
	src.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{src.ID})
	mark := len(e.L.Events)
	// The ETB event fires the ChangesZone trigger; the board holds only the
	// non-artifact source, so Sac<1/Artifact> has zero eligible candidates.
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZHand, To: state.ZBattlefield})
	e.putTriggersOnStack()
	e.resolveTop()
	if d := e.Pending(); d != nil {
		t.Fatalf("an unpayable mandatory cost posed a decision: %+v", d)
	}
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw {
			t.Fatalf("the body ran even though the mandatory sacrifice was unpayable: %+v", ev)
		}
	}
	if o := e.G.Obj(src.ID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the source must not be sacrificed when the cost is unpayable")
	}
}

// TestDalekIntensiveCareMandatoryExileIsAChoice covers the choice-bearing
// mandatory component on Dalek Intensive Care's exact cost shape
// (`Exile<1/Creature.nonDalek/non-Dalek creature>`): with two eligible
// non-Dalek creatures the exile is a real KChoose (Min=Max=1), and the picked
// creature moves to exile before the body runs -- no pay/decline election is
// posed. The fixture carries Dalek's real cost spec verbatim; its trigger is
// an ETB rather than the card's own upkeep line because forge's command-zone
// TriggerZones$ is not scanned by this build's trigger walk (see the report's
// ## Issues), so the real card cannot fire in a test. The cost settle under
// test is exactly the real card's.
func TestDalekIntensiveCareMandatoryExileIsAChoice(t *testing.T) {
	t.Parallel()
	const dalekCostScript = "Name:Dalek Intensive Care\nTypes:Plane Dalek\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigExile\n" +
		"SVar:TrigExile:AB$ Draw | Cost$ Mandatory Exile<1/Creature.nonDalek/non-Dalek creature> | NumCards$ 1\n" +
		"Oracle:x\n"
	const bearScript = "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e := handEngine(t)
	src := e.G.AddObject(card(t, dalekCostScript), 0)
	src.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{src.ID})
	bearA := onBoard(t, e, 0, bearScript)
	bearB := onBoard(t, e, 0, bearScript)

	// The ETB fires the ChangesZone trigger; the window settles the mandatory
	// Exile, which has two eligible non-Dalek creatures and therefore a real
	// choice.
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZHand, To: state.ZBattlefield})
	e.putTriggersOnStack()
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the mandatory exile KChoose, got %+v", d)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("mandatory exile ask bounds = %d..%d, want 1..1", d.Min, d.Max)
	}
	if len(d.Options) != 2 {
		t.Fatalf("mandatory exile ask options = %d, want the 2 eligible creatures", len(d.Options))
	}
	pick := -1
	for _, o := range d.Options {
		if o.Kind != "exile_cost" {
			t.Fatalf("mandatory exile option kind = %q, want exile_cost", o.Kind)
		}
		if o.Obj == bearA {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("the exile ask does not offer the first eligible bear: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, pick)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(bearA); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the picked creature zone = %v, want exile", o)
	}
	if o := e.G.Obj(bearB); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the unpicked creature must stay on the battlefield")
	}
	if o := e.G.Obj(src.ID); o == nil || o.Zone == state.ZExile {
		t.Fatalf("the non-creature source must not be exiled by Creature.nonDalek")
	}
	sawDraw := false
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw {
			sawDraw = true
		}
	}
	if !sawDraw {
		t.Fatal("the body did not run after the mandatory exile was paid")
	}
}

// Kuldotha Flamefiend's ETB body carries `Cost$ Sac<1/Artifact>` (a real
// non-mana component payMana cannot charge). trigcost2 makes it genuinely
// payable: the window's pay arm walks the Sac component and settles it for
// real. The PAY half pins: with an artifact on the battlefield the "pay"
// answer sacrifices it (a real events.Sacrifice) BEFORE the body runs, and
// the 4 damage (targeted at the opponent in the body's earlier target ask)
// lands. No mana is charged -- the cost has none.
func TestKuldothaFlamefiendOptionalSacCostPays(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Kuldotha Flamefiend")
	id := searchMoveByName(t, e, "Kuldotha Flamefiend", state.ZBattlefield)
	artifact := onBoard(t, e, 0, "Name:Test Bauble\nManaCost:0\nTypes:Artifact\nOracle:x\n")
	oppLife := e.G.Players[1].Life

	// The body's target ask (ValidTgts$ Any): target the opponent so the
	// paid damage is observable.
	answerPlayerTargetAsk(t, e, 1)

	d := passUntilNonPriority(t, e, 20)
	if d.Kind != decision.KTriggerOptional {
		t.Fatalf("expected the trigger_optional ask, got %+v", d)
	}
	submitChoices(t, e, 0) // yes, attempt the effect

	pay, _ := triggerCostWindowAsk(t, e)
	if pay < 0 {
		t.Fatalf("the settleable Sac<1/Artifact> cost was not offered as payable: %+v", e.Pending().Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(artifact); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the paying artifact zone = %v, want the graveyard (the settled Sac cost)", o)
	}
	sawSac := false
	for _, ev := range e.L.Events[mark:] {
		if events.IsSacrifice(ev) && ev.Obj == artifact {
			sawSac = true
		}
	}
	if !sawSac {
		t.Fatal("no events.Sacrifice for the paying artifact; the cost was never settled")
	}
	if got := e.G.Players[1].Life; got != oppLife-4 {
		t.Fatalf("opponent life = %d, want %d (the paid body's damage)", got, oppLife-4)
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Flamefiend itself must stay on the battlefield (only the artifact pays)")
	}
}

// TestKuldothaFlamefiendOptionalSacCostDeclineChangesNothing is the decline
// half: a declined Sac-component body sacrifices nothing and deals nothing.
func TestKuldothaFlamefiendOptionalSacCostDeclineChangesNothing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Kuldotha Flamefiend")
	id := searchMoveByName(t, e, "Kuldotha Flamefiend", state.ZBattlefield)
	artifact := onBoard(t, e, 0, "Name:Test Bauble\nManaCost:0\nTypes:Artifact\nOracle:x\n")
	oppLife := e.G.Players[1].Life

	answerPlayerTargetAsk(t, e, 1)

	d := passUntilNonPriority(t, e, 20)
	if d.Kind != decision.KTriggerOptional {
		t.Fatalf("expected the trigger_optional ask, got %+v", d)
	}
	submitChoices(t, e, 0)

	_, decline := triggerCostWindowAsk(t, e)
	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)

	for _, ev := range e.L.Events[mark:] {
		if events.IsSacrifice(ev) || ev.Kind == events.Damage {
			t.Fatalf("a declined Sac-component body still moved or damaged: %+v", ev)
		}
	}
	if o := e.G.Obj(artifact); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the artifact must stay on the battlefield after a decline")
	}
	if got := e.G.Players[1].Life; got != oppLife {
		t.Fatalf("opponent life = %d after a decline, want unchanged %d", got, oppLife)
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Flamefiend left the battlefield on a declined cost")
	}
}

// xFoldAskIndex returns the index of the choose-X option announcing the given
// value within a pending KChoose (the trigger-cost window's payer-chooses X
// announcement), failing the test when that value is not offered.
func xFoldAskIndex(t *testing.T, d *decision.Decision, x int) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == "trigger_cost_x" && o.Amount == x {
			return o.Index
		}
	}
	t.Fatalf("choose-X ask offers no X = %d option: %+v", x, d.Options)
	return -1
}

// xFoldLand is an eventless fixture mana source for the window tests: mana
// pools empty at every step boundary (CR 500.4), so a window cost with mana
// in it is paid by activating untapped sources INSIDE the window
// (paymentManaAsk), exactly as a real seat does.
func xFoldLand(t *testing.T, e *Engine, name, produced string) state.ObjID {
	t.Helper()
	return onBoard(t, e, 0, "Name:"+name+"\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ "+produced+"\nOracle:x\n")
}

// activateWindowMana drives the payment window's mana-activation asks: every
// pending KChoose carrying an "activate" option takes the first one, until
// the cost is covered and the pending decision is no longer a mana ask.
func activateWindowMana(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 16; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		act := -1
		for _, o := range d.Options {
			if o.Kind == "activate" {
				act = o.Index
				break
			}
		}
		if act < 0 {
			return
		}
		submitChoices(t, e, act)
	}
	t.Fatal("the mana-activation window never closed")
}

// TestElendaAndAzorAttackXFoldAnnouncesPaysAndDraws pins the payer-chooses X
// fold on the real corpus attack trigger: Elenda and Azor attacks, the window
// poses a real choose-X ask (X = 0 up to the payable bound, ascending), a
// chosen X = 1 plus the {W}{U}{B} pips is charged from the pool, and the body
// draws exactly 1 card (NumCards$ X reads the binding).
func TestElendaAndAzorAttackXFoldAnnouncesPaysAndDraws(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Elenda and Azor")
	id := searchMoveByName(t, e, "Elenda and Azor", state.ZBattlefield)
	e.G.Obj(id).SummonSick = false
	xFoldLand(t, e, "Test Plains", "W")
	xFoldLand(t, e, "Test Island", "U")
	xFoldLand(t, e, "Test Swamp", "B")
	xFoldLand(t, e, "Test Mountain", "R")

	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepDeclareAttackers)
	submitAttackersOnly(t, e, id)

	// The X announcement: ascending options, X = 0 first (the decline-
	// equivalent a bot's default arm takes), and X = 1 payable against the
	// seat's potential pool (the four sources' production).
	d := passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "trigger_cost_x" {
		t.Fatalf("expected the choose-X announcement ask, got %+v", d)
	}
	if d.Options[0].Amount != 0 {
		t.Fatalf("choose-X options must ascend from 0, first is %d: %+v", d.Options[0].Amount, d.Options)
	}
	submitChoices(t, e, xFoldAskIndex(t, d, 1))

	// The window's mana activations: the pool is empty at the step boundary,
	// so the folded cost (1 generic + W U B) is covered by tapping the
	// sources inside the window.
	activateWindowMana(t, e)
	pay, _ := triggerCostWindowAsk(t, e)
	if pay < 0 {
		t.Fatalf("the folded X W U B cost was not offered as payable: %+v", e.Pending())
	}
	mark := len(e.L.Events)
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[0].Pool; got != (state.Mana{}) {
		t.Fatalf("pool = %v after paying X=1 + pips, want empty", got)
	}
	draws := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
	}
	if draws != 1 {
		t.Fatalf("draw events for seat 0 = %d, want exactly 1 (NumCards$ X with the bound X)", draws)
	}
}

// TestElendaAndAzorAttackXFoldDeclineDrawsNothing is the decline half of the
// same leaf: choosing X = 1 does NOT charge anything by itself, and the
// following pay/decline election's decline leaves the pool untouched and
// draws nothing.
func TestElendaAndAzorAttackXFoldDeclineDrawsNothing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Elenda and Azor")
	id := searchMoveByName(t, e, "Elenda and Azor", state.ZBattlefield)
	e.G.Obj(id).SummonSick = false
	xFoldLand(t, e, "Test Plains", "W")
	xFoldLand(t, e, "Test Island", "U")
	xFoldLand(t, e, "Test Swamp", "B")
	xFoldLand(t, e, "Test Mountain", "R")

	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepDeclareAttackers)
	submitAttackersOnly(t, e, id)

	d := passUntilNonPriority(t, e, 40)
	submitChoices(t, e, xFoldAskIndex(t, d, 1))

	// Pass the mana window unspent: the pending mana ask's "done".
	md := passUntilNonPriority(t, e, 40)
	done := -1
	for _, o := range md.Options {
		if o.Kind == "done" {
			done = o.Index
		}
	}
	if done < 0 {
		t.Fatalf("expected the mana-activation ask with a done option, got %+v", md)
	}
	submitChoices(t, e, done)

	mark := len(e.L.Events)
	_, decline := triggerCostWindowAsk(t, e)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)

	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw {
			t.Fatalf("a declined X-fold window still drew: %+v", ev)
		}
	}
	if got := e.G.Players[0].Pool; got != (state.Mana{}) {
		t.Fatalf("pool = %v after the decline, want empty (nothing was floated)", got)
	}
	for _, tid := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(tid); o != nil && o.Face() != nil && len(o.Face().Types) > 0 && o.Face().Types[0] == "Land" && o.Tapped {
			t.Fatalf("%s tapped on a declined cost", o.Face().Name)
		}
	}
}

// TestVizkopaConfessorETBChoosesLifeAndExilesRevealed pins the PayLife<X>
// payer-chooses fold AND the binding together: the ETB asks how much life to
// pay (bounded by the payer's life), a chosen X = 2 charges 2 life, the
// targeted opponent reveals exactly 2 cards (NumCards$ X reads the binding),
// and the follow-up pick exiles one of them.
func TestVizkopaConfessorETBChoosesLifeAndExilesRevealed(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Vizkopa Confessor")
	myLife := e.G.Players[0].Life
	oppHand := len(e.G.Zone(state.ZHand, 1))

	searchMoveByName(t, e, "Vizkopa Confessor", state.ZBattlefield)
	answerPlayerTargetAsk(t, e, 1) // target the opponent

	d := passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "trigger_cost_x" {
		t.Fatalf("expected the choose-X announcement ask, got %+v", d)
	}
	// The bound is the payer's life: every value 0..life must be offered.
	if last := d.Options[len(d.Options)-1]; last.Amount != int(myLife) {
		t.Fatalf("choose-X bound = %d, want the payer's life %d: %+v", last.Amount, myLife, d.Options)
	}
	submitChoices(t, e, xFoldAskIndex(t, d, 2))

	pay, _ := triggerCostWindowAsk(t, e)
	if pay < 0 {
		t.Fatalf("the folded PayLife<2> cost was not offered as payable: %+v", e.Pending())
	}
	submitChoices(t, e, pay)

	// The body: the opponent CHOOSES which 2 of their hand to reveal (the
	// infernaltutor1 fix -- a hand reveal with NumCards$ < hand size is a
	// choice for the hand's owner), then the caster picks one of the 2
	// revealed to exile.
	d = passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KChoose || d.ResumeKind != "reveal_pick" || d.Player != 1 || d.Min != 2 || d.Max != 2 || len(d.Options) != oppHand {
		t.Fatalf("expected the opponent's reveal-2-of-%d pick, got %+v", oppHand, d)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	d = passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("expected the pick-one ask over exactly the 2 revealed cards, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[0].Life; got != myLife-2 {
		t.Fatalf("life = %d after paying X = 2, want %d", got, myLife-2)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != oppHand-1 {
		t.Fatalf("opponent hand = %d cards after the reveal-and-exile, want %d", got, oppHand-1)
	}
	exiled := 0
	for _, id := range e.G.Zone(state.ZExile, 1) {
		if o := e.G.Obj(id); o != nil && o.Owner == 1 {
			exiled++
		}
	}
	if exiled != 1 {
		t.Fatalf("exiled opponent cards = %d, want exactly the picked 1", exiled)
	}
}

// TestNecrodominanceEndStepPaysLifeAndDrawsThatMany pins the second PayLife<X>
// payer-chooses carrier end to end: at the end step the window asks how much
// life, a chosen X = 3 charges 3 life and draws exactly 3 cards.
func TestNecrodominanceEndStepPaysLifeAndDrawsThatMany(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Necrodominance")
	myLife := e.G.Players[0].Life

	searchMoveByName(t, e, "Necrodominance", state.ZBattlefield)
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)

	d := passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "trigger_cost_x" {
		t.Fatalf("expected the choose-X announcement ask, got %+v", d)
	}
	submitChoices(t, e, xFoldAskIndex(t, d, 3))

	pay, _ := triggerCostWindowAsk(t, e)
	if pay < 0 {
		t.Fatalf("the folded PayLife<3> cost was not offered as payable: %+v", e.Pending())
	}
	mark := len(e.L.Events)
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[0].Life; got != myLife-3 {
		t.Fatalf("life = %d after paying X = 3, want %d", got, myLife-3)
	}
	draws := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
	}
	if draws != 3 {
		t.Fatalf("draw events for seat 0 = %d, want exactly 3", draws)
	}
}

// TestTomakulPhoenixFixedXNoAskPaysPowerAndReturns pins the FIXED X shape:
// Tomakul Phoenix's `Cost$ X R` with SVar:X:Count$CardPower is evaluated once
// at the window -- NO choose-X ask is ever posed -- and paying power + {R}
// from the pool returns it from the graveyard to the battlefield. The paid
// amount is read back off the pool delta, computed with the same evaluation
// the window runs.
func TestTomakulPhoenixFixedXNoAskPaysPowerAndReturns(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Tomakul Phoenix")
	id := searchMoveByName(t, e, "Tomakul Phoenix", state.ZBattlefield)
	for i := 0; i < 6; i++ {
		xFoldLand(t, e, "Test Mountain", "R")
	}

	// The death fires the card's own (cost-less) Pump trigger too; after it
	// drains, the phoenix sits in the graveyard and the BeginCombat trigger
	// opens the window at the next combat beginning.
	killOnBattlefield(t, e, id)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Tomakul Phoenix zone = %v, want the graveyard", o)
	}
	xWant, ok := effects.EvalCountOK(e, &effects.Ctx{Source: id, Controller: 0,
		SVars: e.G.Obj(id).Face().SVars}, "Count$CardPower")
	if !ok || xWant <= 0 {
		t.Fatalf("Count$CardPower on the graveyarded phoenix = (%d, %v); the fixed X must resolve", xWant, ok)
	}

	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepBeginCombat)
	d := passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KChoose {
		t.Fatalf("expected the window's mana-activation ask, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "trigger_cost_x" {
			t.Fatalf("a FIXED X shape posed a choose-X announcement ask: %+v", d.Options)
		}
	}
	// The pool is empty at the step boundary, so the folded cost (X + {R})
	// is covered by tapping X+1 sources inside the window; the fold itself
	// was silent -- no announcement ask was ever posed.
	activateWindowMana(t, e)
	pay, _ := triggerCostWindowAsk(t, e)
	if pay < 0 {
		t.Fatalf("the covered fixed cost was not offered as payable: %+v", e.Pending())
	}
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Tomakul Phoenix zone = %v, want back on the battlefield (the paid body ran)", o)
	}
	tapped := 0
	for _, tid := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(tid); o != nil && o.Face() != nil && len(o.Face().Types) > 0 &&
			o.Face().Types[0] == "Land" && o.Tapped {
			tapped++
		}
	}
	if tapped != int(xWant)+1 {
		t.Fatalf("lands tapped for the cost = %d, want X(%d)+1 (the {R} pip plus the X generic)", tapped, xWant)
	}
}

// TestTivashGainedLifeFixedXNoAskTokensAtPower pins the fixed PayLife<X>
// shape: Tivash's `Cost$ PayLife<X>` with SVar:X:Count$LifeYouGainedThisTurn
// is evaluated once at the window (4 life gained => X = 4), no announcement
// ask is posed, paying 4 life creates the Demon token with TokenPower$ X
// reading 4.
func TestTivashGainedLifeFixedXNoAskTokensAtPower(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Tivash, Gloom Summoner")
	myLife := e.G.Players[0].Life

	searchMoveByName(t, e, "Tivash, Gloom Summoner", state.ZBattlefield)
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 4})
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)

	d := passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KChoose {
		t.Fatalf("expected the trigger-cost window ask, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "trigger_cost_x" {
			t.Fatalf("a FIXED PayLife<X> shape posed a choose-X announcement ask: %+v", d.Options)
		}
	}
	pay := -1
	for _, o := range d.Options {
		if o.Kind == "trigger_cost_pay" {
			pay = o.Index
		}
	}
	if pay < 0 {
		t.Fatalf("the fixed PayLife<4> cost was not offered as payable: %+v", d.Options)
	}
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[0].Life; got != myLife+4-4 {
		t.Fatalf("life = %d after gaining 4 and paying X = 4, want %d", got, myLife)
	}
	var tok *state.Object
	for _, tid := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(tid); o != nil && o.IsToken {
			tok = o
			break
		}
	}
	if tok == nil {
		t.Fatalf("no Demon token on the battlefield; events tail: %+v", e.L.Events[len(e.L.Events)-4:])
	}
	der := e.Derived(tok.ID)
	if der.Power != 4 || der.Toughness != 4 {
		t.Fatalf("Demon token derived P/T = %d/%d, want 4/4 (TokenPower$ X with the fixed X)", der.Power, der.Toughness)
	}
}

// TestTymnaFixedXDeclineStillDeclines pins the zero-hit direction: Tymna's
// now-resolvable player count has no combat hits in this setup, and declining
// the resulting optional payment leaves the player unchanged.
func TestTymnaUnresolvableFixedXDeclineOnly(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Tymna the Weaver")
	myLife := e.G.Players[0].Life

	searchMoveByName(t, e, "Tymna the Weaver", state.ZBattlefield)
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepMain2)

	d := passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KChoose {
		t.Fatalf("expected the decline-only window ask, got %+v", d)
	}
	var hasDecline bool
	for _, o := range d.Options {
		if o.Kind == "trigger_cost_decline" {
			hasDecline = true
		}
	}
	if !hasDecline {
		t.Fatalf("want a decline option, got %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 20)

	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw || ev.Kind == events.LifeChange {
			t.Fatalf("the unresolvable body ran or paid on the decline: %+v", ev)
		}
	}
	if got := e.G.Players[0].Life; got != myLife {
		t.Fatalf("life = %d after the decline, want unchanged %d", got, myLife)
	}
}

// TestUnregisteredBodyXCostStaysDeclineOnly pins the registered-API gate: a
// `Cost$ PayLife<X>` body whose API is not implemented (Maralen of the
// Mornsong Avatar's shape, with a deliberately unknown API now that
// StoreSVar itself is registered) must NOT get the X fold -- posing
// the choose-X ask would let the payer charge real life for a body that can
// only emit the unimplemented-API Note. The window stays decline-only, the
// pre-fold behaviour.
func TestUnregisteredBodyXCostStaysDeclineOnly(t *testing.T) {
	t.Parallel()
	const script = "Name:Life Scribe\nManaCost:2 B\nTypes:Creature Human Cleric\nPT:2/2\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigPay | TriggerDescription$ When CARDNAME enters, pay any amount of life.\n" +
		"SVar:TrigPay:AB$ NoSuchUnregisteredAPI | Cost$ PayLife<X> | SVar$ LifePaid | Type$ CountSVar | Expression$ X\n" +
		"SVar:X:Count$xPaid\n" +
		"Oracle:x\n"
	e := handEngine(t)
	src := e.G.AddObject(card(t, script), 0)
	src.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{src.ID})
	myLife := e.G.Players[0].Life
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZHand, To: state.ZBattlefield})
	e.putTriggersOnStack()
	e.resolveTop()

	d := passUntilNonPriority(t, e, 20)
	if d.Kind != decision.KChoose {
		t.Fatalf("expected the decline-only window ask, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "trigger_cost_pay" || o.Kind == "trigger_cost_x" {
			t.Fatalf("an unimplemented body's X cost offered more than the decline: %+v", d.Options)
		}
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != myLife {
		t.Fatalf("life = %d after the decline, want unchanged %d", got, myLife)
	}
}

// monstrosityOpponents puts n opponent creatures on the battlefield (real
// inline fixture cards, never corpus .txt) and returns their ids. The
// trigger's ValidCards$ Creature.OppCtrl is relative to the resolving
// controller, so these are exactly the victims.
func monstrosityOpponents(t *testing.T, e *Engine, n int) []state.ObjID {
	t.Helper()
	const bearScript = "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	out := make([]state.ObjID, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, onBoard(t, e, 1, bearScript))
	}
	return out
}

// Monstrosity of the Lake is the report's named deck card and the composition
// the window had never been pinned through: an ETB trigger (no
// OptionalDecider$, so there is NO trigger_optional ask) whose body is
// `AB$ TapAll | Cost$ 5 | RememberTapped$ True | SubAbility$ DBStun`, a
// chained `DB$ PutCounter | CounterType$ STUN | Defined$ Remembered` and a
// trailing `DB$ Cleanup | ClearRemembered$ True`. The tap-all and the stun
// counters must be gated behind the {5}.
//
// TestMonstrosityOfTheLakePayTapsStunsAndClears pins the pay arm: floating
// {5}, a paid window drops the pool by exactly 5, every opponent creature is
// tapped AND carries a STUN counter (the RememberTapped$ -> DBStun chain),
// the seat's own creature is untouched, and DBCleanup cleared the source's
// remembered list.
func TestMonstrosityOfTheLakePayTapsStunsAndClears(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Monstrosity of the Lake")
	opponents := monstrosityOpponents(t, e, 2)
	mine := onBoard(t, e, 0, "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	addMana(t, e, 0, "CCCCC")
	id := searchMoveByName(t, e, "Monstrosity of the Lake", state.ZBattlefield)

	pay, _ := triggerCostWindowAsk(t, e)
	if pay < 0 {
		t.Fatalf("the Cost$ 5 body was not offered as payable: %+v", e.Pending())
	}
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool total = %d after paying {5}, want 0 (the cost must come OFF the pool)", got)
	}
	for _, oid := range opponents {
		o := e.G.Obj(oid)
		if o == nil || !o.Tapped {
			t.Fatalf("opponent creature %d Tapped = %v, want true (the paid TapAll)", oid, o != nil && o.Tapped)
		}
		if got := o.Counter("STUN"); got != 1 {
			t.Fatalf("opponent creature %d STUN counters = %d, want 1 (the Remembered -> DBStun chain)", oid, got)
		}
	}
	if o := e.G.Obj(mine); o == nil || o.Tapped {
		t.Fatalf("the resolving seat's own creature must be untouched (Creature.OppCtrl), Tapped = %v", o != nil && o.Tapped)
	}
	if o := e.G.Obj(id); o == nil || len(o.Remembered) != 0 {
		t.Fatalf("source Remembered = %+v, want empty (DBCleanup's ClearRemembered$)", o)
	}
}

// TestMonstrosityOfTheLakeDeclineChangesNothing pins the decline arm: with
// {5} still floating, answering trigger_cost_decline leaves the pool
// untouched, taps nothing, puts no STUN counter anywhere, and the source's
// remembered list stays empty.
func TestMonstrosityOfTheLakeDeclineChangesNothing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Monstrosity of the Lake")
	opponents := monstrosityOpponents(t, e, 2)
	addMana(t, e, 0, "CCCCC")
	id := searchMoveByName(t, e, "Monstrosity of the Lake", state.ZBattlefield)

	_, decline := triggerCostWindowAsk(t, e)
	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[0].Pool.Total(); got != 5 {
		t.Fatalf("pool total = %d after declining, want the untouched 5", got)
	}
	for _, oid := range opponents {
		if o := e.G.Obj(oid); o == nil || o.Tapped || o.Counter("STUN") != 0 {
			t.Fatalf("a declined body still tapped/stunned opponent creature %d: %+v", oid, o)
		}
	}
	if o := e.G.Obj(id); o == nil || len(o.Remembered) != 0 {
		t.Fatalf("source Remembered = %+v after a decline, want empty", o)
	}
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.CounterChange && ev.Counter == "STUN" {
			t.Fatalf("a declined body still placed a stun counter: %+v", ev)
		}
	}
}

// TestMonstrosityOfTheLakeUnpayableIsDeclinedAtSettle pins the fail-closed
// direction the report described ("pay refuses without {5} floating").
//
// Measured on current main, the anti-free-execution guard is NOT a
// decline-only option list for a plain mana cost: with an empty pool and no
// untapped mana source the window still OFFERS trigger_cost_pay
// (triggeredCostPaymentAsk's `payable := tc.amount.Priceable()` -- the
// cumulative-upkeep ask's `Priceable() && costPayable(...)` shape is not
// copied there), and the guard lives in the ANSWER instead: the pay arm's
// `paid := ... && pay.PayManaConv(asPayer(e), ...)` fails on the empty pool, so the body
// is DECLINED at settle (triggeredCostAnswer -> triggeredCostDecline). The
// card's tap-all never runs, so the old free-execution defect stays closed;
// the offered-then-declined option shape is a separate, benign divergence
// recorded in the report's ## Issues (not worth re-touching the merged
// window for). This test pins the invariant that matters: a submitted pay
// with NOTHING to pay from leaves the board untouched.
func TestMonstrosityOfTheLakeUnpayableIsDeclinedAtSettle(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Monstrosity of the Lake")
	opponents := monstrosityOpponents(t, e, 2)
	// No addMana: the pool is empty and the only battlefields hold the
	// (ability-less) creatures, so {5} cannot actually be paid.
	id := searchMoveByName(t, e, "Monstrosity of the Lake", state.ZBattlefield)

	d := passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KChoose {
		t.Fatalf("expected the trigger-cost window ask, got %+v", d)
	}
	pay, decline := triggerCostWindowAskDecision(t, d)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("fixture pool = %d, want 0 (the unpayable premise)", got)
	}
	// Take the pay option even though nothing can pay it: the settle must
	// decline, never run the body for free. If the window is ever changed to
	// decline-only for an unpayable plain cost, the other arm of this test
	// still holds.
	answer := pay
	if answer < 0 {
		answer = decline
	}
	submitChoices(t, e, answer)
	passUntilStackEmpty(t, e, 20)

	for _, oid := range opponents {
		if o := e.G.Obj(oid); o == nil || o.Tapped || o.Counter("STUN") != 0 {
			t.Fatalf("an unpayable body still tapped/stunned opponent creature %d: %+v", oid, o)
		}
	}
	if got := e.G.Players[0].Pool; got != (state.Mana{}) {
		t.Fatalf("pool = %v after an unpayable answer, want empty", got)
	}
	if o := e.G.Obj(id); o == nil || len(o.Remembered) != 0 {
		t.Fatalf("source Remembered = %+v, want empty (the unpayable body never ran)", o)
	}
}
