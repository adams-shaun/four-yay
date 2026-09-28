package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestThoughtseizeLetsTheCasterChoose is the end-to-end carrier for the
// Mode$ RevealYouChoose discard (thoughtseize.txt's exact shape). A targeted
// discard sorcery is cast at seat 1, the engine poses a KModes decision
// mid-resolution over seat 1's DiscardValid$-filtered hand, the CASTER
// chooses a card (deliberately not the front card), and that exact card
// leaves seat 1's hand — not hand[0].
func TestThoughtseizeLetsTheCasterChoose(t *testing.T) {
	t.Parallel()
	ts := "Name:PiT\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ Discard | ValidTgts$ Player | NumCards$ 1 | Mode$ RevealYouChoose | DiscardValid$ Card.nonLand\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 96, ts)

	// Seat 1's hand is exactly frog, island, bird — in that order, so the
	// front card is frog and choosing bird proves the choice is honoured.
	frog := e.G.AddObject(card(t, "Name:Frog\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	isle := e.G.AddObject(card(t, "Name:Islet\nTypes:Land\nOracle:x\n"), 1)
	bird := e.G.AddObject(card(t, "Name:Bird\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	for _, o := range []*state.Object{frog, isle, bird} {
		o.Zone = state.ZHand
	}
	e.G.SetZone(state.ZHand, 1, []state.ObjID{frog.ID, isle.ID, bird.ID})

	addMana(t, e, 0, "B")
	d := castFixture(t, e, id, 1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected a mid-resolution KModes discard decision, got %+v", d)
	}
	if d.Player != 0 {
		t.Fatalf("discard chooser = seat %d, want the caster seat 0", d.Player)
	}
	// DiscardValid$ Card.nonLand: only frog and bird are offered, never the land.
	if len(d.Options) != 2 {
		t.Fatalf("discard options = %d (%+v), want frog+bird only", len(d.Options), d.Options)
	}
	for _, o := range d.Options {
		if o.Obj == isle.ID {
			t.Fatal("a land was offered as a discard choice")
		}
	}
	// The suspension must leave the spell on the stack until the answer arrives.
	if z := e.G.Obj(id).Zone; z != state.ZStack {
		t.Fatalf("the discard spell must suspend on the stack, zone %s", z)
	}

	// Choose the SECOND card (bird), not hand[0] (frog).
	birdIdx := -1
	for _, o := range d.Options {
		if o.Obj == bird.ID {
			birdIdx = o.Index
		}
	}
	if birdIdx < 0 {
		t.Fatal("bird not offered")
	}
	submitChoices(t, e, birdIdx)
	passUntilStackEmpty(t, e, 20)

	if z := e.G.Obj(bird.ID).Zone; z != state.ZGraveyard {
		t.Fatalf("chosen card (bird) zone = %s, want Graveyard", z)
	}
	if z := e.G.Obj(frog.ID).Zone; z != state.ZHand {
		t.Fatalf("un-chosen front card (frog) zone = %s, want Hand — the choice was ignored", z)
	}
	if z := e.G.Obj(isle.ID).Zone; z != state.ZHand {
		t.Fatalf("land zone = %s, want Hand", z)
	}
	if !hasEventKind(e, events.ModeChosen) {
		t.Fatal("no ModeChosen event recorded the discard choice")
	}
}

// TestCleanupDiscardStaysDeterministicProvesThat the CR 514.1 cleanup-step
// discard is unchanged by the new RevealYouChoose path: it is still a KChoose
// "discard" decision (one option per hand card, Min == Max == the count to
// drop), never converted into the mid-resolution KModes ask that a
// RevealYouChoose spell now uses. This is the "do not convert all Discard
// into an ask" guard at the engine level.
func TestCleanupDiscardStaysDeterministic(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Active = 0
	e.G.Step = state.StepCleanup
	onHand(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	onHand(t, e, 0, "Name:Wolf\nManaCost:2 G\nTypes:Creature Wolf\nPT:3/2\nOracle:x\n")

	e.priorityRound()

	d := e.Pending()
	if d == nil {
		t.Fatal("cleanup step asked no discard decision for an oversized hand")
	}
	if d.Kind != decision.KChoose {
		t.Fatalf("cleanup discard kind = %s, want KChoose (unchanged), not a mid-resolution KModes ask", d.Kind)
	}
	if d.Player != 0 {
		t.Fatalf("cleanup discard for player %d, want active seat 0", d.Player)
	}
	// The cleanup discard stays a deterministic "drop down to maxHandSize"
	// choice, and answering it still lands those cards in the graveyard.
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("cleanup Min/Max = %d/%d, want 2/2 (9 cards - 7)", d.Min, d.Max)
	}
	hand := e.G.Zone(state.ZHand, 0)
	submitChoices(t, e, d.Options[7].Index, d.Options[8].Index)
	if got := len(e.G.Zone(state.ZHand, 0)); got != 7 {
		t.Fatalf("hand after cleanup discard = %d, want 7", got)
	}
	if len(e.G.Zone(state.ZGraveyard, 0)) != 2 {
		t.Fatalf("graveyard after cleanup discard = %d, want 2", len(e.G.Zone(state.ZGraveyard, 0)))
	}
	_ = hand
}
