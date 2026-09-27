package rules

// param:api:RepeatEach.ChooseOrder -- the loop must let the chooser order its
// subject list before the first body runs, and then process that order (Forge
// RepeatEachEffect.resolve orders repeatCards via orderMoveToZoneList). The
// focused test below proves the ordering ask exists, that answering it
// permutes which subject each iteration acts on (observable as the order of
// the Damage events), and that the loop still completes across the ask's
// suspension/resume. Without the fix the cast resolves straight to the scan
// order and no order ask is ever pending, so the test fails at its own
// precondition.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// orderProbeSrc is a constructed RepeatEach sorcery: for each opposing
// creature, in the controller's chosen order, deal 1 damage to it. The body's
// Damage event names the iteration's subject, so the event order is the
// iteration order -- the exact thing ChooseOrder is supposed to govern.
const orderProbeSrc = "Name:Order Probe\nManaCost:1\nTypes:Sorcery\n" +
	"A:SP$ RepeatEach | RepeatCards$ Creature.OppCtrl | ChooseOrder$ True | RepeatSubAbility$ DBHit\n" +
	"SVar:DBHit:DB$ DealDamage | Defined$ Imprinted | NumDmg$ 1\nOracle:x\n"

// TestRepeatEachChooseOrderGovernsIterationOrder is the focused ratchet:
// two distinct opposing creatures, the order ask offered once with both, the
// controller answering in reverse of the selector's scan order, and the
// Damage events then reaching the subjects in exactly that answer order.
func TestRepeatEachChooseOrderGovernsIterationOrder(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _ := corpusEngineCfg(t, reg, []*cards.Card{card(t, orderProbeSrc)},
		[]*cards.Card{card(t, "Name:Order Bear A\nTypes:Creature\nPT:2/2\nOracle:x\n"),
			card(t, "Name:Order Bear B\nTypes:Creature\nPT:2/2\nOracle:x\n")})

	spell := moveByName(t, e, 0, "Order Probe", state.ZHand)
	bearA := moveByName(t, e, 1, "Order Bear A", state.ZBattlefield)
	bearB := moveByName(t, e, 1, "Order Bear B", state.ZBattlefield)
	// Precondition: the two subjects really are distinct battlefield
	// creatures controlled by the same opponent, so "changed order" is a
	// meaningful statement and the selector has both to scan.
	if bearA == bearB {
		t.Fatalf("precondition: both subjects resolved to object %d", bearA)
	}
	for _, id := range []state.ObjID{bearA, bearB} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
			t.Fatalf("precondition: subject %d = %+v, want a battlefield creature controlled by seat 1", id, o)
		}
	}

	// Cast the probe (mana cost 0) and step onto the stack. The spell is a
	// constructed card, not a corpus one, so cast it by hand rather than
	// through castSpellNamed (which looks the card up in the corpus registry).
	addMana(t, e, 0, "C")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("before the cast, pending = %+v, want priority", d)
	}
	castIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("no cast option for Order Probe (0-cost): %+v", d.Options)
	}
	submitChoices(t, e, castIdx)
	d = passUntilResolved(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "repeat_choose_order" {
		t.Fatalf("precondition: after the cast, pending = %+v, want the ChooseOrder KChoose ask", d)
	}
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("order ask Min/Max = %d/%d, want 2/2 (a permutation of both subjects)", d.Min, d.Max)
	}
	// Precondition: the ask offers BOTH subjects, and their offered indexes
	// differ -- the two values under comparison actually differ.
	indexOf := map[state.ObjID]int{}
	for _, o := range d.Options {
		indexOf[o.Obj] = o.Index
	}
	if _, ok := indexOf[bearA]; !ok {
		t.Fatalf("order ask does not offer Order Bear A (%d): %+v", bearA, d.Options)
	}
	if _, ok := indexOf[bearB]; !ok {
		t.Fatalf("order ask does not offer Order Bear B (%d): %+v", bearB, d.Options)
	}
	if indexOf[bearA] == indexOf[bearB] {
		t.Fatalf("precondition: both subjects share offered index %d", indexOf[bearA])
	}

	// Answer in the REVERSE of the selector's scan order: whichever subject
	// was offered second is processed first.
	first, second := bearA, bearB
	if indexOf[bearA] > indexOf[bearB] {
		first, second = bearB, bearA
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{indexOf[second], indexOf[first]}}); err != nil {
		t.Fatalf("submit reversed order: %v", err)
	}

	// The answer re-enters the loop; finish the spell. The only remaining
	// asks are priority.
	for n := 0; n < 40 && len(e.G.Stack) > 0; n++ {
		pd := e.Pending()
		if pd == nil {
			t.Fatalf("no decision pending with stack depth %d", len(e.G.Stack))
		}
		submitChoices(t, e, pd.Options[0].Index)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack did not empty: %v", e.G.Stack)
	}

	var order []state.ObjID
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Amount == 1 && (ev.Obj == bearA || ev.Obj == bearB) {
			order = append(order, ev.Obj)
		}
	}
	if len(order) != 2 {
		t.Fatalf("Damage events to the two subjects = %v, want exactly 2 (one per iteration)", order)
	}
	if order[0] != second || order[1] != first {
		t.Fatalf("iteration order = %v, want the answered order [%d %d]", order, second, first)
	}
}

