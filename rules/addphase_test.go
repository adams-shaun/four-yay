package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The DB$ AddPhase leaf tests (Forge's AddPhaseEffect, "after this phase,
// there is an additional combat phase"). The engine's turn walk is a linear
// Step advance, so every test drives a real corpus carrier through a real
// combat and asserts the splice: the extra phase entered at the leaving of
// its splice point, its range walked, the resume point taken, the fold
// (state.Game.ExtraPhases) empty afterwards, and the whole game replaying
// byte-identically from its log.

// submitNoAttackers declines the pending attackers decision (the empty
// declaration is legal; CR 508.8 then skips the rest of the combat).
func submitNoAttackers(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
		t.Fatalf("submit empty attackers: %v", err)
	}
}

// addPhaseEngine is corpusEngineCfg with the starting seat pinned to 0
// (seatZeroStart bumps the seed until the toss starts seat 0), so these
// tests' turn arithmetic ("turn 3, seat 0's second turn") is deterministic
// rather than toss-dependent.
func addPhaseEngine(t *testing.T, reg *cards.Registry, extras0, extras1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	m, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus fixture: Mountain missing")
	}
	fill := func(n int) []*cards.Card {
		out := make([]*cards.Card, n)
		for i := range out {
			out[i] = m
		}
		return out
	}
	cfg := seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{append(append([]*cards.Card{}, extras0...), fill(40-len(extras0))...),
			append(append([]*cards.Card{}, extras1...), fill(40-len(extras1))...)}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// countSteps counts StepChange events to step.
func countSteps(e *Engine, step state.Step) int {
	return countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.StepChange && ev.Step == step
	})
}

// TestAureliaExtraCombatSplicesASecondCombat is api:AddPhase's leaf (real
// corpus Aurelia, the Warleader): the first combat's attack trigger untaps
// the team and grants an extra combat spliced after the end-of-combat step;
// the walk enters the second combat through the ordinary step machinery
// (including its own declare-attackers ask), and after the extra combat's
// end-of-combat step the turn resumes at Main2. The whole game replays
// byte-identically.
func TestAureliaExtraCombatSplicesASecondCombat(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := addPhaseEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Aurelia, the Warleader")}, []*cards.Card{})
	aurelia := moveByName(t, e, 0, "Aurelia, the Warleader", state.ZBattlefield)
	// Aurelia has haste (CR 302.6), so the combat runs in the game's very
	// first turn -- which is also why this test must drive every later turn
	// with attacks DECLINED: the FirstAttack$ gate makes her trigger fire
	// only on each turn's FIRST attack, but the extra combats she grants
	// would still multiply under a bot that attacks every combat, never
	// reaching turn 3.
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, aurelia)
	// The attack trigger (untap all + the chained AddPhase) resolves off the
	// stack; Aurelia attacked tapped and must be untapped again.
	passAll(t, e, 200)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("the extra combat did not reach its own attackers ask (pending %+v, step %s)", d, e.G.Step)
	}
	if e.G.Obj(aurelia).Tapped {
		t.Fatal("Aurelia's untap-all trigger did not untap her")
	}
	// The same passAll crossed the first combat's end-of-combat step, so the
	// grant is already CONSUMED here.
	if len(e.G.ExtraPhases) != 1 {
		t.Fatalf("ExtraPhases = %+v, want exactly one grant", e.G.ExtraPhases)
	}
	ep := e.G.ExtraPhases[0]
	if ep.AfterStep != state.StepEndCombat || ep.Entry != state.StepBeginCombat ||
		ep.RangeEnd != state.StepEndCombat || !ep.Consumed {
		t.Fatalf("grant = %+v, want Combat spliced after EndCombat, consumed", ep)
	}
	// The extra combat's attackers ask: decline. CR 508.8 skips to the
	// end-of-combat step; the consumer completes the grant there and the
	// walk resumes at Main2, the phase after the splice point.
	submitNoAttackers(t, e)
	driveToStep(t, e, 1, 0, state.StepMain2)
	if e.G.Step != state.StepMain2 || e.G.Turn != 1 {
		t.Fatalf("after the extra combat: step %s turn %d, want Main2 of turn 1", e.G.Step, e.G.Turn)
	}
	if len(e.G.ExtraPhases) != 0 {
		t.Fatalf("ExtraPhases not drained: %+v", e.G.ExtraPhases)
	}
	grants := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.ExtraPhase && ev.Amount > 0
	})
	consumes := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.ExtraPhase && ev.Amount == -1
	})
	completes := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.ExtraPhase && ev.Amount == -2
	})
	if grants != 1 || consumes != 1 || completes != 1 {
		t.Fatalf("ExtraPhase events: %d grants, %d consumes, %d completes, want 1/1/1", grants, consumes, completes)
	}
	if e.G.Players[1].Life >= 20 {
		t.Fatal("the first combat's damage never landed")
	}
	replayCheck(t, e, cfg)
}

