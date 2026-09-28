package rules

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The Marvel Super Heroes Commander import's trigger reads, pinned on real
// corpus cards: the four trigger modes the avengers-assemble deck's ratchet
// census named (Cycled, AttackersDeclared, CounterAdded, AttackerBlocked)
// with the mechanics each deck card exercises. Where the flow is a real
// combat/cast decision chain the test drives the engine's own decisions;
// where it is the triggering event's shape alone, the event is emitted
// directly, the way the trigger_referents tests do.

func mshCorpusCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus missing %s", name)
	}
	return c
}

func mshCorpusCardPath(t *testing.T, name, path string) *cards.Card {
	t.Helper()
	c, ds := cards.Parse(filepath.Join("..", ".cards", "cardsfolder", path))
	if len(ds) != 0 {
		t.Fatalf("parse %s: %v", name, ds)
	}
	if ds = c.Link(); len(ds) != 0 {
		t.Fatalf("link %s: %v", name, ds)
	}
	return c
}

// TestCycledTriggerFiresOnTheCycleCostDiscard pins trig:Cycled on Dismantling
// Wave: the cycle activation's cost discard queues the trigger, which resolves
// above the cycling ability (CR 117.5), sweeps artifacts and enchantments but
// not creatures, and the cycle's own draw lands beneath it.
func TestCycledTriggerFiresOnTheCycleCostDiscard(t *testing.T) {
	t.Parallel()
	wave := mshCorpusCard(t, "Dismantling Wave")
	e := handEngine(t, wave)
	ring := onBoard(t, e, 0, "Name:Sol Ring\nManaCost:1\nTypes:Artifact\nOracle:x\n")
	ench := onBoard(t, e, 1, "Name:Wardust Veil\nManaCost:1 W\nTypes:Enchantment\nOracle:x\n")
	bear := onBoard(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	id := e.G.Zone(state.ZHand, 0)[0]
	drawn := e.G.Zone(state.ZLibrary, 0)[0]
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MW] = 6, 2
	var opt decision.Option
	for _, o := range e.legalActions(0) {
		if o.Kind == "ability" && o.Obj == id {
			opt = o
			break
		}
	}
	if opt.Kind != "ability" {
		t.Fatal("cycling activation not offered")
	}
	e.beginActivation(0, opt)
	submitChoices(t, e, 0) // discard the cycling card
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("cycle discard zone = %s, want graveyard", got)
	}
	// The discard emit queued the trigger and Submit's Advance pushed it on
	// top of the resolving cycling ability (the queue drained into the stack
	// before priority was granted).
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack depth after the cycle = %d, want 2 (ability + trigger)", len(e.G.Stack))
	}
	e.resolveTop() // the trigger
	if got := e.G.Obj(ring).Zone; got != state.ZGraveyard {
		t.Fatalf("artifact zone = %s, want graveyard", got)
	}
	if got := e.G.Obj(ench).Zone; got != state.ZGraveyard {
		t.Fatalf("enchantment zone = %s, want graveyard", got)
	}
	if got := e.G.Obj(bear).Zone; got != state.ZBattlefield {
		t.Fatalf("creature zone = %s, want battlefield", got)
	}
	e.resolveTop() // the cycling ability's draw (CR 702.78a)
	if e.G.Obj(drawn).Zone != state.ZHand {
		t.Fatal("cycling did not draw")
	}
}