// ezuriBeastStem is the Forge token script stem Ezuri's Predation's body
// mints (DBToken's TokenScript$ g_4_4_phyrexian_beast). The TokenCreate
// event's Text is that stem, so the loop's per-subject token is observable
// without depending on the battlefield's object naming.
const ezuriBeastStem = "g_4_4_phyrexian_beast"

// TestRepeatEachChooseOrderEzurisPredationAsksBeforeLoop pins the real
// corpus carrier from the ticket: Ezuri's Predation carries
// ChooseOrder$ True over Creature.OppCtrl, so its RepeatEach must pose the
// ordering ask with every opposing creature before the first iteration's
// body runs. It asserts its own precondition (two distinct opposing
// creatures on the battlefield), that the ask offers exactly those two in
// the selector's scan order, and that answering it still runs one iteration
// per subject (two Beast tokens).
//
// The loop's token/fight PAIRING is not asserted here: this build's
// api:Fight reads its opposing list from Ctx.Targets, which is empty for
// Ezuri's `Defined$ Imprinted & Remembered` body, so no fight damage is
// produced (reported under Issues). The order-dependent observable
// behaviour ChooseOrder governs is pinned by the focused synthetic test
// above, whose body (DealDamage Defined$ Imprinted) is unimpeded.
func TestRepeatEachChooseOrderEzurisPredationAsksBeforeLoop(t *testing.T) {
	reg := searchTestRegistry(t)
	ez, ok := reg.Lookup("Ezuri's Predation")
	if !ok {
		t.Skip("corpus fixture: Ezuri's Predation missing")
	}
	e, _ := corpusEngineCfg(t, reg, []*cards.Card{ez},
		[]*cards.Card{card(t, "Name:Order Prey A\nTypes:Creature\nPT:2/2\nOracle:x\n"),
			card(t, "Name:Order Prey B\nTypes:Creature\nPT:2/2\nOracle:x\n")})

	spell := moveByName(t, e, 0, "Ezuri's Predation", state.ZHand)
	preyA := moveByName(t, e, 1, "Order Prey A", state.ZBattlefield)
	preyB := moveByName(t, e, 1, "Order Prey B", state.ZBattlefield)
	// Precondition: two DISTINCT opposing battlefield creatures, so the
	// ordering ask has more than one subject to order (Forge only orders a
	// repeatCards list of size > 1).
	if preyA == preyB {
		t.Fatalf("precondition: both prey resolved to object %d", preyA)
	}
	for _, id := range []state.ObjID{preyA, preyB} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
			t.Fatalf("precondition: prey %d = %+v, want a battlefield creature controlled by seat 1", id, o)
		}
	}

	addMana(t, e, 0, "GGGGGCCC") // {5}{G}{G}{G}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("before the cast, pending = %+v, want priority", d)
	}
	castIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("no cast option for Ezuri's Predation: %+v", d.Options)
	}
	submitChoices(t, e, castIdx)

	d = passUntilResolved(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "repeat_choose_order" {
		t.Fatalf("Ezuri's Predation did not pose the ChooseOrder ask before its loop: %+v", d)
	}
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("order ask Min/Max = %d/%d, want 2/2 over the two opposing creatures", d.Min, d.Max)
	}
	// The ChangeZoneTable$ bracket opens on the first pass, BEFORE the
	// ordering ask suspends: the answered ask re-enters with firstPass
	// consumed, so a bracket opened after the ask would never open at all
	// and the card's ChangesZoneAll batching would silently become
	// per-move. Ezuri's Predation carries both parameters (measured in the
	// corpus), so this is its real pairing, not a synthetic one.
	if !e.zoneBatchOpen {
		t.Fatalf("zone batch not open while the order ask pends: the ChangeZoneTable$ bracket must open before the ChooseOrder ask suspends")
	}
	offered := map[state.ObjID]int{}
	for _, o := range d.Options {
		offered[o.Obj] = o.Index
	}
	if _, ok := offered[preyA]; !ok {
		t.Fatalf("order ask does not offer Order Prey A (%d): %+v", preyA, d.Options)
	}
	if _, ok := offered[preyB]; !ok {
		t.Fatalf("order ask does not offer Order Prey B (%d): %+v", preyB, d.Options)
	}
	// Answer in the reverse of the offered (scan) order: the loop must use
	// the answer, not the scan order.
	first, second := preyA, preyB
	if offered[preyA] > offered[preyB] {
		first, second = preyB, preyA
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{offered[second], offered[first]}}); err != nil {
		t.Fatalf("submit reversed order: %v", err)
	}
	passUntilResolved(t, e, 40)

	// The loop ran one iteration per subject: exactly two Beast tokens.
	beasts := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate && ev.Text == ezuriBeastStem {
			beasts++
		}
	}
	if beasts != 2 {
		t.Fatalf("Beast tokens created = %d, want 2 (one iteration per ordered subject)", beasts)
	}
}
