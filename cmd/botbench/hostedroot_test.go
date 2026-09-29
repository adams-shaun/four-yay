package main

// The hosted-root mode (BP-05, spec 2026-09-28-hosted-bot-packages §5.3):
// internal/bench's search branch, under Hooks.HonestRoot, hands the seat a
// redeal of the live position (searchseat.HonestRoot) instead of the live
// engine; botbench's -hosted-root flag switches it on and counts the
// refusals. These tests pin the three claims the flag and BP-20 depend on:
// the seat really receives a root (a fresh clone whose log carries the
// redeal's Secret events, never the live engine), `search`'s answers are
// exactly what the live-engine run answered (the INFERRED-exact parity the
// smoke re-measures), and a refused root plays the wrapped bot and is
// counted -- never a clairvoyant fallback.

import (
	"context"
	"encoding/json"
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
)

// probeSearchSeat wraps a SearchBot and records, per decision sequence
// number, which branch answered it (search or board), the event-log length of
// the engine handle DecideSearch received, and the intent it answered with.
// The board path is the bench's fallback, not the bot's own -- so every
// "search" record here went through the search branch.
type probeSearchSeat struct {
	*ss.SearchBot
	path    map[uint64]string
	evLens  map[uint64]int
	intents map[uint64]decision.Intent
}

func newProbeSearchSeat(seed uint64, opts ss.Options) *probeSearchSeat {
	return &probeSearchSeat{
		SearchBot: ss.NewSearchBot(seed, opts),
		path:      map[uint64]string{}, evLens: map[uint64]int{}, intents: map[uint64]decision.Intent{},
	}
}

func (p *probeSearchSeat) DecideSearch(ctx context.Context, env ss.Env, d decision.Decision) (decision.Intent, error) {
	p.path[d.Seq] = "search"
	p.evLens[d.Seq] = len(env.Engine.L.Events)
	in, err := p.SearchBot.DecideSearch(ctx, env, d)
	p.intents[d.Seq] = in
	return in, err
}

func (p *probeSearchSeat) DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	p.path[d.Seq] = "board"
	in, err := p.SearchBot.DecideBoard(ctx, b, d)
	p.intents[d.Seq] = in
	return in, err
}

func (p *probeSearchSeat) searchN() int { return countPath(p.path, "search") }

// hostedRootProbeConfig is the mono-pair smoke's own matchup (uw-tempo vs
// mono-red-prowess) as one rules.Config, with the search seat at seat 0 and
// the plain bot at seat 1.
func hostedRootProbeConfig(t *testing.T, seed uint64) rules.Config {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	da, err := testutil.LoadRepoDeck(reg, "uw-tempo")
	if err != nil {
		t.Fatal(err)
	}
	db, err := testutil.LoadRepoDeck(reg, "mono-red-prowess")
	if err != nil {
		t.Fatal(err)
	}
	return rules.Config{
		Seed: seed, Names: []string{"uw-tempo", "mono-red-prowess"}, Decks: [][]*cards.Card{da, db},
		Tokens: reg.Tokens, StartingLife: 20,
	}
}

// cheapSearchOpts cuts the teacher's sampling budget so the probe games stay
// seconds, not minutes: the comparison pins the ROOT HANDOFF and the parity,
// not the teacher's verdicts (the same shape
// TestBenchDriverFeedsTheTeacherLoop uses for its own comparison).
func cheapSearchOpts() ss.Options {
	opts := ss.Defaults()
	opts.Worlds, opts.Attempts = 2, 4
	return opts
}

