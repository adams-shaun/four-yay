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
		name         string
		srcs         []string // deck fixture cards besides the probe
		setup        func(t *testing.T, e *Engine) (probe state.ObjID)
		wantSelfOnly int  // selfOnly members the snapshot must hold
		wantOther    int  // non-selfOnly members the snapshot must hold
		wantTarget   bool // snapshot.validTarget precondition
		wantRefuse   bool
		wantCastable bool
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
			wantSelfOnly: 1, wantTarget: true, wantRefuse: true},
		{name: "target-conditional self static on the priced card: no floor",
			srcs: []string{floorRebukeSrc},
			setup: func(t *testing.T, e *Engine) state.ObjID {
				return floorPlace(t, e, floorRebukeSrc, state.ZHand)
			},
			wantSelfOnly: 1, wantTarget: true, wantRefuse: false},
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
