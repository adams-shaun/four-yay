package rules

// The Rakdos-params brief, gap 12: AddKeyword$ Ward:<non-mana cost> GRANTED
// by a Continuous static (Hexing Squelcher: "Other creatures you control
// have 'Ward—Pay 2 life.'"). A printed K:Ward expands at compile time into a
// face trigger and already works; the granted one existed only in the layer
// system and never fired. The walk now synthesizes the same BecomesTarget
// trigger from the derived keyword (deduped against the face's own printed
// ward lines), the drain pushes a KeywordTriggerPush whose __kwWard: payload
// events.Apply rebuilds (replay-safe), and the ward payment charges 2 life
// through the ordinary ward machinery. CR 702.21a scoping applies to the
// granted copy exactly as to the printed one: a ward never triggers for its
// own controller's spell, and the grant itself (Affects$ Creature.Other+
// YouCtrl) never crosses to another player's creature.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// hexingEngine: Hexing Squelcher and a bare Bear under seat 0, an opposing
// bolt in seat 1's hand.
func hexingEngine(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusAlternativeCard(t, "Hexing Squelcher"))
	for _, o := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(o).Face().Name == "Hexing Squelcher" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o, From: state.ZHand, To: state.ZBattlefield})
			break
		}
	}
	o := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	bolt := e.G.AddObject(card(t, "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"), 1)
	e.G.SetZone(state.ZHand, 1, append(e.G.Zone(state.ZHand, 1), bolt.ID))
	// Any event posts the first priority decision; handEngine's genesis ends
	// with none pending.
	addMana(t, e, 1, "R")
	return e, o.ID
}

// opponentBoltAt passes seat 0's priority, then has seat 1 cast the bolt at
// the target.
func opponentBoltAt(t *testing.T, e *Engine, target state.ObjID) {
	t.Helper()
	bolt := e.G.Zone(state.ZHand, 1)[0]
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "pass" {
			submitChoices(t, e, o.Index)
			break
		}
	}
	d = e.Pending()
	opt := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bolt {
			opt = o.Index
		}
	}
	if opt < 0 {
		t.Fatalf("bolt not offered to seat 1: %+v", d.Options)
	}
	submitChoices(t, e, opt)
	dt := e.Pending()
	if dt == nil || dt.Kind != decision.KTarget {
		t.Fatalf("target ask %+v", dt)
	}
	idx := -1
	for _, o := range dt.Options {
		if o.Obj == target {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("bolt target options missing %d: %+v", target, dt.Options)
	}
	submitChoices(t, e, idx)
	passPriorityTimes(t, e, 2)
}

func TestGrantedWardPayLifeAsksAndPays(t *testing.T) {
	t.Parallel()
	e, bear := hexingEngine(t)
	e.G.Players[1].Life = 12
	opponentBoltAt(t, e, bear)
	// The granted ward fired and its ability sits on the stack; after the
	// response window it resolves into the pay-or-decline ask, answered by
	// the targeting spell's controller (seat 1).
	dw := e.Pending()
	if dw == nil || dw.Kind != decision.KModes || dw.Player != 1 {
		t.Fatalf("no ward pay ask: %+v", dw)
	}
	submitChoices(t, e, dw.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if life := e.G.Players[1].Life; life != 10 {
		t.Fatalf("seat 1 life=%d, want 10 (the ward's PayLife<2> paid)", life)
	}
	if e.G.Obj(bear).Zone != state.ZGraveyard {
		t.Fatalf("bear zone=%s, want graveyard (the bolt resolved after the ward was paid)", e.G.Obj(bear).Zone)
	}
}

func TestGrantedWardDeclineCountersTheSpell(t *testing.T) {
	t.Parallel()
	e, bear := hexingEngine(t)
	e.G.Players[1].Life = 12
	opponentBoltAt(t, e, bear)
	passPriorityTimes(t, e, 2)
	dw := e.Pending()
	if dw == nil || dw.Kind != decision.KModes {
		t.Fatalf("no ward pay ask: %+v", dw)
	}
	submitChoices(t, e, dw.Options[len(dw.Options)-1].Index)
	passUntilStackEmpty(t, e, 20)
	if life := e.G.Players[1].Life; life != 12 {
		t.Fatalf("life=%d, want 12 (declined)", life)
	}
	if e.G.Obj(bear).Zone != state.ZBattlefield || e.G.Obj(bear).Damage != 0 {
		t.Fatalf("bear damage=%d zone=%s, want untouched", e.G.Obj(bear).Damage, e.G.Obj(bear).Zone)
	}
}

func TestGrantedWardStaysScopedToTheGrantorsCreatures(t *testing.T) {
	t.Parallel()
	// Affects$ Creature.Other+YouCtrl: the grant belongs to the grantor's
	// controller. A bear under seat 1's control never wards -- the bolt (seat
	// 0's own, paid from its own pool) resolves with no ward ask.
	e := handEngine(t, corpusAlternativeCard(t, "Hexing Squelcher"))
	for _, o := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(o).Face().Name == "Hexing Squelcher" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o, From: state.ZHand, To: state.ZBattlefield})
			break
		}
	}
	o := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.G.Obj(o.ID).Controller = 1
	bearID := o.ID
	bolt := e.G.AddObject(card(t, "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"), 0)
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), bolt.ID))
	addMana(t, e, 0, "R")
	if len(e.Derived(o.ID).Keywords) != 0 {
		t.Fatalf("granted ward crossed controllers: %v", e.Derived(o.ID).Keywords)
	}
	d := e.Pending()
	opt := -1
	for _, x := range d.Options {
		if x.Kind == "cast" && x.Obj == bolt.ID {
			opt = x.Index
		}
	}
	if opt < 0 {
		t.Fatalf("bolt not offered: %+v", d.Options)
	}
	submitChoices(t, e, opt)
	dt := e.Pending()
	if dt == nil || dt.Kind != decision.KTarget {
		t.Fatalf("target ask %+v", dt)
	}
	idx := -1
	for _, o := range dt.Options {
		if o.Obj == bearID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("bear not offered: %+v", dt.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d != nil && d.Kind == decision.KModes {
		t.Fatalf("ward asked for another player's creature: %+v", d)
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(o.ID).Zone != state.ZGraveyard {
		t.Fatalf("bear zone=%s, want graveyard (no ward, bolt resolved)", e.G.Obj(o.ID).Zone)
	}
}

// passPriorityTimes answers up to n priority decisions with Pass (used to
// walk through the response window between a cast and the ward ability's
// resolution).
func passPriorityTimes(t *testing.T, e *Engine, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			return
		}
		for _, o := range d.Options {
			if o.Kind == "pass" {
				submitChoices(t, e, o.Index)
				break
			}
		}
	}
}
