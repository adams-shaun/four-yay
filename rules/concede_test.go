package rules

// Tests for the M2d-3 "concede" priority option (controller R-M3). The shape
// is the least new surface that satisfies the determinism constraints: a
// "concede" option served last on every priority decision whose taken path
// emits the EXISTING PlayerLost event with Text "conceded" (CR 104.3a) --
// no new event kind, no Registry method, no mutation outside events.Apply.
// PlayerLost's Apply marks the seat Lost, the state-based sweep removes its
// permanents, and checkGameOver ends the game with the last remaining seat
// the winner (CR 104.2a) -- exactly the life-loss path, which is the point.
//
// Scope (R-M3, consciously): a seat can concede only when it holds the
// engine's one decision path that offers the option -- its own priority
// decision. There is no off-priority concede in this build; a forged intent
// against a decision the seat does not hold is rejected by Validate.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// highestPriorityOptionWithKind scans the pending decision's options, last to
// first, for an option of the given kind and returns it, failing the test if
// the decision is not a priority decision or the kind is not offered. There
// is at most one "concede" option; scanning from the end is what pins "last"
// when a test wants the ordering checked elsewhere.
func highestPriorityOptionWithKind(t *testing.T, e *Engine, kind string) decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	for i := len(d.Options) - 1; i >= 0; i-- {
		if d.Options[i].Kind == kind {
			return d.Options[i]
		}
	}
	t.Fatalf("no %q option offered on the pending priority decision: %+v", kind, d.Options)
	return decision.Option{}
}

// TestConcedeAtPriorityEndsTheGameForTheOtherSeat is the plan's step-1
// shape: seat 0 holds the game's first priority decision (turn 1 upkeep),
// chooses "concede", and the game must end with seat 0 Lost and seat 1 the
// winner -- GameOver with the last remaining seat, exactly the life-loss
// path. The log must carry the PlayerLost "conceded" event so a concede
// replays to the same chain head it was played to.
func TestConcedeAtPriorityEndsTheGameForTheOtherSeat(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 131, "Name:Bear\nManaCost:1 G\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e.Advance()
	// find the priority option whose Kind == "concede"; choose it.
	opt := highestPriorityOptionWithKind(t, e, "concede")
	submitChoices(t, e, opt.Index)
	if !e.G.Over || e.G.Winner != 1 || !e.G.Players[0].Lost {
		t.Fatalf("after seat 0 concedes: over=%v winner=%d seat0lost=%v", e.G.Over, e.G.Winner, e.G.Players[0].Lost)
	}
	if e.Pending() != nil {
		t.Fatalf("a finished game must not hold a decision: %+v", e.Pending())
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.PlayerLost && ev.Player == 0 && ev.Text == "conceded" {
			found = true
		}
	}
	if !found {
		t.Fatal("the log carries no PlayerLost event with Text \"conceded\" for seat 0")
	}
	replayCheck(t, e, cfg) // the conceded game replays to the same head
}

// TestConcedeIsTheLastOptionAfterPass pins the option ORDERING (R-M3:
// "always last"): the final option on a priority decision is "concede" and
// the option before it is "pass". legalActions' "Pass is last so a client
// can safely default to the final option" contract stops being true the
// moment concede exists -- a default-to-final client would concede on every
// priority -- so the new ordering must be explicit and pinned, not implicit.
func TestConcedeIsTheLastOptionAfterPass(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 132, "Name:Bear\nManaCost:1 G\nTypes:Creature\nPT:2/2\nOracle:x\n")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || len(d.Options) < 2 {
		t.Fatalf("expected a priority decision with options, got %+v", d)
	}
	last := d.Options[len(d.Options)-1]
	prev := d.Options[len(d.Options)-2]
	if last.Kind != "concede" || last.Label != "Concede" {
		t.Fatalf("final option = %+v, want Kind \"concede\" Label \"Concede\"", last)
	}
	if prev.Kind != "pass" {
		t.Fatalf("option before concede = %+v, want \"pass\"", prev)
	}
}

