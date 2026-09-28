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
// caster pays a {2}{U} instant with an Island and Ancient Tomb ({C}{C} plus
// its own 2 damage), both inside the CR 601.2g payment window. The tomb's
// damage lands mid-payment and drops the payer to -1; the payment must still
// settle against that negative life total (a cost with no life component
// pays 0 life, CR 119.4): the spell ends on the stack fully paid, and the
// loss arrives from the state-based action at the priority boundary --
// never during the payment.
//
// The payment is driven MANUALLY. Ancient Tomb is a last-resort source (spec
// §3.2, aph-producer-tiers): the planner never funds a plan with it, so the
// auto-pay offer carries no witness for this cast and the planned-payment
// executor cannot reach the shape. The test asserts that first, then begins
// the cast directly (the pool-only offer gate never offers a cast the empty
// pool cannot pay, exactly as priority_suspension_test does) and answers the
// window by hand.
//
// The Island is tapped FIRST and the tomb LAST, so the tomb's damage is the
// final activation of the window and the payment settles inside that same
// answer. Pre-fix, resolveManaWith refused the {2}{U} at life -1 and the cast
// reversed "cost no longer payable". The reverse order (tomb, then Island)
// crosses a Submit boundary at life -1, where Submit's closing
// checkStateBased runs while the cast is still mid-window, so the payer
// loses before the Island can be tapped -- a separate CR 704.3 deviation of
// the manual window that this test deliberately does not pin.
//
// The log is walked to pin the ordering the ticket names: the tomb's
// production and damage precede the pool deduction, and no PlayerLost
// appears before the spell is paid for.
func TestManaPaymentContinuesBelowZeroLife(t *testing.T) {
	t.Parallel()
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

	// The tier gate: Ancient Tomb is last resort, so no plan is offered for
	// the cast and the planner names the last-resort source as the reason.
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("precondition: pending = %+v, want priority", d)
	}
	// Payment actions are published lazily (aph-lazy-offers): build them
	// for this ask, as an opted-in consumer would, before reading them.
	e.EnsurePaymentActions()
	for _, a := range d.PaymentActions {
		if a.Cast.Object == watchID && len(a.Plans) > 0 {
			t.Fatalf("auto-pay offered a plan for Keep Watch funded by a last-resort source: %#v", a.Plans)
		}
	}
	got := e.PlanCastPayment(0, decision.PlannedCast{Object: watchID, Face: 0, Origin: "hand"})
	if got.Plan != nil || got.Reason != "insufficient" || got.Detail != "source:last_resort" {
		t.Fatalf("PlanCastPayment = plan %v reason %q detail %q, want no plan, insufficient, source:last_resort",
			got.Plan, got.Reason, got.Detail)
	}
	if castOffered(e, watchID) {
		t.Fatal("precondition: the empty pool cannot pay {2}{U}, yet the pool-only gate offered the cast")
	}

	// Begin the cast directly and answer the CR 601.2g window by hand.
	start := len(e.L.Events)
	e.pending = nil
	e.beginCast(0, decision.Option{Kind: "cast", Obj: watchID})
	e.Advance()
	if w := e.Pending(); w == nil || w.Kind != decision.KChoose {
		t.Fatalf("after beginning the cast = %+v, want the CR 601.2g mana window", w)
	}
	if !cwActivateInWindow(t, e, islandID) {
		t.Fatalf("the window did not offer the Island: %+v", e.Pending())
	}
	// The pool holds only {U}: the window re-poses with the tomb still
	// offered, and the payer is still at 1 life.
	if w := e.Pending(); w == nil || w.Kind != decision.KChoose {
		t.Fatalf("after the Island = %+v, want the window re-posed", w)
	}
	if got := e.G.Players[0].Life; got != 1 || e.G.Players[0].Lost {
		t.Fatalf("after the Island: life %d lost %t, want 1 and alive", got, e.G.Players[0].Lost)
	}
	if !cwActivateInWindow(t, e, tombID) {
		t.Fatalf("the window did not offer Ancient Tomb: %+v", e.Pending())
	}

	// Walk the log once and pin every event the ticket names by index, then
	// assert the ordering.
	//
	// CR ordering: the spell is put on the stack when announced (601.2a),
	// THEN costs are paid (601.2h), so PutOnStack precedes both taps; the
	// tomb's production and its 2 damage (life 1 -> -1) precede the pool
	// deduction; and the payer's loss is recorded only after the payment
	// settles -- never during it.
	stack, islandTap, islandMana, tombTap, tombMana, dmg, paidC, paidU, lost := -1, -1, -1, -1, -1, -1, -1, -1, -1
	for i := start; i < len(e.L.Events); i++ {
		ev := e.L.Events[i]
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
			t.Fatalf("log: no %s event; events=%v", name, e.L.Events[start:])
		}
	}
	if !(stack < islandTap && islandTap < islandMana && islandMana < tombTap &&
		tombTap < tombMana && tombMana < dmg && dmg < paidU && dmg < paidC &&
		paidU < lost && paidC < lost) {
		t.Fatalf("log ordering: stack=%d islandTap=%d islandMana=%d tombTap=%d tombMana=%d "+
			"damage=%d paidU=%d paidC=%d lost=%d, want stack<islandTap<islandMana<tombTap<"+
			"tombMana<damage and both payments between damage and lost",
			stack, islandTap, islandMana, tombTap, tombMana, dmg, paidU, paidC, lost)
	}

	// The spell was never reversed (a reversal would move the card off the
	// stack before the loss) and it is fully paid: it reached the stack and
	// the payment deducted exactly the {2}{U}.
	for i, ev := range e.L.Events[stack+1 : lost] {
		if ev.Kind == events.MoveZone && ev.Obj == watchID && ev.From == state.ZStack {
			t.Fatalf("event %d moved the spell off the stack before the loss: %+v", stack+1+i, ev)
		}
	}
	if !nlPutOnStack(e, watchID) {
		t.Fatal("Keep Watch never reached the stack; the payment did not complete")
	}
	// The state-based action applies at the priority boundary: the payer is
	// dead at -1, and the loss names the life total.
	if got := e.G.Players[0].Life; got != -1 {
		t.Fatalf("caster life after the tomb = %d, want -1", got)
	}
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
	t.Parallel()
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
