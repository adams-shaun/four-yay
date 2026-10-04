package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const firstSpellTargetedProbe = "Name:First Spell Probe\nManaCost:1 R\nTypes:Sorcery\nA:SP$ Pump | ValidTgts$ Creature | NumAtt$ +1\nOracle:x\n"

func TestTargetedFirstSpellNotDiscountedAsSecond(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9320, firstSpellTargetedProbe)
	toMain1(t, e)
	mouse := onBoardCard(t, e, 0, corpusCard(t, "Raging Battle Mouse"))
	e.G.Obj(mouse).SummonSick = false
	onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")

	t.Run("planned", func(t *testing.T) {
		// Use a fresh game so this subtest's cast cannot affect the manual case.
		e, _, spell := newFixtureDeck(t, 9320, firstSpellTargetedProbe)
		toMain1(t, e)
		mouse := onBoardCard(t, e, 0, corpusCard(t, "Raging Battle Mouse"))
		e.G.Obj(mouse).SummonSick = false
		lands := []state.ObjID{onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n"), onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")}
		d := firstSpellAskedPriority(t, e)
		a := firstSpellActionFor(d, spell)
		if a == nil {
			t.Fatalf("no payment plan for first spell: %#v", d.PaymentActions)
		}
		if got := a.Plans[0].Cost; got.Generic != 1 || got.Mana[state.MR] != 1 {
			t.Fatalf("first spell offered at %+v, want {1}{R}", got)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: a.Plans[0]}}); err != nil {
			t.Fatalf("submit planned cast: %v", err)
		}
		p := firstSpellAnswerUntilPriority(t, e)
		if p != nil && p.PaymentFallback != nil {
			t.Fatalf("first spell plan fell back (%s)", p.PaymentFallback.Reason)
		}
		if !e.G.Obj(lands[0]).Tapped || !e.G.Obj(lands[1]).Tapped || e.G.Obj(spell).Zone != state.ZStack {
			t.Fatalf("full-cost payment not executed: lands tapped %v/%v, spell zone %s", e.G.Obj(lands[0]).Tapped, e.G.Obj(lands[1]).Tapped, e.G.Obj(spell).Zone)
		}
	})

	t.Run("manual", func(t *testing.T) {
		addMana(t, e, 0, "RR")
		d := e.Pending()
		idx := firstSpellCastOption(d, spell)
		if idx < 0 {
			t.Fatalf("spell not offered with RR: %#v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
			t.Fatalf("cast: %v", err)
		}
		firstSpellAnswerUntilPriority(t, e)
		if e.G.Obj(spell).Zone != state.ZStack {
			t.Fatalf("spell zone %s, want stack", e.G.Obj(spell).Zone)
		}
		if left := e.G.Players[0].Pool.Total(); left != 0 {
			t.Fatalf("first spell charged {R}: %d mana left of RR, want 0", left)
		}
	})
}

