package rules

// CR 112.7a / CR 400.7: a keyword-triggered ability resolves independently of
// its source. events.Apply arms resolveTop's source-incarnation gate only for
// keyword triggers that demonstrably act on the source permanent
// (keywordTriggerBindsSource), not for every KeywordTriggerPush. These tests
// pin both halves of that classification:
//
//   - a granted Ward and a granted Afflict act on the triggering object / the
//     captured defender, so they must still resolve when their granting
//     creature leaves and returns (a new incarnation, CR 400.7) in response;
//   - Evoke's builtin sacrifice acts on the source permanent, so a source
//     that leaves and returns must NOT be sacrificed by the stale promise.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// stackAbilityForSource returns the stack object of the triggered ability
// whose Source is src and whose registered API is api, or 0.
func stackAbilityForSource(e *Engine, src state.ObjID, api string) state.ObjID {
	for _, id := range e.G.Stack {
		o := e.G.Obj(id)
		if o != nil && o.Source == src && o.Ability != nil && o.Ability.API == api {
			return id
		}
	}
	return 0
}

// TestGrantedWardSurvivesSourceRemoval is the CR 112.7a regression: a
// layer-6 granted Ward's ability is a real triggered ability, and its body
// counters the targeting spell (which it reads off its TriggerContext), never
// the source permanent. Removing the warded creature in response to its own
// ward trigger -- and returning it as a NEW incarnation (CR 400.7) -- must
// not fizzle the ability. Without the fix events.Apply stamps every keyword
// trigger except Gift, so resolveTop's incarnation gate drops this one and
// the ward never asks.
func TestGrantedWardSurvivesSourceRemoval(t *testing.T) {
	e, bear := hexingEngine(t)

	// Precondition: this is the GRANTED ward (a printed K:Ward expands to a
	// face trigger, TriggerPush, and never mints this stack object).
	if !e.HasKeyword(bear, "Ward") {
		t.Fatal("bear has no granted Ward; the scenario is not the granted path")
	}
	if e.G.Obj(bear).Face().HasKeyword("Ward") {
		t.Fatal("bear prints Ward; the grant scenario is not the granted path")
	}

	// Put a real targeting spell on the stack at the warded bear so the
	// granted Ward trigger queues (the printed-ward fixture's low-level
	// emission, which isolates the trigger from the cast flow's own asks).
	cause := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.PutOnStack, Obj: cause, Player: 1, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: cause, IDs: []state.ObjID{bear}})
	e.putTriggersOnStack()
	wardID := stackAbilityForSource(e, bear, "Ward")
	if wardID == 0 {
		t.Fatalf("no granted Ward ability on the stack: %v", e.G.Stack)
	}
	// The correct KeywordTriggerPush body: the rebuilt DB$ Ward carries the
	// PayLife<2> UnlessCost and is sourced by the warded creature.
	if got := e.G.Obj(wardID).Ability.Params["UnlessCost"]; got != "PayLife<2>" {
		t.Fatalf("ward ability UnlessCost = %q, want PayLife<2>", got)
	}

	// Source leaves and returns as a new object (CR 400.7) before resolution.
	before := e.G.Obj(bear).Incarnation
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZGraveyard, To: state.ZBattlefield})
	if after := e.G.Obj(bear).Incarnation; after == before {
		t.Fatalf("precondition: incarnation did not change (%d -> %d)", before, after)
	}

	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.Player != 1 {
		t.Fatalf("granted ward fizzled on source removal: no pay ask (pending %+v, stack %v)", d, e.G.Stack)
	}
	// Decline: the ward itself counters the spell it captured.
	submitChoices(t, e, d.Options[len(d.Options)-1].Index)
	passUntilStackEmpty(t, e, 20)

	if zone := e.G.Obj(cause).Zone; zone != state.ZGraveyard {
		t.Fatalf("targeting spell zone = %s, want graveyard (warded out)", zone)
	}
	counteredByWard := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == cause && ev.Text == "countered by ward" {
			counteredByWard = true
		}
	}
	if !counteredByWard {
		t.Fatal("no 'countered by ward' move: the outcome was not the ward's triggered effect")
	}
	if zone := e.G.Obj(bear).Zone; zone != state.ZBattlefield {
		t.Fatalf("warded creature zone = %s, want battlefield (the bolt was countered)", zone)
	}
}

