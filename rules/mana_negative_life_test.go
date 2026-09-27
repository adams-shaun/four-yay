// resolveManaWith refused EVERY cost once the payer's life total was
// negative: its gate read `life < c.Life`, which at c.Life == 0 is
// `life < 0` -- true for any dead payer -- so a caster whose OWN mana
// source's rider (Ancient Tomb's 2 damage) dropped them below 0 mid-payment
// could not tap the remaining sources and the cast reversed at the exact
// stage it should have completed. State-based actions are not checked until
// a player would receive priority (CR 704.3), and paying 0 life is always
// legal (CR 119.4): a cost with no life component must resolve whatever the
// life total is, and the loss must arrive at the priority boundary instead.
// The Phyrexian boundary is unchanged: paying 2 life still requires
// life >= 2 (CR 119.4), so a {U/P} pip is not payable with life at life <= 1.
//
// Every card below is either a REAL corpus card (Keep Watch, Ancient Tomb) or
// an authored fixture (the basic-Island shape, the {2}{U/P} probe); no Forge
// .txt text is committed here.

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const (
	// nlIslandSrc is the authored basic-Island fixture: card() applies the
	// basic-land intrinsics, so it carries the standard {T}: Add {U}.
	nlIslandSrc = "Name:Island\nTypes:Basic Land Island\nOracle:x\n"
	// nlPhySrc is the {2}{U/P} probe: floating {C}{C} settles the {2}, so the
	// Phyrexian pip's only faces are U mana (absent from the pool, and the
	// board has no mana sources to tap) and 2 life.
	nlPhySrc = "Name:PhyProbe\nManaCost:2 UP\nTypes:Instant\n" +
		"A:SP$ Draw | NumCards$ 1 | SpellDescription$ Draw a card.\nOracle:x\n"
)

// nlCastEngine builds a two-seat game where seat 0 leads with the REAL corpus
// Keep Watch and Ancient Tomb plus the authored Island, moves the tomb and
// island onto seat 0's battlefield and the instant into seat 0's hand, funds
// the pool with the given floating mana, seats the payer at the given life
// total, and returns at a fresh Main-1 priority ask for seat 0.
func nlCastEngine(t *testing.T, seed uint64, life int32, pool string) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	watch, ok := reg.Lookup("Keep Watch")
	if !ok {
		t.Fatal("corpus fixture: Keep Watch missing")
	}
	tomb, ok := reg.Lookup("Ancient Tomb")
	if !ok {
		t.Fatal("corpus fixture: Ancient Tomb missing")
	}
	deck0 := []*cards.Card{watch, tomb, card(t, nlIslandSrc)}
	deck0 = append(deck0, mountainDeck(t, 37)...)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck0, mountainDeck(t, 40)}})
	e := New(cfg)
	e.Advance()
	tombID := searchMoveByNameSeat(t, e, 0, "Ancient Tomb", state.ZBattlefield)
	islandID := searchMoveByNameSeat(t, e, 0, "Island", state.ZBattlefield)
	watchID := searchMoveByNameSeat(t, e, 0, "Keep Watch", state.ZHand)
	addMana(t, e, 0, pool)
	e.G.Players[0].Life = life
	e.pending = nil
	e.priorityRound()
	return e, tombID, islandID, watchID
}

// nlPutOnStack reports whether the log holds a PutOnStack of id onto the
// stack (the cast completing).
func nlPutOnStack(e *Engine, id state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.PutOnStack && ev.Obj == id && ev.To == state.ZStack {
			return true
		}
	}
	return false
}

