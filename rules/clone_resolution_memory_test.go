package rules

import (
	"reflect"
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

func TestCloneOwnsTheSuspendedResolutionsFlipMemory(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	asker := card(t, flipAskFlipSrc)
	exercised := false
	for seed := uint64(1); seed <= 12; seed++ {
		ref := flipAskFlipAtAsk(t, reg, asker, seed)
		cloneMemDrain(t, ref, 100)

		e := flipAskFlipAtAsk(t, reg, asker, seed)
		if e.resume == nil || e.resume.flipMemory == nil {
			t.Fatalf("seed %d precondition: the ask's resume frame carries no flip memory (%+v)", seed, e.resume)
		}
		mem := e.resume.flipMemory
		before := *mem
		before.Results = append([]effects.FlipResult(nil), mem.Results...)
		c := e.Clone()
		cloneMemDrain(t, c, 100)
		if !reflect.DeepEqual(*mem, before) || e.resume == nil || e.resume.flipMemory != mem {
			t.Fatalf("seed %d: driving the clone through its post-ask flip changed the ORIGINAL's flip memory: %+v -> %+v",
				seed, before, *mem)
		}
		cloneMemDrain(t, e, 100)
		if e.L.Head() != ref.L.Head() || c.L.Head() != ref.L.Head() {
			t.Fatalf("seed %d: heads original %s clone %s uncloned reference %s", seed, e.L.Head(), c.L.Head(), ref.L.Head())
		}
		if len(flipNotes(ref)) == 2 && flipNotes(ref)[1] {
			exercised = true
		}
	}
	if !exercised {
		t.Fatal("precondition: no seed's post-ask flip was a win, so the shared tally was never written")
	}
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

// exchangeMemoriesOf collects every ExchangeLife rider memory the engine's
// suspended resolution can write or read: each resume frame's and the parked
// transaction's.
func exchangeMemoriesOf(e *Engine) []*effects.ExchangeMemory {
	var out []*effects.ExchangeMemory
	for rp := e.resume; rp != nil; rp = rp.outer {
		if rp.exchangeMemory != nil {
			out = append(out, rp.exchangeMemory)
		}
	}
	return out
}

func TestCloneOwnsTheSuspendedExchangeLifeMemory(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	ref := misterNegativeAtDredge(t, reg)
	hand0 := len(ref.G.Zone(state.ZHand, 0))
	cloneMemDrain(t, ref, 200)
	if got := len(ref.G.Zone(state.ZHand, 0)) - hand0; got != 5 {
		t.Fatalf("precondition: the uncloned rider drew %d, want 5", got)
	}

	e := misterNegativeAtDredge(t, reg)
	mems := exchangeMemoriesOf(e)
	if len(mems) == 0 {
		t.Fatal("precondition: the suspended resolution carries no ExchangeLife memory")
	}
	if mems[0].Number != 0 {
		t.Fatalf("precondition: the rider is already settled (%d) at the dredge ask", mems[0].Number)
	}
	c := e.Clone()
	for _, m := range exchangeMemoriesOf(c) {
		for _, om := range mems {
			if m == om {
				t.Fatal("the clone's resume frame shares the original's ExchangeLife memory")
			}
		}
	}
	cHand0 := len(c.G.Zone(state.ZHand, 0))
	cloneMemDrain(t, c, 200)
	for _, m := range mems {
		if m.Number != 0 {
			t.Fatalf("settling the CLONE's exchange wrote %d into the ORIGINAL's rider memory", m.Number)
		}
	}
	// The clone's settle and its resumed rider must share ONE copy: the
	// transaction's write is what the resumed SubAbility$ draw reads.
	if got := len(c.G.Zone(state.ZHand, 0)) - cHand0; got != 5 {
		t.Fatalf("the clone's rider drew %d, want 5 (its parked transaction and resume frame lost their shared memory)", got)
	}
	cloneMemDrain(t, e, 200)
	if e.L.Head() != ref.L.Head() || c.L.Head() != ref.L.Head() {
		t.Fatalf("heads original %s clone %s uncloned reference %s", e.L.Head(), c.L.Head(), ref.L.Head())
	}
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

// TestClonePreservesResumeFrameIdentity pins the identity half of the remap:
// handleReplacement compares a parked choice's resumeAtPose against
// Engine.resume by POINTER (rules/replacement_choice.go), and a resumed
// resolution's frames share one flip memory. A clone that copied each
// reference separately -- or shared the original's -- would answer the
// comparison differently from the original (or adopt the original's frame).
func TestClonePreservesResumeFrameIdentity(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	mem := &effects.FlipMemory{Results: []effects.FlipResult{{Player: 0, Heads: true}}}
	outer := &resumePoint{kind: "outer", flipMemory: mem}
	rp := &resumePoint{kind: "inner", flipMemory: mem, outer: outer}
	e.resume = rp
	e.replChoices = []replChoice{{inResolution: true, resumeAtPose: rp}}
	c := e.Clone()
	if c.resume == rp || c.replChoices[0].resumeAtPose == rp {
		t.Fatal("the clone shares the original's resume frame")
	}
	if c.replChoices[0].resumeAtPose != c.resume {
		t.Fatal("the clone's parked choice no longer names the clone's own resume frame")
	}
	if c.resume.flipMemory == mem || c.resume.outer.flipMemory != c.resume.flipMemory {
		t.Fatalf("flip memory: clone shares original's %v; clone's frames share one copy %v",
			c.resume.flipMemory == mem, c.resume.outer.flipMemory == c.resume.flipMemory)
	}
	c.resume.flipMemory.Results = append(c.resume.flipMemory.Results, effects.FlipResult{Player: 1})
	if len(mem.Results) != 1 {
		t.Fatal("a write through the clone's flip memory reached the original's")
	}
}