// TestHostedRootSearchHandsTheSeatARootAndMatchesTheLiveEngine pins the two
// claims of the honest-root mode for `search`: at every search-branch
// decision the seat's engine handle is the redeal (its log is the live log
// plus the redeal's Secret events, so strictly longer than the live engine's
// at the same boundary), and the intents it answers are exactly what the
// live-engine run answered (INFERRED exact, §5.3 -- the parity smoke's
// stop-condition claim for the search entry). A refused redeal would show up
// as a board-path decision here; the probe asserts there are none.
func TestHostedRootSearchHandsTheSeatARootAndMatchesTheLiveEngine(t *testing.T) {
	cfg := hostedRootProbeConfig(t, 20260928)
	opts := cheapSearchOpts()
	const maxIntents = 120

	// Run A: the ordinary drive loop, the live engine in every Env.
	a := newProbeSearchSeat(cfg.Seed^1, opts)
	aOut, _, aErr := gbench.PlayGame(cfg, []seat.Seat{a, seat.NewBot(cfg.Seed ^ 2)}, 200, maxIntents, gbench.Hooks{})
	if aErr != nil {
		t.Fatalf("live-engine run: %v", aErr)
	}

	// Run B: the honest-root drive loop over the same game seed and fresh
	// seats. RootRefused counts the refusals the run may not take.
	refused := 0
	var reasons []string
	hooks := gbench.Hooks{HonestRoot: true, RootRefused: func(seatIdx int, d *decision.Decision, reason string) {
		refused++
		reasons = append(reasons, reason)
	}}
	b := newProbeSearchSeat(cfg.Seed^1, opts)
	bOut, _, bErr := gbench.PlayGame(cfg, []seat.Seat{b, seat.NewBot(cfg.Seed ^ 2)}, 200, maxIntents, hooks)
	if bErr != nil {
		t.Fatalf("honest-root run: %v", bErr)
	}

	// Precondition: the probe saw real search-branch decisions. A run with
	// none proves nothing about the handoff.
	if a.searchN() < 4 {
		t.Fatalf("live-engine run recorded only %d search-branch decisions (of %d); want a game that searches", a.searchN(), len(a.path))
	}
	if len(a.path) != len(b.path) {
		t.Fatalf("the two runs saw different decision counts: live %d, honest-root %d", len(a.path), len(b.path))
	}
	if refused != 0 {
		t.Fatalf("honest-root run refused %d redeal(s) (%v); the parity below is measured only over root-built decisions", refused, reasons)
	}
	if aOut != bOut {
		t.Fatalf("the two runs produced different outcomes: live %+v, honest-root %+v", aOut, bOut)
	}
	for seq, pathA := range a.path {
		pathB, ok := b.path[seq]
		if !ok {
			t.Fatalf("honest-root run has no decision at seq %d (live run does)", seq)
		}
		if pathA != pathB {
			t.Fatalf("seq %d: live run answered on the %s path, honest-root run on the %s path", seq, pathA, pathB)
		}
		if pathA != "search" {
			continue
		}
		// The honesty probe: the redeal is a fresh clone carrying the live
		// log PLUS the redeal's Secret events, so its log is strictly longer
		// than the live engine's at the same boundary. If the bench handed
		// the live engine instead (a clairvoyant fallback), the two lengths
		// would be equal.
		if b.evLens[seq] <= a.evLens[seq] {
			t.Fatalf("seq %d: honest-root engine log has %d events, live engine had %d -- the seat was not handed a redeal", seq, b.evLens[seq], a.evLens[seq])
		}
		if !reflect.DeepEqual(a.intents[seq], b.intents[seq]) {
			t.Fatalf("seq %d: search answered differently under the root: live %v, honest-root %v", seq, a.intents[seq], b.intents[seq])
		}
	}
}

// refusalProbeSeat is the refusal test's seat: like the probe seat, and it
// remembers the feed the driver handed it (the Decision hook reads it).
type refusalProbeSeat struct {
	*ss.SearchBot
	feed **ss.Feed
	path map[uint64]string
}

func (p *refusalProbeSeat) DecideSearch(ctx context.Context, env ss.Env, d decision.Decision) (decision.Intent, error) {
	p.path[d.Seq] = "search"
	if *p.feed == nil {
		*p.feed = env.Feed
	}
	in, err := p.SearchBot.DecideSearch(ctx, env, d)
	return in, err
}

func (p *refusalProbeSeat) DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	p.path[d.Seq] = "board"
	in, err := p.SearchBot.DecideBoard(ctx, b, d)
	return in, err
}

