package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Legal-walk S1: a `ValidCard$ Card.Self` cost static sourced by another
// object cannot price the object under test, so it must not defeat
// offerFloorRefuses (rules/offer_floor_selfstatic.go).

const (
	// floorFreeGiantSrc reduces its OWN cost to nothing: castable from hand
	// with an empty pool, so the floor must not refuse it.
	floorFreeGiantSrc = "Name:Free Giant\nManaCost:5\nTypes:Creature Giant\nPT:5/5\n" +
		"S:Mode$ ReduceCost | ValidCard$ Card.Self | Type$ Spell | Amount$ 5 | EffectZone$ All | Description$ x\nOracle:x\n"
	// floorRebukeSrc has Luminous Rebuke's target-conditional self static.
	floorRebukeSrc = "Name:Floor Rebuke\nManaCost:3 W\nTypes:Instant\n" +
		"S:Mode$ ReduceCost | ValidCard$ Card.Self | Type$ Spell | Amount$ 3 | EffectZone$ All | ValidTarget$ Creature.tapped | Description$ x\n" +
		"A:SP$ Destroy | ValidTgts$ Creature | SpellDescription$ x\nOracle:x\n"
	// floorQualifiedSrc spells the self scope with a qualifier, so it is not
	// the exact `Card.Self` the gate short-circuits on.
	floorQualifiedSrc = "Name:Floor Qualified\nManaCost:5\nTypes:Creature Golem\nPT:5/5\n" +
		"S:Mode$ ReduceCost | ValidCard$ Card.Self+Creature | Type$ Spell | Amount$ 5 | EffectZone$ All | Description$ x\nOracle:x\n"
	floorProbeSrc = "Name:Floor Probe\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	// floorTargetReducerSrc is a GENERAL (non-self) reducer whose predicate
	// reads the chosen targets: validTarget is set and validTargetSelfOnly is
	// false, so the floor must keep the full composition for every card.
	floorTargetReducerSrc = "Name:Floor Target Reducer\nManaCost:2 U\nTypes:Creature Wizard\nPT:2/2\n" +
		"S:Mode$ ReduceCost | ValidCard$ Creature | Type$ Spell | Amount$ 1 | EffectZone$ Battlefield | ValidTarget$ Creature.tapped | Description$ x\nOracle:x\n"
	// floorMutateReduceSrc carries a self-only ReduceCost static AND the Mutate
	// keyword, so a test can stack it BENEATH a vanilla top card. The static's
	// Source is then the pile object; pricing any other card still sees a
	// self-only foreign member (the merged-pile edge row).
	floorMutateReduceSrc = "Name:Floor Mutate Reducer\nManaCost:3 G\nTypes:Creature Beast\nPT:3/3\n" +
		"K:Mutate:1 G\n" +
		"S:Mode$ ReduceCost | ValidCard$ Card.Self | Type$ Spell | Amount$ 3 | EffectZone$ All | Description$ x\nOracle:x\n"
	floorMergedTopSrc = "Name:Floor Merged Top\nManaCost:2 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
)

// floorPlace puts the seat-0 deck card named by src in zone z (a logged move).
func floorPlace(t *testing.T, e *Engine, src string, z state.Zone) state.ObjID {
	t.Helper()
	toMain1(t, e)
	name := card(t, src).Faces[0].Name
	for _, from := range []state.Zone{state.ZLibrary, state.ZHand, state.ZGraveyard, state.ZBattlefield} {
		for _, id := range e.G.Zone(from, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				if from != z {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: z})
					e.pending = nil
				}
				if got := e.G.Obj(id).Zone; got != z {
					t.Fatalf("precondition: %s is in %s, want %s", name, got, z)
				}
				return id
			}
		}
	}
	t.Fatalf("fixture card %q not found for seat 0", name)
	return 0
}

