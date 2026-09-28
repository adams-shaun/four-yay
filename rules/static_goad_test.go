package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// staticGoadEngine builds a 3-seat engine (seats 0/1/2) with the toss pinned
// to seat 0, ready for onBoard placements. The goad leaf tests then make
// seat 1 the active player so the victim (controlled by seat 1) is offered an
// attack decision with two candidate defenders, 0 and 2.
func staticGoadEngine(t *testing.T) *Engine {
	t.Helper()
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{
		mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40),
	}}))
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	return e
}

// corpusCardByName resolves a real compiled corpus card by its Forge name.
func corpusCardByName(t *testing.T, name string) *cards.Card {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus card %q not found", name)
	}
	return c
}

// TestStaticGoadShinyImpetusForcesAttackAway proves the Goad$ True rider of a
// Mode$ Continuous static is read. Shiny Impetus (controlled by seat 0) is
// attached to a creature controlled by seat 1; with seat 1 active in the
// declare-attackers step, the only legal (attacker, defender) pair is the
// victim attacking seat 2 -- attacking the Aura's controller 0 is forbidden
// by CR 701.38b -- and the option is marked Required (CR 508.1d).
func TestStaticGoadShinyImpetusForcesAttackAway(t *testing.T) {
	t.Parallel()
	e := staticGoadEngine(t)
	impetus := onBoardCard(t, e, 0, corpusCardByName(t, "Shiny Impetus"))
	victim := onBoardReady(t, e, 1, "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.emit(events.Event{Kind: events.Attach, Obj: impetus, IDs: []state.ObjID{victim}})

	if got := e.G.Obj(victim).Goads; len(got) != 0 {
		t.Fatalf("event-backed goads = %v, want none (this is a static grant)", got)
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected a KAttackers decision, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != victim || d.Options[0].Player != 2 {
		t.Fatalf("static-goaded attack options = %+v, want only victim attacking player 2", d)
	}
	if !d.Options[0].Required {
		t.Fatal("the static-goaded creature's attack option is not marked Required")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1}); err == nil {
		t.Fatal("static-goaded creature was allowed to skip its required attack")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{d.Options[0].Index}}); err != nil {
		t.Fatalf("legal non-goader attack rejected: %v", err)
	}
	if !e.G.Obj(victim).IsAttacking || e.G.Obj(victim).Attacking != 2 {
		t.Fatal("static-goaded creature did not attack the only non-goader")
	}
}

// TestStaticGoadEndsWhenAuraLeaves proves the static goad has a live lifetime:
// once the Aura leaves the battlefield the creature is no longer required to
// attack and its defender choice is unrestricted. The static is re-derived
// per call (no lifetime bookkeeping), so a real MoveZone to the graveyard
// ends it exactly as the Aura being destroyed in play would.
func TestStaticGoadEndsWhenAuraLeaves(t *testing.T) {
	t.Parallel()
	e := staticGoadEngine(t)
	impetus := onBoardCard(t, e, 0, corpusCardByName(t, "Shiny Impetus"))
	victim := onBoardReady(t, e, 1, "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.emit(events.Event{Kind: events.Attach, Obj: impetus, IDs: []state.ObjID{victim}})

	if !e.goadedBy(e.G.Obj(victim), 0) {
		t.Fatal("static goad from a live Shiny Impetus is not seen")
	}
	// The Aura leaves the battlefield through the logged move, the same path
	// any destroy/sacrifice takes.
	e.emit(events.Event{Kind: events.MoveZone, Obj: impetus, From: state.ZBattlefield,
		To: state.ZGraveyard, Player: 0})
	if e.goadedBy(e.G.Obj(victim), 0) {
		t.Fatal("static goad survived the Aura leaving the battlefield")
	}
	if e.hasActiveGoad(e.G.Obj(victim)) {
		t.Fatal("victim still required to attack after the Aura left")
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected a KAttackers decision, got %+v", d)
	}
	sawZero := false
	for _, o := range d.Options {
		if o.Player == 0 {
			sawZero = true
		}
		if o.Required {
			t.Fatalf("ungoaded victim's option is marked Required: %+v", o)
		}
	}
	if !sawZero {
		t.Fatal("attacking the former Aura controller is still forbidden")
	}
}

// TestStaticGoadBaelothPowerThreshold proves the static read resolves a
// source-relative SVar in Affected$: Baeloth's "Creature.powerLTY+OppCtrl"
// goads an opponent's creature with power less than Baeloth's own (5), and
// does NOT goad one with greater power.
func TestStaticGoadBaelothPowerThreshold(t *testing.T) {
	t.Parallel()
	e := staticGoadEngine(t)
	onBoardCard(t, e, 0, corpusCardByName(t, "Baeloth Barrityl, Entertainer")) // 2/5
	weak := onBoardReady(t, e, 1, "Name:Weak\nTypes:Creature\nPT:1/1\nOracle:x\n")
	strong := onBoardReady(t, e, 1, "Name:Strong\nTypes:Creature\nPT:9/9\nOracle:x\n")

	if !e.goadedBy(e.G.Obj(weak), 0) {
		t.Fatal("Baeloth did not goad an opponent's creature with power less than his (1 < 5)")
	}
	if e.goadedBy(e.G.Obj(strong), 0) {
		t.Fatal("Baeloth goaded an opponent's creature with power greater than his (9 > 5)")
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected a KAttackers decision, got %+v", d)
	}
	weakSeen, strongGoaded := false, false
	for _, o := range d.Options {
		switch o.Obj {
		case weak:
			// The goaded weak creature may only attack player 2, required.
			if o.Player != 2 || !o.Required {
				t.Fatalf("goaded weak creature option = %+v, want required attack at player 2", o)
			}
			weakSeen = true
		case strong:
			if o.Required {
				t.Fatalf("ungoaded strong creature option = %+v, want not required", o)
			}
			if o.Player == 0 {
				strongGoaded = true
			}
		}
	}
	if !weakSeen {
		t.Fatal("goaded weak creature was offered no attack option")
	}
	if !strongGoaded {
		t.Fatal("ungoaded strong creature was forbidden from attacking Baeloth's controller")
	}
}
