package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// livelockSeat panics with a *rules.LivelockError on its first decision --
// the shape the engine's livelock watcher (rules/livelock.go) aborts a
// stuck game with from inside Submit. The harness must convert that panic
// into a recorded outcome carrying the diagnostic, keep the rest of the
// batch going, and fail the run at the end -- never hang on it.
type livelockSeat struct{}

func (livelockSeat) Decide(context.Context, view.View, decision.Decision) (decision.Intent, error) {
	panic(&rules.LivelockError{
		Reason:   "repeating cycle",
		Object:   42,
		Repeats:  31,
		CycleLen: 13,
	})
}

func livelockCfg(t *testing.T) rules.Config {
	t.Helper()
	dir := corpusDirOrSkip(t)
	reg, err := testutil.OpenCorpusRegistry(dir)
	if err != nil {
		t.Fatalf("corpus: %v", err)
	}
	all := testutil.LegacyDeckNames()
	names := all[:2]
	decks := make([][]*cards.Card, 2)
	for i := range decks {
		decks[i], err = testutil.LoadRepoDeck(reg, names[i])
		if err != nil {
			t.Fatalf("deck %s: %v", names[i], err)
		}
	}
	return rules.Config{Seed: 0, Names: names, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Universe()}
}

// TestPlayMatchRecoversLivelockAsStalledOutcome pins playMatchOnce's half:
// the watcher's abort inside Submit comes back as a stalled outcome whose
// stallOn is "livelock" and whose livelock field carries the diagnostic --
// recorded against the game, so the fold can report it and the batch can
// continue -- and NOT as an error (an error would abort the whole matrix)
// and NOT as a hang.
func TestPlayMatchRecoversLivelockAsStalledOutcome(t *testing.T) {
	cfg := livelockCfg(t)
	seats := []seat.Seat{livelockSeat{}, livelockSeat{}}
	o, e, err := playMatchOnce(cfg, []string{"bot", "bot"}, seats, 0, 0, nil, nil)
	if err != nil {
		t.Fatalf("playMatchOnce: %v", err)
	}
	if !o.isStalled() || o.stallOn != "livelock" {
		t.Fatalf("outcome = stallOn %q, want \"livelock\" (stalled)", o.stallOn)
	}
	for _, want := range []string{"livelock detected", "repeating cycle", "repeated 31 time(s)", "cycle of 13 event(s)"} {
		if !strings.Contains(o.livelock, want) {
			t.Errorf("diagnostic %q missing %q", o.livelock, want)
		}
	}
	if e == nil {
		t.Fatal("playMatchOnce returned no engine for the aborted game")
	}
}

// TestBenchLivelockFailsRunButReportsTheOthers pins the batch contract on
// the single-pair fold: one livelocked game is marked LIVELOCK on its own
// line with the diagnostic beside it, the other games are still reported,
// the stall notice names the livelock count -- and the run FAILS (non-zero
// exit) at the end, because a livelock is an engine bug, not a slow game.
func TestBenchLivelockFailsRunButReportsTheOthers(t *testing.T) {
	play := func(seed uint64, pols []string) (gameOutcome, error) {
		if seed%2 == 1 { // game 1 (base seed 0): the livelocked one
			return gameOutcome{stallOn: "livelock", livelock: "livelock detected (repeating cycle): object 42, kind resolve, cycle of 13 event(s) repeated 31 time(s)"}, nil
		}
		return gameOutcome{winner: pols[0], winnerSeat: 0, starter: 0, starterSet: true}, nil
	}
	var buf bytes.Buffer
	err := bench(0, 3, 2, "bot", "bot", play, &buf)
	if err == nil {
		t.Fatal("bench must fail the run when a game livelocked")
	}
	if !strings.Contains(err.Error(), "livelock") {
		t.Errorf("error = %v, want a livelock summary", err)
	}
	out := buf.String()
	if !strings.Contains(out, "winner=LIVELOCK") {
		t.Errorf("output must mark the livelocked game's line:\n%s", out)
	}
	if !strings.Contains(out, "LIVELOCK: livelock detected (repeating cycle)") {
		t.Errorf("output must carry the diagnostic beside the game line:\n%s", out)
	}
	if !strings.Contains(out, "game 0:") || !strings.Contains(out, "game 2:") {
		t.Errorf("the other games must still be reported:\n%s", out)
	}
	if !strings.Contains(out, "aborted with a LIVELOCK") {
		t.Errorf("stall notice must name the livelock count:\n%s", out)
	}
}

