package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The CounterType$ EachFromSource PutCounter shape (task eachfromsource):
// the copy-each-kind-of-counter body -- "put those counters on target
// permanent" -- over the corpus's three referents (TriggeredCardLKICopy,
// Self, Remembered). Resourceful Defense is the deck carrier that filed the
// ticket; The Ozolith, Denry Klin and Heroic Sacrifice pin the other
// referent/destination combinations; every test drives the REAL corpus card
// end to end.

func mustCorpusCardT(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus missing %s", name)
	}
	return c
}

// TestResourcefulDefenseCopiesEachCounterKind is the brief's Done test: a
// 2/2 with two +1/+1 counters and a charge counter leaves the battlefield;
// the target permanent gains exactly two +1/+1 and one charge counter.
// Before the fix the CounterType$ EachFromSource put placed NOTHING (not a
// real counter kind), so the trigger resolved onto an untouched target.
func TestResourcefulDefenseCopiesEachCounterKind(t *testing.T) {
	t.Parallel()
	rd := mustCorpusCardT(t, "Resourceful Defense")
	leaver := card(t, "Name:Leaving Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	recvr := card(t, "Name:Receiving Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGame(t, 447, rd, leaver, recvr)
	moveSeededCard(t, e, 0, rd, state.ZBattlefield)
	srcID := moveSeededCard(t, e, 0, leaver, state.ZBattlefield)
	tgtID := moveSeededCard(t, e, 0, recvr, state.ZBattlefield)

	// Preconditions, asserted so a vacuous setup fails loudly: the leaver
	// carries the two kinds, the target carries none.
	e.emit(events.Event{Kind: events.CounterChange, Obj: srcID, Counter: "P1P1", Amount: 2})
	e.emit(events.Event{Kind: events.CounterChange, Obj: srcID, Counter: "CHARGE", Amount: 1})
	e.pending = nil
	if got := e.G.Obj(srcID).Counter("P1P1"); got != 2 {
		t.Fatalf("precondition: leaver P1P1 = %d, want 2", got)
	}
	if got := e.G.Obj(srcID).Counter("CHARGE"); got != 1 {
		t.Fatalf("precondition: leaver CHARGE = %d, want 1", got)
	}
	if got := e.G.Obj(tgtID).Counter("P1P1"); got != 0 || e.G.Obj(tgtID).Counter("CHARGE") != 0 {
		t.Fatalf("precondition: receiver starts countered (%d/%d)", e.G.Obj(tgtID).Counter("P1P1"), e.G.Obj(tgtID).Counter("CHARGE"))
	}

	// The permanent leaves the battlefield: the trigger queues, its
	// resolution asks for the target permanent, and the copy lands.
	e.emit(events.Event{Kind: events.MoveZone, Obj: srcID, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the trigger's KTarget decision, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == tgtID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the receiver not offered as the trigger's target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(tgtID).Counter("P1P1"); got != 2 {
		t.Fatalf("EachFromSource copy: receiver P1P1 = %d, want 2", got)
	}
	if got := e.G.Obj(tgtID).Counter("CHARGE"); got != 1 {
		t.Fatalf("EachFromSource copy: receiver CHARGE = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestTheOzolithCollectsTheLeftCounters pins the Defined$ Self destination:
// The Ozolith's own trigger has NO target ask (Defined$ Self), so the copy
// must land with no decision at all -- a KTarget here would wedge a test
// that never answers it, and a zero-count Ozolith after the move is the
// pre-fix silence this shape exists to disprove.
func TestTheOzolithCollectsTheLeftCounters(t *testing.T) {
	t.Parallel()
	oz := mustCorpusCardT(t, "The Ozolith")
	leaver := card(t, "Name:Leaving Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGame(t, 449, oz, leaver)
	ozID := moveSeededCard(t, e, 0, oz, state.ZBattlefield)
	srcID := moveSeededCard(t, e, 0, leaver, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: srcID, Counter: "P1P1", Amount: 3})
	e.pending = nil
	if got := e.G.Obj(ozID).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition: Ozolith starts countered (%d)", got)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: srcID, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)

	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("Defined$ Self must not ask for a target: %+v", d)
	}
	if got := e.G.Obj(ozID).Counter("P1P1"); got != 3 {
		t.Fatalf("Ozolith P1P1 = %d, want 3 (copied from the leaver's LKI)", got)
	}
	replayCheck(t, e, cfg)
}

// TestAmbitiousAugmenterCountersRideOntoTheFractalToken pins the Remembered
// destination on a REAL corpus death trigger (the same SVar body shape the
// parameter ratchet's stale Heroic Sacrifice entry names, whose own carrier
// path -- DelayedTrigger Mode$ ChangesZone -- is separately unimplemented):
// the dying creature's counters land on the 0/0 Fractal token, which
// therefore survives its own toughness-0 moment, the card's whole point.
func TestAmbitiousAugmenterCountersRideOntoTheFractalToken(t *testing.T) {
	t.Parallel()
	aa := mustCorpusCardT(t, "Ambitious Augmenter")
	e, cfg := tokenReplGame(t, 453, aa)
	aaID := moveSeededCard(t, e, 0, aa, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: aaID, Counter: "P1P1", Amount: 2})
	e.emit(events.Event{Kind: events.CounterChange, Obj: aaID, Counter: "CHARGE", Amount: 1})
	e.pending = nil
	if got := e.G.Obj(aaID).Counter("P1P1"); got != 2 {
		t.Fatalf("precondition: Augmenter P1P1 = %d, want 2", got)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: aaID, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)

	// The Fractal token: seat 0's battlefield must hold a 0/0 Fractal now
	// carrying exactly two +1/+1 and one charge counter.
	tokID := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o == nil || !o.IsToken || o.Face() == nil || o.Face().Name != "Fractal Token" {
			continue
		}
		if tokID != 0 {
			t.Fatalf("more than one Fractal token on the battlefield")
		}
		tokID = id
	}
	if tokID == 0 {
		t.Fatalf("no Fractal token on the battlefield after the death trigger")
	}
	if got := e.G.Obj(tokID).Counter("P1P1"); got != 2 {
		t.Fatalf("Fractal token P1P1 = %d, want 2", got)
	}
	if got := e.G.Obj(tokID).Counter("CHARGE"); got != 1 {
		t.Fatalf("Fractal token CHARGE = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestZackFairBequeathsCountersThroughTheSacrificeLKI pins the third ladder
// rung: Zack Fair's own ability sacrifices him as its cost, so his live
// counters are gone when the body reads EachFromSource$ Self -- the
// cost-sacrifice LKI snapshot (SacrificedInfo.Counters, captured at the
// instant of the sacrifice) is the only place they still exist.
func TestZackFairBequeathsCountersThroughTheSacrificeLKI(t *testing.T) {
	t.Parallel()
	zack := mustCorpusCardT(t, "Zack Fair")
	recvr := card(t, "Name:Heir Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGame(t, 455, zack, recvr)
	zackID := moveSeededCard(t, e, 0, zack, state.ZBattlefield)
	tgtID := moveSeededCard(t, e, 0, recvr, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: zackID, Counter: "P1P1", Amount: 2})
	e.emit(events.Event{Kind: events.CounterChange, Obj: zackID, Counter: "CHARGE", Amount: 1})
	e.pending = nil
	// Precondition: Zack entered with his printed K:etbCounter +1/+1 already
	// folded, so the sacrifice-time snapshot holds 3 P1P1 (1 entry + 2 added)
	// and 1 CHARGE.
	if got := e.G.Obj(zackID).Counter("P1P1"); got != 3 || e.G.Obj(zackID).Counter("CHARGE") != 1 {
		t.Fatalf("precondition: Zack counters %d P1P1 / %d CHARGE, want 3/1",
			got, e.G.Obj(zackID).Counter("CHARGE"))
	}
	if got := e.G.Obj(tgtID).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition: heir starts countered (%d)", got)
	}

	// {1}, Sacrifice Zack Fair: addMana funds the generic, the activation
	// targets the heir, the resolution pays the sacrifice and runs.
	addMana(t, e, 0, "1")
	opt := abilityOption(t, e, zackID, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Zack's KTarget decision, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == tgtID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the heir not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(tgtID).Counter("P1P1"); got != 3 {
		t.Fatalf("heir P1P1 = %d, want 3 (Zack's sacrifice LKI)", got)
	}
	if got := e.G.Obj(tgtID).Counter("CHARGE"); got != 1 {
		t.Fatalf("heir CHARGE = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestBlueLoyalRaptorEnteringDinosaurCarriesHisKinds pins the ReplacedCard
// referent and the ETB$ True mid-entry placement (the one corpus carrier
// with a non-default CounterNum$ multiplier): each OTHER Dinosaur you
// control enters with a counter of each kind Blue has. The entering
// dinosaur is mid-entry when the replacement body runs, so only the ETB$
// True path can place on it at all.
func TestBlueLoyalRaptorEnteringDinosaurCarriesHisKinds(t *testing.T) {
	t.Parallel()
	blue := mustCorpusCardT(t, "Blue, Loyal Raptor")
	dino := card(t, "Name:Arriving Dinosaur\nTypes:Creature Dinosaur\nPT:3/3\nOracle:x\n")
	e, cfg := tokenReplGame(t, 457, blue, dino)
	blueID := moveSeededCard(t, e, 0, blue, state.ZBattlefield)
	// Blue's Partner-with ETB must settle before this fixture tests its
	// replacement. Owen is absent, so target the controller; its no-candidate
	// search resolves without an additional decision. Leaving the trigger
	// pending would make the generic drain below encounter its target ask.
	if !e.putTriggersOnStack() {
		t.Fatal("Blue Partner-with ETB did not enter the trigger drain")
	}
	targetAsk := e.Pending()
	if targetAsk == nil || targetAsk.Kind != decision.KTarget {
		t.Fatalf("Blue Partner-with ETB decision = %+v, want target player", targetAsk)
	}
	self := -1
	for _, o := range targetAsk.Options {
		if o.Kind == "player" && o.Player == 0 {
			self = o.Index
		}
	}
	if self < 0 {
		t.Fatalf("Blue Partner-with ETB offers no controller target: %+v", targetAsk.Options)
	}
	submitChoices(t, e, self)
	passUntilStackEmpty(t, e, 20)
	e.emit(events.Event{Kind: events.CounterChange, Obj: blueID, Counter: "P1P1", Amount: 2})
	e.emit(events.Event{Kind: events.CounterChange, Obj: blueID, Counter: "CHARGE", Amount: 1})
	e.pending = nil
	if got := e.G.Obj(blueID).Counter("P1P1"); got != 2 || e.G.Obj(blueID).Counter("CHARGE") != 1 {
		t.Fatalf("precondition: Blue counters %d P1P1 / %d CHARGE, want 2/1",
			e.G.Obj(blueID).Counter("P1P1"), e.G.Obj(blueID).Counter("CHARGE"))
	}

	dinoID := moveSeededCard(t, e, 0, dino, state.ZBattlefield)
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(dinoID).Counter("P1P1"); got != 2 {
		t.Fatalf("entering dinosaur P1P1 = %d, want 2 (each kind Blue had)", got)
	}
	if got := e.G.Obj(dinoID).Counter("CHARGE"); got != 1 {
		t.Fatalf("entering dinosaur CHARGE = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}
