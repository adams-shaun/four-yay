package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// offTurnFlashEngine builds the board the target-conditional CastWithFlash
// tests read: the named corpus spell in seat 0's hand, seat 1 active (so seat
// 0's offers are off-sorcery), and seat 0 funded for the spell's printed cost.
// The caller places the permanents that decide whether a qualifying target
// exists.
func offTurnFlashEngine(t *testing.T, name string, generic, blue int32) (*Engine, state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusAlternativeCard(t, name))
	e.G.Active = 1
	e.G.Players[0].Pool[state.MC] = generic
	e.G.Players[0].Pool[state.MU] = blue
	spell := e.G.Zone(state.ZHand, 0)[0]
	// Preconditions: the spell is in the zone the rule reads and the turn is
	// seat 1's, so any seat-0 offer at all is the flash permission.
	if e.G.Obj(spell).Zone != state.ZHand {
		t.Fatalf("precondition: %s is in %s, want hand", name, e.G.Obj(spell).Zone)
	}
	if e.G.Active != 1 {
		t.Fatalf("precondition: active seat = %d, want 1 (off-turn for seat 0)", e.G.Active)
	}
	if e.G.Obj(spell).Face().IsInstant() {
		t.Fatalf("precondition: %s is an instant; it would not need flash", name)
	}
	return e, spell
}

func battlePerm(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), p)
	o.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, p, append(e.G.Zone(state.ZBattlefield, p), o.ID))
	return o.ID
}

func istTargetOptionFor(d *decision.Decision, id state.ObjID) int {
	for _, o := range d.Options {
		if o.Obj == id {
			return o.Index
		}
	}
	return -1
}

// TestIsTargetingFlashPhotography pins the real-corpus Flash Photography
// script: `ValidSA$ Spell.IsTargeting Valid Permanent.YouCtrl`. The sorcery
// must be offered only off-turn and only when a permanent its controller
// controls is a legal target; a cast announced on a non-qualifying (or
// absent) target is reversed by CR 601.2e, and a cast on a qualifying target
// completes.
func TestIsTargetingFlashPhotography(t *testing.T) {
	// No permanent you control: no qualifying target, no flash offer.
	e, spell := offTurnFlashEngine(t, "Flash Photography", 2, 2)
	theirBear := battlePerm(t, e, 1, "Name:Their Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatal("Flash Photography was offered off-turn with no permanent its controller controls")
	}
	if e.G.Obj(theirBear).Zone != state.ZBattlefield {
		t.Fatal("precondition: the opponent's permanent must be on the battlefield")
	}

	// A permanent you control makes the restriction satisfiable.
	myBear := battlePerm(t, e, 0, "Name:My Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatal("Flash Photography was not offered off-turn with a permanent its controller controls")
	}

	// A cast on the opponent's permanent (a legal target that does NOT meet
	// the flash restriction) is reversed before payment.
	e.beginCast(0, decision.Option{Kind: "cast", Obj: spell})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Flash Photography target decision = %+v", d)
	}
	if istTargetOptionFor(d, theirBear) < 0 {
		t.Fatalf("precondition: the non-qualifying permanent must be a legal target: %+v", d.Options)
	}
	before := len(e.L.Events)
	submitChoices(t, e, istTargetOptionFor(d, theirBear))
	if e.G.Obj(spell).Zone != state.ZHand {
		t.Fatalf("a non-qualifying target left the spell in %s, want the reversal back to hand", e.G.Obj(spell).Zone)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack after the reversal = %v, want empty", e.G.Stack)
	}
	if len(e.L.Events) <= before {
		t.Fatal("no events were logged through the reversed cast")
	}

	// A cast on the qualifying permanent completes.
	e.beginCast(0, decision.Option{Kind: "cast", Obj: spell})
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("second target decision = %+v", d)
	}
	idx := istTargetOptionFor(d, myBear)
	if idx < 0 {
		t.Fatalf("precondition: the qualifying permanent must be offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if z := e.G.Obj(spell).Zone; z == state.ZHand {
		t.Fatal("a qualifying target did not let the cast proceed past CR 601.2e")
	}
}

