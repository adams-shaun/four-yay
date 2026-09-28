package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestEntryCounterNoticePublishesAdder(t *testing.T) {
	t.Parallel()
	walker := entryCounterWalker(t)
	e, cfg := tokenReplGame(t, 9401, walker)
	id := moveSeededCard(t, e, 0, walker, state.ZHand)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: walker not in hand: %+v", o)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Counter("LOYALTY") != 4 {
		t.Fatalf("precondition: walker entry = %+v, want battlefield with 4 loyalty", o)
	}
	assertEntryCounterLedger(t, e, id, "LOYALTY", 4, 0)
	replayCheck(t, e, cfg)
}

func assertEntryCounterLedger(t *testing.T, e *Engine, id state.ObjID, kind string, amount int32, actor state.PlayerID) {
	t.Helper()
	for _, row := range e.counterAddsThisTurn {
		if row.object.ID == id && row.kind == kind && row.amount == amount && row.actor == actor {
			return
		}
	}
	t.Fatalf("entry %s placement (%d) absent from ledger with seat %d as adder: %+v", kind, amount, actor, e.counterAddsThisTurn)
}

// TestCastEntryCounterProvenanceNotifiesTriggerAndLedger is the brief's
// headline walk: a REAL cast of an authored Loyalty:4 planeswalker under
// Doubling Season enters with the doubled 8 loyalty AND the settled entry
// notification carries the entrant's controller as adder, so an opponent's
// CounterPlayerAddedAll / ValidSource$ Opponent batch trigger fires on it and
// the Count$CountersAddedThisTurn ledger records the LOYALTY placement with
// the entrant's controller as actor.
func TestCastEntryCounterProvenanceNotifiesTriggerAndLedger(t *testing.T) {
	t.Parallel()
	ds := tokenReplCorpusCard(t, "Doubling Season")
	walker := entryCounterWalker(t)
	patron := card(t, "Name:Entry Patron\nTypes:Creature\nPT:1/1\n"+
		"T:Mode$ CounterPlayerAddedAll | ValidSource$ Opponent | TriggerZones$ Battlefield | Execute$ Draw\n"+
		"SVar:Draw:DB$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n")
	e, cfg := tokenReplGameSeats(t, 9402, []*cards.Card{ds, walker}, []*cards.Card{patron})
	moveSeededCard(t, e, 0, ds, state.ZBattlefield)
	pid := moveSeededCard(t, e, 1, patron, state.ZBattlefield)
	if len(e.G.Obj(pid).Face().Triggers) == 0 {
		t.Fatal("precondition: opponent trigger source has no parsed trigger")
	}
	if o := e.G.Obj(pid); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatal("precondition: patron not on opponent battlefield")
	}
	wid := moveSeededCard(t, e, 0, walker, state.ZHand)
	toMain1(t, e)
	e.priorityRound()
	castSpellOption(t, e, "Entry Walker")
	passPriorityOnce(t, e)
	passPriorityOnce(t, e)
	o := e.G.Obj(wid)
	if o == nil || o.Zone != state.ZBattlefield || o.Counter("LOYALTY") != 8 {
		t.Fatalf("precondition: cast walker = %+v, want battlefield with 8 loyalty", o)
	}
	assertEntryCounterLedger(t, e, wid, "LOYALTY", 8, 0)
	fired := false
	for _, pt := range e.pendingTriggers {
		if pt.Source == pid {
			fired = true
			break
		}
	}
	for _, sid := range e.G.Stack {
		if o := e.G.Obj(sid); o != nil && o.Source == pid {
			fired = true
			break
		}
	}
	if !fired {
		t.Fatalf("opponent CounterPlayerAddedAll trigger did not see doubled entry placement; pending=%+v stack=%v", e.pendingTriggers, e.G.Stack)
	}
	replayCheck(t, e, cfg)
}

// TestDoublingSeasonSagaEntryAndProgressionCounts pins the two Saga halves
// the brief measured: a Saga cast under Doubling Season enters with 2 LORE
// (the entry grant rides the cast's cause, the same precedent that doubles a
// planeswalker's starting loyalty), while the turn-based chapter-progression
// lore counter stays +1 -- it is a turn-based action with no stack cause, so
// the AddCounter matcher's EffectOnly$ gate keeps Doubling Season off it.
func TestDoublingSeasonSagaEntryAndProgressionCounts(t *testing.T) {
	t.Parallel()
	ds := tokenReplCorpusCard(t, "Doubling Season")
	// Three chapters so the doubled 2-lore entry (2 < 3) survives the CR
	// 704.5v sacrifice long enough to measure the turn-based counter.
	saga := card(t, "Name:Triage Saga\nTypes:Enchantment Saga\nK:Chapter:3:D1,D2,D3\n"+
		"SVar:D1:DB$ Draw | Defined$ You | NumCards$ 1\n"+
		"SVar:D2:DB$ Draw | Defined$ You | NumCards$ 1\n"+
		"SVar:D3:DB$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n")
	e, cfg := tokenReplGame(t, 9404, ds, saga)
	moveSeededCard(t, e, 0, ds, state.ZBattlefield)
	id := moveSeededCard(t, e, 0, saga, state.ZHand)
	toMain1(t, e)
	e.priorityRound()
	castSpellOption(t, e, "Triage Saga")
	passPriorityOnce(t, e)
	passPriorityOnce(t, e)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Counter("LORE") != 2 {
		t.Fatalf("precondition: Saga entry = %+v, want battlefield with doubled 2 lore", o)
	}
	// The entry's chapter trigger must be off the stack before the turn-based
	// counter runs: advanceSagas models the after-your-draw-step action, which
	// happens with nothing resolving (actionCause()==0). Drain first so the
	// EffectOnly$ gate reads the real turn-based provenance rather than an
	// in-flight chapter trigger.
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(id).Counter("LORE"); got != 2 {
		t.Fatalf("precondition: chapter trigger drained, saga lore = %d, want 2 before progression", got)
	}
	e.advanceSagas(0)
	if got := e.G.Obj(id).Counter("LORE"); got != 3 {
		t.Fatalf("chapter progression produced %d lore, want 3 (the entry's 2 plus one UN-doubled)", got)
	}
	replayCheck(t, e, cfg)
}

