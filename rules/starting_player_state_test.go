package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCountStartingPlayerUsesTheRecordedToss exercises Desert Cenote's actual
// conditional SVar on both sides of a seed whose toss starts seat 1. The
// source is deliberately kept constant while only the resolving controller
// changes: Count$StartingPlayer answers the player designation, not an object
// property or a hard-coded seat number.
func TestCountStartingPlayerUsesTheRecordedToss(t *testing.T) {
	t.Parallel()
	e := New(tossedTwoSeat(t, 1, 0)) // seed 1 starts seat 1
	if !e.G.IsStartingPlayer(1) || e.G.IsStartingPlayer(0) {
		t.Fatalf("recorded starter = %t/%t, want only seat 1", e.G.IsStartingPlayer(0), e.G.IsStartingPlayer(1))
	}
	desert := corpusCard(t, "Desert Cenote")
	body := desert.Faces[0].SVars["X"]
	if body != "Count$StartingPlayer.0.1" {
		t.Fatalf("Desert Cenote X = %q, want its real StartingPlayer SVar", body)
	}
	// LandTapped's ConditionCheckSVar gate remains independently unimplemented,
	// so evaluate the real count body itself rather than falsely claiming that
	// the unrelated condition machinery executed it.
	if got := effects.EvalCount(e, &effects.Ctx{Controller: 1, SVars: desert.Faces[0].SVars}, body); got != 0 {
		t.Fatalf("starter Count$StartingPlayer = %d, want 0", got)
	}
	if got := effects.EvalCount(e, &effects.Ctx{Controller: 0, SVars: desert.Faces[0].SVars}, body); got != 1 {
		t.Fatalf("non-starter Count$StartingPlayer = %d, want 1", got)
	}

	clone := e.G.Clone()
	if !clone.IsStartingPlayer(1) || clone.IsStartingPlayer(0) {
		t.Fatalf("clone lost recorded starter: %+v", clone)
	}
}

// TestImpatientIguanaOpeningEffectBecomesStartingPlayer drives the real
// !PlayFirst keyword and its linked RevealCard SVar through the post-mulligan
// opening round (rules/opening_hand.go, the CR 103.5/103.4 timing: mulligans
// fix the hands first, then opening-hand effects run before turn one). It
// finds a deterministic seed where seat 0 holds Iguana while seat 1 won the
// toss, keeps everywhere through the London round, accepts the opening
// round's may choice, and proves the replayed StartingPlayerChange event both
// records the designation in state.Game (Count$StartingPlayer reads it) and
// rotates turn one to the accepting seat.
func TestImpatientIguanaOpeningEffectBecomesStartingPlayer(t *testing.T) {
	t.Parallel()
	iguana := corpusCard(t, "Impatient Iguana")
	var e *Engine
	var openingAsk *decision.Decision
	for seed := uint64(1); seed < 500; seed++ {
		deck0 := mountainDeck(t, 40)
		deck0[0] = iguana
		candidate := New(Config{Seed: seed, Mulligans: 1, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{deck0, mountainDeck(t, 40)}})
		if !candidate.G.IsStartingPlayer(1) {
			continue // want the toss in seat 1's favour
		}
		candidate.Advance()
		// CR 103.4/103.5 first: keep everywhere until the pending ask stops
		// being the London round (the opening round runs after bottoming).
		d := candidate.Pending()
		for d != nil && d.Kind == decision.KMulligan {
			if err := candidate.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
				t.Fatalf("seed %d mulligan keep: %v", seed, err)
			}
			d = candidate.Pending()
		}
		if d == nil || d.Kind != decision.KChoose || d.Player != 0 ||
			len(d.Options) == 0 || d.Options[0].Kind != "opening_yes" {
			continue // Iguana not in the opening hand at this seed
		}
		e, openingAsk = candidate, d
		break
	}
	if e == nil {
		t.Fatal("no seed below 500 dealt Impatient Iguana to non-starting seat 0")
	}
	if e.G.IsStartingPlayer(0) {
		t.Fatal("the designation moved before Iguana was accepted")
	}
	if err := e.Submit(decision.Intent{Seq: openingAsk.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("accept opening effect: %v", err)
	}
	if !e.G.IsStartingPlayer(0) || e.G.IsStartingPlayer(1) {
		t.Fatalf("Iguana did not become starting player: %t/%t", e.G.IsStartingPlayer(0), e.G.IsStartingPlayer(1))
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.StartingPlayerChange && ev.Player == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("Iguana starting-player change was not logged")
	}
	if e.G.Active != 0 || e.G.Turn != 1 {
		t.Fatalf("turn one belongs to seat %d (turn %d), want the accepting seat 0", e.G.Active, e.G.Turn)
	}
}

