package rules

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Announce-then-pay (docs/superpowers/specs/2026-09-27-announce-then-pay.md):
// rules-level coverage of the Intent.Announce selector and the announced
// CR 601.2g window's options, Auto-fill, Undo last tap and Cancel cast.

const (
	apIsland   = "Name:Island\nTypes:Basic Land Island\nOracle:x\n"
	apSwamp    = "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n"
	apMountain = "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n"
)

// announceAction returns the pending priority decision's payment action for
// spell, building the lazy extension as a consuming seat would.
func announceAction(t *testing.T, e *Engine, spell state.ObjID) (*decision.Decision, decision.PaymentAction) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %#v, want priority", d)
	}
	for _, a := range e.EnsurePaymentActions() {
		if a.Cast.Object == spell {
			return d, a
		}
	}
	t.Fatalf("no payment action for spell %d in %#v", spell, d.PaymentActions)
	return nil, decision.PaymentAction{}
}

// announce submits Intent.Announce for spell and returns the window.
func announce(t *testing.T, e *Engine, spell state.ObjID) *decision.Decision {
	t.Helper()
	d, a := announceAction(t, e, spell)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Announce: &decision.AnnounceSelection{ActionID: a.ID}}); err != nil {
		t.Fatalf("Submit announce: %v", err)
	}
	w := e.Pending()
	if w == nil || w.Kind != decision.KChoose || w.ManaPayment == nil {
		t.Fatalf("after announce pending = %#v, want the announced mana window", w)
	}
	return w
}

// windowOption finds the window's option by kind and (for kind "mana")
// source and colour.
func apOption(t *testing.T, d *decision.Decision, kind string, obj state.ObjID, colour string) decision.Option {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == kind && (obj == 0 || o.Obj == obj) && (colour == "" || o.ManaSymbol == colour) {
			return o
		}
	}
	t.Fatalf("no %s option obj=%d colour=%q in %#v", kind, obj, colour, d.Options)
	return decision.Option{}
}

func hasWindowOption(d *decision.Decision, kind string) bool {
	for _, o := range d.Options {
		if o.Kind == kind {
			return true
		}
	}
	return false
}

func apAnswer(t *testing.T, e *Engine, o decision.Option) {
	t.Helper()
	d := e.Pending()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
		t.Fatalf("Submit %s: %v", o.Kind, err)
	}
}

func lastDecisionMade(e *Engine, prefix string) string {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		if ev := e.L.Events[i]; ev.Kind == events.DecisionMade && strings.HasPrefix(ev.Text, prefix) {
			return ev.Text
		}
	}
	return ""
}

// A Bolt with only a Mountain: CAST (announce) opens the window, tapping the
// Mountain pays and completes the cast with no further click.
func TestAnnouncePayBoltTapMountainCompletesCast(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9301, "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	mountain := onBoard(t, e, 0, apMountain)
	// Re-ask as the live engine does after an eventless fixture change.
	e.pending = nil
	e.askPriority(0)
	legacy := append([]decision.Option(nil), e.Pending().Options...)
	d, a := announceAction(t, e, spell)
	if !reflect.DeepEqual(d.Options, legacy) || !reflect.DeepEqual(e.legalActions(0), legacy) {
		t.Fatal("building the payment extension changed the legacy options")
	}
	if a.BaseOptionIndex != nil || len(a.Plans) != 1 {
		t.Fatalf("action = %#v, want one plan-only cast", a)
	}
	w := announce(t, e, spell)
	if w.Prompt != "Pay for Bolt" || w.Source != spell || w.ManaPayment.Card != spell {
		t.Fatalf("window header = %q source %d card %d", w.Prompt, w.Source, w.ManaPayment.Card)
	}
	r := state.ManaIndex('R')
	if w.ManaPayment.Cost.Mana[r] != 1 || w.ManaPayment.Owed.Mana[r] != 1 || w.ManaPayment.Pool != (decision.ManaAmount{}) {
		t.Fatalf("readout = %+v, want cost R owed R pool empty", *w.ManaPayment)
	}
	if !reflect.DeepEqual(w.ManaPayment.AutoFill, []state.ObjID{mountain}) {
		t.Fatalf("autofill = %v, want the Mountain", w.ManaPayment.AutoFill)
	}
	kinds := make([]string, len(w.Options))
	for i, o := range w.Options {
		kinds[i] = o.Kind
	}
	if want := []string{"mana", decision.OptAutoFill, decision.OptCancelCast}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("window kinds = %v, want %v", kinds, want)
	}
	if got := lastDecisionMade(e, "priority:"); got != "priority:[];announce:"+a.ID {
		t.Fatalf("DecisionMade = %q", got)
	}
	if got := e.G.Obj(spell).Zone; got != state.ZStack {
		t.Fatalf("announced spell zone = %s, want stack (CR 601.2a)", got)
	}
	apAnswer(t, e, apOption(t, w, "mana", mountain, ""))
	if !e.G.Obj(mountain).Tapped || e.G.Obj(spell).Zone != state.ZStack || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("after tap: tapped=%v zone=%s pool=%v", e.G.Obj(mountain).Tapped, e.G.Obj(spell).Zone, e.G.Players[0].Pool)
	}
	if e.cast != nil {
		t.Fatal("cast still in progress after the cost was covered")
	}
	if p := e.Pending(); p == nil || p.Kind != decision.KPriority {
		t.Fatalf("pending after cast = %#v, want priority", p)
	}
}

