package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Entry-counter staging (task agent-20260923T084704Z-b2386c25): when an
// entry-characteristic counter grant competes under non-commuting AddCounter
// replacements, CR 616.1's order choice must be answered BEFORE the entry
// folds -- the move carries the finalized amounts in its Pairs payload and
// no observer ever sees the un-replaced entry. Before the staging, the move
// folded first and the counters landed after the answer, unmarked.

func entryStagingAnswerFirst(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending after the entry emit")
	}
	if d.Kind != decision.KChoose {
		t.Fatalf("first ask = %v, want the as-enters election (KChoose)", d.Kind)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("submit election: %v", err)
	}
}

// entryStagingAssertStagedAsk is the staging assertion: the order ask is
// pending while the entry move has NOT folded -- no battlefield MoveZone for
// the object may exist in the log yet.
func entryStagingAssertStagedAsk(t *testing.T, e *Engine, id state.ObjID) uint64 {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement {
		t.Fatalf("second ask = %+v, want the CR 616.1 order choice (KReplacement)", d)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZBattlefield {
			t.Fatalf("entry folded before the order answer: %+v", ev)
		}
	}
	return d.Seq
}

// entryStagingAssertAtomic is the atomicity assertion: the entry MoveZone
// carries the final amount in its Pairs payload, and the ONLY CounterChange
// for the object is the notification-only EntryCounterNotice marker.
func entryStagingAssertAtomic(t *testing.T, e *Engine, id state.ObjID, want int32) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: creature not on the battlefield: %+v", o)
	}
	if got := o.Counter("P1P1"); got != want {
		t.Fatalf("entry counter = %d, want %d", got, want)
	}
	foundMove := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZBattlefield {
			foundMove = true
			if len(ev.Pairs) == 0 {
				t.Fatalf("entry not atomic: move carried no pairs: %+v", ev)
			}
		}
		if ev.Kind == events.CounterChange && ev.Obj == id && ev.Counter == "P1P1" &&
			ev.Text != events.EntryCounterNotice {
			t.Fatalf("entry counter placed as a real event after the move: %+v", ev)
		}
	}
	if !foundMove {
		t.Fatal("precondition: no entry move in the log")
	}
}

// entryStagingRiotUnleash authors a creature carrying BOTH as-enters
// election keywords, so one entry grants TWO +1/+1 counters and the staged
// competition must re-pose after the first answer (the stage's own
// continuation, not a fresh stage).
func entryStagingRiotUnleash(t testing.TB) *cards.Card {
	return card(t, "Name:Staging Riot Unleash\nTypes:Creature Goblin Berserker\nPT:2/2\nK:Riot\nK:Unleash\nOracle:x\n")
}

func TestEntryCounterStagingSecondGrantReposesTheStage(t *testing.T) {
	t.Parallel()
	scales := tokenReplCorpusCard(t, "Hardened Scales")
	be := tokenReplCorpusCard(t, "Branching Evolution")
	cre := entryStagingRiotUnleash(t)
	e, cfg := tokenReplGame(t, 225, scales, be, cre)
	sid := moveSeededCard(t, e, 0, scales, state.ZBattlefield)
	bid := moveSeededCard(t, e, 0, be, state.ZBattlefield)
	if e.G.Obj(sid) == nil || e.G.Obj(bid) == nil {
		t.Fatal("precondition: the competing modifiers are not on the battlefield")
	}
	e.SetCounterAdder(0)
	cid := moveSeededCard(t, e, 0, cre, state.ZHand)
	if e.G.Obj(cid).Zone != state.ZHand {
		t.Fatal("precondition: creature not in hand")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: cid, From: state.ZHand, To: state.ZBattlefield})
	// Two as-enters elections (riot, then unleash -- the stage forms only
	// after the LAST election, so the grant set is final), then two order
	// asks (one per grant, the second a re-pose of the same stage). Every
	// answer takes index 0: the counter election, then Scales-first, whose
	// deterministic arithmetic lands (1 -> 2 -> 4) per grant: 8 total.
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("ask %d missing", i+1)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("submit ask %d: %v", i+1, err)
		}
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KReplacement {
		t.Fatalf("both grants settled; no order ask may remain: %+v", d)
	}
	entryStagingAssertAtomic(t, e, cid, 8)
	replayCheck(t, e, cfg)
}

