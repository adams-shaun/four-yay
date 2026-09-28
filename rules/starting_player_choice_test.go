package rules

// CR 103.1's second half: the winner of the pre-game toss chooses who takes
// the first turn. rules.New draws and announces the toss, then leaves the
// choice available; a harness that can answer calls Engine.AskStartingPlayer
// before Advance, and any caller that cannot gets the deterministic R-9
// fallback (the toss winner) from Advance. This file pins both halves, the
// botpolicy arm, and the replay of a game whose choice was really answered.
// Every game here is built with NewStartingPlayerChoice (rules/engine.go) --
// the harness-facing constructor that offers the ask; plain New is the R-9
// no-host fallback and never sets tossChoice.

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// startingPlayerAsked reports whether l recorded the CR 103.1 winner-chooses
// ask -- mirroring replay/replay.go's own helper, which this package cannot
// import (rules -> replay would be a cycle). The log is the only source of
// truth for whether a conditional ask happened.
func startingPlayerAsked(l *events.Log) bool {
	if l == nil {
		return false
	}
	for _, ev := range l.Events {
		if ev.Kind == events.DecisionAsk && ev.Text == string(decision.KStartingPlayer) {
			return true
		}
	}
	return false
}

// optionForSeat returns the offered option naming seat p, or fails.
func optionForSeat(t *testing.T, d *decision.Decision, p state.PlayerID) decision.Option {
	t.Helper()
	for _, o := range d.Options {
		if o.Player == p {
			return o
		}
	}
	t.Fatalf("decision %s offers no option for seat %d: %+v", d.Kind, p, d.Options)
	return decision.Option{}
}

// TestStartingPlayerChoiceIsPosedToTheTossWinner is the core CR 103.1 test:
// New leaves the choice available, AskStartingPlayer poses it to the toss
// winner (the decision's own Player, never the eventual starter), and the
// winner can name the OTHER seat, which becomes both the recorded starting
// player and turn 1's active seat.
func TestStartingPlayerChoiceIsPosedToTheTossWinner(t *testing.T) {
	t.Parallel()
	cfg := tossedTwoSeat(t, 1, 0) // measured: seed 1 tosses to seat 1
	e := NewStartingPlayerChoice(cfg)
	notes := tossNotes(e)
	if len(notes) != 1 {
		t.Fatalf("precondition: %d toss Notes, want 1", len(notes))
	}
	winner := notes[0].Player
	other := state.PlayerID(0)
	if winner == 0 {
		other = 1
	}
	if winner == other {
		t.Fatalf("precondition: winner %d equals the other seat", winner)
	}

	d := e.AskStartingPlayer()
	if d == nil {
		t.Fatal("New left the CR 103.1 choice available but AskStartingPlayer returned no decision")
	}
	if d.Kind != decision.KStartingPlayer {
		t.Fatalf("choice kind = %s, want %s", d.Kind, decision.KStartingPlayer)
	}
	if d.Player != winner {
		t.Fatalf("choice addressed to seat %d, want the toss winner %d", d.Player, winner)
	}
	// Precondition: both living seats are offered, so "choose the other seat"
	// is a real alternative to the default.
	if opt := optionForSeat(t, d, winner); opt.Kind != "player" {
		t.Fatalf("self option kind = %q, want player", opt.Kind)
	}
	opt := optionForSeat(t, d, other)

	// The pending decision is the one AskStartingPlayer returned.
	if p := e.Pending(); p == nil || p.Kind != decision.KStartingPlayer {
		t.Fatalf("pending = %+v, want the posed starting-player decision", p)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: winner, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit the choice: %v", err)
	}
	e.Advance()

	if e.G.StartingPlayer != other {
		t.Fatalf("StartingPlayer = %d, want the chosen seat %d", e.G.StartingPlayer, other)
	}
	if !e.G.IsStartingPlayer(other) || e.G.IsStartingPlayer(winner) {
		t.Fatalf("designation = other:%t winner:%t, want only the chosen seat",
			e.G.IsStartingPlayer(other), e.G.IsStartingPlayer(winner))
	}
	if e.G.Active != other {
		t.Fatalf("turn 1 active = %d, want the chosen seat %d", e.G.Active, other)
	}
}

