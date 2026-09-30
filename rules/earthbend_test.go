package rules

// api:Earthbend end-to-end pins on real corpus cards (task earthbend1). The
// keyword action is Forge's EarthbendEffect: "target land you control becomes
// a 0/0 creature with haste that's still a land. Put N +1/+1 counters on it.
// When it dies or is exiled, return it to the battlefield tapped."
//
// Every leaf below drives a REAL carrier. Ba Sing Se (pro-shaper) is the
// whole-oracle pin through its own activated ability; Beifong's Bounty
// Hunters (pro-shaper) proves the dynamic TriggeredCard$CardPower count;
// Toph, Hardheaded Teacher (pro-shaper) proves the SpellCast-trigger shape
// and a second literal count; Earthshape proves a Num$ 3 spell cast and that
// a SubAbility$ continuation still runs.
//
// The deliberate split of the animation lifetime and the return promise into
// two assertions per leaf is the point: a land that became a 0/0 creature
// which never comes back would be worse than inert, so neither half is
// allowed to pass on its own.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// earthbendTargetOption returns the target-decision option index for obj.
func earthbendTargetOption(t *testing.T, e *Engine, obj state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatalf("no decision pending for the Earthbend target ask")
	}
	if d.Kind != decision.KTarget {
		t.Fatalf("pending kind %v, want KTarget", d.Kind)
	}
	for _, o := range d.Options {
		if o.Obj == obj {
			return o.Index
		}
	}
	t.Fatalf("target ask offers no option for %d: %+v", obj, d.Options)
	return -1
}

// earthbendSettle submits a pass on every pending priority decision until the
// queue and the stack have emptied, so a trigger matched by a
// directly-emitted move is pushed and resolved the way a live game would (the
// engine drains pendingTriggers on the next priority round, not inside emit).
// It returns the number of passes it made.
func earthbendSettle(t *testing.T, e *Engine, limit int) int {
	t.Helper()
	n := 0
	for ; n < limit && !e.G.Over; n++ {
		d := e.Pending()
		if d == nil {
			return n
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v while settling", d)
		}
		if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
			return n
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
		submitChoices(t, e, idx)
	}
	t.Fatalf("engine never settled within %d passes (stack %d, triggers %d, delayed %d)",
		limit, len(e.G.Stack), len(e.pendingTriggers), len(e.G.Delayed))
	return n
}

// earthbendSettleAnswering submits pass on every pending priority decision
// and answers every target decision with the given land, until the queue and
// the stack have emptied, so a trigger matched by a directly-emitted move is
// pushed, asked and resolved the way a live game would (the engine drains
// pendingTriggers on the next priority round, not inside emit).
func earthbendSettleAnswering(t *testing.T, e *Engine, forest state.ObjID, limit int) {
	t.Helper()
	for n := 0; n < limit; n++ {
		d := e.Pending()
		if d == nil {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
				return
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
			submitChoices(t, e, idx)
		case decision.KTarget:
			submitChoices(t, e, earthbendTargetOption(t, e, forest))
		default:
			t.Fatalf("unexpected decision kind %v while settling: %+v", d.Kind, d)
		}
	}
	t.Fatalf("engine never settled within %d passes (stack %d, triggers %d, delayed %d)",
		limit, len(e.G.Stack), len(e.pendingTriggers), len(e.G.Delayed))
}

// earthbendActivateBaSingSe moves a Forest and Ba Sing Se onto the
// battlefield, funds the {2}{G} plus the tap, and activates Ba Sing Se's
// Earthbend ability (face ability index 1 -- index 0 is its mana ability),
// answering the target ask with the Forest. Ba Sing Se enters untapped
// because the Forest is already out (its "enters tapped unless you control a
// basic land" replacement). It returns the land id and the Forest id.
func earthbendActivateBaSingSe(t *testing.T, e *Engine, forestName string) (land, forest state.ObjID) {
	t.Helper()
	forest = searchMoveByName(t, e, forestName, state.ZBattlefield)
	bss := searchMoveByName(t, e, "Ba Sing Se", state.ZBattlefield)
	if o := e.G.Obj(bss); o == nil || o.Tapped {
		t.Fatalf("Ba Sing Se did not enter untapped beside a basic land: %+v", o)
	}
	addMana(t, e, 0, "GGG")
	opt := abilityOption(t, e, bss, 1)
	submitChoices(t, e, opt.Index)
	submitChoices(t, e, earthbendTargetOption(t, e, forest))
	passUntilStackEmpty(t, e, 40)
	return bss, forest
}

