package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The generic activation gate's Activation$ read (task
// inbox-paramcensus-final-stragglers, Sea Gate Wreckage entry). An AB$
// carrying Activation$ <condition> is offered only while the ACTIVATOR
// meets the condition -- Forge's SpellAbilityCondition keyword half. The
// gate lives in the shared offer machinery (rules/legal.go's
// activationConditionOK, wired into the printed-ability loop and the mana
// path), so every API's activation is covered, not Draw's alone.

// wreckSrc is Sea Gate Wreckage's real script shape (both abilities
// verbatim): the {2}{C}, {T} hellbent draw.
const wreckSrc = "Name:Sea Gate Wreckage\nTypes:Land\n" +
	"A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\n" +
	"A:AB$ Draw | Cost$ 2 C T | NumCards$ 1 | Activation$ Hellbent | SpellDescription$ Draw a card. Activate only if you have no cards in hand.\nOracle:x\n"

// TestSeaGateWreckageHellbentGate drives the wreck's activated draw end to
// end: with a card in hand the ability is not offered; with an empty hand it
// is, and answering it pays {2}{C}{T} and draws exactly the one card.
func TestSeaGateWreckageHellbentGate(t *testing.T) {
	t.Parallel()
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}))
	// The genesis hand would hold mountains; this fixture owns the hand.
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.SetZone(state.ZHand, 1, nil)
	wreck := e.G.AddObject(card(t, wreckSrc), 0)
	wreck.Zone = state.ZBattlefield
	e.G.Clock++
	wreck.Timestamp = e.G.Clock
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), wreck.ID))
	// Fill the hand so the hellbent gate holds the ability back.
	filler := e.G.AddObject(card(t, "Name:Bear\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	filler.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), filler.ID))
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	e.priorityRound()

	notOffered := func() bool {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("not at priority: %+v", d)
		}
		for _, o := range d.Options {
			if o.Kind == "ability" && o.Obj == wreck.ID {
				return false
			}
		}
		return true
	}
	if !notOffered() {
		t.Fatal("the hellbent draw was offered while the hand held a card")
	}
	// Empty the hand; the gate opens and the ability is offered.
	e.emit(events.Event{Kind: events.MoveZone, Obj: filler.ID, From: state.ZHand, To: state.ZLibrary, Player: 0})
	e.priorityRound()
	// Fund {2}{C}: three mana cover the generic two plus the colourless pip.
	for _, r := range "CCC" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}
	e.priorityRound()
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == wreck.ID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the hellbent draw was not offered with an empty hand: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 0)); got != 1 {
		t.Fatalf("hand after the activation = %d, want 1 (the drawn card)", got)
	}
	if !hasEventKind(e, events.Draw) {
		t.Fatal("no Draw event recorded")
	}
}

// TestSeaGateWreckageThresholdMetalcraftDelirium pins the other three
// evaluated conditions of the shared gate at the offer boundary.
func TestSeaGateWreckageThresholdMetalcraftDelirium(t *testing.T) {
	t.Parallel()
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}))
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1

	addGrave := func(spec string) {
		o := e.G.AddObject(card(t, spec), 0)
		o.Zone = state.ZGraveyard
		e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), o.ID))
	}
	ab := cardsSAActivation(t, "Hellbent")
	if !e.activationConditionOK(0, ab) {
		t.Fatal("hellbent must hold with an empty hand")
	}
	e.G.SetZone(state.ZHand, 0, nil)
	_ = e.G
	// Threshold: 6 cards short, 7 exactly.
	ab = cardsSAActivation(t, "Threshold")
	if e.activationConditionOK(0, ab) {
		t.Fatal("threshold must fail with an empty graveyard")
	}
	for i := 0; i < 6; i++ {
		addGrave("Name:Filler\nTypes:Creature\nPT:1/1\nOracle:x\n")
	}
	if e.activationConditionOK(0, ab) {
		t.Fatal("threshold must fail with 6 cards")
	}
	addGrave("Name:Filler2\nTypes:Instant\nOracle:x\n")
	if !e.activationConditionOK(0, ab) {
		t.Fatal("threshold must hold with 7 cards")
	}
	// Metalcraft: two artifacts short, three exactly.
	ab = cardsSAActivation(t, "Metalcraft")
	if e.activationConditionOK(0, ab) {
		t.Fatal("metalcraft must fail with no artifacts")
	}
	onBoard(t, e, 0, "Name:Mox A\nManaCost:0\nTypes:Artifact\nOracle:x\n")
	onBoard(t, e, 0, "Name:Mox B\nManaCost:0\nTypes:Artifact\nOracle:x\n")
	if e.activationConditionOK(0, ab) {
		t.Fatal("metalcraft must fail with 2 artifacts")
	}
	onBoard(t, e, 0, "Name:Mox C\nManaCost:0\nTypes:Artifact\nOracle:x\n")
	if !e.activationConditionOK(0, ab) {
		t.Fatal("metalcraft must hold with 3 artifacts")
	}
	// Delirium: distinct core card types among the graveyard's cards.
	ab = cardsSAActivation(t, "Delirium")
	// The graveyard so far: 6 creatures + 1 instant = 2 types.
	if e.activationConditionOK(0, ab) {
		t.Fatal("delirium must fail with 2 graveyard types")
	}
	addGrave("Name:Filler3\nTypes:Land\nOracle:x\n")
	addGrave("Name:Filler4\nTypes:Enchantment\nOracle:x\n")
	if !e.activationConditionOK(0, ab) {
		t.Fatal("delirium must hold with 4 distinct graveyard types")
	}
}

// cardsSAActivation builds a bare AB fixture carrying just the Activation$
// condition, the shape the offer gate reads.
func cardsSAActivation(t *testing.T, cond string) *cards.SA {
	t.Helper()
	c := card(t, "Name:Fixture\nManaCost:0\nTypes:Artifact\n"+
		"A:AB$ Draw | Cost$ T | NumCards$ 1 | Activation$ "+cond+" | SpellDescription$ x\nOracle:x\n")
	return c.Faces[0].Abilities[0]
}