// TestBenchCleanRunStaysClean: the livelock machinery must not disturb a
// run with none -- the notice stays silent and the run exits zero.
func TestBenchCleanRunStaysClean(t *testing.T) {
	play := func(seed uint64, pols []string) (gameOutcome, error) {
		return gameOutcome{winner: pols[0], winnerSeat: 0, starter: 0, starterSet: true}, nil
	}
	var buf bytes.Buffer
	if err := bench(0, 3, 2, "bot", "bot", play, &buf); err != nil {
		t.Fatalf("bench: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "LIVELOCK") || strings.Contains(out, "STALLED") {
		t.Errorf("clean run must print no livelock/stall notice:\n%s", out)
	}
}

// TestGrindContinuesAfterLivelockAndFails pins the grind contract: a
// livelocked game is recorded against the deck and seed, the deck's grind
// KEEPS PLAYING its remaining iterations, the diagnostic reaches the
// progress writer, the report's table counts it -- and runGrind fails at
// the end.
func TestGrindContinuesAfterLivelockAndFails(t *testing.T) {
	old := grindSeats
	grindSeats = func(uint64) []seat.Seat { return []seat.Seat{livelockSeat{}, livelockSeat{}} }
	defer func() { grindSeats = old }()

	dir := corpusDirOrSkip(t)
	reg, err := testutil.OpenCorpusRegistry(dir)
	if err != nil {
		t.Fatalf("corpus: %v", err)
	}
	deck, err := testutil.LoadRepoDeck(reg, testutil.RepoDeckNames()[0])
	if err != nil {
		t.Fatalf("deck: %v", err)
	}
	var prog bytes.Buffer
	g, err := grindOne(0, 0, "deck-under-test", deck, nil, false, time.Time{}, 3, 0, 0, nil, nil, &prog)
	if err != nil {
		t.Fatalf("grindOne itself must not fail -- a livelock is a recorded stall, not an abort: %v", err)
	}
	if g.iters != 3 || g.livelocks != 3 || g.stalls != 3 {
		t.Errorf("tally = iters %d stalls %d livelocks %d, want 3/3/3 (the grind continues after a livelock)",
			g.iters, g.stalls, g.livelocks)
	}
	if !strings.Contains(g.firstLivelock, "livelock detected") || !strings.Contains(g.firstLivelock, "iteration 0 (seed ") {
		t.Errorf("firstLivelock %q must carry the diagnostic and the seed", g.firstLivelock)
	}
	if !strings.Contains(prog.String(), "LIVELOCK: livelock detected") {
		t.Errorf("progress writer must carry the diagnostic:\n%s", prog.String())
	}

	var buf bytes.Buffer
	werr := writeGrindReport(&buf, 0, 0, 3, false, []grindDeck{g}, nil, nil)
	if werr == nil {
		t.Fatal("writeGrindReport must fail the run when a deck livelocked")
	}
	if !strings.Contains(buf.String(), "grind deck-under-test: LIVELOCK:") {
		t.Errorf("report must carry the diagnostic after the table:\n%s", buf.String())
	}
	if !strings.Contains(werr.Error(), "3 game(s) aborted with a livelock") {
		t.Errorf("error = %v, want the livelock summary", werr)
	}
}

// panicSeat panics with an ordinary value on its first decision -- the shape
// of any engine panic raised inside the drive loop other than the livelock
// watcher's. The harness records it as a "panic" abort carrying the value,
// seed and stack, exactly as it records a livelock: one bad game must not
// kill a multi-thousand-game sweep, and the run still fails at the end.
type panicSeat struct{}

func (panicSeat) Decide(context.Context, view.View, decision.Decision) (decision.Intent, error) {
	panic("rules: synthetic engine bug")
}

func TestPlayMatchRecoversEnginePanicAsAbortedOutcome(t *testing.T) {
	cfg := livelockCfg(t)
	seats := []seat.Seat{panicSeat{}, panicSeat{}}
	o, e, err := playMatchOnce(cfg, []string{"bot", "bot"}, seats, 0, 0, nil, nil)
	if err != nil {
		t.Fatalf("playMatchOnce: %v", err)
	}
	if !o.isStalled() || o.stallOn != "panic" {
		t.Fatalf("outcome = stallOn %q, want \"panic\" (stalled)", o.stallOn)
	}
	for _, want := range []string{"engine panic (seed 0, intent 0)", "rules: synthetic engine bug", "goroutine"} {
		if !strings.Contains(o.livelock, want) {
			t.Errorf("diagnostic missing %q:\n%s", want, o.livelock)
		}
	}
	if winnerLabel(o) != "PANIC" {
		t.Errorf("winnerLabel = %q, want PANIC", winnerLabel(o))
	}
	if e == nil {
		t.Fatal("playMatchOnce returned no engine for the aborted game")
	}
	play := func(seed uint64, pols []string) (gameOutcome, error) {
		if seed == 1 {
			return o, nil
		}
		return gameOutcome{winner: pols[0], winnerSeat: 0, starter: 0, starterSet: true}, nil
	}
	var buf bytes.Buffer
	if err := bench(0, 3, 2, "bot", "bot", play, &buf); err == nil || !strings.Contains(err.Error(), "1 of 3 game(s) aborted") {
		t.Fatalf("bench must fail the run naming the aborted game, got %v", err)
	}
}

// TestBenchLivelockLeavesTheWinRateDenominator pins the single-pair fold's
// denominator: a livelocked (or panicked) game credits no seat, so it must
// leave the win-rate denominator exactly like a -max-turns/-max-intents
// stall. Before the fix the multi-seat summary said "over 4 non-stalled
// game(s)" with one of four games aborted, and the A win rate divided by 4.
func TestBenchLivelockLeavesTheWinRateDenominator(t *testing.T) {
	play := func(seed uint64, pols []string) (gameOutcome, error) {
		if seed == 1 {
			return gameOutcome{stallOn: "livelock", livelock: "livelock detected (repeating cycle): object 7"}, nil
		}
		return gameOutcome{winner: pols[0], winnerSeat: 0, starter: 0, starterSet: true}, nil
	}
	var buf bytes.Buffer
	if err := bench(0, 4, 4, "bot", "bot", play, &buf); err == nil {
		t.Fatal("bench must fail the run when a game livelocked")
	}
	out := buf.String()
	if !strings.Contains(out, "win rates are over 3 non-stalled game(s)") {
		t.Errorf("stall notice must exclude the livelocked game from the denominator:\n%s", out)
	}
	// Every non-aborted game is won by seat 0; A holds seat 0 in games 0 and
	// 2 (aPlaysSeat), so A won 2 of the 3 counted games.
	if !strings.Contains(out, "A win rate: 66.7%") {
		t.Errorf("A win rate must be over the 3 non-aborted games:\n%s", out)
	}
}