// TestMoraugExtraCombatUntapsAtItsBeginning is the ExtraPhaseDelayedTrigger$
// pair's leaf (real corpus Moraug, Fury of Akoum): the landfall grant in
// Main1 splices an extra combat after the main phase, and the forwarded
// delayed trigger fires exactly once at the extra combat's beginning,
// untapping the tapped Bear. Without the splice point the grant resolved in,
// the ordinary combat follows the extra one.
func TestMoraugExtraCombatUntapsAtItsBeginning(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := addPhaseEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Moraug, Fury of Akoum"), card(t, bearSrc)}, []*cards.Card{})
	moveByName(t, e, 0, "Moraug, Fury of Akoum", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Tap, Obj: bear})
	for driveToTurn(t, e, 3, 0) {
	}
	// Play a real land from the hand (the deck is Mountains): the landfall
	// trigger fires in Main1 and resolves the grant.
	moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	// The pending priority decision must be answered before the landfall
	// trigger is even pushed onto the stack (the drain runs at the next
	// priority round), so one pass opens the round and the drain resolves
	// the trigger -- emitting the grant.
	passOnce(t, e)
	passUntilStackEmpty(t, e, 40)
	if len(e.G.ExtraPhases) != 1 {
		t.Fatalf("ExtraPhases = %+v, want one grant", e.G.ExtraPhases)
	}
	ep := e.G.ExtraPhases[0]
	if ep.AfterStep != state.StepMain1 || ep.Entry != state.StepBeginCombat || ep.Consumed {
		t.Fatalf("grant = %+v, want an extra combat after Main1, unconsumed", ep)
	}
	if !ep.HasDelayedPhase || ep.DelayedPhase != state.StepBeginCombat ||
		ep.Execute != "TrigUntapAll" || ep.ValidPlayer != "You" {
		t.Fatalf("delayed rider = %+v, want DelTrigUntap/TrigUntapAll forwarded with ValidPlayer You", ep)
	}
	// Leave Main1: the extra combat begins, and the delayed trigger fires at
	// its beginning -- the Bear untaps.
	passToKind(t, e, decision.KAttackers)
	if e.G.Obj(bear).Tapped {
		t.Fatal("the extra combat's beginning did not untap the tapped Bear")
	}
	if !e.G.ExtraPhases[0].Consumed {
		t.Fatalf("grant not consumed at the extra combat: %+v", e.G.ExtraPhases[0])
	}
	// Decline the extra combat's attack: the grant completes and the walk
	// resumes at Main1's natural successor -- the ORDINARY combat. Decline
	// that one too; the turn then proceeds to Main2.
	submitNoAttackers(t, e)
	passToKind(t, e, decision.KAttackers)
	if e.G.Step != state.StepDeclareAttackers {
		t.Fatalf("after the extra combat: step %s, want the ordinary combat's attackers ask", e.G.Step)
	}
	submitNoAttackers(t, e)
	driveToStep(t, e, 3, 0, state.StepMain2)
	if e.G.Step != state.StepMain2 || e.G.Turn != 3 {
		t.Fatalf("after the ordinary combat: step %s turn %d, want Main2 of turn 3", e.G.Step, e.G.Turn)
	}
	if len(e.G.ExtraPhases) != 0 {
		t.Fatalf("ExtraPhases not drained: %+v", e.G.ExtraPhases)
	}
	replayCheck(t, e, cfg)
}

