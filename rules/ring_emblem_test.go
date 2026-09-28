package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// ringEmblemEngine is layerEngine plus the Config its replay needs.
func ringEmblemEngine(t *testing.T) (*Engine, Config) {
	t.Helper()
	cfg := seatZeroStart(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	return New(cfg), cfg
}

// ringTemptCount labels a level for diagnostics.
func ringTemptCount(e *Engine, p state.PlayerID) int32 { return e.G.Players[p].RingTempted }

// countLifeChange sums the LifeChange events for one seat (the emblem's level
// 4 drain is a LoseLife -> LifeChange with a negative amount).
func countLifeChange(e *Engine, p state.PlayerID) int32 {
	var total int32
	for _, ev := range e.L.Events {
		if ev.Kind == events.LifeChange && ev.Player == p {
			total += ev.Amount
		}
	}
	return total
}

// temptSeat emits one RingTemptsYou for p with the given bearer and drains
// nothing -- the caller does. Used to build a temptation count without the
// Call of the Ring payoff trigger complicating the queue.
func temptSeat(e *Engine, p state.PlayerID, bearer state.ObjID) {
	e.emit(events.Event{Kind: events.RingTemptsYou, Player: p, Obj: bearer,
		Amount: e.G.Players[p].RingTempted + 1})
}

// clearHand removes every card from p's hand (the "if you can't discard"
// board): each card is moved to the library with a real logged event, so the
// replay still reproduces the state.
func clearHand(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZHand, p)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
	}
}

// TestRingEmblemLevel1DrawsWhenRingBearerAttacks pins CR 701.54c's level 1
// ("Whenever your Ring-bearer attacks, draw a card") end to end from the real
// corpus tempt source, Call of the Ring's upkeep. Before any temptation the
// emblem has no level, so the same attack draws nothing; once Call of the Ring
// has tempted seat 0 the bearer attacking draws exactly one card.
func TestRingEmblemLevel1DrawsWhenRingBearerAttacks(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Call of the Ring"))

	// Level 0: no temptation yet, so the emblem fires nothing on an attack.
	before := countDraws(e, 0)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{bear}})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatalf("level 0: attacking queued %d emblem triggers, want none", len(e.G.Stack))
	}
	if n := countDraws(e, 0) - before; n != 0 {
		t.Fatalf("level 0: attacking drew %d, want 0", n)
	}

	// Call of the Ring's upkeep tempts seat 0 (CR 701.54a). The bear (first
	// creature in battlefield zone order) becomes the Ring-bearer. The Call's
	// own RingTemptsYou payoff trigger then asks to pay 2 life; decline it.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	e.resolveTop()
	e.putTriggersOnStack()
	e.resolveTop()
	declineTriggerCost(t, e)
	if ringTemptCount(e, 0) != 1 || e.G.Players[0].RingBearer != bear {
		t.Fatalf("after Call of the Ring's upkeep: tempted %d bearer %d, want 1/%d",
			ringTemptCount(e, 0), e.G.Players[0].RingBearer, bear)
	}

	// Level 1 is live: the Ring-bearer attacking draws a card.
	before = countDraws(e, 0)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{bear}})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("level 1: attacking queued %d stack objects, want the emblem ability", len(e.G.Stack))
	}
	e.resolveTop()
	if n := countDraws(e, 0) - before; n != 1 {
		t.Fatalf("level 1: Ring-bearer attacking drew %d, want 1", n)
	}
}

// TestRingEmblemLevel1IgnoresNonBearerAttacks pins that level 1 names the
// Ring-bearer, not "a creature you control": a second attacker that is not the
// bearer does not draw, and the bearer must be the one in the attacker list.
func TestRingEmblemLevel1IgnoresNonBearerAttacks(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	other := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	temptSeat(e, 0, bear)

	before := countDraws(e, 0)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{other}})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatalf("a non-bearer attacker queued %d emblem triggers, want none", len(e.G.Stack))
	}
	if n := countDraws(e, 0) - before; n != 0 {
		t.Fatalf("a non-bearer attacker drew %d, want 0", n)
	}
}