// A {B}{B}{U} spell over two Swamps and a Swamp/Island dual: the dual's two
// abilities are separate options, and choosing its {U} pays the {U} pip.
func TestAnnouncePayDualLandOffersEachAbility(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9302, "Name:Three Pip\nManaCost:B B U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	s1 := onBoard(t, e, 0, apSwamp)
	s2 := onBoard(t, e, 0, apSwamp)
	dual := onBoard(t, e, 0, "Name:Sea Dual\nTypes:Land Island Swamp\nOracle:x\n")
	w := announce(t, e, spell)
	var dualCols []string
	for _, o := range w.Options {
		if o.Kind == "mana" && o.Obj == dual {
			dualCols = append(dualCols, o.ManaSymbol+"|"+o.Label)
		}
	}
	if len(dualCols) != 2 {
		t.Fatalf("dual options = %v, want one per ability", dualCols)
	}
	b, u := state.ManaIndex('B'), state.ManaIndex('U')
	if w.ManaPayment.Cost.Mana[b] != 2 || w.ManaPayment.Cost.Mana[u] != 1 {
		t.Fatalf("cost = %+v", w.ManaPayment.Cost)
	}
	var dualU decision.Option
	for _, o := range w.Options {
		if o.Kind == "mana" && o.Obj == dual && strings.HasSuffix(o.Label, "U") {
			dualU = o
		}
	}
	if dualU.Kind == "" {
		t.Fatalf("no {U} option on the dual: %v", dualCols)
	}
	apAnswer(t, e, dualU)
	w = e.Pending()
	if w.ManaPayment == nil || e.G.Players[0].Pool[u] != 1 || w.ManaPayment.Owed.Mana[u] != 0 || w.ManaPayment.Owed.Mana[b] != 2 {
		t.Fatalf("after dual U: pool=%v readout=%+v", e.G.Players[0].Pool, w.ManaPayment)
	}
	apAnswer(t, e, apOption(t, w, "mana", s1, ""))
	apAnswer(t, e, apOption(t, e.Pending(), "mana", s2, ""))
	if e.G.Obj(spell).Zone != state.ZStack || e.cast != nil || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("cast did not complete: zone=%s cast=%v pool=%v", e.G.Obj(spell).Zone, e.cast != nil, e.G.Players[0].Pool)
	}
}

// Produced$ Any flattens to one option per colour for a planner-tier source.
func TestAnnouncePayAnyColourSourceFlattens(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9303, "Name:Green Spell\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	birds := onBoard(t, e, 0, "Name:Bird Test\nTypes:Creature Bird\nPT:0/1\nA:AB$ Mana | Cost$ T | Produced$ Any | SpellDescription$ Add one mana of any color.\nOracle:x\n")
	e.G.Obj(birds).SummonSick = false
	e.pending = nil
	e.askPriority(0)
	w := announce(t, e, spell)
	var cols []string
	for _, o := range w.Options {
		if o.Kind == "mana" && o.Obj == birds {
			cols = append(cols, o.ManaSymbol)
		}
	}
	if want := []string{"W", "U", "B", "R", "G"}; !reflect.DeepEqual(cols, want) {
		t.Fatalf("birds colours = %v, want %v", cols, want)
	}
	apAnswer(t, e, apOption(t, w, "mana", birds, "G"))
	if e.G.Obj(spell).Zone != state.ZStack || e.cast != nil {
		t.Fatal("green spell not cast from the flattened {G}")
	}
}