// TestEomerExtraCombatChainsThroughTheTrigger is the trigger → SubAbility$
// → AddPhase chain's leaf (real corpus Éomer, Marshal of Rohan): a legendary
// ally attacks and dies in combat, Éomer's death trigger untaps the team and
// grants the extra combat after the end-of-combat step.
func TestEomerExtraCombatChainsThroughTheTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := card(t, bearSrc)
	e, cfg := addPhaseEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Éomer, Marshal of Rohan"), lookup(t, reg, "Isamaru, Hound of Konda"), bear},
		[]*cards.Card{bear})
	moveByName(t, e, 0, "Éomer, Marshal of Rohan", state.ZBattlefield)
	isa := moveByName(t, e, 0, "Isamaru, Hound of Konda", state.ZBattlefield)
	for driveToTurn(t, e, 3, 0) {
	}
	// The blocker enters only now: a turn-1 Bear gets attacked into (and
	// trades itself away) by the drive bot's own turn-2 attack, and Éomer's
	// trigger needs a living blocker at turn 3's combat. Summoning sickness
	// does not stop blocking (CR 302.6 gates attacking, not blocking).
	blocker := moveByName(t, e, 1, "Bear", state.ZBattlefield)
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	// Isamaru attacks alone (Éomer need not attack: the trigger watches
	// OTHER attacking legendary creatures dying).
	submitAttackersOnly(t, e, isa)
	passAll(t, e, 100)
	passToKind(t, e, decision.KBlockers)
	submitBlockers(t, e, blocker)
	// The trade kills Isamaru; Éomer's trigger untaps the team and chains
	// the AddPhase grant (AfterPhase$ EndCombat).
	passAll(t, e, 200)
	if o := e.G.Obj(isa); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Isamaru did not die in combat: %+v", o)
	}
	if len(e.G.ExtraPhases) != 1 || e.G.ExtraPhases[0].AfterStep != state.StepEndCombat {
		t.Fatalf("ExtraPhases = %+v, want one grant spliced after EndCombat", e.G.ExtraPhases)
	}
	// The extra combat begins at the end-of-combat step's leaving.
	passToKind(t, e, decision.KAttackers)
	if !e.G.ExtraPhases[0].Consumed {
		t.Fatalf("grant not consumed at the extra combat: %+v", e.G.ExtraPhases[0])
	}
	submitNoAttackers(t, e)
	driveToStep(t, e, 3, 0, state.StepMain2)
	if e.G.Step != state.StepMain2 || e.G.Turn != 3 {
		t.Fatalf("after the extra combat: step %s turn %d, want Main2 of turn 3", e.G.Step, e.G.Turn)
	}
	if len(e.G.ExtraPhases) != 0 {
		t.Fatalf("ExtraPhases not drained: %+v", e.G.ExtraPhases)
	}
	replayCheck(t, e, cfg)
}

