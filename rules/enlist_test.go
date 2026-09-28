package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Enlist (CR 702.160, task enlist1) pinned end to end on real corpus cards.
// Aradesh, the Founder is the deck carrier the brief names (its attack
// trigger gates on `ValidCard$ Creature.YouCtrl+enlistedThisCombat`, an
// intervening-if that was an unknown predicate and failed closed); Guardian
// of New Benalia carries the corpus's other enlist shape, the `Mode$
// Enlisted` listener (its DB$ Scry body is fully implemented). Both engines
// give seat 0 the carrier plus a 3/3 Hill Giant to enlist and seat 1 only
// Mountains, so nothing dies and the first legal attack is turn 3 seat 0
// Main1 (driveToExertTurn, the exert tests' shared drive).

func enlistEngine(t *testing.T, reg *cards.Registry, carrier *cards.Card) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	e, cfg := addPhaseEngine(t, reg, []*cards.Card{carrier, lookup(t, reg, "Hill Giant")}, nil)
	c := moveByName(t, e, 0, carrierName(carrier), state.ZBattlefield)
	giant := moveByName(t, e, 0, "Hill Giant", state.ZBattlefield)
	return e, cfg, c, giant
}

// carrierName reads the card's printed name (the deck carrier passed in).
func carrierName(c *cards.Card) string {
	if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
		return ""
	}
	return c.Faces[0].Name
}

// driveEnlistAttack declares the carrier attacking and answers the enlist
// election with the given option index (0 = decline, 1 = enlist the Hill
// Giant, the only candidate in battlefield zone order), asserting the
// election's shape on the way.
func driveEnlistAttack(t *testing.T, e *Engine, carrier state.ObjID, opt int) {
	t.Helper()
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, carrier)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != carrier ||
		len(d.Options) != 2 || d.Options[0].Kind != "enlist" || d.Options[0].Obj != 0 {
		t.Fatalf("pending = %+v, want the enlist election for object %d with a decline first", d, carrier)
	}
	submitChoices(t, e, opt)
}

// TestAradeshEnlistElectionPumpAndTrigger is the brief's leaf: the enlist
// election is offered at attack with the Hill Giant as a candidate; enlisting
// taps the giant and pumps the attacker +3/+0 (its power binds at enlist
// time, CR 702.160a); the enlistedThisCombat predicate turns true, so
// Aradesh's `Mode$ Attacks` trigger fires on his OWN attack, its DB$ Pump
// grants Double Strike, and the DBDraw sub fires because the pumped power is
// 4 (GE4). The whole game replays byte-identically.
func TestAradeshEnlistElectionPumpAndTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, aradesh, giant := enlistEngine(t, reg, lookup(t, reg, "Aradesh, the Founder"))
	if got := e.Power(aradesh); got != 1 {
		t.Fatalf("Aradesh printed power = %d, want 1", got)
	}
	driveToExertTurn(t, e)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, aradesh)

	// The election: option 0 declines, option 1 enlists the Hill Giant.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != aradesh || d.Player != 0 {
		t.Fatalf("pending = %+v, want Aradesh's enlist election asked of seat 0", d)
	}
	if len(d.Options) != 2 || d.Options[0].Obj != 0 || d.Options[1].Obj != giant {
		t.Fatalf("options = %+v, want [decline, enlist Hill Giant %d]", d.Options, giant)
	}
	tpBefore := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.TriggerPush })
	submitChoices(t, e, 1)

	// CR 702.160a's action: the enlisted creature is tapped, the attacker
	// carries +3/+0 until end of turn, and the Enlist event records the
	// action (IDs[0] the enlisted creature).
	if !e.G.Obj(giant).Tapped {
		t.Fatal("the enlisted Hill Giant was not tapped")
	}
	if got := e.Power(aradesh); got != 4 {
		t.Fatalf("Aradesh power after enlisting a 3-power creature = %d, want 4", got)
	}
	if countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.Enlist && ev.Obj == aradesh && len(ev.IDs) == 1 && ev.IDs[0] == giant
	}) != 1 {
		t.Fatal("no Enlist event recording Aradesh enlisting the Hill Giant")
	}
	// The declaration completed normally: Aradesh is the (non-Vigilance)
	// attacker.
	if !e.G.Obj(aradesh).Tapped {
		t.Fatal("Aradesh the attacker was not tapped")
	}
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.TriggerPush }); n != tpBefore+1 {
		t.Fatalf("TriggerPush events = %d, want %d (Aradesh's enlistedThisCombat attack trigger)", n, tpBefore+1)
	}

	// Resolve the trigger: the DB$ Pump grants Double Strike and the DBDraw
	// sub fires because the pumped power is 4. (The queue may already have
	// drained the trigger onto the stack, so drive both shapes.)
	handBefore := len(e.G.Zone(state.ZHand, 0))
	for i := 0; i < 20; i++ {
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
			continue
		}
		if len(e.G.Stack) > 0 {
			e.resolveTop()
			continue
		}
		break
	}
	sawDS := false
	for _, k := range e.Derived(aradesh).Keywords {
		if k == "Double Strike" {
			sawDS = true
		}
	}
	if !sawDS {
		t.Fatalf("Aradesh keywords after the trigger = %v, want Double Strike", e.Derived(aradesh).Keywords)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("hand = %d, want %d (the GE4 branch's draw never fired)", got, handBefore+1)
	}
	replayCheck(t, e, cfg)
}