// TestCycledTriggerDistinguishesDiscardProvenance pins the discriminator: a
// cost discard TAGGED as a cycling ability's cost is a cycle, an effect
// discard of the same card is not, and a cost discard whose cause is NOT a
// cycling ability never queues a Cycled trigger even when the card prints
// Cycling. Synthetic fixtures isolate the event provenance from every other
// mechanic.
func TestCycledTriggerDistinguishesDiscardProvenance(t *testing.T) {
	t.Parallel()
	const src = "Name:Cycle Watcher\nManaCost:U\nTypes:Creature\nPT:1/1\nK:Cycling:U\nOracle:x\nT:Mode$ Cycled | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ When you cycle CARDNAME, draw a card.\nSVar:TrigDraw:DB$ Draw | NumCards$ 1\n"
	e := handEngine(t, card(t, src))
	id := e.G.Zone(state.ZHand, 0)[0]

	// An effect discard is not a cycle.
	e.emit(events.Discard(id, 0))
	if len(e.pendingTriggers) != 0 {
		t.Fatal("an effect discard queued the Cycled trigger")
	}
	// Restore the card to hand the way no real path does, purely to place the
	// next fixture: only the event marker differs from here on.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZHand})

	// A cost discard of a card WITHOUT printed cycling is not a cycle either.
	const plain = "Name:Plain Card\nManaCost:U\nTypes:Instant\nOracle:x\nT:Mode$ Cycled | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ When you cycle CARDNAME, draw a card.\nSVar:TrigDraw:DB$ Draw | NumCards$ 1\n"
	plainID := onBoardCard(t, e, 0, card(t, plain)) // parked on the battlefield only to hold an object id
	e.emit(events.Event{Kind: events.MoveZone, Obj: plainID, From: state.ZBattlefield, To: state.ZHand})
	e.emit(events.DiscardCost(plainID))
	if len(e.pendingTriggers) != 0 {
		t.Fatal("a non-cycling cost discard queued the Cycled trigger")
	}

	// A cost discard whose recorded cause is the cycling ability itself is a
	// cycle. The provenance rides the event (events.DiscardCostCycling), the
	// same tag the real K:Cycling activation emits.
	e.emit(events.DiscardCostCycling(id, "Cycling"))
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("pendingTriggers after the cycle = %d, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if len(e.G.Zone(state.ZHand, 0)) != 1 {
		t.Fatalf("cycled draw: hand = %d, want 1", len(e.G.Zone(state.ZHand, 0)))
	}
}

// TestAttackersDeclaredFiresPerDefenderGroup pins trig:AttackersDeclared on
// Love on the Battlefield: attacking with exactly two creatures grants both
// first strike (the plural TriggeredAttackers referent) and draws a card; one
// or three attackers never queue the trigger. The declared batch is the real
// engine event shape handleAttackers emits (one per defender).
func TestAttackersDeclaredFiresPerDefenderGroup(t *testing.T) {
	t.Parallel()
	love := mshCorpusCardPath(t, "Love on the Battlefield", "l/love_on_the_battlefield.txt")
	for _, tc := range []struct {
		name    string
		attack  int
		wantQ   bool
		wantDrr int
	}{
		{"exactly two", 2, true, 1},
		{"one", 1, false, 0},
		{"three", 3, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := combatEngine(t)
			onBoardCard(t, e, 0, love)
			atk := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
			atk2 := onBoardReady(t, e, 0, "Name:Fox\nManaCost:1 W\nTypes:Creature Fox\nPT:2/2\nOracle:x\n")
			atk3 := onBoardReady(t, e, 0, "Name:Ox\nManaCost:2\nTypes:Creature Ox\nPT:2/2\nOracle:x\n")
			handBefore := len(e.G.Zone(state.ZHand, 0))
			all := []state.ObjID{atk, atk2, atk3}[:tc.attack]
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: all})
			if (len(e.pendingTriggers) > 0) != tc.wantQ {
				t.Fatalf("pendingTriggers = %d, wantQ = %v", len(e.pendingTriggers), tc.wantQ)
			}
			if !tc.wantQ {
				return
			}
			e.putTriggersOnStack()
			e.resolveTop()
			if !e.HasKeyword(atk, "First Strike") || !e.HasKeyword(atk2, "First Strike") {
				t.Fatalf("first strike missing: %v %v", e.HasKeyword(atk, "First Strike"), e.HasKeyword(atk2, "First Strike"))
			}
			if tc.attack == 3 && e.HasKeyword(atk3, "First Strike") {
				t.Fatal("the third attacker was pumped by a two-creature trigger")
			}
			if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+tc.wantDrr {
				t.Fatalf("hand = %d, want %d", got, handBefore+tc.wantDrr)
			}
		})
	}
}