// TestAggravatedAssaultExtraCombatFollowedByMain2 is the FollowedBy$ leaf
// (real corpus Aggravated Assault): the activation resolves in Main2 (no
// AfterPhase$ -- the grant splices after the phase it resolved in), and the
// promised ADDITIONAL main phase is real: after the extra combat the turn
// goes to Main2 AGAIN, not to the end step.
func TestAggravatedAssaultExtraCombatFollowedByMain2(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := addPhaseEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Aggravated Assault"), card(t, bearSrc)}, []*cards.Card{})
	// The UntapAll activation is an ability of the ENCHANTMENT on the
	// battlefield (moved there directly, the Aurelia/Moraug pattern), and it
	// is offered only with one eligible creature present; the Bear also gives
	// the crossed combat an attackers ask to decline.
	moveByName(t, e, 0, "Bear", state.ZBattlefield)
	agg := moveByName(t, e, 0, "Aggravated Assault", state.ZBattlefield)
	for driveToTurn(t, e, 3, 0) {
	}
	driveToStep(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitNoAttackers(t, e)
	driveToStep(t, e, e.G.Turn, 0, state.StepMain2)
	// addMana would drive BACK to Main1 (its helper does a toMain1), which
	// the turn's main phase has already left -- emit the activation's cost
	// straight into the pool and refresh the priority ask instead.
	for _, r := range "CCCRR" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision for the activation (got %+v)", d)
	}
	idx := -1
	for _, o := range d.Options {
		// A battlefield permanent's printed AB is a Kind "ability" option.
		if o.Kind == "ability" && o.Obj == agg {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no ability option for Aggravated Assault: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit activation: %v", err)
	}
	passUntilStackEmpty(t, e, 40)
	if len(e.G.ExtraPhases) != 1 {
		t.Fatalf("ExtraPhases = %+v, want one grant", e.G.ExtraPhases)
	}
	ep := e.G.ExtraPhases[0]
	if ep.AfterStep != state.StepMain2 || !ep.HasFollowedBy || ep.FollowedBy != state.StepMain2 {
		t.Fatalf("grant = %+v, want an extra combat after Main2 followed by Main2", ep)
	}
	mains := countSteps(e, state.StepMain2)
	combats := countSteps(e, state.StepBeginCombat)
	// Leave Main2: the grant consumes and the extra combat's attackers ask
	// (the Bear is eligible) comes up; decline it -- CR 508.8 skips to the
	// end-of-combat step, the consumer completes the grant there and the
	// walk resumes at Main2, the explicit FollowedBy$ main phase.
	passAll(t, e, 200)
	submitNoAttackers(t, e)
	driveToStep(t, e, e.G.Turn, 0, state.StepMain2)
	if got := countSteps(e, state.StepBeginCombat) - combats; got != 1 {
		t.Fatalf("%d extra combats since the activation, want 1", got)
	}
	if got := countSteps(e, state.StepMain2) - mains; got != 1 {
		t.Fatalf("%d extra Main2 entries since the activation, want 1 (the FollowedBy$ main phase)", got)
	}
	if e.G.Turn != 3 || e.G.Step != state.StepMain2 {
		t.Fatalf("step %s turn %d, want Main2 of turn 3 still", e.G.Step, e.G.Turn)
	}
	if len(e.G.ExtraPhases) != 0 {
		t.Fatalf("ExtraPhases not drained: %+v", e.G.ExtraPhases)
	}
	// The turn then ends normally: the second Main2 walks to the end step,
	// cleanup and the next turn, with no third combat.
	driveToStep(t, e, 4, 1, state.StepMain1)
	if e.G.Turn != 4 {
		t.Fatalf("turn %d, want the ordinary rotation into turn 4", e.G.Turn)
	}
	if got := countSteps(e, state.StepBeginCombat) - combats; got != 1 {
		t.Fatalf("a third combat appeared in the following turns (%d total)", got)
	}
	replayCheck(t, e, cfg)
}

// TestRaiyuuFirstCombatGateBlocksTheSecondGrant is ConditionFirstCombat$'s
// leaf (real corpus Raiyuu, Storm's Edge): the first combat's lone attack
// grants the extra combat; the SAME trigger in the extra combat must not
// grant again -- the gate reads the per-turn combat count, and without it
// every extra combat Raiyuu attacks in would queue another one forever.
func TestRaiyuuFirstCombatGateBlocksTheSecondGrant(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := addPhaseEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Raiyuu, Storm's Edge")}, []*cards.Card{})
	raiyuu := moveByName(t, e, 0, "Raiyuu, Storm's Edge", state.ZBattlefield)
	for driveToTurn(t, e, 3, 0) {
	}
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, raiyuu)
	passOnce(t, e) // the 508.2 window's first pass: the attack trigger pushes
	passUntilStackEmpty(t, e, 40)
	if len(e.G.ExtraPhases) != 1 {
		t.Fatalf("ExtraPhases = %+v, want one grant from the first combat", e.G.ExtraPhases)
	}
	passAll(t, e, 200)
	passToKind(t, e, decision.KAttackers)
	if !e.G.ExtraPhases[0].Consumed {
		t.Fatalf("grant not consumed at the extra combat: %+v", e.G.ExtraPhases[0])
	}
	// Attack alone in the extra combat: the trigger fires again, and the
	// ConditionFirstCombat$ gate must keep it from granting a second time.
	submitAttackersOnly(t, e, raiyuu)
	passOnce(t, e)
	passUntilStackEmpty(t, e, 40)
	grants := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.ExtraPhase && ev.Amount > 0
	})
	if grants != 1 {
		t.Fatalf("%d ExtraPhase grants after the extra combat's lone attack, want 1 (the gate must block the second)", grants)
	}
	if e.G.CombatsThisTurn != 2 {
		t.Fatalf("CombatsThisTurn = %d, want 2", e.G.CombatsThisTurn)
	}
	// The extra combat still ends and the turn resumes at Main2.
	driveToStep(t, e, 3, 0, state.StepMain2)
	if e.G.Step != state.StepMain2 || e.G.Turn != 3 {
		t.Fatalf("after the extra combat: step %s turn %d, want Main2 of turn 3", e.G.Step, e.G.Turn)
	}
	if len(e.G.ExtraPhases) != 0 {
		t.Fatalf("ExtraPhases not drained: %+v", e.G.ExtraPhases)
	}
	replayCheck(t, e, cfg)
}

