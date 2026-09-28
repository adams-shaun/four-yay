package rules

// K:Ascend (CR 702.131, task ascend1): the city's blessing. The grant has two
// halves -- a permanent with the keyword grants continuously while its
// controller controls ten or more permanents (rules/ascend.go's emit-side
// scan), and an instant/sorcery with the keyword grants AS IT RESOLVES,
// before its own body and condition checks read the latch (resolveTop's
// spell branch). The latch is one-way. The pins below drive the grant through
// the real emitted-event paths (a library->battlefield MoveZone for the
// permanent half, resolveTop for the spell half) on real corpus carriers.

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// enterOnBattlefield puts a real corpus card on p's battlefield through the
// REAL MoveZone event path (the fixture placements onBoard/onBoardCard are
// eventless by design), so Engine.emit's post-fold Ascend scan sees the entry
// exactly as a cast would deliver it.
func enterOnBattlefield(t testing.TB, e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, p)
	e.G.SetZone(state.ZLibrary, p, append(e.G.Zone(state.ZLibrary, p), o.ID))
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	return o.ID
}

// vanillaBears places n distinct 2/2 Bears (no keywords) under p's control,
// eventlessly, as Ascend-count filler.
func vanillaBears(t testing.TB, e *Engine, p state.PlayerID, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		onBoard(t, e, p, fmt.Sprintf("Name:Filler Bear %d\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", i))
	}
}

// TestAscendPermanentGrantsTheBlessingAtTenPermanents pins the permanent
// half on a real corpus carrier (Dusk Charger): the tenth permanent entering
// under an Ascend controller latches the blessing; an opponent with the same
// board shape but no Ascend permanent never gets it; and the latch survives
// the Ascend permanent's own death (CR 702.131a, "for the rest of the game").
func TestAscendPermanentGrantsTheBlessingAtTenPermanents(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	vanillaBears(t, e, 0, 9)
	dusk := enterOnBattlefield(t, e, 0, corpusCard(t, "Dusk Charger"))
	if !e.G.Players[0].Blessing {
		t.Fatalf("ten permanents with an Ascend carrier did not grant the blessing")
	}
	if e.G.Players[1].Blessing {
		t.Fatalf("seat 1 holds the blessing with no Ascend permanent")
	}
	// The latch never clears: the Ascend permanent dies, the blessing stays.
	e.emit(events.Event{Kind: events.MoveZone, Obj: dusk, From: state.ZBattlefield, To: state.ZGraveyard})
	if !e.G.Players[0].Blessing {
		t.Fatalf("the blessing cleared when the Ascend permanent left the battlefield")
	}
}

// TestAscendNinePermanentsNeverGrants pins the gate's count side: nine
// permanents under an Ascend controller hold no blessing.
func TestAscendNinePermanentsNeverGrants(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	vanillaBears(t, e, 0, 8)
	enterOnBattlefield(t, e, 0, corpusCard(t, "Dusk Charger"))
	if e.G.Players[0].Blessing {
		t.Fatalf("nine permanents granted the blessing")
	}
}

// TestDetectiveOfTheMonthCantBlockByNeedsTheBlessing is the report's named
// pin, driven through the real compiled card text: with the blessing,
// Detective of the Month's Condition$ Blessing CantBlockBy makes a Detective
// unblockable; at nine permanents the gate denies and the Detective blocks
// normally.
func TestDetectiveOfTheMonthCantBlockByNeedsTheBlessing(t *testing.T) {
	t.Parallel()
	t.Run("blessed/unblockable", func(t *testing.T) {
		e := layerEngine(t)
		vanillaBears(t, e, 0, 9)
		enterOnBattlefield(t, e, 0, corpusCard(t, "Detective of the Month"))
		attacker := onBoard(t, e, 0, "Name:Beat Cop\nManaCost:1 W\nTypes:Creature Detective\nPT:2/2\nOracle:x\n")
		blocker := onBoard(t, e, 1, "Name:Wall Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		if !e.blockRestricted(blocker, attacker) {
			t.Fatalf("blessed Detective still blockable")
		}
	})
	t.Run("nine permanents/blockable", func(t *testing.T) {
		e := layerEngine(t)
		vanillaBears(t, e, 0, 8)
		enterOnBattlefield(t, e, 0, corpusCard(t, "Detective of the Month"))
		attacker := onBoard(t, e, 0, "Name:Beat Cop\nManaCost:1 W\nTypes:Creature Detective\nPT:2/2\nOracle:x\n")
		blocker := onBoard(t, e, 1, "Name:Wall Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		if e.blockRestricted(blocker, attacker) {
			t.Fatalf("unblessed Detective unblockable (Condition$ Blessing not gated)")
		}
	})
}