// Undo last tap reverses the most recent reversible activation: the source
// untaps, its mana leaves the pool, one ManaUndo per ManaAdd is logged, and
// the log still replays. A wrong tap can then be replaced by the right one.
func TestAnnouncePayUndoLastTap(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9304, "Name:Blue Two\nManaCost:1 U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	island := onBoard(t, e, 0, apIsland)
	swamp := onBoard(t, e, 0, apSwamp)
	w := announce(t, e, spell)
	if hasWindowOption(w, decision.OptUndoTap) {
		t.Fatal("undo offered before any tap")
	}
	apAnswer(t, e, apOption(t, w, "mana", swamp, ""))
	w = e.Pending()
	undo := apOption(t, w, decision.OptUndoTap, swamp, "")
	if undo.Label != "Undo tapping Swamp" {
		t.Fatalf("undo label = %q", undo.Label)
	}
	apAnswer(t, e, undo)
	w = e.Pending()
	if e.G.Obj(swamp).Tapped || e.G.Players[0].Pool.Total() != 0 || w == nil || w.ManaPayment == nil {
		t.Fatalf("after undo: swamp tapped=%v pool=%v pending=%#v", e.G.Obj(swamp).Tapped, e.G.Players[0].Pool, w)
	}
	if hasWindowOption(w, decision.OptUndoTap) {
		t.Fatal("undo still offered with nothing left to undo")
	}
	undos := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.ManaUndo {
			undos++
			if ev.Obj != swamp || ev.Counter != "B" || ev.Amount != 1 {
				t.Fatalf("ManaUndo = %+v", ev)
			}
		}
	}
	if undos != 1 {
		t.Fatalf("ManaUndo events = %d, want 1", undos)
	}
	// The swamp is offered again; pay with it and the Island.
	apAnswer(t, e, apOption(t, w, "mana", island, ""))
	apAnswer(t, e, apOption(t, e.Pending(), "mana", swamp, ""))
	if e.G.Obj(spell).Zone != state.ZStack || e.cast != nil {
		t.Fatal("cast did not complete after undo")
	}
	// The refund is not a spend: exactly the {1}{U} paid counts as mana
	// spent to cast the spell.
	if got := e.manaSpentForCast(0, spell); got != 2 {
		t.Fatalf("mana spent for cast = %d, want 2 (a ManaUndo must not read as spending)", got)
	}
}

// A source whose activation does more than tap and add mana (a damage rider)
// is not undoable.
func TestAnnouncePayUndoWithheldForRiderSource(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9305, "Name:Blue Two\nManaCost:1 U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, apIsland)
	pain := onBoard(t, e, 0, "Name:Pain Test\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ B | SubAbility$ DBPain | SpellDescription$ Add B.\nSVar:DBPain:DB$ DealDamage | Defined$ You | NumDmg$ 1\nOracle:x\n")
	w := announce(t, e, spell)
	apAnswer(t, e, apOption(t, w, "mana", pain, ""))
	w = e.Pending()
	if w == nil || w.ManaPayment == nil {
		t.Fatalf("pending = %#v, want the window", w)
	}
	if hasWindowOption(w, decision.OptUndoTap) {
		t.Fatal("undo offered for an activation that dealt damage")
	}
}

// Cancel cast reverses reversible taps and the cast (CR 733.1): the spell is
// back in hand, the land untapped, the pool empty, and the cast is offered
// again at the next priority.
func TestAnnouncePayCancelCast(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9306, "Name:Blue Two\nManaCost:1 U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	island := onBoard(t, e, 0, apIsland)
	onBoard(t, e, 0, apSwamp)
	w := announce(t, e, spell)
	apAnswer(t, e, apOption(t, w, "mana", island, ""))
	apAnswer(t, e, apOption(t, e.Pending(), decision.OptCancelCast, 0, ""))
	if e.G.Obj(spell).Zone != state.ZHand || e.G.Obj(island).Tapped || e.G.Players[0].Pool.Total() != 0 || e.cast != nil {
		t.Fatalf("after cancel: zone=%s island tapped=%v pool=%v cast=%v",
			e.G.Obj(spell).Zone, e.G.Obj(island).Tapped, e.G.Players[0].Pool, e.cast != nil)
	}
	d, a := announceAction(t, e, spell)
	if d.Player != 0 || len(a.Plans) == 0 {
		t.Fatalf("cancelled cast not offered again: %#v", a)
	}
}