// TestAttackersDeclaredReplaysExactly replays the two-attacker Love batch
// through a Clone: the queued referents and the pump's grant order must be
// byte-identical, the way every replayed trigger is.
func TestAttackersDeclaredReplaysExactly(t *testing.T) {
	t.Parallel()
	love := mshCorpusCardPath(t, "Love on the Battlefield", "l/love_on_the_battlefield.txt")
	e := combatEngine(t)
	onBoardCard(t, e, 0, love)
	atk := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	atk2 := onBoardReady(t, e, 0, "Name:Fox\nManaCost:1 W\nTypes:Creature Fox\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{atk, atk2}})
	clone := e.Clone()
	for _, eng := range []*Engine{e, clone} {
		eng.putTriggersOnStack()
		eng.resolveTop()
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the AttackersDeclared resolution")
	}
	if !e.HasKeyword(atk, "First Strike") {
		t.Fatal("replayed trigger did not grant first strike")
	}
}

// TestCounterAddedTenthCounterFiresOnceOnCrossing pins trig:CounterAdded on
// Shang-Chi and the Ten Rings: the tenth +1/+1 counter crosses the gate once
// (a 9+1 pair, a single 10, a single 11 from below and a 9+2 overshoot all
// cross it -- the tenth is put whether the batch lands on it or past it),
// draws five and gains 5 life; puts that do not cross (7+1), puts that start
// past it (11+1), and later puts after the crossing, queue nothing.
func TestCounterAddedTenthCounterFiresOnceOnCrossing(t *testing.T) {
	t.Parallel()
	shang := mshCorpusCard(t, "Shang-Chi and the Ten Rings")
	e := combatEngine(t)
	id := onBoardCard(t, e, 0, shang)
	handBefore := len(e.G.Zone(state.ZHand, 0))
	lifeBefore := e.G.Players[0].Life

	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: 9})
	if len(e.pendingTriggers) != 0 {
		t.Fatal("the ninth counter queued the tenth-counter trigger")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: 1})
	if len(e.pendingTriggers) != 1 {

		t.Fatalf("pendingTriggers after the crossing put = %d, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+5 {
		t.Fatalf("hand = %d, want %d", got, handBefore+5)
	}
	if got := e.G.Players[0].Life; got != lifeBefore+5 {
		t.Fatalf("life = %d, want %d", got, lifeBefore+5)
	}
	// A single put of the whole ten crosses too.
	e2 := combatEngine(t)
	id2 := onBoardCard(t, e2, 0, shang)
	hand2 := len(e2.G.Zone(state.ZHand, 0))
	e2.emit(events.Event{Kind: events.CounterChange, Obj: id2, Counter: "P1P1", Amount: 10})
	if len(e2.pendingTriggers) != 1 {
		t.Fatalf("single-put pendingTriggers = %d, want 1", len(e2.pendingTriggers))
	}
	e2.putTriggersOnStack()
	e2.resolveTop()
	if got := len(e2.G.Zone(state.ZHand, 0)); got != hand2+5 {
		t.Fatalf("single-put hand = %d, want %d", got, hand2+5)
	}

	// A batch that overshoots the gate from below (9+2 on EQ10) crossed it: the
	// tenth counter is among the eleven, so the crossing fires even though the
	// post-event total is not == n. This is the shape applyCompare(after, EQ, n)
	// missed (the finding that fixed the gate).
	e3 := combatEngine(t)
	id3 := onBoardCard(t, e3, 0, shang)
	hand3 := len(e3.G.Zone(state.ZHand, 0))
	e3.emit(events.Event{Kind: events.CounterChange, Obj: id3, Counter: "P1P1", Amount: 9})
	e3.emit(events.Event{Kind: events.CounterChange, Obj: id3, Counter: "P1P1", Amount: 2})
	if len(e3.pendingTriggers) != 1 {
		t.Fatalf("overshoot-batch pendingTriggers = %d, want 1", len(e3.pendingTriggers))
	}
	e3.putTriggersOnStack()
	e3.resolveTop()
	if got := len(e3.G.Zone(state.ZHand, 0)); got != hand3+5 {
		t.Fatalf("overshoot-batch hand = %d, want %d", got, hand3+5)
	}

	// A single put past n from below crosses too (a bare 11 puts the tenth among
	// the batch), and the put after it no longer fires -- so a put that starts at
	// or past the gate (the 11+1 shape) never crosses either.
	e4 := combatEngine(t)
	id4 := onBoardCard(t, e4, 0, shang)
	e4.emit(events.Event{Kind: events.CounterChange, Obj: id4, Counter: "P1P1", Amount: 11})
	if len(e4.pendingTriggers) != 1 {
		t.Fatalf("single-11 pendingTriggers = %d, want 1", len(e4.pendingTriggers))
	}
	e4.putTriggersOnStack()
	e4.resolveTop()
	// resolveTop drew five cards and each draw queued Shang-Chi's own Drawn
	// trigger ("put a +1/+1 counter") -- those five pending entries are the
	// draws' puts, not the crossing gate; drop them so the assertion below
	// sees only the gate's answer to the follow-up put.
	e4.pendingTriggers = nil
	e4.emit(events.Event{Kind: events.CounterChange, Obj: id4, Counter: "P1P1", Amount: 1})
	if len(e4.pendingTriggers) != 0 {
		t.Fatalf("post-crossing pendingTriggers = %d, want 0", len(e4.pendingTriggers))
	}
}