// TestAscendSpellResolutionGrantsTheBlessing pins the spell half on a real
// corpus instant/sorcery (Golden Demise): the blessing is granted BEFORE the
// resolving spell's own body runs, and only when the controller controls ten
// or more permanents. The spell's own Count$Blessing-gated branch is a
// separate (unimplemented) head -- this pin asserts the state change only.
func TestAscendSpellResolutionGrantsTheBlessing(t *testing.T) {
	t.Parallel()
	t.Run("ten permanents", func(t *testing.T) {
		e := layerEngine(t)
		vanillaBears(t, e, 0, 10)
		spell := e.G.AddObject(corpusCard(t, "Golden Demise"), 0)
		spell.Zone = state.ZStack
		e.G.SetZone(state.ZStack, 0, []state.ObjID{spell.ID})
		e.resolveTop()
		if !e.G.Players[0].Blessing {
			t.Fatalf("resolving an Ascend spell with ten permanents did not grant the blessing")
		}
		if len(e.G.Stack) != 0 {
			t.Fatalf("resolved spell stayed on the stack (%d objects)", len(e.G.Stack))
		}
	})
	t.Run("nine permanents", func(t *testing.T) {
		e := layerEngine(t)
		vanillaBears(t, e, 0, 9)
		spell := e.G.AddObject(corpusCard(t, "Golden Demise"), 0)
		spell.Zone = state.ZStack
		e.G.SetZone(state.ZStack, 0, []state.ObjID{spell.ID})
		e.resolveTop()
		if e.G.Players[0].Blessing {
			t.Fatalf("resolving an Ascend spell with nine permanents granted the blessing")
		}
	})
}

// TestAscendContinuousBuffFollowsTheBlessing pins the Condition$ Blessing
// Continuous read on the real carrier (Dusk Charger's +2/+2): buffed only
// while the latch holds, re-derived across the emitted BlessingChange.
func TestAscendContinuousBuffFollowsTheBlessing(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	dusk := onBoardCard(t, e, 0, corpusCard(t, "Dusk Charger"))
	if got := e.Power(dusk); got != 3 {
		t.Fatalf("unblessed Dusk Charger power = %d, want 3", got)
	}
	e.emit(events.Event{Kind: events.BlessingChange, Player: 0})
	if got := e.Power(dusk); got != 5 {
		t.Fatalf("blessed Dusk Charger power = %d, want 5", got)
	}
}

