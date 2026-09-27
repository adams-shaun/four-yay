package rules

// The Rakdos-params brief, gap 8: AddKeyword$ PayLifeInsteadOf:B -- K'rrik,
// Son of Yawgmoth's "For each {B} in a cost, you may pay 2 life rather than
// pay that mana." The grant is an S:Mode$ Continuous static whose Affected$
// You names the controller as a player, so the payment machinery consumes it:
// every plain {B} pip in every cost the granting player pays also accepts 2
// life, under the same deterministic prefer-mana-then-life assignment the
// printed {B/P} pips use. The offer gate (castable), the X bounds, the mana
// window and payMana all go through payerPayable/payerGrantsPayLifeInsteadOfB,
// so a cost payable only by life is offered and charged consistently.
//
// K'rrik's own printed {B/P} pips were already native; this is the GRANTED
// side, tested on a second spell's plain {B} pip.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func krrikEngine(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusAlternativeCard(t, "K'rrik, Son of Yawgmoth"))
	for _, o := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(o).Face().Name == "K'rrik, Son of Yawgmoth" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o, From: state.ZHand, To: state.ZBattlefield})
			break
		}
	}
	o := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	bolt := e.G.AddObject(card(t, "Name:Doom Blade\nManaCost:1 B\nTypes:Instant\nA:SP$ Destroy | ValidTgts$ Creature.nonBlack\nOracle:x\n"), 0)
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), bolt.ID))
	addMana(t, e, 0, "GG")
	return e, o.ID
}

func castDoomBlade(t *testing.T, e *Engine, target state.ObjID) {
	t.Helper()
	hand := e.G.Zone(state.ZHand, 0)
	doom := hand[len(hand)-1]
	d := e.Pending()
	opt := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == doom {
			opt = o.Index
		}
	}
	if opt < 0 {
		t.Fatalf("Doom Blade not offered: %+v", d.Options)
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
		t.Fatalf("target options missing %d: %+v", target, dt.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
}

func TestKrrikGrantPaysBlackPipWithLife(t *testing.T) {
	e, bear := krrikEngine(t)
	e.G.Players[0].Life = 20
	// The pool holds only green: the {1} generic comes from it, the {B} pip
	// from 2 life.
	castDoomBlade(t, e, bear)
	if life := e.G.Players[0].Life; life != 18 {
		t.Fatalf("life=%d, want 18 (the {B} pip paid with 2 life)", life)
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 1 {
		t.Fatalf("pool=%+v, want 1 (one green spent on the {1}, the other untouched)", pool)
	}
	if e.G.Obj(bear).Zone != state.ZGraveyard {
		t.Fatalf("bear zone=%s, want graveyard (Doom Blade resolved)", e.G.Obj(bear).Zone)
	}
}

func TestKrrikGrantPrefersManaWhenItExists(t *testing.T) {
	e, bear := krrikEngine(t)
	e.G.Players[0].Life = 20
	addMana(t, e, 0, "B")
	castDoomBlade(t, e, bear)
	// The pip prefers the pool's black; life untouched.
	if life := e.G.Players[0].Life; life != 20 {
		t.Fatalf("life=%d, want 20 (the pip preferred the pool's {B})", life)
	}
	if e.G.Obj(bear).Zone != state.ZGraveyard {
		t.Fatalf("bear zone=%s", e.G.Obj(bear).Zone)
	}
}

func TestKrrikGrantIsTheGrantorsCostsOnly(t *testing.T) {
	// Without K'rrik on the battlefield the same cast is NOT offered on the
	// same board: the {B} pip has no mana and no life route.
	e := handEngine(t, corpusAlternativeCard(t, "K'rrik, Son of Yawgmoth"))
	o := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	bolt := e.G.AddObject(card(t, "Name:Doom Blade\nManaCost:1 B\nTypes:Instant\nA:SP$ Destroy | ValidTgts$ Creature.nonBlack\nOracle:x\n"), 0)
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), bolt.ID))
	addMana(t, e, 0, "GG")
	d := e.Pending()
	for _, x := range d.Options {
		if x.Kind == "cast" && x.Obj == bolt.ID {
			t.Fatalf("Doom Blade offered without the grant: %+v", d.Options)
		}
	}
}

func TestKrrikGrantDoesNotCoverOtherColours(t *testing.T) {
	// The grant names {B} only: a {R} pip stays mana-only.
	e := handEngine(t, corpusAlternativeCard(t, "K'rrik, Son of Yawgmoth"))
	for _, o := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(o).Face().Name == "K'rrik, Son of Yawgmoth" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o, From: state.ZHand, To: state.ZBattlefield})
			break
		}
	}
	shock := e.G.AddObject(card(t, "Name:Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2\nOracle:x\n"), 0)
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), shock.ID))
	addMana(t, e, 0, "GG")
	d := e.Pending()
	for _, x := range d.Options {
		if x.Kind == "cast" && x.Obj == shock.ID {
			t.Fatalf("a {R} pip payable via the B-only grant: %+v", d.Options)
		}
	}
}