// TestHostedRootRefusalPlaysTheBotAndIsCounted pins the fail-closed half: a
// redeal that refuses (nil root) plays the wrapped bot at that decision --
// never the clairvoyant engine -- and is counted through Hooks.RootRefused.
//
// The refusal is forced by sabotaging the seat's own observation stream
// (test-only: the feed is the driver's read-only state, and the engine and
// the game log are untouched): the Decision hook injects one unparseable
// frame into the history after the first search-branch answer, and the
// feed's known-card tracker folds it at the next decision, errors, and stays
// dead for the rest of the game -- so every later redeal refuses with the
// tracker's reason.
func TestHostedRootRefusalPlaysTheBotAndIsCounted(t *testing.T) {
	cfg := hostedRootProbeConfig(t, 20260928)
	opts := cheapSearchOpts()
	const maxIntents = 120

	var feedPtr *ss.Feed
	probe := &refusalProbeSeat{SearchBot: ss.NewSearchBot(cfg.Seed^1, opts), feed: &feedPtr, path: map[uint64]string{}}
	seats := []seat.Seat{probe, seat.NewBot(cfg.Seed ^ 2)}

	refusals := 0
	var reasons []string
	poisoned := false
	hooks := gbench.Hooks{
		HonestRoot: true,
		RootRefused: func(seatIdx int, d *decision.Decision, reason string) {
			refusals++
			reasons = append(reasons, reason)
		},
		Decision: func(seatIdx int, d *decision.Decision, in decision.Intent, brd *botpolicy.Board) error {
			if seatIdx != 0 || poisoned || feedPtr == nil {
				return nil
			}
			// Sabotage the history: one frame whose Board is not JSON. The
			// tracker folds it at the next Known() call and the projection
			// is dead for the rest of the game.
			h := feedPtr.HistoryRef()
			h.Frames = append(h.Frames, searchprobe.Frame{Board: json.RawMessage("{")})
			poisoned = true
			return nil
		},
	}
	out, _, err := gbench.PlayGame(cfg, seats, 200, maxIntents, hooks)
	if err != nil {
		t.Fatalf("honest-root run with a sabotaged feed: %v", err)
	}
	if !poisoned {
		t.Fatalf("the sabotage hook never ran; the probe never answered a seat-0 decision")
	}
	if out.Turns <= 0 || out.Intents <= 0 {
		t.Fatalf("the game did not finish: %+v", out)
	}

	// The refusals carry fail-closed reasons and actually happened.
	if refusals == 0 {
		t.Fatalf("a sabotaged known-card projection produced %d refusals; the redeal did not refuse", refusals)
	}
	for _, r := range reasons {
		if r == "" {
			t.Fatalf("a refusal was counted with an empty reason")
		}
	}

	// Precondition: at least one decision answered on the search branch
	// before the sabotage took hold (the injected frame only breaks the
	// NEXT redeal onwards).
	first, ok := firstPathSearch(probe.path)
	if !ok {
		t.Fatalf("no seat-0 decision ever answered on the search branch (paths: %v)", probe.path)
	}
	// Every seat-0 decision after the sabotage must have played the wrapped
	// bot: the tracker's error is sticky, so no later redeal can be built.
	boardN, searchN := countPath(probe.path, "board"), 0
	for seq, p := range probe.path {
		if seq > first && p == "search" {
			searchN++
		}
	}
	if searchN != 0 {
		t.Fatalf("%d seat-0 decision(s) after the sabotaged projection still searched; the refusal must fail closed", searchN)
	}
	if boardN == 0 {
		t.Fatalf("no decision played the wrapped bot after the sabotage; the probe saw only %d decisions", len(probe.path))
	}
}

// firstPathSearch returns the smallest sequence number whose path is
// "search", or ok=false when none is.
func firstPathSearch(path map[uint64]string) (uint64, bool) {
	best, ok := uint64(0), false
	for seq, p := range path {
		if p != "search" {
			continue
		}
		if !ok || seq < best {
			best, ok = seq, true
		}
	}
	return best, ok
}

// TestHostedRootFlagReachesTheBenchHooks pins the botbench plumbing: with
// hostedRootEnabled set, playMatchOnceTraced's hooks carry HonestRoot (the
// seat's engine handle is a redeal, log strictly longer than the live
// engine's at the same boundary) and without it they do not. If the flag
// stopped being plumbed through, the two runs' log lengths would come out
// equal and this fails.
func TestHostedRootFlagReachesTheBenchHooks(t *testing.T) {
	cfg := hostedRootProbeConfig(t, 20260929)
	opts := cheapSearchOpts()
	const maxIntents = 120

	lensUnder := func() (map[uint64]int, int) {
		probe := newProbeSearchSeat(cfg.Seed^1, opts)
		seats := []seat.Seat{probe, seat.NewBot(cfg.Seed ^ 2)}
		_, _, err := playMatchOnceTraced(cfg, []string{"search", "bot"}, seats, 200, maxIntents, nil, nil, nil, traceDecisionMeta{})
		if err != nil {
			t.Fatalf("bench run: %v", err)
		}
		return probe.evLens, probe.searchN()
	}

	hostedRootEnabled = false
	liveLens, liveSearchN := lensUnder()
	hostedRootEnabled = true
	hostedRootRefusals.Store(0)
	rootLens, rootSearchN := lensUnder()
	hostedRootEnabled = false
	hostedRootRefusals.Store(0)

	if liveSearchN < 4 || rootSearchN < 4 {
		t.Fatalf("the probes saw only %d/%d search-branch decisions; want games that search", liveSearchN, rootSearchN)
	}
	if len(liveLens) != len(rootLens) {
		t.Fatalf("the two runs saw different decision counts: %d vs %d", len(liveLens), len(rootLens))
	}
	longer := 0
	for seq, l := range liveLens {
		rl, ok := rootLens[seq]
		if !ok {
			t.Fatalf("hosted-root run has no decision at seq %d (live run does)", seq)
		}
		if rl > l {
			longer++
		} else if rl < l {
			t.Fatalf("seq %d: hosted-root engine log SHORTER than the live engine's (%d < %d)", seq, rl, l)
		}
	}
	if longer == 0 {
		t.Fatalf("no decision saw a longer engine log under -hosted-root; the flag did not reach the bench hooks")
	}
	if hostedRootRefusals.Load() != 0 {
		t.Fatalf("the plumbing run refused %d redeal(s); the parity comparison below expects root-built decisions", hostedRootRefusals.Load())
	}
}

func countPath(path map[uint64]string, want string) int {
	n := 0
	for _, p := range path {
		if p == want {
			n++
		}
	}
	return n
}
