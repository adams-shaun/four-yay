package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task fx37: pin the event stream of the CR 800.4f departed-chooser path —
// releasePendingDecisionOfDepartedPlayer (rules/sba.go). A player leaves the
// game while one of their OWN decisions is outstanding; that decision can
// never be answered, so the engine clears e.pending and, if a resolution was
// suspended (e.resume != nil, a mid-resolution ask), resumes it with a nil
// answer so the asking instruction gets no selection and the rest of the
// effect chain — including CR 608.2n's completion tail — still runs.
//
// This engine's contract is byte-identical deterministic replay over a
// sha256 hash-chained event log, so a path that reaches the right game state
// by a different sequence (or number) of events is a divergence even when
// every board assertion passes. The departed-chooser path is exactly the
// shape where that hides: it runs a resolution tail with a nil answer, and
// nothing before this test asserted which events that tail emits. This test
// pins the event count, the kinds and order of the resumption tail, the
// resulting chain head as a constant, and that a log-only replay
// (replayFromLog, the rules-internal reconstruction that folds the recorded
// events straight through events.Apply — a state change that bypassed an
// event emit is invisible to it) rebuilds the identical Game.
//
// Fixture choice: a REAL corpus card — Chain Lightning — because its
// DealDamage->CopySpellAbility chain poses a genuine mid-resolution
// UnlessCost$ ask (the ResumeKind "unless_pay" shape), which is precisely
// the suspended-resolution arm of releasePendingDecisionOfDepartedPlayer.
// A synthetic fixture could manufacture the same ask, but Chain Lightning
// reaches it through the real cast/resolve path with no manufactured SAs,
// and its pay-or-copy oracle is the repo's own established
// departed-payer probe (see TestCR608CompletedSpellLeavesStackAfterDeparted-
// Payer). The departure is a concession (CR 104.3a): it is the only lawful
// way to leave while your own resolution is suspended on the stack, and it
// is emitted directly rather than through an intent — a concession is a
// priority option, but the suspended ask is NOT a priority decision, so no
// Submit can express it at this point, and a 3-seat game keeps playing on
// after the loss.
func TestDepartedChooserResumptionEventStreamIsDeterministic(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cfg := Config{Seed: 42, Tokens: reg.Tokens}
	for _, n := range []string{"ur-delver", "death-n-taxes", "ur-delver"} {
		cfg.Names = append(cfg.Names, n)
		cfg.Decks = append(cfg.Decks, testutil.RepoDeck(t, reg, n))
	}
	// Seat 0 is the protagonist; seatZeroStart advances the seed until the
	// CR 103.1 toss starts seat 0. It runs here, after the decks are
	// appended -- on the deck-less Config it would be a no-op (New with no
	// Names draws no toss).
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	chain := crAbortMove(t, e, 0, "Chain Lightning", state.ZHand)
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -17})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 2})
	e.askPriority(0)
	crAbortAnswer(t, e, "Chain Lightning", crAbortOption(t, e, "Chain Lightning", "cast", chain))
	crResolutionPlayerTarget(t, e, 1)
	crResolutionRound(t, e)

	// We are standing exactly on the shape under test: a resolution
	// suspended on player 1's own UnlessCost$ paid-or-copy ask.
	d := e.Pending()
	if d == nil || d.Player != 1 || d.ResumeKind != "unless_pay" || !e.Suspended() || e.G.Players[1].Lost {
		t.Fatalf("portended state: want a suspended unless_pay ask at a living seat 1; "+
			"pending=%+v suspended=%v life=%d lost=%v", d, e.Suspended(), e.G.Players[1].Life, e.G.Players[1].Lost)
	}

	// The path under test: the departure itself, then the checkStateBased
	// call that runs the CR 800.4a sweep and the release hook.
	start := len(e.L.Events)
	e.emit(events.Event{Kind: events.PlayerLost, Player: 1, Text: "conceded"})
	e.checkStateBased()
	path := e.L.Events[start:]
	// 64 -> 65 (hostb1): a suspended resolution used to log its priority
	// grant at SUSPENSION time, from handlePriority's pass branch, while the
	// engine was parked on this very unless_pay ask and nobody had priority.
	// That grant now happens when the resolution actually completes, which is
	// inside this measured path, so the tail gains one Priority. Two adjacent
	// Priority events is the correct shape and not a duplicate: it is exactly
	// what an UNSUSPENDED resolution has always produced here (the
	// resolution's own "back to active" marker, then grantPriority opening
	// the next round), so the suspended path now matches the unsuspended one
	// instead of logging its marker a question early.
	if len(path) != 65 {
		t.Fatalf("departed-chooser path emitted %d events, want 65 (events change => a divergence is hiding here)",
			len(path))
	}

	// Kinds and order. The departure sweep (CR 800.4a: the departed player's
	// owned objects cease to exist) is the move_zone "player left the game"
	// group; the resumption tail is the single chain completion MoveZone,
	// then the resumed priority round's Priority and DecisionAsk. Encode the
	// kind stream as run lengths so both the count and the order are pinned.
	runs := kindRuns(path)
	want := [][2]int{{int(events.PlayerLost), 1}, {int(events.MoveZone), 61},
		{int(events.Priority), 2}, {int(events.DecisionAsk), 1}}
	if len(runs) != len(want) {
		t.Fatalf("tail kind runs = %v, want %v", runs, want)
	}
	for i := range want {
		if runs[i] != want[i] {
			t.Fatalf("tail kind run[%d] = %v, want %v (full runs %v)", i, runs[i], want[i], runs)
		}
	}

	// CR 608.2n's completion tail: the suspended Chain Lightning leaves the
	// stack for the graveyard — the last MoveZone before the resumed
	// priority — and no Resolve re-fires (it fired once, before the ask).
	// hostb1 shifted these indices by one: the tail is now
	// [... MoveZone x61][Priority x2][DecisionAsk], because the resolution's
	// own "back to active" marker is emitted at COMPLETION (here) rather than
	// at suspension, ahead of grantPriority's marker for the resumed round.
	// Both are asserted, so a future change that drops either one fails.
	if got := path[len(path)-4]; got.Kind != events.MoveZone || got.Obj != chain ||
		got.From != state.ZStack || got.To != state.ZGraveyard {
		t.Fatalf("completion tail = %+v (seq %d), want MoveZone of the chain from stack to graveyard",
			got, got.Seq)
	}
	if got := path[len(path)-3]; got.Kind != events.Priority {
		t.Fatalf("event at -3 = %s (seq %d), want the completed resolution's Priority", got.Kind, got.Seq)
	}
	if got := path[len(path)-2]; got.Kind != events.Priority {
		t.Fatalf("event before last = %s (seq %d), want Priority resuming the round", got.Kind, got.Seq)
	}
	if got := path[len(path)-1]; got.Kind != events.DecisionAsk {
		t.Fatalf("last event = %s (seq %d), want DecisionAsk for the resumed round", got.Kind, got.Seq)
	}

	// The 61 MoveZone events: 60 are the departure sweep, and exactly one is
	// the chain's completion move. Everything else in the tail must be the
	// resumed-round preface (Priority, DecisionAsk) — no stray Resolve,
	// ModeChosen, Note, Damage or LifeChange from the abandoned continuation.
	sweeps, completions, other := 0, 0, 0
	for _, ev := range path {
		switch {
		case ev.Kind == events.MoveZone && ev.Text == "player left the game":
			sweeps++
		case ev.Kind == events.MoveZone && ev.Obj == chain:
			completions++
		case ev.Kind == events.Priority || ev.Kind == events.DecisionAsk:
			// the resumed round's preface — counted implicitly below.
		case ev.Kind == events.PlayerLost:
			// the departure record itself (the first event of the path).
		default:
			other++
		}
		if ev.Player == 1 {
			// The departure PlayerLost (the first event) is necessarily
			// attributed to seat 1 as the record of the loss; nothing the
			// resumption tail emits may be.
			if ev.Kind != events.PlayerLost {
				t.Fatalf("resumption event attributed to the departed player 1: seq %d %s", ev.Seq, ev.Kind)
			}
		}
	}
	if sweeps != 60 {
		t.Fatalf("departure sweep emitted %d 'player left the game' moves, want 60", sweeps)
	}
	if completions != 1 {
		t.Fatalf("chain completion move emitted %d times, want 1", completions)
	}
	if other != 0 {
		t.Fatalf("resumption tail emitted %d unexpected events", other)
	}
	if !e.G.Players[1].Lost || e.G.Over {
		t.Fatalf("concession must leave a three-seat game running: lost=%v over=%v", e.G.Players[1].Lost, e.G.Over)
	}
	if e.G.Obj(chain).Zone != state.ZGraveyard || len(e.G.Stack) != 0 {
		t.Fatalf("chain zone=%s stack=%v, want graveyard and an empty stack", e.G.Obj(chain).Zone, e.G.Stack)
	}

	// The chain head as a constant: a reordering that preserves counts would
	// still change it, so this is the assertion a count-only test cannot make.
	// Regenerated for jj-f01, which is exactly such a reordering: the cast
	// proposal became a transaction (CR 601.2a push, then the 601.2c target
	// ask, then 601.2h payment), so a targeted cast emits the same events in a
	// different order. Every assertion above this line -- the resumption tail's
	// event count, the concession leaving a three-seat game running, the chain
	// in the graveyard with an empty stack -- still holds unchanged, which is
	// what says the reordering is all that moved.
	// Regenerated again for hostb1, which is an ADDITION rather than a
	// reordering: this scenario is a suspended resolution, so it gains the
	// completion Priority the fix relocates out of the suspension point. The
	// count assertion above moved 64 -> 65 for that one event and every other
	// assertion here -- the concession leaving a three-seat game running, the
	// chain in the graveyard with an empty stack, the departure sweep's 61
	// MoveZone events, no stray Resolve from the abandoned continuation --
	// holds unchanged, which is what says the added grant is all that moved.
	// Regenerated for rv2a: the sole stream change is moving the existing
	// toss Note before the first Shuffle (CR 103.1 precedes 103.2-103.4).
	// rv2a starting-player state is genesis-derived through events.Apply, so
	// it preserves this recorded stream while a replay of Config derives the
	// same designation before folding it.
	// Regenerated for game-long damage-by-source provenance
	// (agent-20260923T114033Z-a57ee463): every landed Damage event now emits a
	// DamageProvenance bookkeeping event beside it, so the whole-log head
	// moves while this resumption TAIL -- the departure sweep's 60 MoveZone
	// events, the one completion move, the resumed preface, and `other == 0`
	// -- is unchanged, which is what says only the added record moved it.
	if got := e.L.Head(); got != "77b3b266283ab5a9" {
		t.Fatalf("chain head = %s, want 77b3b266283ab5a9", got)
	}

	// T21-e: a log-only replay must reconstruct the identical Game. If any
	// state change on this path bypassed an event emit (the release clears
	// e.resume and e.pending by hand, the natural place such a bypass would
	// hide), a reconstruction that folds only the logged events cannot know
	// about it and this comparison is what catches it.
	fresh := replayFromLog(t, cfg, e.L.Events)
	if diff := diffGames(e.G, fresh); diff != "" {
		t.Fatalf("log-only replay diverges:\n%s", diff)
	}
}