// TestEntryCounterStagingOrderIsDecidedBeforeEntry drives the staged ask with
// BOTH answers: the finalized amount differs by the order (Scales first:
// 1 -> 2 -> 4; Branching Evolution first: 1 -> 2 -> 3), so a test that only
// ever saw one answer could not tell a real order choice from a fixed one.
func TestEntryCounterStagingOrderIsDecidedBeforeEntry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		pick  int
		want  int32
		order string
	}{{"scales-first", 0, 4, "plus one, then twice"}, {"evolution-first", 1, 3, "twice, then plus one"}} {
		t.Run(tc.name, func(t *testing.T) {
			scales := tokenReplCorpusCard(t, "Hardened Scales")
			be := tokenReplCorpusCard(t, "Branching Evolution")
			riot := tokenReplCorpusCard(t, "Zhur-Taa Goblin")
			e, cfg := tokenReplGame(t, 211, scales, be, riot)
			sid := moveSeededCard(t, e, 0, scales, state.ZBattlefield)
			bid := moveSeededCard(t, e, 0, be, state.ZBattlefield)
			if e.G.Obj(sid) == nil || e.G.Obj(sid).Zone != state.ZBattlefield ||
				e.G.Obj(bid) == nil || e.G.Obj(bid).Zone != state.ZBattlefield {
				t.Fatal("precondition: the competing modifiers are not on the battlefield")
			}
			e.SetCounterAdder(0)
			rid := moveSeededCard(t, e, 0, riot, state.ZHand)
			if e.G.Obj(rid).Zone != state.ZHand {
				t.Fatal("precondition: riot creature not in hand")
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: rid, From: state.ZHand, To: state.ZBattlefield})
			entryStagingAnswerFirst(t, e)
			seq := entryStagingAssertStagedAsk(t, e, rid)
			if err := e.Submit(decision.Intent{Seq: seq, Player: e.Pending().Player, Choices: []int{tc.pick}}); err != nil {
				t.Fatalf("submit order answer: %v", err)
			}
			entryStagingAssertAtomic(t, e, rid, tc.want)
			replayCheck(t, e, cfg)
		})
	}
}

// TestEntryCounterStagingUnleashElectionStagesToo drives the Unleash path
// through the same stage: the election is answered, then the non-commuting
// counter competition asks, and only then does the entry fold atomically.
func TestEntryCounterStagingUnleashElectionStagesToo(t *testing.T) {
	scales := tokenReplCorpusCard(t, "Hardened Scales")
	be := tokenReplCorpusCard(t, "Branching Evolution")
	cre := unleashCard(t, "Rakdos Cackler")
	e, cfg := tokenReplGame(t, 213, scales, be, cre)
	sid := moveSeededCard(t, e, 0, scales, state.ZBattlefield)
	bid := moveSeededCard(t, e, 0, be, state.ZBattlefield)
	if e.G.Obj(sid) == nil || e.G.Obj(bid) == nil {
		t.Fatal("precondition: the competing modifiers are not on the battlefield")
	}
	e.SetCounterAdder(0)
	cid := moveSeededCard(t, e, 0, cre, state.ZHand)
	if e.G.Obj(cid).Zone != state.ZHand {
		t.Fatal("precondition: cackler not in hand")
	}
	enterWithUnleashChoice(t, e, cid, state.ZHand, true)
	entryStagingAssertStagedAsk(t, e, cid)
	d := e.Pending()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("submit order answer: %v", err)
	}
	entryStagingAssertAtomic(t, e, cid, 4)
	replayCheck(t, e, cfg)
}