// TestCantBlockByConditionGateThresholdNotBlessingSpecific proves the new
// per-static condition gate in blockRestricted is condition-driven, not
// hardcoded to Blessing: Cephalid Inkmage's real CantBlockBy (Condition$
// Threshold) only restricts once its controller's graveyard holds seven cards.
// TestBlessingReadersUseThePerPlayerLatch pins the two READ paths on real
// corpus cards: Twilight Prophet's upkeep trigger and Radiant Destiny's
// vigilance static. Both must be absent before the latch and present after it;
// the setup checks that each source and affected creature is actually on the
// battlefield so neither assertion can pass vacuously.
func TestBlessingReadersUseThePerPlayerLatch(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	prophet := onBoardCard(t, e, 0, corpusCard(t, "Twilight Prophet"))
	prophetFace := e.G.Obj(prophet).Face()
	if e.G.Obj(prophet).Zone != state.ZBattlefield || prophetFace == nil {
		t.Fatal("Twilight Prophet was not placed on the battlefield")
	}
	var blessingTrigger *cards.Trigger
	for i := range prophetFace.Triggers {
		tr := &prophetFace.Triggers[i]
		if tr.Params["Blessing"] == "True" {
			blessingTrigger = tr
			break
		}
	}
	if blessingTrigger == nil {
		t.Fatal("Twilight Prophet lost its Blessing$ True trigger parameter")
	}
	if e.triggerConditionHolds(*blessingTrigger, prophet) {
		t.Fatal("Twilight Prophet blessing trigger fired while unblessed")
	}

	destiny := onBoardCard(t, e, 0, corpusCard(t, "Radiant Destiny"))
	// Radiant Destiny's real static is scoped to its chosen creature type;
	// seed the completed ETB choice so this test isolates Blessing$.
	e.G.Obj(destiny).ChosenType = "Bear"
	e.staticEpoch = -1
	bear := onBoard(t, e, 0, "Name:Blessing Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if e.G.Obj(destiny).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("Radiant Destiny or its affected creature was not placed on the battlefield")
	}
	if e.HasKeyword(bear, "Vigilance") {
		t.Fatal("Radiant Destiny granted vigilance while unblessed")
	}
	e.emit(events.Event{Kind: events.BlessingChange, Player: 0})
	if !e.G.Players[0].Blessing {
		t.Fatal("BlessingChange did not establish the blessing precondition")
	}
	if !e.triggerConditionHolds(*blessingTrigger, prophet) {
		t.Fatal("Twilight Prophet blessing trigger did not fire while blessed")
	}
	if !e.HasKeyword(bear, "Vigilance") {
		t.Fatal("Radiant Destiny did not grant vigilance while blessed")
	}
}

func TestCantBlockByConditionGateThresholdNotBlessingSpecific(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	ink := onBoardCard(t, e, 0, corpusCard(t, "Cephalid Inkmage"))
	blocker := onBoard(t, e, 1, "Name:Wall Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if e.blockRestricted(blocker, ink) {
		t.Fatalf("Inkmage unblockable below threshold")
	}
	for i := 0; i < 7; i++ {
		addToGraveyardType(t, e, 0, "Sorcery")
	}
	if !e.blockRestricted(blocker, ink) {
		t.Fatalf("Inkmage still blockable at threshold")
	}
}

// TestAscendGrantedKeywordStillGrantsTheBlessing pins the exactness of the
// scan's pre-filter (baseMayHaveKeyword + activeGrantsKeyword): an object
// whose printed face has no Ascend is skipped only while no active layer-6
// effect grants Ascend, so a lord's AddKeyword$ Ascend still makes its
// controller's tenth permanent latch the blessing, while an opponent with
// the same board shape and only printed-less creatures does not.
func TestAscendGrantedKeywordStillGrantsTheBlessing(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoard(t, e, 0, "Name:Ascend Lord\nManaCost:2\nTypes:Creature Lord\nPT:1/1\nS:Mode$ Continuous | Affected$ Creature.YouCtrl | AddKeyword$ Ascend | Description$ Creatures you control have ascend.\nOracle:x\n")
	vanillaBears(t, e, 0, 8)
	vanillaBears(t, e, 1, 9)
	enterOnBattlefield(t, e, 1, corpusCard(t, "Grizzly Bears"))
	if e.G.Players[1].Blessing {
		t.Fatalf("seat 1 holds the blessing with no Ascend permanent (the lord grants only its controller's creatures)")
	}
	enterOnBattlefield(t, e, 0, corpusCard(t, "Grizzly Bears"))
	if !e.G.Players[0].Blessing {
		t.Fatalf("ten permanents under a lord granting Ascend did not latch the blessing")
	}
}

// TestAscendScanSkipsOnlyObjectsThatCannotHaveIt pins baseMayHaveKeyword's
// two answers on real objects: a printed Ascend carrier and a vanilla
// creature.
func TestAscendScanSkipsOnlyObjectsThatCannotHaveIt(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	dusk := onBoardCard(t, e, 0, corpusCard(t, "Dusk Charger"))
	bear := onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	if !baseMayHaveKeyword(e.G.Obj(dusk), "Ascend") {
		t.Fatalf("Dusk Charger's printed Ascend was pre-filtered away")
	}
	if baseMayHaveKeyword(e.G.Obj(bear), "Ascend") {
		t.Fatalf("a vanilla Grizzly Bears reads as a possible Ascend carrier")
	}
	if e.activeGrantsKeyword("Ascend") {
		t.Fatalf("an active Ascend grant with no granting static on the board")
	}
}
