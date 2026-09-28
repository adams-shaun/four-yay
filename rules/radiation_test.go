package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// radiationCorpusCard looks a radiation-carrier corpus card up by name.
func radiationCorpusCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus missing %s", name)
	}
	return c
}

// radiationLibrary replaces p's whole library with the given cards in TOP
// order (index 0 is the next card milled/drawn), so the land/nonland split the
// drain walks is exactly the authored list rather than the seat fixture's 40
// Mountains.
func radiationLibrary(t *testing.T, e *Engine, p state.PlayerID, cs ...*cards.Card) []state.ObjID {
	t.Helper()
	ids := make([]state.ObjID, 0, len(cs))
	for _, c := range cs {
		o := e.G.AddObject(c, p)
		o.Zone = state.ZLibrary
		ids = append(ids, o.ID)
	}
	e.G.SetZone(state.ZLibrary, p, ids)
	return ids
}

// radiationLand is a land library card (the drain must skip it for the
// life-loss/counter-removal half).
func radiationLand(t testing.TB) *cards.Card {
	return card(t, "Name:Rad Test Land\nTypes:Land\nOracle:x\n")
}

// radiationSpell is a nonland library card.
func radiationSpell(t testing.TB, n string) *cards.Card {
	return card(t, "Name:"+n+"\nManaCost:1 R\nTypes:Sorcery\nOracle:x\n")
}

// The inherent CR 728.1 ability exists without any card permanent to serve as
// its source. Its event-created stack object must retain that zero source.
func TestRadiationDrainIsSourceLessTriggeredAbility(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	seedPlayerCounter(t, e, 0, "RAD", 2)
	if got := e.G.Players[0].Counter("RAD"); got != 2 {
		t.Fatalf("precondition: RAD=%d, want 2", got)
	}
	e.G.Step = state.StepMain1
	e.finishEnteredStep()
	if len(e.pendingTriggers) != 1 || !e.pendingTriggers[0].RadiationDrain {
		t.Fatalf("precombat-main did not queue radiation trigger: %+v", e.pendingTriggers)
	}
	e.pushTrigger(e.pendingTriggers[0])
	if len(e.G.Stack) != 1 {
		t.Fatalf("radiation ability stack length=%d, want 1", len(e.G.Stack))
	}
	o := e.G.Obj(e.G.Stack[0])
	if o == nil || o.StackKind != state.StackKindTriggered || o.Source != 0 || o.Controller != 0 {
		t.Fatalf("inherent drain stack object=%+v, want triggered, source-less, controller 0", o)
	}
	if got := e.G.Players[0].Counter("RAD"); got != 2 {
		t.Fatalf("RAD changed before response window: %d, want 2", got)
	}
}

func TestRadiationDrainNotQueuedWithoutCounters(t *testing.T) {
	t.Parallel()
	// The no-op assertion below would also pass with the whole mechanic
	// unregistered, so pin that both primitives are reachable first.
	for _, api := range []string{"api:Radiation", "api:RadiationDrain"} {
		if !effects.Supported()[api] {
			t.Fatalf("precondition: %s is not registered", api)
		}
	}
	e := newSeats(t, 2)
	if got := e.G.Players[0].Counter("RAD"); got != 0 {
		t.Fatalf("precondition: RAD=%d, want 0", got)
	}
	e.G.Step = state.StepMain1
	e.finishEnteredStep()
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("zero-RAD player queued a drain: %+v", e.pendingTriggers)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("zero-RAD player has stack object(s): %v", e.G.Stack)
	}
}