// TestDeclinedImpatientIguanaKeepsTheRecordedStarter is the other half of the
// may choice: declining the opening round's ask leaves the toss winner as the
// recorded starting player and hands turn one to seat 1. The seed search is
// the acceptance test's, with the opposite answer.
func TestDeclinedImpatientIguanaKeepsTheRecordedStarter(t *testing.T) {
	t.Parallel()
	iguana := corpusCard(t, "Impatient Iguana")
	var e *Engine
	var openingAsk *decision.Decision
	for seed := uint64(1); seed < 500; seed++ {
		deck0 := mountainDeck(t, 40)
		deck0[0] = iguana
		candidate := New(Config{Seed: seed, Mulligans: 1, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{deck0, mountainDeck(t, 40)}})
		if !candidate.G.IsStartingPlayer(1) {
			continue
		}
		candidate.Advance()
		d := candidate.Pending()
		for d != nil && d.Kind == decision.KMulligan {
			if err := candidate.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
				t.Fatalf("seed %d mulligan keep: %v", seed, err)
			}
			d = candidate.Pending()
		}
		if d == nil || d.Kind != decision.KChoose || d.Player != 0 ||
			len(d.Options) == 0 || d.Options[0].Kind != "opening_yes" {
			continue
		}
		e, openingAsk = candidate, d
		break
	}
	if e == nil {
		t.Fatal("no seed below 500 dealt Impatient Iguana to non-starting seat 0")
	}
	if err := e.Submit(decision.Intent{Seq: openingAsk.Seq, Player: 0, Choices: []int{1}}); err != nil {
		t.Fatalf("decline opening effect: %v", err)
	}
	if !e.G.IsStartingPlayer(1) || e.G.IsStartingPlayer(0) {
		t.Fatalf("decline moved the starter: %t/%t", e.G.IsStartingPlayer(0), e.G.IsStartingPlayer(1))
	}
	if e.G.Active != 1 || e.G.Turn != 1 {
		t.Fatalf("turn one belongs to seat %d (turn %d), want the toss winner seat 1", e.G.Active, e.G.Turn)
	}
}

func TestMulliganDeclarationsPassRoundTheTable(t *testing.T) {
	t.Parallel()
	for _, seats := range []int{2, 3} {
		names := make([]string, seats)
		decks := make([][]*cards.Card, seats)
		for i := range names {
			names[i] = string(rune('a' + i))
			decks[i] = mountainDeck(t, 40)
		}
		e := New(Config{Seed: 1, Mulligans: 2, Names: names, Decks: decks})
		starter := e.G.StartingPlayer
		order := e.G.AliveFrom(starter)
		other := order[1]
		want := append([]state.PlayerID{}, order...)
		want = append(want, order[:2]...)
		want = append(want, starter)
		var got []state.PlayerID
		takenStarter, takenOther := 0, 0
		e.Advance()
		for len(got) < len(want) {
			d := e.Pending()
			if d == nil || d.Kind != decision.KMulligan || d.Options[0].Kind != "keep" {
				t.Fatalf("seats=%d declaration %d = %+v, want keep/mulligan ask", seats, len(got), d)
			}
			got = append(got, d.Player)
			choice := 0 // keep
			if d.Player == starter && takenStarter < 2 {
				choice, takenStarter = 1, takenStarter+1
			} else if d.Player == other && takenOther == 0 {
				choice, takenOther = 1, takenOther+1
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{choice}}); err != nil {
				t.Fatalf("seats=%d declaration %d: %v", seats, len(got), err)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("seats=%d declaration order %v, want CR 103.5 passes %v", seats, got, want)
		}
		if takenStarter != 2 || takenOther != 1 {
			t.Fatalf("seats=%d mulligans starter=%d other=%d, want 2 and 1", seats, takenStarter, takenOther)
		}
	}
}