// krrikWindowEngine is the krrik-window regression's setup: K'rrik on the
// battlefield, a {B}-only targeted instant in hand, an untapped Swamp on the
// battlefield and a Bear to aim at, with a deliberately EMPTY pool. Only the
// K'rrik life grant makes the {B} cast offered, and only the Swamp makes its
// pip payable with mana rather than life.
func krrikWindowEngine(t *testing.T) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusAlternativeCard(t, "K'rrik, Son of Yawgmoth"))
	for _, o := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(o).Face().Name == "K'rrik, Son of Yawgmoth" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o, From: state.ZHand, To: state.ZBattlefield})
			break
		}
	}
	bear := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear.ID, From: state.ZLibrary, To: state.ZBattlefield})
	swamp := e.G.AddObject(card(t, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: swamp.ID, From: state.ZLibrary, To: state.ZBattlefield})
	bolt := e.G.AddObject(card(t, "Name:Doom Bolt\nManaCost:B\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2\nOracle:x\n"), 0)
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), bolt.ID))
	// No addMana: the pool stays empty so the window's question is real.
	toMain1(t, e)
	e.priorityRound()
	return e, bolt.ID, bear.ID, swamp.ID
}

// TestKrrikGrantPosesManaWindowWhenSourceCanPay is the krrik-paylife ticket's
// regression. Before the fix, manaWindowAsk's "the pool already pays" gate
// read the SAME resolveMana the payment does, so K'rrik's PayLifeInsteadOf:B
// grant made a {B} pip count as paid: with an empty pool and an untapped
// Swamp the CR 601.2g window was skipped and the cast silently spent 2 life
// with the Swamp still untapped. The window's gate now suspends the grant, so
// the payer is offered the Swamp and may choose the mana; "done" still spends
// the life through the ordinary payment.
func TestKrrikGrantPosesManaWindowWhenSourceCanPay(t *testing.T) {
	e, bolt, bear, swamp := krrikWindowEngine(t)
	e.G.Players[0].Life = 20
	// Precondition: no mana is floating -- otherwise the pool could pay the pip
	// by itself and the window would be (correctly) skipped.
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("precondition: pool must be empty, got %+v", pool)
	}
	if o := e.G.Obj(swamp); o == nil || o.Tapped {
		t.Fatalf("precondition: the Swamp must be untapped on the battlefield")
	}
	d := e.Pending()
	if d == nil {
		t.Fatalf("precondition: a priority decision is pending")
	}
	castOpt := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bolt {
			castOpt = o.Index
		}
	}
	if castOpt < 0 {
		t.Fatalf("precondition: the {B} cast must be offered via the life grant: %+v", d.Options)
	}
	submitChoices(t, e, castOpt)

	dt := e.Pending()
	if dt == nil || dt.Kind != decision.KTarget {
		t.Fatalf("precondition: the target ask, got %+v", dt)
	}
	tgt := -1
	for _, o := range dt.Options {
		if o.Obj == bear {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("precondition: the Bear is a legal target: %+v", dt.Options)
	}
	submitChoices(t, e, tgt)

	// The CR 601.2g mana window: the pool alone cannot pay the {B} pip and the
	// untapped Swamp can, so the window must be posed (this is the assertion
	// that fails without the fix -- payCast pays silently instead).
	w := e.Pending()
	if w == nil || w.Kind != decision.KChoose {
		t.Fatalf("the mana window must be posed for a {B} pip an untapped source can pay, got %+v", w)
	}
	swampOpt := -1
	for _, o := range w.Options {
		if o.Kind == "activate" && o.Obj == swamp {
			swampOpt = o.Index
		}
	}
	if swampOpt < 0 {
		t.Fatalf("the untapped Swamp must be offered in the window: %+v", w.Options)
	}
	submitChoices(t, e, swampOpt)
	passUntilStackEmpty(t, e, 20)

	if life := e.G.Players[0].Life; life != 20 {
		t.Fatalf("life=%d, want 20 (the pip must be paid with the tapped Swamp's mana, not 2 life)", life)
	}
	if !e.G.Obj(swamp).Tapped {
		t.Fatalf("the Swamp must be tapped to pay the pip")
	}
	if e.G.Obj(bear).Zone != state.ZGraveyard {
		t.Fatalf("bear zone=%s, want graveyard (the spell resolved)", e.G.Obj(bear).Zone)
	}
}

// TestKrrikGrantPaysLifeFromWindowWhenNoSourceCan answers the posed window
// with "done": the {B} pip then spends the granted 2 life through the
// ordinary payment, proving the window did not remove the life route.
func TestKrrikGrantPaysLifeFromWindowWhenNoSourceCan(t *testing.T) {
	e, bolt, bear, swamp := krrikWindowEngine(t)
	e.G.Players[0].Life = 20
	d := e.Pending()
	castOpt := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bolt {
			castOpt = o.Index
		}
	}
	if castOpt < 0 {
		t.Fatalf("precondition: the {B} cast must be offered via the life grant: %+v", d.Options)
	}
	submitChoices(t, e, castOpt)
	dt := e.Pending()
	if dt == nil || dt.Kind != decision.KTarget {
		t.Fatalf("precondition: the target ask, got %+v", dt)
	}
	tgt := -1
	for _, o := range dt.Options {
		if o.Obj == bear {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("precondition: the Bear is a legal target: %+v", dt.Options)
	}
	submitChoices(t, e, tgt)
	w := e.Pending()
	if w == nil || w.Kind != decision.KChoose {
		t.Fatalf("the mana window must be posed, got %+v", w)
	}
	doneOpt := -1
	for _, o := range w.Options {
		if o.Kind == "done" {
			doneOpt = o.Index
		}
	}
	if doneOpt < 0 {
		t.Fatalf("the window must offer \"done\": %+v", w.Options)
	}
	submitChoices(t, e, doneOpt)
	passUntilStackEmpty(t, e, 20)
	if life := e.G.Players[0].Life; life != 18 {
		t.Fatalf("life=%d, want 18 (\"done\" spends the {B} pip's 2 granted life)", life)
	}
	if e.G.Obj(swamp).Tapped {
		t.Fatalf("the Swamp must stay untapped when the payer answers \"done\"")
	}
}