// floorProbe prices id (a plain spell) the way the walk's hand section does and
// returns the floor's verdict, the full offer's verdict and the snapshot.
func floorProbe(t *testing.T, e *Engine, id state.ObjID) (refuses, castable bool, statics costStaticViews) {
	t.Helper()
	if n := e.G.Players[0].Pool.Total(); n != 0 {
		t.Fatalf("precondition: pool holds %d mana, the floor reads only an empty pool", n)
	}
	w := &legalWalk{e: e, p: 0, costStatics: costStaticSource{e: e}, actionStatics: actionStaticSource{e: e}}
	statics = w.costStatics.get()
	base := e.castOfferBase(0, id)
	if base.Generic == 0 && base.Colored.Total() == 0 {
		t.Fatalf("precondition: %d has a free printed cost, the floor has nothing to refuse", id)
	}
	refuses = w.offerFloorRefuses(&statics, 0, id, &base, spellScope(""))
	castable = w.offerCastable(0, id, base, spellScope(""), false)
	return refuses, castable, statics
}

func floorSelfOnlyCount(statics costStaticViews) (selfOnly, other int) {
	for _, list := range [][]staticView{statics.raise, statics.reduce, statics.set} {
		for _, sv := range list {
			if sv.selfOnly {
				selfOnly++
			} else {
				other++
			}
		}
	}
	return
}