// TestIsTargetingTimelyWard pins Timely Ward's
// `ValidSA$ Spell.IsTargeting Valid Card.IsCommander`: the Aura is offered
// off-turn only while a commander is a legal target, and a non-commander
// target is not enough.
func TestIsTargetingTimelyWard(t *testing.T) {
	e := handEngine(t, corpusAlternativeCard(t, "Timely Ward"))
	e.format = FormatCommander
	e.G.Active = 1
	e.G.Players[0].Pool[state.MC] = 2
	e.G.Players[0].Pool[state.MW] = 1
	spell := e.G.Zone(state.ZHand, 0)[0]
	if e.G.Obj(spell).Zone != state.ZHand {
		t.Fatalf("precondition: Timely Ward is in %s, want hand", e.G.Obj(spell).Zone)
	}
	if e.G.Active != 1 {
		t.Fatalf("precondition: active seat = %d, want 1", e.G.Active)
	}

	// A plain creature is not a commander: no offer.
	plain := battlePerm(t, e, 0, "Name:Plain Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if e.G.Obj(plain) == nil {
		t.Fatal("precondition: the plain creature is missing")
	}
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatal("Timely Ward was offered off-turn with only a non-commander creature available")
	}

	// A commander on the battlefield satisfies the restriction.
	cmdr := battlePerm(t, e, 0, "Name:My Commander\nManaCost:1 G\nTypes:Legendary Creature Human\nPT:2/2\nOracle:x\n")
	e.G.Players[0].Commanders = []state.ObjID{cmdr}
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatal("Timely Ward was not offered off-turn with a commander available")
	}

	// Announcing it on the non-commander is reversed (CR 601.2e).
	e.beginCast(0, decision.Option{Kind: "cast", Obj: spell})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Timely Ward target decision = %+v", d)
	}
	if istTargetOptionFor(d, plain) < 0 {
		t.Fatalf("precondition: the non-commander must be a legal target: %+v", d.Options)
	}
	submitChoices(t, e, istTargetOptionFor(d, plain))
	if e.G.Obj(spell).Zone != state.ZHand {
		t.Fatalf("a non-commander target left Timely Ward in %s, want hand", e.G.Obj(spell).Zone)
	}

	// Announcing it on the commander completes.
	e.beginCast(0, decision.Option{Kind: "cast", Obj: spell})
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("second Timely Ward target decision = %+v", d)
	}
	idx := istTargetOptionFor(d, cmdr)
	if idx < 0 {
		t.Fatalf("precondition: the commander must be offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if z := e.G.Obj(spell).Zone; z == state.ZHand {
		t.Fatal("a commander target did not let Timely Ward proceed past CR 601.2e")
	}
}

// TestIsTargetingHeadOfTheClass pins Head of the Class's
// `ValidSpell$ Spell.IsTargeting Valid Creature` reduction: the W/B discount
// applies only while the announced target is a creature, and only during your
// own turn.
func TestIsTargetingHeadOfTheClass(t *testing.T) {
	spellSrc := "Name:Wounded Spell\nManaCost:W B\nTypes:Sorcery\n" +
		"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ +1 | NumDef$ +1\nOracle:x\n"
	e := handEngine(t, card(t, spellSrc))
	head := e.G.AddObject(corpusAlternativeCard(t, "Head of the Class"), 0)
	head.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{head.ID})
	spell := e.G.Zone(state.ZHand, 0)[0]
	if e.G.Obj(spell).Zone != state.ZHand {
		t.Fatalf("precondition: the spell is in %s, want hand", e.G.Obj(spell).Zone)
	}
	if e.G.Obj(head.ID).Zone != state.ZBattlefield {
		t.Fatal("precondition: Head of the Class is not on the battlefield")
	}
	if e.G.Active != 0 {
		t.Fatalf("precondition: active seat = %d, want 0 (Head of the Class's PlayerTurn)", e.G.Active)
	}

	// A land is a legal target of the spell but is NOT a creature, so it must
	// not earn the reduction; a creature earns it. (Head of the Class is
	// itself a creature, so the battlefield always holds a qualifying target
	// here -- the full-price case is tested through a non-creature target and
	// through the PlayerTurn gate below.)
	land := battlePerm(t, e, 0, "Name:A Land\nTypes:Basic Land Mountain\nOracle:x\n")
	bear := battlePerm(t, e, 0, "Name:A Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if e.G.Obj(land).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("precondition: land and creature must both be on the battlefield")
	}
	if e.G.Obj(land).Face().IsCreature() || !e.G.Obj(bear).Face().IsCreature() {
		t.Fatal("precondition: the land must not be a creature and the bear must be")
	}

	// An empty pool: the spell is offered only because the creature target
	// makes the W/B pips free.
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatal("the W/B spell was not offered free with a creature target and PlayerTurn")
	}

	// The reduction is target-conditional and colored: the price with a
	// non-creature target stays full while the creature target makes it free.
	full := e.costModifiersForTargets(0, spell, spellScope(""), []state.Target{{Obj: land}})
	targeted := e.costModifiersForTargets(0, spell, spellScope(""), []state.Target{{Obj: bear}})
	fullCost := full.apply(e.parseCost("W B"))
	targetedCost := targeted.apply(e.parseCost("W B"))
	if fullCost.Colored != (state.Mana{state.MW: 1, state.MB: 1}) || fullCost.CMC() != 2 {
		t.Fatalf("non-creature-target price = %+v, want the full W1 B1", fullCost)
	}
	if targetedCost.Colored != (state.Mana{}) || targetedCost.CMC() != 0 {
		t.Fatalf("creature-target colored reduction wrong: %+v", targetedCost)
	}

	// A different player's turn: Condition$ PlayerTurn denies the discount, so
	// the full W B price is unaffordable from the empty pool.
	e.G.Active = 1
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatal("the discount applied off-turn; Condition$ PlayerTurn was not honoured")
	}
}