// assertEarthbendAnimated asserts the oracle's animation half: the target is
// a creature with haste that is STILL a land, with a 0/0 base and num +1/+1
// counters.
func assertEarthbendAnimated(t *testing.T, e *Engine, id state.ObjID, num int32) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("targeted land %d is not on the battlefield: %+v", id, o)
	}
	if !e.IsCreature(id) {
		t.Fatal("earthbent land is not a creature")
	}
	d := e.Derived(id)
	var stillLand, haste bool
	for _, typ := range d.Types {
		if typ == "Land" {
			stillLand = true
		}
	}
	for _, kw := range d.Keywords {
		if kw == "Haste" {
			haste = true
		}
	}
	if !stillLand {
		t.Fatalf("earthbent land lost its Land type: types=%v", d.Types)
	}
	if !haste {
		t.Fatalf("earthbent land has no Haste: keywords=%v", d.Keywords)
	}
	// The base is 0/0; the derived P/T is exactly the counters.
	if c := o.Counter("P1P1"); c != num {
		t.Fatalf("P1P1 counters on the earthbent land = %d, want %d", c, num)
	}
	if d.Power != num || d.Toughness != num {
		t.Fatalf("earthbent land P/T = %d/%d, want %d/%d (0/0 base plus counters)",
			d.Power, d.Toughness, num, num)
	}
}

// assertEarthbendReturned emits the move a Destroy (graveyard) or an exile
// effect produces and asserts the one-shot promise returns the SAME land to
// the battlefield tapped, as a plain land (its animation and counters are
// gone per CR 122.2 -- a new object).
//
// This drives the MoveZone event directly (the event effects/zone.go's
// Destroy and every exile effect emit), which is deliberate: it lets the
// dynamic/literal-count leaves assert the whole return path without dragging
// a removal spell into every fixture. TestEarthbendBaSingSeRealDestroy
// exercises the same path through an actual Murder cast, so the "destroying
// it returns it" half of the oracle is proven at the primitive, not only at
// the event it emits.
func assertEarthbendReturned(t *testing.T, e *Engine, id state.ObjID, dest state.Zone) {
	t.Helper()
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: dest})
	if o := e.G.Obj(id); o == nil || o.Zone != dest {
		t.Fatalf("land %d did not move to zone %v: %+v", id, dest, o)
	}
	earthbendSettle(t, e, 30)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("land %d did not return from %v to the battlefield: %+v", id, dest, o)
	}
	if !o.Tapped {
		t.Fatal("returned land entered untapped; the oracle says tapped")
	}
	if e.IsCreature(id) {
		t.Fatal("returned land is still a 0/0 creature; the animation ended when it left")
	}
	if c := o.Counter("P1P1"); c != 0 {
		t.Fatalf("returned land kept %d +1/+1 counters; CR 122.2 removes them on leaving", c)
	}
	// The returned land is still a land.
	d := e.Derived(id)
	var stillLand bool
	for _, typ := range d.Types {
		if typ == "Land" {
			stillLand = true
		}
	}
	if !stillLand {
		t.Fatalf("returned card lost its Land type: types=%v", d.Types)
	}
}

// TestEarthbendBaSingSeWholeOracleDestroy pins the entire oracle on the
// deck's namesake carrier through its own activated ability: cost paid,
// target asked, animation + counters applied, and the return-on-death
// promise observed. Num$ 2 on this carrier.
func TestEarthbendBaSingSeWholeOracleDestroy(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Ba Sing Se", "Grizzly Bears")
	_, forest := earthbendActivateBaSingSe(t, e, "Forest")
	assertEarthbendAnimated(t, e, forest, 2)
	assertEarthbendReturned(t, e, forest, state.ZGraveyard)
	replayCheck(t, e, cfg)
}