// entryStagingCounterLock authors the "no counters on permanents" static
// the zero-loyalty canary needs: Solemnity itself does NOT reach
// planeswalkers (its own ValidCard$ list names artifacts, creatures,
// enchantments and lands only), so the walker-suppression shape uses an
// authored CantPutCounter static that does.
func entryStagingCounterLock(t testing.TB) *cards.Card {
	return card(t, "Name:Entry Counter Lock\nTypes:Enchantment\n"+
		"S:Mode$ CantPutCounter | AffectedZone$ Battlefield | ValidCard$ Permanent.inZoneBattlefield | Description$ Counters can't be put on permanents.\n"+
		"Oracle:x\n")
}

// TestEntryCounterStagingZeroLoyaltySBASeesCompletedEntry is the brief's SBA
// canary: a walker whose starting-loyalty placement is prevented enters AT
// ZERO and the zero-loyalty SBA sees the COMPLETED entry -- the walker is
// swept, never parked at printed loyalty. The un-prevented precondition
// variant pins that the entry path places the counters at all.
func TestEntryCounterStagingZeroLoyaltySBASeesCompletedEntry(t *testing.T) {
	t.Parallel()
	walker := entryCounterWalker(t)
	e, cfg := tokenReplGame(t, 215, walker)
	wid := moveSeededCard(t, e, 0, walker, state.ZBattlefield)
	if o := e.G.Obj(wid); o == nil || o.Zone != state.ZBattlefield || o.Counter("LOYALTY") != 4 {
		t.Fatalf("precondition: un-prevented entry = %+v, want the battlefield with 4 loyalty", o)
	}
	replayCheck(t, e, cfg)

	lock := entryStagingCounterLock(t)
	e, cfg = tokenReplGame(t, 217, lock, walker)
	lid := moveSeededCard(t, e, 0, lock, state.ZBattlefield)
	if e.G.Obj(lid) == nil || e.G.Obj(lid).Zone != state.ZBattlefield {
		t.Fatal("precondition: counter lock not on the battlefield")
	}
	wid = moveSeededCard(t, e, 0, walker, state.ZBattlefield)
	// CR 704.4: state-based actions run when the engine next reaches the
	// priority boundary -- drive there so the sweep actually runs.
	e.Advance()
	if o := e.G.Obj(wid); o != nil && o.Zone == state.ZBattlefield {
		t.Fatalf("a 0-loyalty walker survived the SBA: %+v", o)
	}
	entered := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == wid && ev.To == state.ZBattlefield {
			entered = true
		}
	}
	if !entered {
		t.Fatal("precondition: the walker's entry move is absent (it must enter and THEN be swept)")
	}
	replayCheck(t, e, cfg)
}

// TestEntryCounterStagingVorinclexHalvesOpponentWalker and the Doubling
// Season riot half below are the commuting single-replacement guards the
// brief names: a lone modifier settles in the pre-pass with no ask, and the
// entry still folds atomically.
func TestEntryCounterStagingVorinclexHalvesOpponentWalker(t *testing.T) {
	t.Parallel()
	vori := tokenReplCorpusCard(t, "Vorinclex, Monstrous Raider")
	walker := entryCounterWalker(t)
	e, cfg := tokenReplGameSeats(t, 219, []*cards.Card{vori}, []*cards.Card{walker})
	vid := moveSeededCard(t, e, 0, vori, state.ZBattlefield)
	if o := e.G.Obj(vid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Vorinclex not on the battlefield")
	}
	e.SetCounterAdder(1)
	wid := moveSeededCard(t, e, 1, walker, state.ZBattlefield)
	o := e.G.Obj(wid)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: walker not on the battlefield: %+v", o)
	}
	if got := o.Counter("LOYALTY"); got != 2 {
		t.Fatalf("opponent walker entry loyalty = %d, want 2 (printed 4 halved by the opponent's Vorinclex)", got)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == wid && ev.To == state.ZBattlefield && len(ev.Pairs) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("halved entry was not folded into the move atomically")
	}
	replayCheck(t, e, cfg)
}