func TestOfferFloorSelfStatic(t *testing.T) {
	t.Parallel()
	type row struct {
		name               string
		srcs               []string // deck fixture cards besides the probe
		setup              func(t *testing.T, e *Engine) (probe state.ObjID)
		wantSelfOnly       int  // selfOnly members the snapshot must hold
		wantOther          int  // non-selfOnly members the snapshot must hold
		wantTarget         bool // snapshot.validTarget precondition
		wantTargetSelfOnly bool // snapshot.validTargetSelfOnly precondition
		wantRefuse         bool
		wantCastable       bool
	}
	probeInHand := func(t *testing.T, e *Engine) state.ObjID { return floorPlace(t, e, floorProbeSrc, state.ZHand) }
	rows := []row{
		{name: "self-static card itself in hand: composition runs and offers it",
			srcs: []string{floorFreeGiantSrc},
			setup: func(t *testing.T, e *Engine) state.ObjID {
				return floorPlace(t, e, floorFreeGiantSrc, state.ZHand)
			},
			wantSelfOnly: 1, wantRefuse: false, wantCastable: true},
		{name: "other card, self-static card in library: floor refuses",
			srcs: []string{floorFreeGiantSrc},
			setup: func(t *testing.T, e *Engine) state.ObjID {
				floorPlace(t, e, floorFreeGiantSrc, state.ZLibrary)
				return probeInHand(t, e)
			},
			wantSelfOnly: 1, wantRefuse: true},
		{name: "other card, self-static card in hand: floor refuses",
			srcs: []string{floorFreeGiantSrc},
			setup: func(t *testing.T, e *Engine) state.ObjID {
				floorPlace(t, e, floorFreeGiantSrc, state.ZHand)
				return probeInHand(t, e)
			},
			wantSelfOnly: 1, wantRefuse: true},
		{name: "other card, self-static card in graveyard: floor refuses",
			srcs: []string{floorFreeGiantSrc},
			setup: func(t *testing.T, e *Engine) state.ObjID {
				floorPlace(t, e, floorFreeGiantSrc, state.ZGraveyard)
				return probeInHand(t, e)
			},
			wantSelfOnly: 1, wantRefuse: true},
		{name: "other card, self-static card on the battlefield: floor refuses",
			srcs: []string{floorFreeGiantSrc},
			setup: func(t *testing.T, e *Engine) state.ObjID {
				floorPlace(t, e, floorFreeGiantSrc, state.ZBattlefield)
				return probeInHand(t, e)
			},
			wantSelfOnly: 1, wantRefuse: true},
		{name: "target-conditional self static (Rebuke) elsewhere: floor refuses",
			srcs: []string{floorRebukeSrc},
			setup: func(t *testing.T, e *Engine) state.ObjID {
				floorPlace(t, e, floorRebukeSrc, state.ZLibrary)
				return probeInHand(t, e)
			},
			wantSelfOnly: 1, wantTarget: true, wantTargetSelfOnly: true, wantRefuse: true},
		{name: "target-conditional self static on the priced card: no floor",
			srcs: []string{floorRebukeSrc},
			setup: func(t *testing.T, e *Engine) state.ObjID {
				return floorPlace(t, e, floorRebukeSrc, state.ZHand)
			},
			wantSelfOnly: 1, wantTarget: true, wantTargetSelfOnly: true, wantRefuse: false},
		{name: "self static plus a general reducer: no floor",
			srcs: []string{floorFreeGiantSrc, biomancersFamiliarSrc},
			setup: func(t *testing.T, e *Engine) state.ObjID {
				floorPlace(t, e, floorFreeGiantSrc, state.ZLibrary)
				floorPlace(t, e, biomancersFamiliarSrc, state.ZBattlefield)
				return probeInHand(t, e)
			},
			wantSelfOnly: 1, wantOther: 1, wantRefuse: false},
		{name: "Card.Self+qualifier spelling is not self-only: no floor",
			srcs: []string{floorQualifiedSrc},
			setup: func(t *testing.T, e *Engine) state.ObjID {
				floorPlace(t, e, floorQualifiedSrc, state.ZLibrary)
				return probeInHand(t, e)
			},
			wantOther: 1, wantRefuse: false},
		{name: "general target-conditional reducer elsewhere: no floor",
			srcs: []string{floorTargetReducerSrc},
			setup: func(t *testing.T, e *Engine) state.ObjID {
				floorPlace(t, e, floorTargetReducerSrc, state.ZBattlefield)
				return probeInHand(t, e)
			},
			wantOther: 1, wantTarget: true, wantTargetSelfOnly: false, wantRefuse: false},
		{name: "self static plus a general target reader: no floor",
			srcs: []string{floorFreeGiantSrc, floorTargetReducerSrc},
			setup: func(t *testing.T, e *Engine) state.ObjID {
				floorPlace(t, e, floorFreeGiantSrc, state.ZLibrary)
				floorPlace(t, e, floorTargetReducerSrc, state.ZBattlefield)
				return probeInHand(t, e)
			},
			wantSelfOnly: 1, wantOther: 1, wantTarget: true, wantTargetSelfOnly: false, wantRefuse: false},
		{name: "face-down self-static permanent has no printed statics: floor refuses",
			srcs: []string{floorFreeGiantSrc},
			setup: func(t *testing.T, e *Engine) state.ObjID {
				id := floorPlace(t, e, floorFreeGiantSrc, state.ZBattlefield)
				e.G.Obj(id).FaceDown = true
				return probeInHand(t, e)
			},
			wantRefuse: true},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			e, _, _ := newFixtureDeck(t, 9301, floorProbeSrc, r.srcs...)
			probe := r.setup(t, e)
			if o := e.G.Obj(probe); o == nil || o.Zone != state.ZHand {
				t.Fatalf("precondition: the priced card must be in hand: %+v", o)
			}
			refuses, castable, statics := floorProbe(t, e, probe)
			selfOnly, other := floorSelfOnlyCount(statics)
			if selfOnly != r.wantSelfOnly || other != r.wantOther || statics.validTarget != r.wantTarget {
				t.Fatalf("snapshot precondition: selfOnly=%d other=%d validTarget=%v, want %d %d %v",
					selfOnly, other, statics.validTarget, r.wantSelfOnly, r.wantOther, r.wantTarget)
			}
			if refuses != r.wantRefuse {
				t.Errorf("offerFloorRefuses = %v, want %v", refuses, r.wantRefuse)
			}
			if castable != r.wantCastable {
				t.Errorf("offerCastable = %v, want %v", castable, r.wantCastable)
			}
		})
	}
}

