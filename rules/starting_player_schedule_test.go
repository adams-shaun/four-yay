package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func hasPriorityOption(d *decision.Decision, kind string) bool {
	for _, o := range d.Options {
		if o.Kind == kind {
			return true
		}
	}
	return false
}

func submitPriorityPass(t *testing.T, e *Engine, d *decision.Decision) {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == "pass" {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("pass priority: %v", err)
			}
			e.Advance()
			return
		}
	}
	t.Fatalf("priority decision has no pass: %+v", d.Options)
}

// TestStartingPlayerChoiceScheduleKeepsLandOffersAnchoredToTheActiveSeat pins
// the schedule after an answered CR 103.1 ask. Turn number alone is not a seat
// turn: choosing seat 0 makes its own turn 2 the game's turn 3. An observer
// that asks whether seat 0 has a land offer at turn 2 instead sees the other
// seat's turn and mistakes the correct non-active-player absence for a defect.
func TestStartingPlayerChoiceScheduleKeepsLandOffersAnchoredToTheActiveSeat(t *testing.T) {
	t.Parallel()
	cfg := tossedTwoSeat(t, 1, 0) // seed 1 tosses to seat 1; choose the other seat, 0.
	e := NewStartingPlayerChoice(cfg)
	d := e.AskStartingPlayer()
	if d == nil {
		t.Fatal("precondition: no starting-player ask")
	}
	pick := optionForSeat(t, d, 0)
	if pick.Player == d.Player {
		t.Fatalf("precondition: chosen seat %d must differ from toss winner %d", pick.Player, d.Player)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick.Index}}); err != nil {
		t.Fatalf("answer starting-player ask: %v", err)
	}
	e.Advance()

	seenChosenTurn2, seenChosenTurn3, seenOtherTurn2 := false, false, false
	for n := 0; n < 400 && !(seenChosenTurn2 && seenChosenTurn3 && seenOtherTurn2); n++ {
		if answerIfDiscard(t, e) {
			e.Advance()
			continue
		}
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("decision %d = %+v, want priority", n, d)
		}
		if e.G.Step == state.StepMain1 && d.Player == 0 {
			switch {
			case e.G.Active == 0 && e.G.Turn == 3:
				seenChosenTurn2 = true
				if hand := e.G.Zone(state.ZHand, 0); len(hand) == 0 || e.G.Obj(hand[0]).Face().Name != "Mountain" {
					t.Fatalf("precondition: seat 0 has no Mountain in hand at own turn 2 (hand=%v)", hand)
				}
				if got := e.G.Players[0].LandsPlayed; got != 0 {
					t.Fatalf("precondition: seat 0 land drop already used on own turn 2: %d", got)
				}
				if !hasPriorityOption(d, "play_land") {
					t.Fatalf("chosen seat's own turn-2 main1 (active=%d turn=%d): play_land MISSING", e.G.Active, e.G.Turn)
				}
			case e.G.Active == 0 && e.G.Turn == 5:
				seenChosenTurn3 = true
				if len(e.G.Zone(state.ZHand, 0)) == 0 || e.G.Obj(e.G.Zone(state.ZHand, 0)[0]).Face().Name != "Mountain" || e.G.Players[0].LandsPlayed != 0 {
					t.Fatalf("precondition: seat 0 must retain a Mountain and unused land drop on own turn 3")
				}
				if !hasPriorityOption(d, "play_land") {
					t.Fatalf("chosen seat's own turn-3 main1 (active=%d turn=%d): play_land MISSING", e.G.Active, e.G.Turn)
				}
			case e.G.Active == 1 && e.G.Turn == 2:
				seenOtherTurn2 = true
				if hand := e.G.Zone(state.ZHand, 0); len(hand) == 0 || e.G.Obj(hand[0]).Face().Name != "Mountain" || e.G.Players[0].LandsPlayed != 0 {
					t.Fatalf("precondition: seat 0 must hold a Mountain with an unused land drop at the non-active turn-2 window")
				}
				if hasPriorityOption(d, "play_land") {
					t.Fatalf("non-active seat was offered play_land at turn=%d active=%d", e.G.Turn, e.G.Active)
				}
			}
		}
		submitPriorityPass(t, e, d)
	}
	if !seenChosenTurn2 || !seenChosenTurn3 || !seenOtherTurn2 {
		t.Fatalf("schedule windows not all observed: chosen turn2=%t chosen turn3=%t other turn2=%t", seenChosenTurn2, seenChosenTurn3, seenOtherTurn2)
	}
}

func talismanCorpusCard(t *testing.T, reg interface {
	Lookup(string) (*cards.Card, bool)
}, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("missing corpus card %q", name)
	}
	return c
}

// TestStartingPlayerTalismanSmokeDrive mirrors the smoke fixture's 16 Mountain
// / 14 Talisman deck and its answered-echo toss. The active-seat schedule shift
// means the chosen seat's own turn 2 is game turn 3; this drive follows actual
// priority decisions instead of treating an absolute turn number as a seat.
func TestStartingPlayerTalismanSmokeDrive(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	mountain := talismanCorpusCard(t, reg, "Mountain")
	talisman := talismanCorpusCard(t, reg, "Talisman of Indulgence")
	deck := make([]*cards.Card, 0, 30)
	for i := 0; i < 16; i++ {
		deck = append(deck, mountain)
	}
	for i := 0; i < 14; i++ {
		deck = append(deck, talisman)
	}
	e := NewStartingPlayerChoice(Config{Seed: 7, Mulligans: 0, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck, deck}})
	ask := e.AskStartingPlayer()
	if ask == nil || len(ask.Options) == 0 {
		t.Fatal("precondition: starting-player ask has no options")
	}
	chosen := ask.Options[0]
	if err := e.Submit(decision.Intent{Seq: ask.Seq, Player: ask.Player, Choices: []int{chosen.Index}}); err != nil {
		t.Fatalf("echo first toss option: %v", err)
	}
	e.Advance()

	for n := 0; n < 400; n++ {
		if answerIfDiscard(t, e) {
			e.Advance()
			continue
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("decision %d: no pending decision before Talisman stop shape", n)
		}
		if d.Kind == decision.KPriority && hasPriorityOptionLabel(d, "activate", "Talisman") {
			return
		}
		choices := []int(nil)
		if d.Kind == decision.KPriority {
			step := e.G.Step
			if step.IsMain() {
				for _, kind := range []string{"play_land", "cast", "activate", "pass"} {
					idx := -1
					for _, o := range d.Options {
						if o.Kind == kind && !(kind == "cast" && !strings.Contains(o.Label, "Talisman")) &&
							!(kind == "activate" && strings.Contains(o.Label, "Talisman")) {
							idx = o.Index
							break
						}
					}
					if idx >= 0 {
						choices = []int{idx}
						break
					}
				}
			} else {
				for _, o := range d.Options {
					if o.Kind == "pass" {
						choices = []int{o.Index}
						break
					}
				}
			}
		} else if d.Kind == decision.KStartingPlayer && len(d.Options) > 0 {
			choices = []int{d.Options[0].Index}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
			t.Fatalf("decision %d submit %s: %v", n, d.Kind, err)
		}
		e.Advance()
	}
	t.Fatal("did not reach a priority window with a Talisman activation within 400 decisions")
}

func hasPriorityOptionLabel(d *decision.Decision, kind, labelPart string) bool {
	for _, o := range d.Options {
		if o.Kind == kind && strings.Contains(o.Label, labelPart) {
			return true
		}
	}
	return false
}