// TestRingEmblemLevel2DiscardsThenDoesNotSacrifice pins CR 701.54c's level 2
// positive arm: the Ring-bearer becomes blocked, seat 0 has cards so it
// discards one (the ask answered), and because a card WAS discarded the
// "if you can't, sacrifice it" chain does not run -- the bearer survives.
func TestRingEmblemLevel2DiscardsThenDoesNotSacrifice(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	blocker := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	temptSeat(e, 0, bear)
	temptSeat(e, 0, bear) // level 2

	handBefore := len(e.G.Zone(state.ZHand, 0))
	if handBefore < 2 {
		t.Fatalf("fixture needs a hand with a choice, got %d", handBefore)
	}

	e.emit(events.Event{Kind: events.DeclareBlockers, Player: 1,
		Pairs: [][2]state.ObjID{{bear, blocker}}})
	e.putTriggersOnStack()
	e.resolveTop()
	// A discard ask (KChoose) is posed: the discarding player chooses.
	d := e.Pending()
	if d == nil {
		t.Fatal("level 2: no discard ask posed")
	}
	submitChoices(t, e, 0)
	if n := countEvents(e, events.IsDiscard); n != 1 {
		t.Fatalf("level 2: Discard events = %d, want 1", n)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("level 2: a card WAS discarded, so the bearer must survive; zone %v", e.G.Obj(bear).Zone)
	}
}

// TestRingEmblemLevel2SacrificesOnEmptyHand pins CR 701.54c's level 2 "if you
// can't" arm: with an empty hand nothing is discarded and no ask is posed, so
// the chained sacrifice runs and the Ring-bearer is sacrificed.
func TestRingEmblemLevel2SacrificesOnEmptyHand(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	blocker := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	temptSeat(e, 0, bear)
	temptSeat(e, 0, bear)
	clearHand(t, e, 0)
	if len(e.G.Zone(state.ZHand, 0)) != 0 {
		t.Fatalf("hand not cleared: %d", len(e.G.Zone(state.ZHand, 0)))
	}

	e.emit(events.Event{Kind: events.DeclareBlockers, Player: 1,
		Pairs: [][2]state.ObjID{{bear, blocker}}})
	e.putTriggersOnStack()
	if e.Pending() != nil {
		t.Fatal("level 2 on an empty hand posed a decision, want none")
	}
	e.resolveTop()
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("level 2 empty hand: bearer zone %v, want graveyard (sacrificed)", e.G.Obj(bear).Zone)
	}
}

// TestRingEmblemLevel3SacrificesBearerOnCombatDamage pins CR 701.54c's level
// 3: when the Ring-bearer deals combat damage to a player it is sacrificed.
// The combat flag pair (e.damaging/e.combatDamaging) is set exactly as
// dealCombatDamage sets it for each assignment.
func TestRingEmblemLevel3SacrificesBearerOnCombatDamage(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	for i := 0; i < 3; i++ {
		temptSeat(e, 0, bear)
	}

	e.damaging = bear
	e.combatDamaging = true
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.combatDamaging = false
	e.damaging = 0
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("level 3: combat damage queued %d stack objects, want the emblem", len(e.G.Stack))
	}
	e.resolveTop()
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("level 3: bearer zone %v, want graveyard", e.G.Obj(bear).Zone)
	}
}

// TestRingEmblemLevel3NonCombatDamageDoesNotSacrifice pins that level 3 names
// COMBAT damage: the identical Damage event with the combat flag unset (an
// ordinary burn spell) fires nothing.
func TestRingEmblemLevel3NonCombatDamageDoesNotSacrifice(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	for i := 0; i < 3; i++ {
		temptSeat(e, 0, bear)
	}

	e.damaging = bear
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.damaging = 0
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatalf("non-combat damage queued %d emblem triggers, want none", len(e.G.Stack))
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("non-combat damage sacrificed the bearer (zone %v)", e.G.Obj(bear).Zone)
	}
}