// TestGrantedAfflictSurvivesSourceRemoval is the CR 112.7a twin for
// Afflict: the granted ability drains the player the trigger captured
// (Defined$ TriggeredDefendingPlayer) and never reads the blocked attacker.
// Removing the attacker in response -- and returning it as a new incarnation
// -- must not fizzle the drain.
func TestGrantedAfflictSurvivesSourceRemoval(t *testing.T) {
	monarch := mshCorpusCardPath(t, "Lost Monarch of Ifnir", "l/lost_monarch_of_ifnir.txt")
	e := combatEngine(t)
	onBoardCard(t, e, 0, monarch)
	zomb := onBoardReady(t, e, 0, "Name:Zomb\nManaCost:1 B\nTypes:Creature Zombie\nPT:2/2\nOracle:x\n")
	// onBoard is an eventless placement (Incarnation stays 0), and a source
	// incarnation of 0 means "no stamp" to resolveTop's gate -- so the bug
	// this test targets would be invisible. Give the attacker a real
	// incarnation by bouncing it across the battlefield boundary once before
	// combat, exactly as a creature that entered from a zone would.
	e.emit(events.Event{Kind: events.MoveZone, Obj: zomb, From: state.ZBattlefield, To: state.ZHand})
	e.emit(events.Event{Kind: events.MoveZone, Obj: zomb, From: state.ZHand, To: state.ZBattlefield})
	e.G.Obj(zomb).SummonSick = false
	blk := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")

	if e.G.Obj(zomb).Incarnation == 0 {
		t.Fatal("precondition: attacker incarnation is 0, so the gate never arms")
	}

	if !e.HasKeyword(zomb, "Afflict") {
		t.Fatal("granted Afflict missing from the zombie's derived keywords")
	}
	if e.G.Obj(zomb).Face().HasKeyword("Afflict") {
		t.Fatal("zombie prints Afflict; the grant scenario is not the granted path")
	}

	e.askAttackers()
	submitAttackersOnly(t, e, zomb)
	drainCombatPriority(t, e)
	if d := e.Pending(); d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", e.Pending())
	}
	submitBlockersOnly(t, e, blk)
	// Triggered abilities go on the stack after blockers; answer the ordering
	// ask (there is one attacker, so it is a formality) and pass any response
	// window until the granted afflict ability is on the stack.
	afflictID := state.ObjID(0)
	for i := 0; i < 8 && afflictID == 0; i++ {
		if afflictID = stackAbilityForSource(e, zomb, "LoseLife"); afflictID != 0 {
			break
		}
		if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOrder {
			submitPresentedOrder(t, e)
			continue
		}
		passOnceP(t, e)
	}
	if afflictID == 0 {
		t.Fatalf("no granted Afflict ability on the stack: %v", e.G.Stack)
	}
	if got := e.G.Obj(afflictID).Ability.Params["LifeAmount"]; got != "3" {
		t.Fatalf("afflict ability LifeAmount = %q, want 3", got)
	}

	// The attacker leaves and returns as a new object before resolution.
	before := e.G.Obj(zomb).Incarnation
	e.emit(events.Event{Kind: events.MoveZone, Obj: zomb, From: state.ZBattlefield, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: zomb, From: state.ZGraveyard, To: state.ZBattlefield})
	if after := e.G.Obj(zomb).Incarnation; after == before {
		t.Fatalf("precondition: incarnation did not change (%d -> %d)", before, after)
	}

	lifeBefore := e.G.Players[1].Life
	e.resolveTop()
	if got := e.G.Players[1].Life; got != lifeBefore-3 {
		t.Fatalf("defending player life = %d, want %d (granted afflict 3 survived source removal)", got, lifeBefore-3)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("attacking player life = %d, want 20 (Afflict drains the DEFENDER)", got)
	}
}

// TestEvokeTriggerDoesNotSacrificeReturnedSource is the negative half: Evoke's
// builtin sacrifice body (DB$ Sacrifice | Defined$ Self) genuinely acts on
// the source permanent, so it MUST stay stamped. A source that leaves and
// returns as a new incarnation is not the object the promise named (CR
// 400.7) and must not be sacrificed -- without keywordTriggerBindsSource
// guarding it, unstamping Evoke would make the stale trigger sacrifice the
// returned creature.
func TestEvokeTriggerDoesNotSacrificeReturnedSource(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 923, []string{"Nulldrifter", "Stifle"}, nil, nil)
	null := findCardObj(t, e, 0, "Nulldrifter", state.ZHand)
	addMana(t, e, 0, "CCCUU")
	submitChoices(t, e, castModeOption(t, e, null, "evoked"))

	// Pass only until the evoked creature enters and its keyword trigger is
	// on the stack (the existing evoke fixture's poll).
	var evokeAbility state.ObjID
	for i := 0; i < 8; i++ {
		for _, stackID := range e.G.Stack {
			o := e.G.Obj(stackID)
			if o != nil && o.Ability != nil && o.Source == null && o.Ability.API == "Sacrifice" {
				evokeAbility = stackID
				break
			}
		}
		if evokeAbility != 0 {
			break
		}
		passOnceP(t, e)
	}
	if evokeAbility == 0 {
		t.Fatalf("no evoke trigger after entry: stack=%v", e.G.Stack)
	}
	if o := e.G.Obj(evokeAbility); o == nil || o.Ability == nil || o.Source != null {
		t.Fatalf("evoke stack object: %+v", o)
	}

	// Nulldrifter leaves and returns as a new object while its evoke trigger
	// is still on the stack.
	before := e.G.Obj(null).Incarnation
	e.emit(events.Event{Kind: events.MoveZone, Obj: null, From: state.ZBattlefield, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: null, From: state.ZGraveyard, To: state.ZBattlefield})
	if after := e.G.Obj(null).Incarnation; after == before {
		t.Fatalf("precondition: incarnation did not change (%d -> %d)", before, after)
	}

	e.resolveTop()
	if got := e.G.Obj(null).Zone; got != state.ZBattlefield {
		t.Fatalf("evoke sacrifice hit the returned incarnation: Nulldrifter zone = %s, want battlefield", got)
	}
	replayCheck(t, e, cfg)
}