// TestManaPaymentContinuesBelowZeroLife is the brief's repro: at 1 life the
// caster pays a {2}{U} instant with Ancient Tomb ({C}{C} plus its own 2
// damage) and then an Island, both inside the CR 601.2g payment window. The
// damage lands mid-payment, the payer drops to -1, and the Island's bare {T}
// must still pay: the spell ends on the stack fully paid, and the loss
// arrives from the state-based action at the priority boundary -- never
// during the payment.
//
// The offered witness is exactly Ancient Tomb then Island, so the engine's
// planned-payment executor is the real repro (it re-enters the window between
// steps). The log is walked to pin the ordering the ticket names: the tomb's
// production and its damage precede the Island's tap, and no PlayerLost
// appears before the spell reaches the stack.
func TestManaPaymentContinuesBelowZeroLife(t *testing.T) {
	e, tombID, islandID, watchID := nlCastEngine(t, 421, 1, "")
	// Preconditions: the caster really is at 1 life with the spell in hand,
	// and both mana sources really are on the battlefield where the window
	// offers them.
	if got := e.G.Players[0].Life; got != 1 {
		t.Fatalf("precondition: caster life = %d, want 1", got)
	}
	if o := e.G.Obj(tombID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Ancient Tomb not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(islandID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Island not on the battlefield: %+v", o)
	}
	if e.G.Obj(watchID) == nil || e.G.Obj(watchID).Zone != state.ZHand {
		t.Fatalf("precondition: Keep Watch not in hand: %+v", e.G.Obj(watchID))
	}
	if c := e.G.Obj(watchID).Face().ManaCost; c != "2 U" {
		t.Fatalf("precondition: Keep Watch mana cost = %q, want %q", c, "2 U")
	}

	// The offering gate priced the cast, so its witness must be exactly the
	// two sources in the order Ancient Tomb then Island (the cost's {2} takes
	// the tomb's {C}{C} and the {U} takes the island). A different witness
	// would not exercise the ticket's shape -- assert it rather than trust it.
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("precondition: pending = %+v, want priority", d)
	}
	a := paymentPlanActionFor(t, d, watchID)
	if len(a.Plans) == 0 {
		t.Fatal("precondition: the cast was offered with no payment witness")
	}
	plan := a.Plans[0]
	if len(plan.Activations) != 2 || plan.Activations[0].Source != tombID || plan.Activations[1].Source != islandID {
		t.Fatalf("precondition: witness = %#v, want Ancient Tomb then Island", plan.Activations)
	}

	submitPaymentPlan(t, e, d, a)

	// The whole plan runs inside the one Submit. Walk the log once and pin
	// every event the ticket names by index, then assert the ordering.
	//
	// CR ordering: the spell is put on the stack when announced (601.2a),
	// THEN durations/costs are paid (601.2h), so PutOnStack precedes both
	// taps; the tomb's production and its 2 damage precede the island's tap;
	// the pool is deducted only after both taps; and the payer's loss is
	// recorded only after the payment settles -- never during it.
	stack, tombTap, tombMana, dmg, islandTap, islandMana, paidC, paidU, lost := -1, -1, -1, -1, -1, -1, -1, -1, -1
	for i, ev := range e.L.Events {
		switch {
		case ev.Kind == events.PutOnStack && ev.Obj == watchID && ev.To == state.ZStack && stack < 0:
			stack = i
		case ev.Kind == events.Tap && ev.Obj == tombID && tombTap < 0:
			tombTap = i
		case ev.Kind == events.Tap && ev.Obj == islandID && islandTap < 0:
			islandTap = i
		case ev.Kind == events.ManaAdd && ev.Player == 0 && ev.Counter == "C" && ev.Amount == 2 && tombMana < 0:
			tombMana = i
		case ev.Kind == events.ManaAdd && ev.Player == 0 && ev.Counter == "U" && ev.Amount == 1 && islandMana < 0:
			islandMana = i
		case ev.Kind == events.ManaAdd && ev.Player == 0 && ev.Counter == "C" && ev.Amount == -2 && paidC < 0:
			paidC = i
		case ev.Kind == events.ManaAdd && ev.Player == 0 && ev.Counter == "U" && ev.Amount == -1 && paidU < 0:
			paidU = i
		case ev.Kind == events.Damage && ev.Player == 0 && ev.Amount == 2 && dmg < 0:
			dmg = i
		case ev.Kind == events.PlayerLost && ev.Player == 0 && lost < 0:
			lost = i
		}
	}
	for name, idx := range map[string]int{"stack": stack, "tomb tap": tombTap, "tomb mana": tombMana,
		"ancient tomb damage": dmg, "island tap": islandTap, "island mana": islandMana,
		"generic paid": paidC, "blue paid": paidU, "player lost": lost} {
		if idx < 0 {
			t.Fatalf("log: no %s event; events=%v", name, e.L.Events)
		}
	}
	if !(stack < tombTap && tombTap < tombMana && tombMana < dmg && dmg < islandTap &&
		islandTap < islandMana && islandMana < paidU && islandMana < paidC &&
		paidU < lost && paidC < lost) {
		t.Fatalf("log ordering: stack=%d tombTap=%d tombMana=%d damage=%d islandTap=%d "+
			"islandMana=%d paidU=%d paidC=%d lost=%d, want stack<tombTap<tombMana<damage<"+
			"islandTap<islandMana and both payments between islandMana and lost",
			stack, tombTap, tombMana, dmg, islandTap, islandMana, paidU, paidC, lost)
	}

	// The spell was never reversed after payment (a reversal would move the
	// card off the stack before the loss) and it is fully paid: the pool is
	// empty in the final state and the spell is (or was) on the stack.
	for i, ev := range e.L.Events[stack+1 : lost] {
		if ev.Kind == events.MoveZone && ev.Obj == watchID && ev.From == state.ZStack {
			t.Fatalf("event %d moved the spell off the stack before the loss: %+v", stack+1+i, ev)
		}
	}
	if !nlPutOnStack(e, watchID) {
		t.Fatal("Keep Watch never reached the stack; the payment did not complete")
	}
	// The state-based action applies at the priority boundary: the payer is
	// dead, and the loss names the life total.
	if !e.G.Players[0].Lost {
		t.Fatal("the caster was not lost at the priority boundary after the payment")
	}
	if got := e.L.Events[lost].Text; got != "life total is 0 or less" {
		t.Fatalf("first player loss = %q, want %q", got, "life total is 0 or less")
	}
}