// TestConcedeOnlyOnThePriorityHolder: the enforcements around "who may
// concede" -- (a) the pending priority decision always offers "concede" to
// ITS OWN player, and (b) an intent forged against a decision the seat does
// not hold is rejected by Validate with the game untouched, so there is no
// off-priority concede path at all in this build (R-M3's out-of-scope note:
// real Magic allows concession any time; this fixture offers it exactly at
// the conceding seat's own priority, which the engine grants every living
// seat at least once per turn).
func TestConcedeOnlyOnThePriorityHolder(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 133, "Name:Bear\nManaCost:1 G\nTypes:Creature\nPT:2/2\nOracle:x\n")
	// Seat 0 holds the first priority decision; pass, so seat 1 holds the
	// next one. That decision must offer "concede" -- to seat 1.
	submitChoices(t, e, highestPriorityOptionWithKind(t, e, "pass").Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 1 {
		t.Fatalf("after seat 0 passes, expected seat 1's priority decision, got %+v", d)
	}
	highestPriorityOptionWithKind(t, e, "concede")
	concedeIdx := 0
	for i, o := range d.Options {
		if o.Kind == "concede" {
			concedeIdx = i
		}
	}
	before := len(e.L.Events)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{concedeIdx}}); err == nil {
		t.Fatal("seat 0's concede intent against seat 1's decision must be rejected")
	} else if !strings.Contains(err.Error(), "decision is for player 1") {
		t.Fatalf("rejection reason = %q", err)
	}
	if e.G.Players[0].Lost || e.G.Over || e.G.Players[1].Lost {
		t.Fatalf("rejected intent must not touch the game: seat0.%v seat1.%v over=%v",
			e.G.Players[0].Lost, e.G.Players[1].Lost, e.G.Over)
	}
	if len(e.L.Events) != before {
		t.Fatalf("a rejected intent must not reach the log: %d -> %d events", before, len(e.L.Events))
	}
	if len(e.L.Intents) != 1 { // only seat 0's genuine pass was recorded
		t.Fatalf("intents = %+v, want only the pass", e.L.Intents)
	}
}

// TestConcedeIsNotOfferedOnANonPriorityDecision: the mulligan round offers
// every living seat a decision that is NOT priority (KMulligan, R-8.4's
// round between the deal and turn 1). A seat holding that decision must not
// be offered "concede" -- the option lives only on the priority decision.
// The round completes normally (keep, keep) and turn 1's first priority
// decision then offers it.
func TestConcedeIsNotOfferedOnANonPriorityDecision(t *testing.T) {
	t.Parallel()
	m := card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	cfg := Config{Seed: 134, Names: []string{"a", "b"},
		Decks:     [][]*cards.Card{{m, m, m, m, m, m, m, m}, {m, m, m, m, m, m, m, m}},
		Mulligans: 1}
	e := New(cfg)
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KMulligan {
		t.Fatalf("expected the mulligan round's keep/mulligan decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "concede" {
			t.Fatalf("a non-priority decision must not offer concede: %+v", d.Options)
		}
	}
	// Complete the round: keep for both seats; nobody mulliganed, so the
	// bottoming phase is skipped and turn 1 begins with seat 0's priority.
	for e.Pending() != nil && e.Pending().Kind == decision.KMulligan {
		submitChoices(t, e, 0) // "keep", offered first in askKeepMulligan
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("after the round, expected seat 0's turn-1 priority decision, got %+v", d)
	}
	highestPriorityOptionWithKind(t, e, "concede")
}