// TestStartingPlayerChoiceDefaultsToTheTossWinner pins the R-9 fallback: a
// caller with no decision channel (a scenario test, the fuzzer) never calls
// AskStartingPlayer, and Advance resolves the choice to the toss winner --
// the pre-choice seat -- so the game proceeds exactly as before.
func TestStartingPlayerChoiceDefaultsToTheTossWinner(t *testing.T) {
	t.Parallel()
	cfg := tossedTwoSeat(t, 1, 0)
	e := NewStartingPlayerChoice(cfg)
	notes := tossNotes(e)
	if len(notes) != 1 {
		t.Fatalf("precondition: %d toss Notes, want 1", len(notes))
	}
	winner := notes[0].Player
	// Precondition: the unposed choice is really available and unposed.
	if !e.tossChoice.active {
		t.Fatal("New did not leave the CR 103.1 choice available")
	}
	if e.Pending() != nil {
		t.Fatalf("New posed a decision (%+v) instead of leaving the choice unposed", e.Pending())
	}

	e.Advance()

	if e.tossChoice.active {
		t.Fatal("the choice stayed available after Advance defaulted it")
	}
	if e.G.StartingPlayer != winner {
		t.Fatalf("default StartingPlayer = %d, want the toss winner %d", e.G.StartingPlayer, winner)
	}
	if e.G.Active != winner {
		t.Fatalf("default turn 1 active = %d, want the toss winner %d", e.G.Active, winner)
	}
	// The default path never posits the ask, so the log carries no
	// starting-player DecisionAsk.
	if startingPlayerAsked(e.L) {
		t.Fatal("the default path recorded a starting-player DecisionAsk")
	}
}

// TestStartingPlayerChoiceAnyLivingSeatFourSeats: at four seats the winner
// may name a seat that is neither itself nor the next in turn order, and the
// engine both records and starts with exactly that seat.
func TestStartingPlayerChoiceAnyLivingSeatFourSeats(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b", "c", "d"}
	e := NewStartingPlayerChoice(Config{Seed: 3, Names: names, Mulligans: 0,
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}})
	notes := tossNotes(e)
	if len(notes) != 1 {
		t.Fatalf("precondition: %d toss Notes, want 1", len(notes))
	}
	winner := notes[0].Player
	d := e.AskStartingPlayer()
	if d == nil {
		t.Fatal("no choice posed at four seats")
	}
	if len(d.Options) != 4 {
		t.Fatalf("offered %d seats, want 4: %+v", len(d.Options), d.Options)
	}
	// Pick a seat that is neither the winner nor the winner's next seat, so
	// the assertion cannot pass by coincidence of turn order.
	pick := state.PlayerID((int(winner) + 2) % 4)
	if pick == winner {
		t.Fatalf("precondition: pick %d equals the winner", pick)
	}
	opt := optionForSeat(t, d, pick)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: winner, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	e.Advance()
	if e.G.StartingPlayer != pick || e.G.Active != pick {
		t.Fatalf("chosen seat %d: StartingPlayer=%d Active=%d", pick, e.G.StartingPlayer, e.G.Active)
	}
}

// TestStartingPlayerBotArmNamesItselfAndValidates runs the bot policy's own
// answer through Decision.Validate on the live ask: the arm must name the
// offering seat (the deterministic default) and whatever it returns must be a
// legal answer, or the hosted bot would livelock on a rejected intent.
func TestStartingPlayerBotArmNamesItselfAndValidates(t *testing.T) {
	t.Parallel()
	cfg := tossedTwoSeat(t, 1, 0)
	e := NewStartingPlayerChoice(cfg)
	d := e.AskStartingPlayer()
	if d == nil {
		t.Fatal("no choice posed")
	}
	b := newTestBot(7)
	in := b.answer(e, d)
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer rejected: %v", err)
	}
	chosen := d.Chosen(in)
	if len(chosen) != 1 {
		t.Fatalf("bot chose %d options, want 1", len(chosen))
	}
	if chosen[0].Player != d.Player {
		t.Fatalf("bot chose seat %d, want itself (the offering seat %d)", chosen[0].Player, d.Player)
	}
	if got := botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), d, newTestBot(1).r); len(got.Choices) != 1 ||
		got.Choices[0] != chosen[0].Index {
		t.Fatalf("botpolicy.Decide chose %v, want the self option index %d", got.Choices, chosen[0].Index)
	}
}

// TestStartingPlayerChoiceReplaysWhenAnswered drives a game through the real
// posed-and-answered choice (the bot names itself) and replays it: the choice
// intent is recorded, the replay re-poses the ask from the log's DecisionAsk,
// and the chain heads agree.
func TestStartingPlayerChoiceReplaysWhenAnswered(t *testing.T) {
	t.Parallel()
	cfg := tossedTwoSeat(t, 1, 0)
	e := NewStartingPlayerChoice(cfg)
	d := e.AskStartingPlayer()
	if d == nil {
		t.Fatal("precondition: no choice posed")
	}
	b := newTestBot(7)
	if err := e.Submit(b.answer(e, d)); err != nil {
		t.Fatalf("answer the choice: %v", err)
	}
	e.Advance()
	passAll(t, e, 200)

	if !startingPlayerAsked(e.L) {
		t.Fatal("precondition: the answered run must record the starting-player ask")
	}
	re, err := replayFor(cfg, e.L)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if re.L.Head() != e.L.Head() {
		t.Fatalf("replayed head %s, live head %s", re.L.Head(), e.L.Head())
	}
	if re.RNGDraws() != e.RNGDraws() {
		t.Fatalf("replayed rng draws %d, live %d", re.RNGDraws(), e.RNGDraws())
	}
}