// TestRingEmblemLevel4DrainsOnFourthTemptationNotThird pins CR 701.54c's
// level 4 gate edge: "Whenever the Ring tempts you, each opponent loses 1
// life" is live exactly once the tempted seat's count reaches 4. The count is
// folded BEFORE checkTriggers runs, so the 4th temptation itself fires it
// (post-fold count 4) and the 3rd does not (post-fold count 3).
func TestRingEmblemLevel4DrainsOnFourthTemptationNotThird(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))

	// Temptations 1..3: level 4 is not live yet.
	for i := 0; i < 3; i++ {
		temptSeat(e, 0, bear)
		e.putTriggersOnStack()
		if len(e.G.Stack) != 0 {
			t.Fatalf("temptation %d (count %d) queued the level-4 drain early",
				i+1, ringTemptCount(e, 0))
		}
	}
	before := countLifeChange(e, 1)

	// The 4th temptation: the count folds to 4 in Apply before the trigger
	// check, so level 4 fires on this very temptation.
	temptSeat(e, 0, bear)
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("4th temptation queued %d stack objects, want the level-4 drain", len(e.G.Stack))
	}
	e.resolveTop()
	if got := before - countLifeChange(e, 1); got != 1 {
		t.Fatalf("4th temptation drained %d life from the opponent, want 1", got)
	}

	// The 5th temptation also fires (the count stays >= 4).
	before = countLifeChange(e, 1)
	temptSeat(e, 0, bear)
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("5th temptation queued %d stack objects, want the level-4 drain", len(e.G.Stack))
	}
	e.resolveTop()
	if got := before - countLifeChange(e, 1); got != 1 {
		t.Fatalf("5th temptation drained %d life from the opponent, want 1", got)
	}
}

// TestRingEmblemLevel4DrainsWithNoBearer pins CR 701.54d: the temptation still
// counts even when no creature could become the Ring-bearer, and level 4 --
// which names the temptation itself, not the bearer -- still fires.
func TestRingEmblemLevel4DrainsWithNoBearer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	_ = reg
	// No creatures at all: Obj 0 temptations.
	for i := 0; i < 3; i++ {
		temptSeat(e, 0, 0)
	}
	before := countLifeChange(e, 1)
	temptSeat(e, 0, 0)
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("Obj-0 4th temptation queued %d stack objects, want the level-4 drain", len(e.G.Stack))
	}
	e.resolveTop()
	if got := before - countLifeChange(e, 1); got != 1 {
		t.Fatalf("Obj-0 4th temptation drained %d life, want 1", got)
	}
	if e.G.Players[0].RingBearer != 0 {
		t.Fatalf("Obj-0 temptation designated a bearer %d, want none", e.G.Players[0].RingBearer)
	}
}

// TestRingEmblemAbilitiesReplayByteIdentically drives all four level
// abilities and then replays the recorded log through events.Apply alone.
// The emblem's stack objects are minted by the RingEmblemPush event inside
// events.Apply (Ruling T20-a), so a log-only replay reconstructs the
// identical game state -- no unlogged Game.AddObject names an ObjID the
// replay never learns about.
//
// The baseline is a clone taken just before the emblem events: the fixture's
// board placement (onBoardCard) is deliberately eventless, so replayFromLog's
// genesis would not carry it and every object id would shift. Cloning keeps
// the ids the log was written against, which is the property under test.
func TestRingEmblemAbilitiesReplayByteIdentically(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	blocker := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))

	base := e.G.Clone()
	mark := len(e.L.Events)

	// Temptations 1..4: levels 1-3 become live and the 4th fires level 4's
	// drain (each resolved in turn).
	for i := 0; i < 4; i++ {
		temptSeat(e, 0, bear)
		e.putTriggersOnStack()
		for len(e.G.Stack) > 0 {
			e.resolveTop()
		}
	}

	// Level 1: the bearer attacks and draws.
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{bear}})
	e.putTriggersOnStack()
	e.resolveTop()

	// Level 2: the bearer becomes blocked; the hand has cards, so it discards
	// one (answer the ask) and no sacrifice follows.
	e.emit(events.Event{Kind: events.DeclareBlockers, Player: 1,
		Pairs: [][2]state.ObjID{{bear, blocker}}})
	e.putTriggersOnStack()
	e.resolveTop()
	if e.Pending() != nil {
		submitChoices(t, e, 0)
	}

	// Level 3: the bearer deals combat damage to a player and is sacrificed.
	e.damaging = bear
	e.combatDamaging = true
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.combatDamaging = false
	e.damaging = 0
	e.putTriggersOnStack()
	for len(e.G.Stack) > 0 {
		e.resolveTop()
	}

	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.RingEmblemPush }); n != 4 {
		t.Fatalf("RingEmblemPush events = %d, want 4 (one per level)", n)
	}

	replayed := base.Clone()
	for _, ev := range e.L.Events[mark:] {
		events.Apply(replayed, ev)
	}
	if diff := diffGames(e.G, replayed); diff != "" {
		t.Fatalf("log-only replay of the emblem events differs:\n%s", diff)
	}
}