// TestLastButOneConcedeEndsWithTheRightWinner: three seats; the last-but-one
// seat to concede ends the game and the winner must be the ONE seat that
// never conceded -- not "some seat". Seat 1 controls a Bear while it is
// alive; the moment it concedes, the state-based sweep removes its
// permanents exactly like a life-loss elimination (the same checkLoseConditions
// path), before the game ends on seat 2's concession.
func TestLastButOneConcedeEndsWithTheRightWinner(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 3)
	bear := onBoard(t, e, 1, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	concealed := map[state.PlayerID]bool{1: true, 2: true}
	for i := 0; i < 200 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("game stalled with no pending decision")
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("expected only priority decisions in this fixture, got %+v", d)
		}
		opt := highestPriorityOptionWithKind(t, e, "concede")
		if concealed[d.Player] {
			submitChoices(t, e, opt.Index)
		} else {
			submitChoices(t, e, highestPriorityOptionWithKind(t, e, "pass").Index)
		}
	}
	if !e.G.Over {
		t.Fatal("conceding the last-but-one seat must end the game")
	}
	if e.G.Draw {
		t.Fatal("a game with a real survivor is not a draw")
	}
	if e.G.Winner != 0 {
		t.Fatalf("winner = %d, want 0 (the only seat that never conceded)", e.G.Winner)
	}
	if !e.G.Players[1].Lost || !e.G.Players[2].Lost {
		t.Fatalf("both conceding seats must be Lost: %v %v", e.G.Players[1].Lost, e.G.Players[2].Lost)
	}
	if z := e.G.Obj(bear).Zone; z != state.ZCeased {
		t.Fatalf("the conceding seat's Bear zone = %s, want ceased (CR 800.4a sweep)", z)
	}
}

// TestConcedeInFourSeatsLeavesTheRestPlayingOn: seat 0 concedes at its own
// first priority in a 4-seat game; the match must go on with the other three,
// the conceding seat must never be asked another decision, and the natural
// end (every remaining seat decks out) must crown one of the survivors --
// never seat 0. This is also the acceptance-bot safety shape: it drives the
// real Submit path for hundreds of intents with "concede" offered on every
// priority decision and asserts none of them choose it for seat 0.
func TestConcedeInFourSeatsLeavesTheRestPlayingOn(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 4)
	// Seat 0 concedes at its own turn-1 upkeep decision.
	submitChoices(t, e, highestPriorityOptionWithKind(t, e, "concede").Index)
	if e.G.Over {
		t.Fatal("a 4-seat game must not end when one seat concedes")
	}
	if e.G.AliveCount() != 3 {
		t.Fatalf("alive = %d, want 3", e.G.AliveCount())
	}
	// Drive the rest of the match: pass for every seat; every decision must
	// belong to a living seat (never seat 0) and must offer concede, and the
	// game must reach its own end (the other three deck out, CR 704.5c).
	intents := 0
	for !e.G.Over && intents < 4000 {
		d := e.Pending()
		if d == nil {
			t.Fatal("game stalled with no pending decision")
		}
		if d.Player == 0 {
			t.Fatalf("the conceded seat was asked again: %+v", d)
		}
		// CR 514.1 (Task D1): a cleanup discard is a real decision a living
		// active player answers now. It is not a priority decision, has no
		// "concede"/"pass" option, and is never addressed to a conceded seat
		// (seat 0 never becomes active), so answer it naively (first-Max) and
		// continue rather than failing the way the pre-Task-D1 loop could.
		if d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "discard" {
			if d.Player == 0 {
				t.Fatalf("a discard was offered to the conceded seat: %+v", d)
			}
			submitDiscard(t, e, d.Options[0].Obj) // one card here; drives by identity
			intents++
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("expected only priority decisions in a creature-free fixture, got %+v", d)
		}
		highestPriorityOptionWithKind(t, e, "concede") // offered on every priority decision
		submitChoices(t, e, highestPriorityOptionWithKind(t, e, "pass").Index)
		intents++
	}
	if !e.G.Over {
		t.Fatalf("4-seat mountain game did not terminate within %d intents", intents)
	}
	if e.G.Players[0].Lost == false {
		t.Fatal("seat 0 must stay Lost")
	}
	if e.G.Winner == 0 {
		t.Fatal("the conceding seat cannot be the winner")
	}
	if e.G.Draw || e.G.Winner == 0 || e.G.Winner > 3 {
		t.Fatalf("over=%v winner=%d draw=%v", e.G.Over, e.G.Winner, e.G.Draw)
	}
}