// TestAradeshDeclinedEnlistIsNoTrigger is the negative half: declining the
// election emits no Enlist event, leaves the predicate false, so Aradesh's
// trigger never queues and the attacker's power is unchanged; the election's
// only effect was Aradesh's ordinary attack tap.
func TestAradeshDeclinedEnlistIsNoTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, aradesh, giant := enlistEngine(t, reg, lookup(t, reg, "Aradesh, the Founder"))
	driveToExertTurn(t, e)
	driveEnlistAttack(t, e, aradesh, 0)

	if e.G.Obj(giant).Tapped {
		t.Fatal("declining enlisted the Hill Giant anyway")
	}
	if got := e.Power(aradesh); got != 1 {
		t.Fatalf("Aradesh power after a declined enlist = %d, want 1", got)
	}
	if !e.G.Obj(aradesh).Tapped {
		t.Fatal("Aradesh the attacker was not tapped (the declaration itself still completed)")
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("pendingTriggers = %d, want 0 (enlistedThisCombat must fail closed on a declined enlist)", len(e.pendingTriggers))
	}
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.TriggerPush }); n != 0 {
		t.Fatalf("TriggerPush events = %d, want 0 (a declined enlist must not fire the attack trigger)", n)
	}
	if countEvents(e, func(ev events.Event) bool { return ev.Kind == events.Enlist }) != 0 {
		t.Fatal("a declined enlist still emitted an Enlist event")
	}
	replayCheck(t, e, cfg)
}

// TestGuardianOfNewBenaliaEnlistedTriggerScr is the Mode$ Enlisted listener
// end to end on its real corpus carrier: Guardian enlists the Hill Giant, its
// `T:Mode$ Enlisted | Execute$ TrigScry` fires, and the resolution poses the
// scry-2 KArrange over the library's top cards.
func TestGuardianOfNewBenaliaEnlistedTriggerScr(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, guardian, giant := enlistEngine(t, reg, lookup(t, reg, "Guardian of New Benalia"))
	driveToExertTurn(t, e)
	driveEnlistAttack(t, e, guardian, 1)

	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.TriggerPush }); n != 1 {
		t.Fatalf("TriggerPush events = %d, want 1 (Guardian's Mode$ Enlisted trigger)", n)
	}
	for i := 0; i < 20; i++ {
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
			continue
		}
		if d := e.Pending(); d != nil && d.Kind == decision.KArrange {
			break
		}
		if len(e.G.Stack) > 0 {
			e.resolveTop()
			continue
		}
		break
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KArrange || len(d.Options) != 2 {
		t.Fatalf("pending = %+v, want the Enlisted body's scry-2 KArrange over 2 cards", d)
	}
	if d.Player != 0 {
		t.Fatalf("scry player = %d, want 0 (the library owner)", d.Player)
	}
	submitChoices(t, e, 0, 1)
	passUntilStackEmpty(t, e, 40)
	if !e.G.Obj(giant).Tapped {
		t.Fatal("the enlisted Hill Giant was not tapped")
	}
	replayCheck(t, e, cfg)
}

