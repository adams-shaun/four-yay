package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestExchangeLifeSettlesAfterLichDrawSuspendsOnDredge is the finding-t2
// MAJOR repro: a consumed GainLife→Draw replacement body (Lich) that itself
// suspends on a Dredge ask must not orphan the exchange transaction. The
// losing side (seat 0) is emitted first and settles; the gaining side
// (seat 1) is consumed by Lich, whose draw seats a Dredge ask and suspends.
// When that ask is answered, the exchange must still finish: no side applied
// silently, no half state.
func TestExchangeLifeSettlesAfterLichDrawSuspendsOnDredge(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lich := mustCorpusCard(t, reg, "Lich")
	thug := mustCorpusCard(t, reg, "Golgari Thug")
	e := newSeats(t, 2)
	// Seat 1 is the exchanging opponent and the gaining side, so Lich must be
	// on seat 1's battlefield (CR 616.1: the affected player's replacement).
	lichID := onBoardCard(t, e, 1, lich)
	if o := e.G.Obj(lichID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Lich not on seat 1's battlefield: %+v", o)
	}
	// A dredger in seat 1's graveyard so Lich's draw suspends on a Dredge ask.
	thugObj := e.G.AddObject(thug, 1)
	thugID := thugObj.ID
	e.emit(events.Event{Kind: events.MoveZone, Obj: thugID, From: thugObj.Zone, To: state.ZGraveyard})
	if o := e.G.Obj(thugID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: dredger not in graveyard: %+v", o)
	}
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 20 {
		t.Fatalf("precondition: lives = %d/%d, want 20/20", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	// Make the lives differ so the exchange has work to do: seat 1 at 15.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -5})
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 15 {
		t.Fatalf("precondition: lives = %d/%d, want distinct 20/15", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	handBefore := len(e.G.Zone(state.ZHand, 1))
	ctx := &effects.Ctx{Source: lichID, Controller: 0, ExchangeNumberBound: true}
	e.pending = nil
	e.ExchangeLife(events.Event{Kind: events.LifeChange, Player: 0, Amount: -5},
		events.Event{Kind: events.LifeChange, Player: 1, Amount: 5}, 0, 20, ctx, true)
	// The loss side applied (20→15); the gain side was consumed by Lich, whose
	// first draw must now be suspended on a Dredge ask.
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "dredge" {
		t.Fatalf("pending after the exchange = %+v, want Lich's dredge ask", d)
	}
	// Decline every dredge ask: each of Lich's 5 draws poses its own ask (the
	// thug stays in the graveyard), and the exchange transaction must still
	// complete after the last one is answered.
	tasks := 0
	for d != nil && d.Kind == decision.KModes && d.ResumeKind == "dredge" {
		tasks++
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
			Choices: []int{len(d.Options) - 1}}); err != nil {
			t.Fatalf("submit dredge decline %d: %v", tasks, err)
		}
		d = e.Pending()
	}
	if tasks != 5 {
		t.Fatalf("Lich posed %d dredge asks for the replaced 5-point gain, want 5", tasks)
	}
	// The exchange: seat 0's 20 became 15; seat 1's +5 gain was consumed by
	// Lich into 5 draws, so seat 1 stays at 15 (no life gained).
	if got0, got1 := e.G.Players[0].Life, e.G.Players[1].Life; got0 != 15 || got1 != 15 {
		t.Fatalf("lives after the suspended exchange = %d/%d, want 15/15", got0, got1)
	}
	if got := len(e.G.Zone(state.ZHand, 1)) - handBefore; got != 5 {
		t.Fatalf("Lich replacement drew %d cards, want 5", got)
	}
	if ctx.ExchangeNumber != 5 {
		t.Fatalf("RememberOwnLoss = %d, want the 5 life seat 0 lost", ctx.ExchangeNumber)
	}
}

// TestExchangeLifeSettlesAfterFirstSideSuspendsOnDredge is the sibling of the
// case above: the FIRST side emitted by ExchangeLife is the one a replacement
// body consumes and suspends on. The original code skipped finishLifeExchange
// whenever the synchronous emit left a decision pending, so the second side
// was never emitted at all. Here the gaining side (the first argument) is
// consumed by Lich; the losing side must still be applied after the dredge
// answers.
func TestExchangeLifeSettlesAfterFirstSideSuspendsOnDredge(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lich := mustCorpusCard(t, reg, "Lich")
	thug := mustCorpusCard(t, reg, "Golgari Thug")
	e := newSeats(t, 2)
	lichID := onBoardCard(t, e, 1, lich)
	if o := e.G.Obj(lichID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Lich not on seat 1's battlefield: %+v", o)
	}
	thugObj := e.G.AddObject(thug, 1)
	e.emit(events.Event{Kind: events.MoveZone, Obj: thugObj.ID, From: thugObj.Zone, To: state.ZGraveyard})
	if o := e.G.Obj(thugObj.ID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: dredger not in graveyard: %+v", o)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -5})
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 15 {
		t.Fatalf("precondition: lives = %d/%d, want distinct 20/15", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	handBefore := len(e.G.Zone(state.ZHand, 1))
	ctx := &effects.Ctx{Source: lichID, Controller: 0, ExchangeNumberBound: true}
	e.pending = nil
	// The GAIN is the first argument, so it is the synchronous emit that
	// suspends; the loss (second) is only emitted by the parked continuation.
	e.ExchangeLife(events.Event{Kind: events.LifeChange, Player: 1, Amount: 5},
		events.Event{Kind: events.LifeChange, Player: 0, Amount: -5}, 0, 20, ctx, true)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "dredge" {
		t.Fatalf("pending after the exchange = %+v, want Lich's dredge ask", d)
	}
	tasks := 0
	for d != nil && d.Kind == decision.KModes && d.ResumeKind == "dredge" {
		tasks++
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
			Choices: []int{len(d.Options) - 1}}); err != nil {
			t.Fatalf("submit dredge decline %d: %v", tasks, err)
		}
		d = e.Pending()
	}
	if tasks != 5 {
		t.Fatalf("Lich posed %d dredge asks for the replaced 5-point gain, want 5", tasks)
	}
	if got0, got1 := e.G.Players[0].Life, e.G.Players[1].Life; got0 != 15 || got1 != 15 {
		t.Fatalf("lives after the first-side suspended exchange = %d/%d, want 15/15", got0, got1)
	}
	if got := len(e.G.Zone(state.ZHand, 1)) - handBefore; got != 5 {
		t.Fatalf("Lich replacement drew %d cards, want 5", got)
	}
}

// TestMisterNegativeExchangeSettlesOnStackWhenGainReplaced exercises the
// suspended settle through the REAL card path: Mister Negative's ETB resolves
// as a stack object, so the exchange's second side (the opponent's gain) is
// emitted during resolution (e.resume != nil) -- the resumeResolution drain
// arm, not the direct one the two synthetic tests above take. Lich replaces
// that gain with draws whose dredge asks suspend it. The exchange must still
// settle both sides afterwards.
func TestMisterNegativeExchangeSettlesOnStackWhenGainReplaced(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	lich := mustCorpusCard(t, reg, "Lich")
	thug := mustCorpusCard(t, reg, "Golgari Thug")
	e, _ := searchEngine(t, reg, "Mister Negative")
	// Seat 1 is the exchange opponent and the gaining side, so Lich must be on
	// seat 1's battlefield and a dredger must sit in seat 1's graveyard.
	lichID := onBoardCard(t, e, 1, lich)
	if o := e.G.Obj(lichID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Lich not on seat 1's battlefield: %+v", o)
	}
	thugObj := e.G.AddObject(thug, 1)
	e.emit(events.Event{Kind: events.MoveZone, Obj: thugObj.ID, From: thugObj.Zone, To: state.ZGraveyard})
	if o := e.G.Obj(thugObj.ID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: dredger not in graveyard: %+v", o)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -5})
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 15 {
		t.Fatalf("precondition: lives = %d/%d, want distinct 20/15", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	id := searchMoveByName(t, e, "Mister Negative", state.ZBattlefield)
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatal("precondition: Mister Negative not on battlefield")
	}
	dredges, targetPosed := 0, false
	for i := 0; i < 200; i++ {
		d := e.Pending()
		// The exchange settles the moment the ETB leaves the stack; stop at
		// that first idle priority rather than driving on into later turns
		// (whose draw steps pose their own dredge asks and whose cleanup poses
		// a discard ask — both outside this test).
		if targetPosed && len(e.G.Stack) == 0 && d != nil && d.Kind == decision.KPriority {
			break
		}
		if d == nil {
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KTarget:
			idx := indexOfPlayerOption(d, 1)
			if idx < 0 {
				t.Fatalf("opponent not offered: %+v", d.Options)
			}
			targetPosed = true
			submitChoices(t, e, idx)
		case decision.KTriggerOptional, decision.KChoose:
			submitChoices(t, e, 0)
		case decision.KModes:
			if d.ResumeKind != "dredge" {
				t.Fatalf("unexpected modal ask: %+v", d)
			}
			dredges++
			if d.Player != 1 {
				t.Fatalf("dredge ask player = %d, want the gaining seat 1", d.Player)
			}
			submitChoices(t, e, len(d.Options)-1)
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
					break
				}
			}
			if idx < 0 {
				t.Fatal("no pass")
			}
			submitChoices(t, e, idx)
		default:
			t.Fatalf("unexpected ask: %s %+v", d.Kind, d)
		}
	}
	if !targetPosed {
		t.Fatal("precondition: Mister Negative's ETB never posed its target ask")
	}
	if dredges != 5 {
		t.Fatalf("Lich posed %d dredge asks for the replaced 5-point gain, want 5", dredges)
	}
	if got0, got1 := e.G.Players[0].Life, e.G.Players[1].Life; got0 != 15 || got1 != 15 {
		t.Fatalf("lives after the stack exchange = %d/%d, want 15/15", got0, got1)
	}
	if got := len(e.G.Zone(state.ZHand, 1)) - 0; got == 0 {
		t.Fatal("Lich replacement drew no cards for seat 1")
	}
	// The transaction settles, which is what this test is for. Mister
	// Negative's RememberOwnLoss rider (SVar:X Count$RememberedNumber) is a
	// SEPARATE, pre-existing defect when the exchange suspends: the resumed
	// effect chain rebuilds a fresh Ctx, so ExchangeNumberBound is lost and X
	// evaluates to 0. Filed as a follow-up; this test deliberately does not
	// assert the rider value either way.
}