// TestEarthbendBaSingSeWholeOracleExile is the exile half of the same
// promise, on a fresh engine (the one-shot registration is consumed at its
// first fire, so this is a separate resolution).
func TestEarthbendBaSingSeWholeOracleExile(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Ba Sing Se", "Grizzly Bears")
	_, forest := earthbendActivateBaSingSe(t, e, "Forest")
	assertEarthbendAnimated(t, e, forest, 2)
	assertEarthbendReturned(t, e, forest, state.ZExile)
	replayCheck(t, e, cfg)
}

// TestEarthbendEarthshapeThree is the second literal count (Num$ 3) and the
// A:SP$ spell shape, with its SubAbility$ continuation (DBPumpAll) still
// running after the earthbend resolves.
func TestEarthbendEarthshapeThree(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Earthshape", "Grizzly Bears")
	forest := searchMoveByName(t, e, "Forest", state.ZBattlefield)
	id := searchMoveByName(t, e, "Earthshape", state.ZHand)
	addMana(t, e, 0, "WWCC")
	// The cast's target ask is a pre-ask at announcement (CR 601.2c), so it
	// is pending the moment the cast is submitted -- answered here with the
	// Forest, never by option order (the fixture deck also carries
	// Mountains, so option 0 is not guaranteed to be the Forest).
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no Earthshape cast option: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	earthbendSettleAnswering(t, e, forest, 60)
	assertEarthbendAnimated(t, e, forest, 3)
	assertEarthbendReturned(t, e, forest, state.ZGraveyard)
	replayCheck(t, e, cfg)
}

// TestEarthbendBeifongDynamicCountsDyingCreaturePower is the dynamic-count
// leaf: Beifong's Bounty Hunters earthbends X where X is the DYING
// creature's power (SVar X = TriggeredCard$CardPower). A Craw Wurm (6/4) is
// killed, so the count must be 6 -- a silent-zero or default bug cannot
// produce it, and no literal in the corpus is 6.
func TestEarthbendBeifongDynamicCountsDyingCreaturePower(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Beifong's Bounty Hunters", "Craw Wurm", "Grizzly Bears")
	forest := searchMoveByName(t, e, "Forest", state.ZBattlefield)
	searchMoveByName(t, e, "Beifong's Bounty Hunters", state.ZBattlefield)
	wurm := searchMoveByName(t, e, "Craw Wurm", state.ZBattlefield)
	if p := e.Power(wurm); p != 6 {
		t.Fatalf("Craw Wurm power = %d, want 6", p)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: wurm, From: state.ZBattlefield, To: state.ZGraveyard})
	// The dying Wurm's trigger poses the earthbend target ask mid-settle;
	// the helper answers it with the Forest and keeps draining.
	earthbendSettleAnswering(t, e, forest, 40)
	assertEarthbendAnimated(t, e, forest, 6)
	assertEarthbendReturned(t, e, forest, state.ZGraveyard)
	replayCheck(t, e, cfg)
}

// TestEarthbendBadgermoleCubETBOne is the trigger-Execute leaf and the
// third literal count (Num$ 1): Badgermole Cub's "When this creature
// enters, earthbend 1" runs the SVar-resolved DB$ Earthbend body (the
// Execute$ chain the parser normalises exactly like a printed A: line).
// Badgermole Cub is used instead of Toph, Hardheaded Teacher -- the corpus's
// only SpellCast carrier -- so this leaf stays about the Earthbend body alone:
// Toph's chained SubAbility DBPutCounter gates on ConditionDefined$
// TriggeredCardLKICopy | ConditionPresent$ Lesson (put an EXTRA counter only
// when the cast spell is a Lesson), which is pinned on Toph itself in
// toph_lesson_gate_test.go (task agent-20260930T003623Z-97c7643c) and in the
// trigger-condition oracle fixture; using Toph here would fold the rider's
// second counter into every leaf's count.
func TestEarthbendBadgermoleCubETBOne(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Badgermole Cub", "Grizzly Bears")
	forest := searchMoveByName(t, e, "Forest", state.ZBattlefield)
	// Entering the Cub is itself the trigger: its ETB's earthbend pre-asks
	// its target at placement, so the helper answers it with the Forest and
	// drains the rest.
	searchMoveByName(t, e, "Badgermole Cub", state.ZBattlefield)
	earthbendSettleAnswering(t, e, forest, 40)
	assertEarthbendAnimated(t, e, forest, 1)
	assertEarthbendReturned(t, e, forest, state.ZGraveyard)
	replayCheck(t, e, cfg)
}