// TestEntryCounterStagingDoublingSeasonDoublesRiotElection is the brief's
// Doubling Season half. Season's line carries EffectOnly$ True, so the entry
// must ride a REAL cast -- the resolving spell on the stack is the effect the
// line admits -- which is why this test casts the goblin instead of emitting
// its entry move directly. A single commuting modifier settles in the
// pre-pass with no order ask, and the doubled entry still folds atomically.
func TestEntryCounterStagingDoublingSeasonDoublesRiotElection(t *testing.T) {
	t.Parallel()
	ds := tokenReplCorpusCard(t, "Doubling Season")
	riot := tokenReplCorpusCard(t, "Zhur-Taa Goblin")
	e, cfg := tokenReplGame(t, 221, ds, riot)
	did := moveSeededCard(t, e, 0, ds, state.ZBattlefield)
	if o := e.G.Obj(did); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Doubling Season not on the battlefield")
	}
	rid := moveSeededCard(t, e, 0, riot, state.ZHand)
	// Settle Season's own entry triggers so the priority ask below offers
	// the cast options.
	e.pending = nil
	e.Advance()
	passUntilStackEmpty(t, e, 20)
	addMana(t, e, 0, "RG")
	submitChoices(t, e, castCardOption(t, e, rid).Index)
	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("post-cast ask = %+v, want the riot election (KChoose)", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("submit election: %v", err)
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KReplacement {
		t.Fatalf("a single commuting modifier must not pose an order ask: %+v", d)
	}
	entryStagingAssertAtomic(t, e, rid, 2)
	replayCheck(t, e, cfg)
}

// entryStagingTapUntapWalker authors a planeswalker whose entry carries TWO
// Updated Moved replacements -- one tapping, one untapping. The bodies fight
// over the same tapped bit, so the all-Updated composition parks CR 616.1's
// order choice over the MOVE itself; the answer's resume re-folds the move.
// This is the resume the staged entry must survive: before the fix the
// resume emitted the move RAW, which silently dropped the entry grants -- a
// walker entering under the park landed at zero loyalty and died to the SBA.
func entryStagingTapUntapWalker(t testing.TB) *cards.Card {
	return card(t, "Name:Staging Tap Untap Walker\nTypes:Planeswalker Entry\nLoyalty:4\n"+
		"R:Event$ Moved | Destination$ Battlefield | ReplacementResult$ Updated | ValidCard$ Card.Self | ReplaceWith$ TapIt | Description$ it enters tapped\n"+
		"SVar:TapIt:DB$ Tap | Defined$ Self\n"+
		"R:Event$ Moved | Destination$ Battlefield | ReplacementResult$ Updated | ValidCard$ Card.Self | ReplaceWith$ UntapIt | Description$ it enters untapped\n"+
		"SVar:UntapIt:DB$ Untap | Defined$ Self\n"+
		"Oracle:x\n")
}

func TestEntryCounterStagingUpdatedParkKeepsEntryGrants(t *testing.T) {
	t.Parallel()
	walker := entryStagingTapUntapWalker(t)
	e, cfg := tokenReplGame(t, 223, walker)
	wid := moveSeededCard(t, e, 0, walker, state.ZHand)
	if e.G.Obj(wid).Zone != state.ZHand {
		t.Fatal("precondition: walker not in hand")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: wid, From: state.ZHand, To: state.ZBattlefield})
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement {
		t.Fatalf("precondition: tap-vs-untap competition did not pose the order ask: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("submit order answer: %v", err)
	}
	o := e.G.Obj(wid)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("walker did not survive the entry: %+v", o)
	}
	if got := o.Counter("LOYALTY"); got != 4 {
		t.Fatalf("walker entry loyalty = %d, want 4 (the parked entry's grants must survive the order answer)", got)
	}
	replayCheck(t, e, cfg)
}