// Cancel keeps an irreversible activation's mana floating and leaves its
// source tapped; only the reversible ones after it could be reversed.
func TestAnnouncePayCancelKeepsIrreversibleActivation(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9307, "Name:Blue Two\nManaCost:1 U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, apIsland)
	pain := onBoard(t, e, 0, "Name:Pain Test\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ B | SubAbility$ DBPain | SpellDescription$ Add B.\nSVar:DBPain:DB$ DealDamage | Defined$ You | NumDmg$ 1\nOracle:x\n")
	w := announce(t, e, spell)
	apAnswer(t, e, apOption(t, w, "mana", pain, ""))
	apAnswer(t, e, apOption(t, e.Pending(), decision.OptCancelCast, 0, ""))
	if e.G.Obj(spell).Zone != state.ZHand || !e.G.Obj(pain).Tapped || e.G.Players[0].Pool[state.ManaIndex('B')] != 1 {
		t.Fatalf("after cancel: zone=%s pain tapped=%v pool=%v", e.G.Obj(spell).Zone, e.G.Obj(pain).Tapped, e.G.Players[0].Pool)
	}
}

// Auto-fill pays the remainder with the planner's activations, exactly the
// sources the readout named.
func TestAnnouncePayAutoFill(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9308, "Name:Three Mixed\nManaCost:1 U B\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	island := onBoard(t, e, 0, apIsland)
	onBoard(t, e, 0, apSwamp)
	onBoard(t, e, 0, apMountain)
	w := announce(t, e, spell)
	// Pay the {U} by hand first; Auto-fill covers the rest.
	apAnswer(t, e, apOption(t, w, "mana", island, ""))
	w = e.Pending()
	fill := apOption(t, w, decision.OptAutoFill, 0, "")
	sources := append([]state.ObjID(nil), w.ManaPayment.AutoFill...)
	if len(sources) != 2 || !strings.HasPrefix(fill.Label, "Auto-fill: tap ") {
		t.Fatalf("autofill = %v label %q", sources, fill.Label)
	}
	apAnswer(t, e, fill)
	if e.G.Obj(spell).Zone != state.ZStack || e.cast != nil || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("autofill did not complete the cast: zone=%s pool=%v", e.G.Obj(spell).Zone, e.G.Players[0].Pool)
	}
	for _, id := range sources {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("autofill source %d not tapped", id)
		}
	}
}

// The window is posed even with no untapped source left, so a caster who
// tapped a dual for the wrong colour can still undo or cancel; the legacy
// window would fall through to payment and reverse the cast.
func TestAnnouncePayWindowStaysOpenWithNoSourceLeft(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9309, "Name:Sea Spell\nManaCost:U B\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	dual := onBoard(t, e, 0, "Name:Sea Dual\nTypes:Land Island Swamp\nOracle:x\n")
	swamp := onBoard(t, e, 0, apSwamp)
	w := announce(t, e, spell)
	var dualB decision.Option
	for _, o := range w.Options {
		if o.Kind == "mana" && o.Obj == dual && strings.HasSuffix(o.Label, "B") {
			dualB = o
		}
	}
	apAnswer(t, e, dualB)
	apAnswer(t, e, apOption(t, e.Pending(), "mana", swamp, ""))
	w = e.Pending()
	if w == nil || w.ManaPayment == nil || e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("pending = %#v, want the window with the spell still on the stack", w)
	}
	for _, o := range w.Options {
		if o.Kind == "mana" {
			t.Fatalf("a source is still offered: %#v", o)
		}
	}
	if w.ManaPayment.Owed.Mana[state.ManaIndex('U')] != 1 {
		t.Fatalf("owed = %+v, want {U}", w.ManaPayment.Owed)
	}
	apAnswer(t, e, apOption(t, w, decision.OptUndoTap, swamp, ""))
	apAnswer(t, e, apOption(t, e.Pending(), decision.OptUndoTap, dual, ""))
	w = e.Pending()
	var dualU decision.Option
	for _, o := range w.Options {
		if o.Kind == "mana" && o.Obj == dual && strings.HasSuffix(o.Label, "U") {
			dualU = o
		}
	}
	apAnswer(t, e, dualU)
	apAnswer(t, e, apOption(t, e.Pending(), "mana", swamp, ""))
	if e.cast != nil || e.G.Obj(spell).Zone != state.ZStack || e.G.Players[0].Pool.Total() != 0 {
		t.Fatal("cast did not complete after undoing both taps")
	}
}