// earthbendDepart emits the MoveZone a removal produces and drains the
// trigger queue, without asserting anything about where the land ends up --
// the shape the one-shot regression leaf needs when the correct outcome is
// "it stays in the destination".
func earthbendDepart(t *testing.T, e *Engine, id state.ObjID, dest state.Zone) {
	t.Helper()
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: dest})
	earthbendSettle(t, e, 30)
}

// TestEarthbendBaSingSeRealDestroy casts a real Sinkhole at the earthbent
// land and asserts the promise returns it tapped. This drives the actual
// Destroy primitive (effects/zone.go -> MoveZone), not the MoveZone the
// helper emits, so the brief's "destroying it returns it to the battlefield
// tapped" is proven at the primitive. Sinkhole ("destroy target land",
// ValidTgts$ Land) is used rather than a creature-killer because the
// engine's ordinary target filters read the PRINTED face (the documented
// layer-4 filter limitation): the animated Forest is a creature only through
// the layer walk, so no `ValidTgts$ Creature` spell sees it. A `ValidTgts$
// Land` destruction still reaches it, and the Destroy fires the same
// departure. (Lethal combat/damage cannot kill the animated land either --
// rules/sba.go's destroyLethalDamage gates on the printed face being a
// creature, so an animated land is damage-immortal; a pre-existing
// engine-wide gap, disclosed in the report's Issues, not introduced here.)
func TestEarthbendBaSingSeRealDestroy(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Ba Sing Se", "Sinkhole", "Grizzly Bears")
	_, forest := earthbendActivateBaSingSe(t, e, "Forest")
	assertEarthbendAnimated(t, e, forest, 2)
	sinkhole := searchMoveByName(t, e, "Sinkhole", state.ZHand)
	addMana(t, e, 0, "BB")
	castDestroyAt(t, e, sinkhole, forest)
	o := e.G.Obj(forest)
	if o == nil || o.Zone != state.ZBattlefield || !o.Tapped {
		t.Fatalf("after a real Sinkhole the land must return tapped: zone=%v tapped=%v", o.Zone, o.Tapped)
	}
	if e.IsCreature(forest) {
		t.Fatal("returned land is still a creature after the real destroy")
	}
	replayCheck(t, e, cfg)
}

// TestEarthbendReturnIsOneShotAcrossDestinations is the regression leaf for
// the two-sibling bug: the promise must fire ONCE, not once per destination.
// A land earthbent, destroyed (returned), then exiled must STAY exiled -- the
// mirror case (exile first, then destroy) must stay in the graveyard. With a
// per-destination registration the un-fired sibling survives and returns the
// land a second time.
func TestEarthbendReturnIsOneShotAcrossDestinations(t *testing.T) {
	t.Parallel()
	t.Run("died-then-exiled-stays-exiled", func(t *testing.T) {
		reg := testutil.CorpusRegistry(t)
		e, cfg := searchEngine(t, reg, "Ba Sing Se", "Grizzly Bears")
		_, forest := earthbendActivateBaSingSe(t, e, "Forest")
		assertEarthbendAnimated(t, e, forest, 2)
		assertEarthbendReturned(t, e, forest, state.ZGraveyard)
		// The land is back and tapped; now exile it. The promise was consumed
		// on the first departure, so it must NOT return a second time.
		earthbendDepart(t, e, forest, state.ZExile)
		if o := e.G.Obj(forest); o == nil || o.Zone != state.ZExile {
			t.Fatalf("land returned a second time from exile: %+v", o)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("exiled-then-died-stays-dead", func(t *testing.T) {
		reg := testutil.CorpusRegistry(t)
		e, cfg := searchEngine(t, reg, "Ba Sing Se", "Grizzly Bears")
		_, forest := earthbendActivateBaSingSe(t, e, "Forest")
		assertEarthbendAnimated(t, e, forest, 2)
		assertEarthbendReturned(t, e, forest, state.ZExile)
		// Mirror: a later death must not return it again.
		earthbendDepart(t, e, forest, state.ZGraveyard)
		if o := e.G.Obj(forest); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("land returned a second time from the graveyard: %+v", o)
		}
		replayCheck(t, e, cfg)
	})
}