// TestExtraPhaseQueueDoesNotSurviveTheTurn is the turn-boundary leaf: a
// queued-but-unconsumed extra phase (here a grant whose splice point -- the
// untap step -- has already passed this turn) is dropped at the TurnChange
// fold, so no later turn of the game ever runs a second combat because of
// it.
func TestExtraPhaseQueueDoesNotSurviveTheTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := addPhaseEngine(t, reg, nil, nil)
	for driveToTurn(t, e, 3, 0) {
	}
	e.emit(events.Event{Kind: events.ExtraPhase, Player: 0, Amount: 1,
		Step: state.StepUntap, IDs: []state.ObjID{state.ObjID(state.StepBeginCombat)}})
	if len(e.G.ExtraPhases) != 1 {
		t.Fatalf("ExtraPhases = %+v, want the queued grant", e.G.ExtraPhases)
	}
	// Drive three full turns: the splice point never comes back this turn
	// (the queue's AfterStep is behind the walk), and the TurnChange fold
	// drops it -- every later turn runs exactly one combat.
	passAll(t, e, 400)
	if e.G.Turn < 6 {
		t.Fatalf("turn %d, want the drive to have crossed several turn boundaries", e.G.Turn)
	}
	if len(e.G.ExtraPhases) != 0 {
		t.Fatalf("the stale grant survived into later turns: %+v", e.G.ExtraPhases)
	}
	// Exactly one combat per turn: count the BeginCombat entries between the
	// last two TurnChange events.
	lastTurn, prevTurn := -1, -1
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		if e.L.Events[i].Kind != events.TurnChange {
			continue
		}
		if lastTurn < 0 {
			lastTurn = i
		} else {
			prevTurn = i
			break
		}
	}
	if prevTurn < 0 {
		t.Fatal("no two turn boundaries in the log")
	}
	combats := 0
	for _, ev := range e.L.Events[prevTurn:lastTurn] {
		if ev.Kind == events.StepChange && ev.Step == state.StepBeginCombat {
			combats++
		}
	}
	if combats != 1 {
		t.Fatalf("%d combats in the latest full turn, want exactly 1", combats)
	}
	replayCheck(t, e, cfg)
}