func TestTargetedFirstSpellNotDiscountedAsSecond_SecondSpellStillGetsDiscount(t *testing.T) {
	t.Parallel()
	first := "Name:First Probe\nManaCost:R\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	targeted := "Name:Second Probe\nManaCost:1 R\nTypes:Instant\nA:SP$ Pump | ValidTgts$ Creature | NumAtt$ +1\nOracle:x\n"
	e, _, firstID := newFixtureDeck(t, 9321, first, targeted)
	toMain1(t, e)
	var secondID state.ObjID
	for _, id := range append(e.G.Zone(state.ZHand, 0), e.G.Zone(state.ZLibrary, 0)...) {
		if e.G.Obj(id).Face().Name == "Second Probe" {
			secondID = id
		}
	}
	if secondID == 0 {
		t.Fatal("targeted second spell fixture not found")
	}
	if e.G.Obj(secondID).Zone != state.ZHand {
		e.emit(events.Event{Kind: events.MoveZone, Obj: secondID, From: e.G.Obj(secondID).Zone, To: state.ZHand})
	}
	mouse := onBoardCard(t, e, 0, corpusCard(t, "Raging Battle Mouse"))
	e.G.Obj(mouse).SummonSick = false
	addMana(t, e, 0, "RR")
	d := e.Pending()
	idx := firstSpellCastOption(d, firstID)
	if idx < 0 {
		t.Fatalf("first spell not offered: %#v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("cast first spell: %v", err)
	}
	firstSpellAnswerUntilPriority(t, e)
	if e.G.Obj(firstID).Zone != state.ZStack || e.G.Players[0].Pool.Total() != 1 {
		t.Fatalf("first spell setup: zone=%s pool=%d; want first spell on stack and one {R} left", e.G.Obj(firstID).Zone, e.G.Players[0].Pool.Total())
	}
	d = e.Pending()
	idx = firstSpellCastOption(d, secondID)
	if idx < 0 {
		t.Fatalf("discounted targeted second spell not offered with remaining {R}: %#v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("cast second spell: %v", err)
	}
	firstSpellAnswerUntilPriority(t, e)
	if e.G.Obj(secondID).Zone != state.ZStack || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("second spell zone=%s pool=%d; expected discounted {R} payment", e.G.Obj(secondID).Zone, e.G.Players[0].Pool.Total())
	}
}

func TestCostCompositionExcludesOnlyCurrentPushForAnnouncedX(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9322, firstSpellTargetedProbe)
	toMain1(t, e)
	mouse := onBoardCard(t, e, 0, corpusCard(t, "Raging Battle Mouse"))
	if e.G.Obj(mouse).Zone != state.ZBattlefield {
		t.Fatal("precondition: Raging Battle Mouse must be on the battlefield")
	}
	e.cast = &pendingCast{card: spell, ability: -1}
	defer func() { e.cast = nil }()
	cost := Cost{Generic: 1, Colored: state.Mana{state.MR: 1}}
	priceX := func() Cost {
		t.Helper()
		return e.costModifiersForTargetsX(0, spell, spellScope(""), nil, 1).Apply(cost)
	}

	// The in-flight targeted cast is already in the log. Announced-X pricing
	// must exclude this one push, or the first spell incorrectly costs only R.
	e.L.Append(events.Event{Kind: events.PutOnStack, Player: 0, Obj: spell})
	if got := priceX(); got.Generic != 1 || got.Colored[state.MR] != 1 {
		t.Fatalf("first announced-X cast repriced to %+v, want {1}{R}", got)
	}
	// A prior cast of the same object remains part of the count: only the
	// latest (in-flight) push is excluded, so this is the second spell.
	e.L.Append(events.Event{Kind: events.PutOnStack, Player: 0, Obj: spell})
	if got := priceX(); got.Generic != 0 || got.Colored[state.MR] != 1 {
		t.Fatalf("second announced-X cast repriced to %+v, want {R}", got)
	}
}

func TestSplitCardFirstSpellPaysFullCost(t *testing.T) {
	t.Parallel()
	coward := corpusCard(t, "Coward")
	cfg := seatZeroStart(Config{Seed: 9330, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append([]*cards.Card{coward}, mountainDeck(t, 39)...), mountainDeck(t, 40)}, Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	var spell state.ObjID
	for _, id := range append(e.G.Zone(state.ZHand, 0), e.G.Zone(state.ZLibrary, 0)...) {
		if e.G.Obj(id).Face().Name == "Coward" {
			spell = id
		}
	}
	if spell == 0 {
		t.Fatal("Coward fixture not found")
	}
	toMain1(t, e)
	if e.G.Obj(spell).Zone != state.ZHand {
		e.emit(events.Event{Kind: events.MoveZone, Obj: spell, From: e.G.Obj(spell).Zone, To: state.ZHand})
	}
	mouse := onBoardCard(t, e, 0, corpusCard(t, "Raging Battle Mouse"))
	e.G.Obj(mouse).SummonSick = false
	addMana(t, e, 0, "RR")
	d := e.Pending()
	idx := firstSpellCastOption(d, spell)
	if idx < 0 {
		t.Fatalf("Coward not offered with RR: %#v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("cast Coward: %v", err)
	}
	firstSpellAnswerUntilPriority(t, e)
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("Coward zone %s, want stack", e.G.Obj(spell).Zone)
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("first Coward spell charged {R}: %d mana left of RR, want 0", left)
	}
}

func firstSpellAskedPriority(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %#v, want priority", d)
	}
	// Payment actions are published lazily (aph-lazy-offers): build them
	// for this ask, as an opted-in consumer would.
	e.EnsurePaymentActions()
	return d
}

func firstSpellActionFor(d *decision.Decision, obj state.ObjID) *decision.PaymentAction {
	for i := range d.PaymentActions {
		if d.PaymentActions[i].Cast.Object == obj && len(d.PaymentActions[i].Plans) > 0 {
			return &d.PaymentActions[i]
		}
	}
	return nil
}

func firstSpellCastOption(d *decision.Decision, obj state.ObjID) int {
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == obj && o.Mode == "" {
			return o.Index
		}
	}
	return -1
}

func firstSpellAnswerUntilPriority(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 8; i++ {
		p := e.Pending()
		if p == nil || p.Kind == decision.KPriority || p.PaymentFallback != nil {
			return p
		}
		if err := e.Submit(decision.Intent{Seq: p.Seq, Player: p.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("answer %s: %v", p.Kind, err)
		}
	}
	return e.Pending()
}