// The alternate-face probe never reaches the floor; with a foreign self static
// in the library it still answers by the full composition.
func TestOfferFloorSelfStaticAlternateFace(t *testing.T) {
	t.Parallel()
	e, _, swindle := newFixtureDeck(t, 9302, adventureFixtureSrc, floorFreeGiantSrc, adventureBearSrc)
	floorPlace(t, e, floorFreeGiantSrc, state.ZLibrary)
	floorPlace(t, e, adventureFixtureSrc, state.ZHand)
	floorPlace(t, e, adventureBearSrc, state.ZBattlefield) // Swipe needs a target
	w := &legalWalk{e: e, p: 0, costStatics: costStaticSource{e: e}, actionStatics: actionStaticSource{e: e}}
	if st := w.costStatics.get(); len(st.reduce) != 1 || !st.reduce[0].selfOnly {
		t.Fatalf("precondition: want exactly the foreign self-only static, got %+v", st.reduce)
	}
	offered := func() bool {
		for _, o := range e.legalActions(0) {
			if o.Obj == swindle && o.Mode == "adventure_alt" {
				return true
			}
		}
		return false
	}
	if offered() {
		t.Fatal("Swipe offered with an empty pool")
	}
	addMana(t, e, 0, "1R")
	if !offered() {
		t.Fatal("Swipe not offered with {1}{R} available")
	}
}

// A self-only cost static printed on a card BENEATH a Merged pile (CR
// 702.140d) is collected with the PILE object as its Source. Pricing any
// other card still sees one self-only foreign member, so the floor must
// refuse it exactly as it does a self-static in any other zone. This is the
// spec's merged-pile edge row; the face-down row is in the table above.
func TestOfferFloorSelfStaticMergedPile(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 9303, floorProbeSrc, floorMutateReduceSrc, floorMergedTopSrc)
	reducer := floorPlace(t, e, floorMutateReduceSrc, state.ZHand)
	top := putToken(t, e, 0, floorMergedTopSrc, state.ZBattlefield)
	addMana(t, e, 0, "GG") // the reducer's Mutate cost {1}{G}: two green pays it in full

	// CR 702.140a: mutate requires a non-Human creature; the vanilla Bear is
	// the merge target. Place the reducer UNDER the Bear, so the pile's top
	// card stays the Bear and the static is only reachable through the pile
	// view.
	mutateCastOnto(t, e, mutatedCastOption(t, e, reducer), top, false)
	mutateDrain(t, e, 40)
	// The floor reads an empty pool; clear whatever the mutate payment left
	// (a logged ManaClear, so a replayCheck would rebuild the same state).
	e.emit(events.Event{Kind: events.ManaClear, Player: 0})
	e.pending = nil
	pile := e.G.Obj(top)
	if pile == nil || pile.Face() == nil || pile.Face().Name != card(t, floorMergedTopSrc).Faces[0].Name {
		t.Fatalf("precondition: pile top = %+v, want the vanilla top card", pile)
	}
	if len(pile.MergedCards) != 1 || pile.MergedCards[0].Obj != reducer {
		t.Fatalf("precondition: reducer not merged under the top: %+v", pile.MergedCards)
	}

	probe := floorPlace(t, e, floorProbeSrc, state.ZHand)
	refuses, castable, statics := floorProbe(t, e, probe)
	selfOnly, other := floorSelfOnlyCount(statics)
	if selfOnly != 1 || other != 0 {
		t.Fatalf("precondition: want exactly one selfOnly member, got selfOnly=%d other=%d (%+v)",
			selfOnly, other, statics.reduce)
	}
	if statics.reduce[0].Source != top {
		t.Fatalf("precondition: merged static Source = %d, want the pile %d", statics.reduce[0].Source, top)
	}
	if !refuses {
		t.Errorf("offerFloorRefuses = false, want true (a self-only static on the pile must be inert)")
	}
	if castable {
		t.Errorf("offerCastable = true, want false with an empty pool and no reducer for the probe")
	}
}