func TestEntryBodyCounterFinalEmitPublishesAdder(t *testing.T) {
	t.Parallel()
	creature := entryCounterEtbCreature(t)
	e, cfg := tokenReplGame(t, 9403, creature)
	id := moveSeededCard(t, e, 0, creature, state.ZHand)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != 2 {
		t.Fatalf("precondition: recipient = %+v, want 2 counters", o)
	}
	assertEntryCounterLedger(t, e, id, "P1P1", 2, 0)
	replayCheck(t, e, cfg)
}

// TestEntryBodyRewrittenFinalEmitPublishesAdder pins the OTHER final-emit
// face (emitAddCounterReplacement): a NON-ABSORBABLE Moved body (it carries
// a SubAbility$, so entryBodyAbsorbable is false) places counters inside the
// replacement window, and two non-commuting AddCounter replacements (Hardened
// Scales +1, Branching Evolution double) force that placement through the
// parked CR 616.1 order choice whose answer finishes at
// emitAddCounterReplacement. By then the body window has closed, so the face
// must republish the adder captured when the competition was posed or the
// placement is attributed to nobody and no Count$CountersAddedThisTurn ledger
// row appears (measured: empty ledger, patron silent).
func TestEntryBodyRewrittenFinalEmitPublishesAdder(t *testing.T) {
	t.Parallel()
	hs := tokenReplCorpusCard(t, "Hardened Scales")
	be := tokenReplCorpusCard(t, "Branching Evolution")
	patron := card(t, "Name:Rewritten Patron\nTypes:Creature\nPT:1/1\n"+
		"T:Mode$ CounterPlayerAddedAll | ValidObject$ Permanent.inRealZoneBattlefield | ValidSource$ Opponent | TriggerZones$ Battlefield | Execute$ Draw\n"+
		"SVar:Draw:DB$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n")
	cre := card(t, "Name:Rewritten Etb\nTypes:Creature Bear\nPT:2/2\n"+
		"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ ETBC | ReplacementResult$ Updated\n"+
		"SVar:ETBC:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 2 | ETB$ True | SubAbility$ ETBDraw\n"+
		"SVar:ETBDraw:DB$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n")
	e, cfg := tokenReplGameSeats(t, 9408, []*cards.Card{hs, be, cre}, []*cards.Card{patron})
	moveSeededCard(t, e, 0, hs, state.ZBattlefield)
	moveSeededCard(t, e, 0, be, state.ZBattlefield)
	pid := moveSeededCard(t, e, 1, patron, state.ZBattlefield)
	if o := e.G.Obj(pid); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatal("precondition: patron not on opponent battlefield")
	}
	id := moveSeededCard(t, e, 0, cre, state.ZHand)
	toMain1(t, e)
	e.priorityRound()
	castSpellOption(t, e, "Rewritten Etb")
	// Answer the CR 616.1 order ask (plus-then-double or double-then-plus:
	// either way 2 becomes 6) and drain the rest.
	answered := false
	patronFired := false
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		switch d.Kind {
		case decision.KReplacement:
			answerReplacementOrderAsk(t, e)
			answered = true
			// The rewritten emit runs inside the answer's resume; its
			// CounterPlayerAddedAll match is queued synchronously, so read it
			// here before the remaining passes resolve it away.
			for _, sid := range e.G.Stack {
				if s := e.G.Obj(sid); s != nil && s.Source == pid {
					patronFired = true
				}
			}
			for _, pt := range e.pendingTriggers {
				if pt.Source == pid {
					patronFired = true
				}
			}
		case decision.KPriority:
			passPriorityOnce(t, e)
		default:
			i = 40
		}
	}
	if !answered {
		t.Fatal("precondition: the Hardened Scales/Branching Evolution order ask never fired")
	}
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: entrant not on battlefield: %+v", o)
	}
	if got := o.Counter("P1P1"); got != 6 {
		t.Fatalf("precondition: rewritten placement = %d counters, want 6 (2 plus-one then double)", got)
	}
	assertEntryCounterLedger(t, e, id, "P1P1", 6, 0)
	if !patronFired {
		t.Fatalf("opponent CounterPlayerAddedAll trigger did not see the rewritten placement; pending=%+v stack=%v", e.pendingTriggers, e.G.Stack)
	}
	replayCheck(t, e, cfg)
}