// TestRingEmblemLevelsRequireALiveBearer pins that levels 1-3 name "your
// Ring-bearer": a seat tempted with no creature (Obj 0, CR 701.54d) has a
// count but no bearer, so attacking/blocked/combat-damage fire nothing.
func TestRingEmblemLevelsRequireALiveBearer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	blocker := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	// No creature was available at tempt time: count 3, bearer 0.
	for i := 0; i < 3; i++ {
		temptSeat(e, 0, 0)
	}
	if e.G.Players[0].RingBearer != 0 {
		t.Fatalf("fixture: bearer %d, want 0", e.G.Players[0].RingBearer)
	}

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{bear}})
	e.emit(events.Event{Kind: events.DeclareBlockers, Player: 1,
		Pairs: [][2]state.ObjID{{bear, blocker}}})
	e.damaging = bear
	e.combatDamaging = true
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.combatDamaging = false
	e.damaging = 0
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatalf("a seat with no Ring-bearer queued %d emblem triggers, want none", len(e.G.Stack))
	}
}

// TestRingEmblemLevel3BearerDiedInCombatIsANoop pins CR 701.54c's level 3
// when the Ring-bearer dies in the same combat: the trigger still resolves,
// but the SacValid$ set is empty (the designation was cleared when it left
// the battlefield), so there is no ask and no sacrifice.
func TestRingEmblemLevel3BearerDiedInCombatIsANoop(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	for i := 0; i < 3; i++ {
		temptSeat(e, 0, bear)
	}

	e.damaging = bear
	e.combatDamaging = true
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.combatDamaging = false
	e.damaging = 0
	// Combat damage is dealt simultaneously: the bearer dies before the
	// triggered ability resolves (CR 704.5g), clearing the designation.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield,
		To: state.ZGraveyard})
	if e.G.Players[0].RingBearer != 0 {
		t.Fatalf("fixture: the dead bearer is still designated (%d)", e.G.Players[0].RingBearer)
	}
	e.putTriggersOnStack()
	if e.Pending() != nil {
		t.Fatalf("level 3 with a dead bearer posed a decision %+v, want none", e.Pending())
	}
	e.resolveTop()
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("bearer zone %v, want graveyard", e.G.Obj(bear).Zone)
	}
}

// TestRingEmblemPendingTriggerIntegration pins the two queue-side touch
// points the emblem entries rely on: triggerLabel (read by an ordering ask
// and the optional-trigger prompt) names the level without a face to read
// from, and optionalDecider reports the level abilities as mandatory
// (CR 701.54c's "whenever" abilities have no OptionalDecider$ and no face for
// triggerOf to walk). A coexistence ordering ask is therefore safe.
func TestRingEmblemPendingTriggerIntegration(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	for _, lvl := range []int{1, 2, 3, 4} {
		pt := pendingTrigger{RingEmblem: lvl}
		if got := e.triggerLabel(pt); got == "" {
			t.Fatalf("level %d has no trigger label", lvl)
		}
		who, optional, askable := e.optionalDecider(pt)
		if optional || askable {
			t.Fatalf("level %d reported optional=%v askable=%v, want mandatory",
				lvl, optional, askable)
		}
		_ = who
	}
}
