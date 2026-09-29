package manabrew

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// Inline fixture cards, per the repo's GPL-3.0 boundary rule: no Forge card
// script text is ever committed, so every card this test needs is written
// here from scratch, not pulled from .cards/. Watcher Alpha/Beta each watch
// "a creature you control enters" and gain a DISTINCT, order-revealing amount
// of life, so the real engine's event log -- not a hand-simulated shortcut --
// proves which trigger actually resolved first.
const orderWatcherAlphaSrc = "Name:Order Watcher Alpha\nManaCost:1 G\nTypes:Creature Elf\nPT:1/1\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Creature.YouCtrl | TriggerZones$ Battlefield | Execute$ TrigGainAlpha\n" +
	"SVar:TrigGainAlpha:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

const orderWatcherBetaSrc = "Name:Order Watcher Beta\nManaCost:1 G\nTypes:Creature Elf\nPT:1/1\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Creature.YouCtrl | TriggerZones$ Battlefield | Execute$ TrigGainBeta\n" +
	"SVar:TrigGainBeta:DB$ GainLife | Defined$ You | LifeAmount$ 2\nOracle:x\n"

const orderFillerSrc = "Name:Order Filler\nTypes:Creature Filler\nPT:1/1\nOracle:x\n"

// parseOrderFixtureCard parses one inline fixture, failing the test on any
// diagnostic (a fixture typo should never silently degrade into an untriggered
// card and a confusing later assertion).
func parseOrderFixtureCard(t *testing.T, name, src string) *cards.Card {
	t.Helper()
	c, diags := cards.ParseBytes(name, []byte(src))
	if len(diags) != 0 {
		t.Fatalf("parse %s: %v", name, diags)
	}
	c.Link()
	return c
}

// orderAutoAnswer answers one decision the test does not care about (an
// incidental mulligan keep, a hand-size discard from a passed-through turn,
// or anything else a filler-only two-player game can pose on its own) with
// its first Min options -- an arbitrary but always-legal choice, since none
// of this fixture's assertions depend on which filler card gets discarded or
// how a mulligan resolves.
func orderAutoAnswer(t *testing.T, e *rules.Engine, d *decision.Decision) {
	t.Helper()
	n := d.Min
	if n > len(d.Options) {
		n = len(d.Options)
	}
	choices := make([]int, n)
	for i := range choices {
		choices[i] = i
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("auto-answer %s (options %+v): %v", d.Kind, d.Options, err)
	}
}

// orderSettle drives the engine forward -- passing every priority decision
// and auto-answering anything else (see orderAutoAnswer) -- until priority is
// idle with an empty stack: nothing left to resolve, and no ordering decision
// mid-resolution. It is the test's way of fully resolving a single (non-
// ordering) trigger, plus any incidental noise a real two-player game
// generates on its own (draws overflowing hand size, and so on), before the
// real two-trigger moment the test cares about.
func orderSettle(t *testing.T, e *rules.Engine, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		e.Advance()
		d := e.Pending()
		if d == nil {
			t.Fatal("game ended while settling")
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				return
			}
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
					break
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision has no pass option: %+v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
			continue
		}
		orderAutoAnswer(t, e, d)
	}
	t.Fatalf("did not settle to an idle priority window within %d steps", limit)
}

// orderAdvanceUntilTriggerOrder drives the engine forward the same way
// orderSettle does, but stops and returns as soon as a KTriggerOrder decision
// appears -- the exact moment this test wants to intercept with the ManaBrew
// translation instead of an ordinary auto-pass.
func orderAdvanceUntilTriggerOrder(t *testing.T, e *rules.Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit; i++ {
		e.Advance()
		d := e.Pending()
		if d == nil {
			t.Fatal("game ended while waiting for the trigger-order decision")
		}
		if d.Kind == decision.KTriggerOrder {
			return d
		}
		if d.Kind == decision.KPriority {
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
					break
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision has no pass option: %+v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
			continue
		}
		orderAutoAnswer(t, e, d)
	}
	t.Fatalf("no trigger-order decision appeared within %d steps", limit)
	return nil
}