// TestPhyrexianLifeBranchStillNeedsTwoLife pins the UNCHANGED boundary on the
// same machinery: a {U/P} pip cannot be paid with life at life <= 1, and the
// 2-life face still works at life >= 2.
func TestPhyrexianLifeBranchStillNeedsTwoLife(t *testing.T) {
	// At life 1 with floating {C}{C} and no mana sources, the {2}{U/P} cast
	// is not payable: the {2} can be paid, the pip's U face has no mana, and
	// its life face needs 2. The offer gate must not price it payable.
	e, phyID := nlPhyEngine(t, 422, 1)
	if castOffered(e, phyID) {
		t.Fatal("the {2}{U/P} cast was offered at life 1 with no U mana; the 2-life face was paid at life < 2")
	}

	// The same board at life 3: the 2-life face is payable, the cast is
	// offered, and the flow asks the pip's face. Choosing life completes the
	// payment for 2 life and leaves the spell on the stack.
	e2, phyID2 := nlPhyEngine(t, 422, 3)
	if !castOffered(e2, phyID2) {
		t.Fatal("the {2}{U/P} cast was not offered at life 3; the 2-life face does not price payable")
	}
	submitChoices(t, e2, castOptionFor(t, e2, phyID2).Index)
	d := e2.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("want the {U/P} pip-face ask, got %+v", d)
	}
	lifeOpt := -1
	for _, o := range d.Options {
		if o.Kind == "pay_life" {
			lifeOpt = o.Index
		}
	}
	if lifeOpt < 0 {
		t.Fatalf("the pip ask offers no life face at life 3: %+v", d)
	}
	submitChoices(t, e2, lifeOpt)
	if got := e2.G.Players[0].Life; got != 1 {
		t.Fatalf("after paying the {U/P} pip with life, life = %d, want 1", got)
	}
	if !nlPutOnStack(e2, phyID2) {
		t.Fatal("PhyProbe never reached the stack; the life face did not complete the payment")
	}
}

// nlPhyEngine builds the {2}{U/P} board: PhyProbe in seat 0's hand, floating
// {C}{C}, NO mana sources on the battlefield (the deck is Mountains and none
// are moved out), payer at the given life, at a fresh Main-1 priority ask.
func nlPhyEngine(t *testing.T, seed uint64, life int32) (*Engine, state.ObjID) {
	t.Helper()
	fixture := card(t, nlPhySrc)
	name := fixture.Faces[0].Name
	if c := ParseCost("UP"); len(c.Phyrexian) != 1 {
		t.Fatalf("precondition: ParseCost(UP) = %+v, want one Phyrexian pip", c)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{fixture}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		}})
	e := New(cfg)
	e.Advance()
	phyID := searchMoveByNameSeat(t, e, 0, name, state.ZHand)
	addMana(t, e, 0, "CC")
	e.G.Players[0].Life = life
	e.pending = nil
	e.priorityRound()
	return e, phyID
}