// TestEnlistElectionExcludesTheDeclaredFellow is the CR 702.160a
// "nonattacking" pin on the two-Enlist-attacker shape: with Aradesh AND
// Guardian of New Benalia both declared, IsAttacking has not folded yet (the
// DeclareAttackers events follow every election), so the declared set is
// derived from the recorded declaration itself -- Aradesh's election offers
// only the true nonattackers (Hill Giant, Bear), never his fellow attacker.
func TestEnlistElectionExcludesTheDeclaredFellow(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := addPhaseEngine(t, reg, []*cards.Card{
		lookup(t, reg, "Aradesh, the Founder"), lookup(t, reg, "Guardian of New Benalia"),
		lookup(t, reg, "Hill Giant"), lookup(t, reg, "Grizzly Bears")}, nil)
	aradesh := moveByName(t, e, 0, "Aradesh, the Founder", state.ZBattlefield)
	guardian := moveByName(t, e, 0, "Guardian of New Benalia", state.ZBattlefield)
	giant := moveByName(t, e, 0, "Hill Giant", state.ZBattlefield)
	bears := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	driveToExertTurn(t, e)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, aradesh, guardian)

	// Aradesh's election (the first in declaration option order): exactly
	// [decline, giant, bears] -- no option names Guardian, the other attacker.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != aradesh {
		t.Fatalf("pending = %+v, want Aradesh's enlist election", d)
	}
	if len(d.Options) != 3 || d.Options[1].Obj != giant || d.Options[2].Obj != bears {
		t.Fatalf("options = %+v, want [decline, enlist Hill Giant, enlist Grizzly Bears]", d.Options)
	}
	for _, o := range d.Options {
		if o.Obj == guardian {
			t.Fatalf("Aradesh's enlist election offered his fellow attacker %d: %+v", guardian, d.Options)
		}
	}
	submitChoices(t, e, 2) // enlist the Grizzly Bears
	if !e.G.Obj(bears).Tapped {
		t.Fatal("the enlisted Grizzly Bears was not tapped")
	}
	if got := e.Power(aradesh); got != 3 {
		t.Fatalf("Aradesh power after enlisting the 2-power Grizzly Bears = %d, want 3", got)
	}

	// Guardian's election: the Bears are now tapped and Aradesh is declared, so
	// the only candidate is the Hill Giant; decline it and nothing further
	// happens (exactly one Enlist event in the whole game).
	d2 := e.Pending()
	if d2 == nil || d2.Kind != decision.KChoose || d2.Source != guardian {
		t.Fatalf("pending = %+v, want Guardian's enlist election", d2)
	}
	if len(d2.Options) != 2 || d2.Options[1].Obj != giant {
		t.Fatalf("options = %+v, want [decline, enlist Hill Giant]", d2.Options)
	}
	for _, o := range d2.Options {
		if o.Obj == aradesh {
			t.Fatalf("Guardian's enlist election offered her fellow attacker %d: %+v", aradesh, d2.Options)
		}
	}
	submitChoices(t, e, 0)
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.Enlist }); n != 1 {
		t.Fatalf("Enlist events = %d, want 1 (the declined second election emits nothing)", n)
	}
	if e.G.Obj(giant).Tapped {
		t.Fatal("the declined election tapped the Hill Giant anyway")
	}
	replayCheck(t, e, cfg)
}

// TestEnlistedMatchesValidEnlistedSpec pins the matcher's two spec gates
// directly, on Goblin Morale Sergeant's real compiled trigger
// (ValidCard$ Card.Self names the attacking source; ValidEnlisted$
// Creature.!token filters the tapped creature): a real enlist action matches,
// and an enlisted id that is not a creature fails the ValidEnlisted spec.
func TestEnlistedMatchesValidEnlistedSpec(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, goblin, giant := enlistEngine(t, reg, lookup(t, reg, "Goblin Morale Sergeant"))
	f := e.G.Obj(goblin).Face()
	var tr cards.Trigger
	found := false
	for _, c := range f.Triggers {
		if c.Mode == "Enlisted" {
			tr, found = c, true
		}
	}
	if !found {
		t.Fatal("fixture: Goblin Morale Sergeant's compiled face carries no Enlisted trigger")
	}
	if !e.enlistedMatches(tr, goblin,
		events.Event{Kind: events.Enlist, Obj: goblin, IDs: []state.ObjID{giant}}, nil) {
		t.Fatal("a real enlist action did not match Goblin Morale Sergeant's Enlisted trigger")
	}
	mtn := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	if e.enlistedMatches(tr, goblin,
		events.Event{Kind: events.Enlist, Obj: goblin, IDs: []state.ObjID{mtn}}, nil) {
		t.Fatal("enlisting a non-creature matched ValidEnlisted$ Creature.!token")
	}
}
