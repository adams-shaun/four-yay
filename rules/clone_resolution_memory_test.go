package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The resolution-scoped memories (effects.FlipMemory, effects.ExchangeMemory)
// are mutated in place by the resolution that owns them, and a suspended
// resolution keeps them on its resume frames (resumePoint.flipMemory /
// exchangeMemory) and on a parked ExchangeLife transaction. A clone taken at
// that suspension must own its own copy: a search clone's hypothetical flip
// or exchange settle otherwise writes straight into the live game's memory.
// Within ONE engine the pointer must stay shared (every frame of the
// resolution reads the same memory), so the clone's copy must preserve that
// identity as well -- the resume frame and the parked transaction of the
// clone name one and the same copy.

// flipAskFlipSrc flips (counting wins), suspends on a real mid-resolution
// ask, flips again into the SAME cumulative tally and then loses life equal
// to it: the post-ask flip is exactly the write that leaked through a shared
// memory.
const flipAskFlipSrc = `Name:Flip Ask Flip
ManaCost:2 R
Types:Creature
PT:1/1
A:AB$ FlipCoin | NoCall$ True | RememberNumber$ Wins | SubAbility$ DBAsk
SVar:DBAsk:DB$ ChoosePlayer | SubAbility$ DBFlip2
SVar:DBFlip2:DB$ FlipCoin | NoCall$ True | RememberNumber$ Wins | SubAbility$ DBLose
SVar:DBLose:DB$ LoseLife | Defined$ You | LifeAmount$ X
SVar:X:Count$RememberedNumber
Oracle:x
`

// cloneMemAnswer is the deterministic answer the tests below give every
// decision, so the original, its clone and an uncloned reference are all
// driven by the same intents.
func cloneMemAnswer(d *decision.Decision) decision.Intent {
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}
	switch {
	case d.Kind == decision.KPriority:
		for _, o := range d.Options {
			if o.Kind == "pass" {
				in.Choices = []int{o.Index}
				break
			}
		}
	case d.Kind == decision.KModes && d.ResumeKind == "dredge":
		in.Choices = []int{d.Options[len(d.Options)-1].Index}
	case len(d.Options) > 0:
		in.Choices = []int{d.Options[0].Index}
	}
	return in
}

// cloneMemDrain answers until the stack is empty at a priority decision.
func cloneMemDrain(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for n := 0; n < limit; n++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			if e.Pending() == nil {
				return
			}
			continue
		}
		if d.Kind == decision.KPriority && len(e.G.Stack) == 0 {
			return
		}
		if err := e.Submit(cloneMemAnswer(d)); err != nil {
			t.Fatalf("submit %s: %v", d.Kind, err)
		}
	}
	t.Fatal("cloneMemDrain did not converge")
}

// flipAskFlipAtAsk builds the Flip Ask Flip game for seed and drives it to
// the ChoosePlayer ask that suspends the resolution between the two flips.
func flipAskFlipAtAsk(t *testing.T, reg *cards.Registry, asker *cards.Card, seed uint64) *Engine {
	t.Helper()
	e, _ := flipEngine(t, reg, seed, []*cards.Card{asker}, nil)
	src := moveByName(t, e, 0, "Flip Ask Flip", state.ZBattlefield)
	e.G.Obj(src).SummonSick = false
	e.priorityRound()
	activateAPIOption(t, e, src, "FlipCoin")
	for n := 0; n < 50; n++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision before the ChoosePlayer ask")
		}
		if d.Kind != decision.KPriority {
			return e
		}
		if err := e.Submit(cloneMemAnswer(d)); err != nil {
			t.Fatalf("pass: %v", err)
		}
	}
	t.Fatal("the ChoosePlayer ask was never posed")
	return nil
}

// misterNegativeAtDredge drives the real Mister Negative ETB (with Lich and a
// dredger on the opponent's side) to the first dredge ask the Lich-replaced
// gain poses while the exchange transaction is parked mid-resolution.
func misterNegativeAtDredge(t *testing.T, reg *cards.Registry) *Engine {
	t.Helper()
	e, _ := searchEngine(t, reg, "Mister Negative")
	onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Lich"))
	thug := e.G.AddObject(mustCorpusCard(t, reg, "Golgari Thug"), 1)
	e.emit(events.Event{Kind: events.MoveZone, Obj: thug.ID, From: thug.Zone, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -5})
	searchMoveByName(t, e, "Mister Negative", state.ZBattlefield)
	for i := 0; i < 200; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		if d.Kind == decision.KModes && d.ResumeKind == "dredge" {
			return e
		}
		in := cloneMemAnswer(d)
		if d.Kind == decision.KTarget {
			in.Choices = []int{indexOfPlayerOption(d, 1)}
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("submit %s: %v", d.Kind, err)
		}
	}
	t.Fatal("no dredge ask was posed")
	return nil
}

// exchangeAtLifeOrderAsk parks an ExchangeLife whose gaining side (seat 1)
// meets two non-commuting GainLife replacements (Lich's draw-instead and Boon
// Reflection's doubling): the CR 616.1 order ask holds the transaction on the
// parked replChoice.
func exchangeAtLifeOrderAsk(t *testing.T, reg *cards.Registry) (*Engine, *effects.ExchangeMemory) {
	t.Helper()
	e := newSeats(t, 2)
	lichID := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Lich"))
	onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Boon Reflection"))
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -5})
	mem := &effects.ExchangeMemory{Bound: true}
	ctx := &effects.Ctx{Source: lichID, Controller: 0, ExchangeMemory: mem}
	e.pending = nil
	e.ExchangeLife(events.Event{Kind: events.LifeChange, Player: 0, Amount: -5},
		events.Event{Kind: events.LifeChange, Player: 1, Amount: 5}, 0, 20, ctx, true)
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement {
		t.Fatalf("pending after the exchange = %+v, want the CR 616.1 order ask", d)
	}
	if len(e.replChoices) == 0 || e.replChoices[0].exchange == nil {
		t.Fatal("precondition: the parked order choice holds no exchange transaction")
	}
	return e, mem
}

func TestCloneOwnsTheParkedLifeChoicesExchange(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	drainAnswers := func(e *Engine) {
		for n := 0; n < 50; n++ {
			d := e.Pending()
			if d == nil || d.Kind == decision.KPriority {
				return
			}
			if err := e.Submit(cloneMemAnswer(d)); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		}
		t.Fatal("the exchange's asks did not settle")
	}
	ref, refMem := exchangeAtLifeOrderAsk(t, reg)
	drainAnswers(ref)

	e, mem := exchangeAtLifeOrderAsk(t, reg)
	tx := e.replChoices[0].exchange
	stage, staged := tx.stage, len(tx.staged)
	c := e.Clone()
	if c.replChoices[0].exchange == tx {
		t.Fatal("the clone's parked order choice shares the original's exchange transaction")
	}
	drainAnswers(c)
	if tx.stage != stage || len(tx.staged) != staged || mem.Number != 0 {
		t.Fatalf("settling the CLONE's exchange changed the ORIGINAL's transaction: stage %d->%d, staged %d->%d, rider %d",
			stage, tx.stage, staged, len(tx.staged), mem.Number)
	}
	drainAnswers(e)
	if e.L.Head() != ref.L.Head() || c.L.Head() != ref.L.Head() {
		t.Fatalf("heads original %s clone %s uncloned reference %s", e.L.Head(), c.L.Head(), ref.L.Head())
	}
	if mem.Number != refMem.Number {
		t.Fatalf("original rider %d, uncloned reference %d", mem.Number, refMem.Number)
	}
}