// TestAttackerBlockedCountsEachBlockingCreature pins trig:AttackerBlocked on
// She-Hulk, Wallbreaker: a blocked Hero gets one +1/+1 counter per creature
// blocking it (the Creature.blockingTriggeredAttacker referent), the trigger
// queues once per blocked Hero with that Hero's own blocker count, and an
// unblocked attacker queues nothing.
func TestAttackerBlockedCountsEachBlockingCreature(t *testing.T) {
	t.Parallel()
	hulk := mshCorpusCardPath(t, "She-Hulk, Wallbreaker", "s/she_hulk_wallbreaker.txt")
	e := combatEngine(t)
	onBoardCard(t, e, 0, hulk)
	hero := onBoardReady(t, e, 0, "Name:Cap\nManaCost:1 W\nTypes:Creature Hero\nPT:2/2\nOracle:x\n")
	bear := onBoardReady(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	b1 := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	b2 := onBoard(t, e, 1, "Name:Memnite Two\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")

	e.askAttackers()
	submitAttackersOnly(t, e, hero, bear)
	drainCombatPriority(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	clone := e.Clone()
	for _, eng := range []*Engine{e, clone} {
		submitBlockersOnly(t, eng, b1, b2)
		// The blocker declaration queued the trigger and Submit's Advance put
		// it on the stack; resolve it.
		eng.resolveTop()
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the AttackerBlocked resolution")
	}
	if got := e.G.Obj(hero).Counter("P1P1"); got != 2 {
		t.Fatalf("Hero P1P1 counters = %d, want 2 (one per blocker)", got)
	}
	if got := e.G.Obj(bear).Counter("P1P1"); got != 0 {
		t.Fatalf("unblocked Bear counters = %d, want 0", got)
	}

	// Two Heroes blocked by distinct creatures are two trigger instances,
	// each counting its own blockers; the same controller orders the pair.
	e3 := combatEngine(t)
	onBoardCard(t, e3, 0, hulk)
	h1 := onBoardReady(t, e3, 0, "Name:Cap\nManaCost:1 W\nTypes:Creature Hero\nPT:2/2\nOracle:x\n")
	h2 := onBoardReady(t, e3, 0, "Name:Iron\nManaCost:3\nTypes:Artifact Creature Hero\nPT:3/3\nOracle:x\n")
	ob1 := onBoard(t, e3, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	ob2 := onBoard(t, e3, 1, "Name:Memnite Two\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	e3.askAttackers()
	submitAttackersOnly(t, e3, h1, h2)
	drainCombatPriority(t, e3)
	d = e3.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	h1Idx, h2Idx := -1, -1
	for _, o := range d.Options {
		switch {
		case o.Attacker == h1 && o.Obj == ob1:
			h1Idx = o.Index
		case o.Attacker == h2 && o.Obj == ob2:
			h2Idx = o.Index
		}
	}
	if h1Idx < 0 || h2Idx < 0 {
		t.Fatalf("block options missing: %+v", d.Options)
	}
	if err := e3.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{h1Idx, h2Idx}}); err != nil {
		t.Fatal(err)
	}
	d = e3.Pending()
	if d != nil && d.Kind == decision.KTriggerOrder {
		if err := e3.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err != nil {
			t.Fatal(err)
		}
	}
	e3.resolveTop()
	e3.resolveTop()
	if got := e3.G.Obj(h1).Counter("P1P1"); got != 1 {
		t.Fatalf("first Hero counters = %d, want 1", got)
	}
	if got := e3.G.Obj(h2).Counter("P1P1"); got != 1 {
		t.Fatalf("second Hero counters = %d, want 1", got)
	}
}