// TestRadiationPlacementAcquiredMutationAttacks drives the REAL corpus Aura
// through its own attack trigger: `T:Mode$ Attacks | ValidCard$
// Creature.EnchantedBy | Execute$ TrigRadiation` ->
// `DB$ Radiation | Defined$ TriggeredDefendingPlayer | Num$ 2`. The defending
// player gets exactly two rad counters and the attacking player gets none.
func TestRadiationPlacementAcquiredMutationAttacks(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	aura := onBoardCard(t, e, 0, radiationCorpusCard(t, "Acquired Mutation"))
	bear := onBoard(t, e, 0, "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// Precondition: the trigger under test is the DB$ Radiation body and the
	// Aura is attached to the creature it must trigger off.
	trig := resolveSVarOf(t, e.G.Obj(aura).Face(), "TrigRadiation")
	if trig.API != "Radiation" || trig.Params["Defined"] != "TriggeredDefendingPlayer" || trig.Params["Num"] != "2" {
		t.Fatalf("test precondition: TrigRadiation = %+v, want DB$ Radiation/TriggeredDefendingPlayer/2", trig)
	}
	ao := e.G.Obj(aura)
	ao.AttachedTo = bear
	if e.G.Obj(bear).AttachedTo == aura {
		t.Fatal("test precondition: AttachedTo is symmetric, want the Aura pointing at the creature")
	}

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{bear}})
	e.putTriggersOnStack()
	e.resolveTop()

	if got := e.G.Players[1].Counter("RAD"); got != 2 {
		t.Fatalf("defending player RAD=%d, want 2", got)
	}
	if got := e.G.Players[0].Counter("RAD"); got != 0 {
		t.Fatalf("attacking player RAD=%d, want 0 (the trigger names the defender only)", got)
	}
}

// TestRadiationPlacementVault12ChapterI drives the REAL Saga's chapter-I body
// (`DB$ Radiation | Defined$ Player | Num$ 3`) and asserts EVERY player gains
// three rad counters. The chapter is queued by the lore counter the Saga
// enters with, exactly as a live entry does.
func TestRadiationPlacementVault12ChapterI(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 3)
	saga := onBoardCard(t, e, 0, radiationCorpusCard(t, "Vault 12: The Necropolis"))
	// Precondition: chapter I is the DB$ Radiation body over every player.
	first := resolveSVarOf(t, e.G.Obj(saga).Face(), "DBRadiation")
	if first.API != "Radiation" || first.Params["Defined"] != "Player" || first.Params["Num"] != "3" {
		t.Fatalf("test precondition: DBRadiation = %+v, want DB$ Radiation/Player/3", first)
	}
	if e.G.Obj(saga).Zone != state.ZBattlefield {
		t.Fatal("precondition: the Saga is not on the battlefield")
	}

	// The entry lore counter queues chapter I (rules/saga.go).
	e.emit(events.Event{Kind: events.CounterChange, Obj: saga, Counter: "LORE", Amount: 1})
	e.putTriggersOnStack()
	e.resolveTop()

	for p := state.PlayerID(0); p < 3; p++ {
		if got := e.G.Players[p].Counter("RAD"); got != 3 {
			t.Fatalf("player %d RAD=%d, want 3", p, got)
		}
	}

	// The Saga's own SVar:X reads the group sum, so it must now resolve over
	// the three counters just placed (3 players x 3 rad counters = 9).
	body := svarBodyOf(t, e.G.Obj(saga).Face(), "X")
	if body != "PlayerCountPlayers$Counters.RAD" {
		t.Fatalf("test precondition: Vault 12 SVar:X = %q", body)
	}
	if got, ok := effects.EvalCountOK(e, &effects.Ctx{Source: saga, Controller: 0,
		SVars: e.G.Obj(saga).Face().SVars}, body); !ok || got != 9 {
		t.Fatalf("Vault 12 SVar:X = (%d,%v), want (9,true)", got, ok)
	}
}