// kindRuns run-length encodes a slice of events as (kind, count) pairs.
func kindRuns(evs []events.Event) [][2]int {
	var runs [][2]int
	for _, ev := range evs {
		if n := len(runs); n > 0 && runs[n-1][0] == int(ev.Kind) {
			runs[n-1][1]++
			continue
		}
		runs = append(runs, [2]int{int(ev.Kind), 1})
	}
	return runs
}

// A seat that pays its last life to a PayLife mana ability loses to
// state-based actions while that ability's colour choice is posed. Releasing
// the departed seat's decision must also end the flow it routed: the next
// seat's priority is posed with no choose flow armed and no parked colour
// activation (the A/B payment-plan mirror measured both surviving).
func TestStateBasedLossDuringManaColourAskClearsTheFlow(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 3)
	toMain1(t, e)
	src := onBoard(t, e, 0, "Name:Confluence Test\nTypes:Land\nA:AB$ Mana | Cost$ T PayLife<1> | Produced$ Any | SpellDescription$ Add one mana of any color.\nOracle:x\n")
	e.G.Obj(src).SummonSick = false
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 1 - e.G.Players[0].Life})
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	pick := -1
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == src {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("no activate option for the fixture in %#v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{pick}}); err != nil {
		t.Fatal(err)
	}
	if !e.G.Players[0].Lost {
		t.Fatalf("seat 0 life %d, lost %v: the fixture did not reach the state-based loss", e.G.Players[0].Life, e.G.Players[0].Lost)
	}
	if d := e.Pending(); d == nil || d.Player == 0 {
		t.Fatalf("pending = %#v, want a surviving seat's decision", d)
	}
	if e.choosing != chooseNone || e.manaColorActivation != nil {
		t.Errorf("after seat 0 left the game: choosing=%d manaColorActivation set=%v, want the lost seat's colour flow cleared",
			e.choosing, e.manaColorActivation != nil)
	}
}