// TestTriggerOrderFirstIdResolvesFirst runs a two-trigger fixture through the
// real engine (CR 603.3b / Ruling U2/R1) to prove the KTriggerOrder direction
// flip end to end, not just in a hand-built decision: submitting a ManaBrew
// reorder answer that names Watcher Beta's item FIRST must make Beta's
// LifeChange (amount 2) land in the event log BEFORE Alpha's (amount 1) --
// the opposite of gorge's own "Choices[0] placed first, resolves last"
// convention, which is exactly the bug a non-reversing adapter would produce
// silently (the whole reason the ticket calls for a live-engine test here).
func TestTriggerOrderFirstIdResolvesFirst(t *testing.T) {
	alphaCard := parseOrderFixtureCard(t, "order-watcher-alpha.txt", orderWatcherAlphaSrc)
	betaCard := parseOrderFixtureCard(t, "order-watcher-beta.txt", orderWatcherBetaSrc)
	fillerCard := parseOrderFixtureCard(t, "order-filler.txt", orderFillerSrc)
	oppFiller := parseOrderFixtureCard(t, "order-filler-opp.txt", orderFillerSrc)

	deck := make([]*cards.Card, 40)
	deck[0], deck[1] = alphaCard, betaCard
	for i := 2; i < len(deck); i++ {
		deck[i] = fillerCard
	}
	oppDeck := make([]*cards.Card, 40)
	for i := range oppDeck {
		oppDeck[i] = oppFiller
	}

	e := rules.New(rules.Config{Seed: 4101, Names: []string{"alice", "bob"},
		Decks: [][]*cards.Card{deck, oppDeck}})

	var alpha, beta state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		switch o.Face().Name {
		case "Order Watcher Alpha":
			alpha = id
		case "Order Watcher Beta":
			beta = id
		}
	}
	if alpha == 0 || beta == 0 {
		t.Fatalf("fixture watchers not found in seat 0's library: alpha=%d beta=%d", alpha, beta)
	}

	// Alpha enters alone first: only its own self-referential trigger fires
	// (a single trigger, no ordering needed), so it is fully settled -- along
	// with whatever incidental noise a real two-player game generates on its
	// own -- before the real test setup (Beta's entry, which fires BOTH
	// watchers) happens.
	e.Emit(events.Event{Kind: events.MoveZone, Obj: alpha, From: state.ZLibrary, To: state.ZBattlefield, Player: 0})
	orderSettle(t, e, 2000)

	// Only events from here on belong to the two-trigger moment under test:
	// Alpha's own entry above already logged its own (single, unordered)
	// LifeChange, which must not be confused with the group being tested.
	startIdx := len(e.L.Events)

	e.Emit(events.Event{Kind: events.MoveZone, Obj: beta, From: state.ZLibrary, To: state.ZBattlefield, Player: 0})
	d := orderAdvanceUntilTriggerOrder(t, e, 2000)
	if len(d.Options) != 2 {
		t.Fatalf("precondition: want a 2-option KTriggerOrder decision, got %+v", d)
	}

	tr := New("table", 1, nil)
	msg, err := tr.promptOrder(d, nil)
	if err != nil {
		t.Fatalf("promptOrder: %v", err)
	}
	reorder, ok := msg.Input.Value.(mb.ReorderInput)
	if !ok {
		t.Fatalf("promptOrder did not build a reorder input: %#v", msg.Input.Value)
	}
	if len(reorder.Items) != 2 {
		t.Fatalf("reorder prompt carries %d items, want 2", len(reorder.Items))
	}

	// Find each watcher's item id by matching Card.ID back to the option
	// list's Obj (never assuming a particular array position -- the whole
	// point of this test is that position must not matter).
	var alphaItem, betaItem string
	for i, opt := range d.Options {
		id := reorder.Items[i].ID
		switch opt.Obj {
		case alpha:
			alphaItem = id
		case beta:
			betaItem = id
		default:
			t.Fatalf("option %d names unexpected source %v", i, opt.Obj)
		}
	}
	if alphaItem == "" || betaItem == "" {
		t.Fatalf("could not resolve both item ids: alpha=%q beta=%q", alphaItem, betaItem)
	}

	// The ManaBrew answer: Beta's item FIRST, meaning "Beta resolves first".
	pending := &Pending{Prompt: msg, Decision: d}
	outcome := tr.parseTriggerOrder(mb.ReorderDecision{OrderedIDs: []string{betaItem, alphaItem}}, pending)
	if outcome.Err != nil {
		t.Fatalf("parseTriggerOrder: %s: %s", outcome.Err.Code, outcome.Err.Message)
	}
	if outcome.Intent == nil {
		t.Fatal("parseTriggerOrder returned no intent")
	}
	if err := e.Submit(*outcome.Intent); err != nil {
		t.Fatalf("submit trigger order intent %+v: %v", outcome.Intent.Choices, err)
	}
	orderSettle(t, e, 2000)

	// Read the two LifeChange events off the real log, in the order the
	// engine actually resolved them, and check Beta (amount 2) precedes
	// Alpha (amount 1).
	var amounts []int32
	for _, ev := range e.L.Events[startIdx:] {
		if ev.Kind == events.LifeChange && ev.Player == 0 && (ev.Amount == 1 || ev.Amount == 2) {
			amounts = append(amounts, ev.Amount)
		}
	}
	if len(amounts) != 2 {
		t.Fatalf("want exactly 2 life-change events (1 and 2), got %v", amounts)
	}
	if amounts[0] != 2 || amounts[1] != 1 {
		t.Fatalf("resolution order = %v, want [2 1] (Beta's requested-first trigger resolving first); "+
			"a [1 2] result means the direction flip is backwards", amounts)
	}
}