// TestRadiationDrainMillsAndDrains drives the CR 728.1 inherent trigger end to
// end on seat 0's own precombat main with a known TOP-ordered library. The
// library is [Land, Spell, Spell, Land, Spell], seat 0 has 4 rad counters, so
// the drain mills 4 (Land, Spell, Spell, Land), losing 1 life and removing 1
// rad counter for each of the 2 NONLANDS milled -- the land exclusion is what
// a naive "remove one per card milled" implementation would fail.
func TestRadiationDrainMillsAndDrains(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	top := radiationLibrary(t, e, 0,
		radiationLand(t), radiationSpell(t, "Mill One"), radiationSpell(t, "Mill Two"), radiationLand(t), radiationSpell(t, "Below"))
	seedPlayerCounter(t, e, 0, "RAD", 4)
	lifeBefore := e.G.Players[0].Life
	gyBefore := len(e.G.Zone(state.ZGraveyard, 0))
	if got := e.G.Players[0].Counter("RAD"); got != 4 {
		t.Fatalf("precondition: RAD=%d, want 4", got)
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != 5 {
		t.Fatalf("precondition: library=%d, want 5", got)
	}

	e.G.Step = state.StepMain1
	e.finishEnteredStep()
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("drain not on the stack: %v", e.G.Stack)
	}
	e.resolveTop()

	if got := len(e.G.Zone(state.ZGraveyard, 0)) - gyBefore; got != 4 {
		t.Fatalf("cards milled=%d, want 4 (one per rad counter)", got)
	}
	// The four top cards must be the ones milled, in order.
	for i := 0; i < 4; i++ {
		if got := e.G.Obj(top[i]).Zone; got != state.ZGraveyard {
			t.Fatalf("library card %d zone=%v, want graveyard", i, got)
		}
	}
	if got := e.G.Obj(top[4]).Zone; got != state.ZLibrary {
		t.Fatalf("the fifth library card zone=%v, want still library", got)
	}
	if got := lifeBefore - e.G.Players[0].Life; got != 2 {
		t.Fatalf("life lost=%d, want 2 (one per NONLAND milled)", got)
	}
	if got := e.G.Players[0].Counter("RAD"); got != 2 {
		t.Fatalf("RAD after drain=%d, want 2 (4 minus one per nonland milled)", got)
	}
}

// TestRadiationDrainZeroCountersDoesNothing crosses the same player into their
// precombat main with a full library but no rad counters: no drain is queued,
// nothing is milled and no life is lost. The registration precondition is
// pinned by TestRadiationDrainNotQueuedWithoutCounters.
func TestRadiationDrainZeroCountersDoesNothing(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	radiationLibrary(t, e, 0, radiationLand(t), radiationSpell(t, "Untouched"))
	lifeBefore := e.G.Players[0].Life
	if got := e.G.Players[0].Counter("RAD"); got != 0 {
		t.Fatalf("precondition: RAD=%d, want 0", got)
	}

	e.G.Step = state.StepMain1
	e.finishEnteredStep()
	if len(e.pendingTriggers) != 0 || len(e.G.Stack) != 0 {
		t.Fatalf("zero-RAD player queued/pushed a drain: triggers=%+v stack=%v", e.pendingTriggers, e.G.Stack)
	}

	if got := len(e.G.Zone(state.ZLibrary, 0)); got != 2 {
		t.Fatalf("library=%d, want 2 (nothing milled)", got)
	}
	if got := e.G.Players[0].Life; got != lifeBefore {
		t.Fatalf("life=%d, want %d (nothing lost)", got, lifeBefore)
	}
}

// TestRadiationDrainIsOnTheStackBeforeResolving is the CR 728.1 response-window
// pin: the drain is a real triggered ability, so after the precombat main
// queues it and it is pushed, the counters and library are untouched until the
// stack object resolves.
func TestRadiationDrainIsOnTheStackBeforeResolving(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	radiationLibrary(t, e, 0, radiationSpell(t, "Mill Me"))
	seedPlayerCounter(t, e, 0, "RAD", 1)

	e.G.Step = state.StepMain1
	e.finishEnteredStep()
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("drain not on the stack: %v", e.G.Stack)
	}
	if got := e.G.Players[0].Counter("RAD"); got != 1 {
		t.Fatalf("RAD=%d before resolution, want 1 (the ability is response-able)", got)
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != 1 {
		t.Fatalf("library=%d before resolution, want 1 (nothing milled yet)", got)
	}

	e.resolveTop()

	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 1 {
		t.Fatalf("graveyard=%d after resolution, want 1", got)
	}
	if got := e.G.Players[0].Counter("RAD"); got != 0 {
		t.Fatalf("RAD=%d after resolution, want 0", got)
	}
}