// Rejections leave the decision pending and the log untouched.
func TestAnnouncePayRejectsMalformedSelectors(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9311, "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, apMountain)
	d, a := announceAction(t, e, spell)
	events0, intents0 := len(e.L.Events), len(e.L.Intents)
	bad := []decision.Intent{
		{Seq: d.Seq, Player: d.Player, Announce: &decision.AnnounceSelection{ActionID: "nope"}},
		{Seq: d.Seq, Player: d.Player, Announce: &decision.AnnounceSelection{}},
		{Seq: d.Seq, Player: d.Player, Choices: []int{0}, Announce: &decision.AnnounceSelection{ActionID: a.ID}},
		{Seq: d.Seq, Player: d.Player, Announce: &decision.AnnounceSelection{ActionID: a.ID},
			Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: a.Plans[0]}},
		{Seq: d.Seq + 1, Player: d.Player, Announce: &decision.AnnounceSelection{ActionID: a.ID}},
		{Seq: d.Seq, Player: 1, Announce: &decision.AnnounceSelection{ActionID: a.ID}},
	}
	for i, in := range bad {
		if err := e.Submit(in); err == nil {
			t.Fatalf("bad announce %d accepted", i)
		}
	}
	if len(e.L.Events) != events0 || len(e.L.Intents) != intents0 || e.Pending() != d {
		t.Fatal("a rejected announce changed the engine")
	}
	// An action with no plan cannot be announced.
	cp := d.Clone()
	cp.PaymentActions[0].Plans = nil
	if err := cp.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Announce: &decision.AnnounceSelection{ActionID: a.ID}}); err == nil {
		t.Fatal("planless action announced")
	}
}

// A clone taken inside the window answers identically: the tap records and
// the announce flag travel with the cast, and the two engines' heads agree.
func TestAnnouncePayCloneAndReexecuteAreDeterministic(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9312, "Name:Blue Two\nManaCost:1 U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	island := onBoard(t, e, 0, apIsland)
	swamp := onBoard(t, e, 0, apSwamp)
	w := announce(t, e, spell)
	apAnswer(t, e, apOption(t, w, "mana", swamp, ""))
	c := e.Clone()
	for _, eng := range []*Engine{e, c} {
		apAnswer(t, eng, apOption(t, eng.Pending(), decision.OptUndoTap, swamp, ""))
		apAnswer(t, eng, apOption(t, eng.Pending(), "mana", island, ""))
		apAnswer(t, eng, apOption(t, eng.Pending(), "mana", swamp, ""))
	}
	if e.L.Head() != c.L.Head() || !reflect.DeepEqual(e.G, c.G) {
		t.Fatal("clone diverged from the live engine inside the announced window")
	}
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatal("cast did not complete")
	}
}

// A cast begun from the legacy option or a planned selector never sees the
// announced window's fields or kinds.
func TestAnnouncePayLegacyWindowUnchanged(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9313, "Name:Blue Two\nManaCost:1 U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, apIsland)
	onBoard(t, e, 0, apSwamp)
	// Float {U} only, so the cast is a legacy option the pool cannot finish:
	// the legacy window opens for the remaining {1}.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 1})
	e.pending = nil
	e.askPriority(0)
	var cast decision.Option
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == spell {
			cast = o
		}
	}
	if cast.Kind == "" {
		t.Fatal("setup: no legacy cast option with two floating U")
	}
	// Raise nothing; just drain one U so the window opens.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: -1})
	d := e.Pending()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{cast.Index}}); err != nil {
		t.Fatalf("legacy cast: %v", err)
	}
	w := e.Pending()
	if w == nil || w.Kind != decision.KChoose {
		t.Fatalf("pending = %#v, want the legacy window", w)
	}
	if w.ManaPayment != nil {
		t.Fatal("legacy window carries the announced readout")
	}
	for _, o := range w.Options {
		if o.Kind != "activate" && o.Kind != "done" {
			t.Fatalf("legacy window offers %q", o.Kind)
		}
	}
}
