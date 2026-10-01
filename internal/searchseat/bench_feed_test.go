package searchseat_test

// The driver-side pins: internal/bench.PlayGame must feed a search seat
// EXACTLY the observation stream the teacher's own loop assembles (the same
// capture points, the same recorded answers), and a search seat with no
// covered kinds must play byte-identically to the plain bot it wraps. The
// first is what makes "the seat plays the teacher" mean the teacher that was
// measured; the second is the delegation contract.

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	ss "github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

// feedSearchBot remembers the feed the driver handed it, so the test can read
// the exact history the seat was scored against.
type feedSearchBot struct {
	*ss.SearchBot
	feed **ss.Feed
}

func (f *feedSearchBot) DecideSearch(ctx context.Context, env ss.Env, d decision.Decision) (decision.Intent, error) {
	*f.feed = env.Feed
	return f.SearchBot.DecideSearch(ctx, env, d)
}

// TestBenchDriverFeedsTheTeacherLoop pins the bench's search-seat branch
// against the teacher loop re-implemented inline (the pre-Feed playGame
// shape, with the seat's own Choose at the eligible decisions): the two games
// must emit identical event streams and the bench's feed must carry the same
// frames and recorded answers. Corpus-gated: Sample needs real card
// definitions.
func TestBenchDriverFeedsTheTeacherLoop(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	da, err := testutil.LoadRepoDeck(reg, "mono-red-prowess")
	if err != nil {
		t.Fatal(err)
	}
	db, err := testutil.LoadRepoDeck(reg, "mono-blue-tempo")
	if err != nil {
		t.Fatal(err)
	}
	const seed = uint64(30000000)
	setup := searchprobe.PublicGame{
		Names: []string{"mono-red-prowess", "mono-blue-tempo"},
		Decks: [][]*cards.Card{da, db}, Tokens: reg.Tokens,
	}
	cfg := rules.Config{Seed: seed, Names: setup.Names, Decks: setup.Decks, Tokens: setup.Tokens, StartingLife: setup.StartingLife}
	const maxIntents = 80

	// Cheap search knobs: the comparison pins the DRIVER's assembly, not the
	// teacher's verdict, so the sampling budget is cut to keep the test fast.
	// Both sides get the same knobs, so they must agree either way.
	opts := ss.Defaults()
	opts.Worlds, opts.Attempts = 2, 4

	// The oracle loop: the pre-Feed inline shape.
	e := rules.New(cfg)
	e.Advance()
	rngs := searchprobe.BotRandoms(seed, 2)
	board := botpolicy.NewBoard(2)
	actor := state.PlayerID(0)
	collector := searchprobe.NewCollector(actor)
	h := searchprobe.History{Actor: actor, Answers: map[int][]searchprobe.Action{}}
	pos := 0
	for steps := 0; !e.G.Over && steps < maxIntents; steps++ {
		d := e.Pending()
		if d == nil {
			break
		}
		b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &board)
		in := botpolicy.Decide(b, d, rngs[d.Player])
		f, err := collector.Capture(e, e.L.Events[pos:])
		if err != nil {
			t.Fatalf("oracle capture: %v", err)
		}
		h.Frames = append(h.Frames, f)
		if d.Player == actor {
			if ss.Eligible(d, opts) {
				// The seat's own contract: Eligible runs Choose, which returns
				// the bot's intent on every failure/fallback path -- so the
				// oracle plays exactly what the seat would play.
				chosen, _, _ := ss.Choose(setup, h, collector, e, d, in, f, opts)
				in = chosen
			}
			a, err := collector.Actions(d, in)
			if err != nil {
				t.Fatalf("oracle answer record: %v", err)
			}
			h.Answers[len(h.Frames)-1] = a
		}
		pos = len(e.L.Events)
		if err := e.Submit(in); err != nil {
			t.Fatalf("oracle submit: %v", err)
		}
	}

	// The bench: same cfg, seat 0 the search seat (the ctor's own seed
	// derivation: game seed ^ seat+1).
	var feedPtr *ss.Feed
	seats := []seat.Seat{
		&feedSearchBot{SearchBot: ss.NewSearchBot(seed^1, opts), feed: &feedPtr},
		seat.NewBot(seed ^ 2),
	}
	_, _, berr := gbench.PlayGame(cfg, seats, 200, maxIntents, gbench.Hooks{})
	if berr != nil {
		t.Fatalf("bench play: %v", berr)
	}
	if feedPtr == nil {
		t.Fatalf("the driver never handed the seat a feed")
	}
	got := feedPtr.History()
	if feedPtr.Frames() != len(h.Frames) {
		t.Fatalf("bench frames = %d, teacher loop = %d (a capture point differs)", feedPtr.Frames(), len(h.Frames))
	}
	for i := range h.Frames {
		of, gf := h.Frames[i], got.Frames[i]
		if len(of.Events) != len(gf.Events) {
			t.Fatalf("frame %d: bench events = %d, teacher loop = %d", i, len(gf.Events), len(of.Events))
		}
		for j := range of.Events {
			if !reflect.DeepEqual(of.Events[j], gf.Events[j]) {
				t.Fatalf("frame %d event %d differs from the teacher loop's", i, j)
			}
		}
		if !reflect.DeepEqual(of.Board, gf.Board) {
			t.Fatalf("frame %d: board differs from the teacher loop's", i)
		}
	}
	if !reflect.DeepEqual(got.Answers, h.Answers) {
		t.Fatalf("bench answers differ from the teacher loop's recorded answers")
	}
}

// TestSearchSeatWithoutCoveredKindsIsTheBotExactly pins the delegation
// contract end to end: a search seat whose Kinds gate admits nothing answers
// every decision with the wrapped bot's answer, so the game is byte-identical
// to the same game played by the plain bot. Corpus-free (SampleDecks decks).
func TestSearchSeatWithoutCoveredKindsIsTheBotExactly(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 2)
	cfg := rules.Config{Seed: 21, Names: names, Decks: decks}
	const seatSeed = uint64(77)
	play := func(a seat.Seat) []string {
		out := make([]string, 0, 256)
		o, e, err := gbench.PlayGame(cfg, []seat.Seat{a, seat.NewBot(seatSeed ^ 2)}, 200, 200, gbench.Hooks{})
		if err != nil {
			t.Fatalf("play: %v", err)
		}
		out = append(out, fmt.Sprintf("winner=%d draw=%v stall=%q turns=%d intents=%d", o.WinnerSeat, o.Draw, o.StallOn, o.Turns, o.Intents))
		for _, ev := range e.L.Events {
			out = append(out, fmt.Sprintf("%d/%d/%d", ev.Kind, ev.Player, ev.Obj))
		}
		return out
	}
	delegate := ss.NewSearchBot(seatSeed, ss.Options{Kinds: map[string]bool{}})
	if !reflect.DeepEqual(play(delegate), play(seat.NewBot(seatSeed))) {
		t.Fatalf("a search seat with no covered kinds did not reproduce the wrapped bot's game")
	}
}

// TestSearchSeatGameCompletesWithCoverage drives the first hundred intents
// of a game through the bench with the teacher's measured default knobs and
// pins that the search seat is actually asked (asked > 0) and that the
// sampler produces worlds often enough that some decisions are covered
// (covered > 0) -- the bench integration the cost report and the gate both
// depend on. The intent cap keeps the package inside its test budget; the
// full-game shape is what the botbench gate runs. Wall-clock hooks stay nil
// here: the seat must be answer-identical untimed, and the timing assertions
// belong to cmd/botbench's cost report.
func TestSearchSeatGameCompletesWithCoverage(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	da, err := testutil.LoadRepoDeck(reg, "mono-red-prowess")
	if err != nil {
		t.Fatal(err)
	}
	db, err := testutil.LoadRepoDeck(reg, "mono-blue-tempo")
	if err != nil {
		t.Fatal(err)
	}
	const seed = uint64(30000000)
	setup := searchprobe.PublicGame{
		Names: []string{"mono-red-prowess", "mono-blue-tempo"},
		Decks: [][]*cards.Card{da, db}, Tokens: reg.Tokens,
	}
	cfg := rules.Config{Seed: seed, Names: setup.Names, Decks: setup.Decks, Tokens: setup.Tokens, StartingLife: setup.StartingLife}

	watch := ss.Watch
	var asked, covered int
	ss.Watch = func(dg ss.Diag) {
		asked++
		if dg.Trace.Covered {
			covered++
		}
	}
	defer func() { ss.Watch = watch }()

	o, _, err := gbench.PlayGame(cfg, []seat.Seat{
		ss.NewSearchBot(seed^1, ss.Defaults()),
		seat.NewBot(seed ^ 2),
	}, 200, 100, gbench.Hooks{})
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if o.IsStalled() && o.StallOn != "intents" {
		t.Fatalf("game stalled on %s", o.StallOn)
	}
	if asked == 0 {
		t.Fatalf("no decision was ever asked: the driver or the seat never opened a search")
	}
	if covered == 0 {
		t.Fatalf("asked %d decisions, none covered -- the sampler never produced worlds through the bench path", asked)
	}
}
