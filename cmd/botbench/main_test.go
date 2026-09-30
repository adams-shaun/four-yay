package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// corpusDirOrSkip resolves the repo root the way testutil.CorpusRegistry and
// cmd/mtgsim do -- `git rev-parse --show-toplevel`, never a hard-coded
// relative path -- and Skips when .cards/ is absent, so this package still
// passes on a clean checkout with nothing fetched. The bench drives real
// repo decks through the same engine path a served match uses, so its tests
// need the real corpus; everything else in this package is pure functions
// and needs none.
func corpusDirOrSkip(t *testing.T) string {
	t.Helper()
	// Run the child git with cards.GitEnv(): a test binary's cwd is this
	// package directory, not the repo root, and git exports GIT_INDEX_FILE --
	// and in a linked worktree GIT_DIR -- to every hook it runs, as a path
	// that names the enclosing repository. An inherited relative GIT_* would
	// redirect "-C ." at a .git that does not resolve from here, so rev-parse
	// fails and the corpus is never found (the gitiso hazard; see
	// cards/gitenv.go and internal/testutil/decks.go).
	cmd := exec.Command("git", "-C", ".", "rev-parse", "--show-toplevel")
	cmd.Env = cards.GitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("could not resolve git repo root: %v", err)
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), ".cards")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("no .cards/ corpus present -- run `make fetch-cards compile-cards`")
	}
	return dir
}

// TestBenchIsDeterministic pins invariant 2: the same (base seed, policies,
// games) twice produces byte-identical output. Nothing in the path --
// engine, seat, seat assignment, report -- may read the wall clock or range
// over a map in an order that reaches the output. Both runs drive the real
// 2-seat engine path (rules.New, per-seat bota, view.Project), so the
// byte-equality covers the whole chain, not just the report formatting.
func TestBenchIsDeterministic(t *testing.T) {
	dir := corpusDirOrSkip(t)
	var b1, b2 bytes.Buffer
	for _, buf := range []*bytes.Buffer{&b1, &b2} {
		// maxTurns=0 keeps this an uncapped determinism run, exactly today's
		// behaviour; the turn watchdog must not be what makes it pass.
		if err := run(11, 3, 2, 0, 0, "bot", "bot", dir, 0, 0, false, buf); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	if b1.String() != b2.String() {
		t.Fatalf("two runs with identical inputs differed:\n--- run 1 ---\n%s\n--- run 2 ---\n%s",
			b1.String(), b2.String())
	}
}

// TestSeatAssignmentAlternates pins the M1 invariant: over an even N, A
// holds each seat in exactly half the games. With the (game+seat) rule A
// holds seat 0 on even games and seat 1 on odd games, so a deck-list
// advantage (each seat keeps its own deck list for the whole run) is spread
// equally across the policies instead of masquerading as one of them being
// better -- the FIRST TURN is not part of this any more, since the CR 103.1
// toss draws the starting seat per game (the report's starting-player split
// is the play/draw measurement now).
func TestSeatAssignmentAlternates(t *testing.T) {
	const games, seats = 8, 2
	held := make([]int, seats)
	for g := 0; g < games; g++ {
		for s := 0; s < seats; s++ {
			if aPlaysSeat(g, s) {
				held[s]++
			}
		}
	}
	for s := 0; s < seats; s++ {
		if want := games / 2; held[s] != want {
			t.Errorf("seat %d: A held it %d of %d games, want %d (assignment must alternate)", s, held[s], games, want)
		}
	}
}

// TestTheIntervalWidensOnASmallSample pins the tool's reason for existing: a
// thin sample must report a materially wider interval than a large one at
// the same rate, instead of a misleadingly crisp number. The pure-function
// form (same observed rate .5 at both sample sizes) isolates the effect of
// n -- a test that derived its rates from real game outcomes would be
// measuring the bot, not the interval, and would flake as those outcomes
// change under later bot work. At p̂=.5 the Wald half-width is
// 1.96·√(.25/n) against n, so n=10 is ~4.5x wider than n=200; the
// assertion demands 3x so the regression is unmistakable.
func TestTheIntervalWidensOnASmallSample(t *testing.T) {
	bigLo, bigHi := ci95(100, 200)
	smallLo, smallHi := ci95(5, 10)
	bigW, smallW := bigHi-bigLo, smallHi-smallLo
	if !(smallW > 3*bigW) {
		t.Errorf("interval at n=10 (%.3f..%.3f) must be materially wider than at n=200 (%.3f..%.3f): width %.3f vs %.3f",
			smallLo, smallHi, bigLo, bigHi, smallW, bigW)
	}
}

// TestPerGameSeedVariesWithTheIndex pins M3's non-negotiable: game i must
// be seeded from base+i, so no two games in a run are the same game. Bare
// determinism (TestBenchIsDeterministic) cannot see this mutation -- a
// bench that ground out the same game N times over would still be
// byte-identical run over run -- which is exactly why the per-game line in
// the report prints the seed next to every result: the run shows "the same
// game N times" openly. This test keeps the seed function honest at the
// root instead of waiting for the symptom.
func TestPerGameSeedVariesWithTheIndex(t *testing.T) {
	const base = 7
	for i := 0; i < 16; i++ {
		for j := i + 1; j < 16; j++ {
			si, sj := gameSeed(base, i), gameSeed(base, j)
			if si == sj {
				t.Errorf("gameSeed(%d, %d) == gameSeed(%d, %d) == %d: per-game seeds must vary with the index",
					base, i, base, j, si)
			}
		}
	}
}

// summaryRe is the shape of the report block bench() appends after the
// per-game lines; the E2E and attribution tests below parse it to prove the
// run actually reported, that the numbers are coherent with each other, and
// that the seat split is in the output. The A/B line's optional same-policy
// marker suffix is swallowed by the `.*`, and the seat line is the two-seat
// form (a `-seats` run above 2 prints per-seat counts without a rate; no
// test here uses that shape).
//
// Groups: 1 played, 2 A wins, 3 B wins, 4 draws, 5 A rate, 6 CI lo,
// 7 CI hi, 8 seat 0 wins, 9 seat 1 wins, 10 seat-0 rate, 11 seat-0 CI lo,
// 12 seat-0 CI hi, 13 mean turns.
var summaryRe = regexp.MustCompile(
	`(?m)^games played: (\d+)$\n` +
		`^A wins: (\d+)  B wins: (\d+)  draws: (\d+).*$\n` +
		`^A win rate: ([\d.]+)%  95% CI \[([\d.]+)%, ([\d.]+)%\].*$\n` +
		`^seat 0 wins: (\d+)  seat 1 wins: (\d+)  seat 0 win rate: ([\d.]+)%  95% CI \[([\d.]+)%, ([\d.]+)%\].*$\n` +
		`^mean turns per game: ([\d.]+)$`)

// atoi and atof unwrap the summary parse results; the regex already matched,
// so a conversion failure is a bug in the test or the format, not in the
// bench.
func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return n
}

func atof(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(err)
	}
	return f
}

// runSynthetic drives bench with a synthetic match player -- no engine game
// is played -- and returns the parsed summary block. Synthetic outcomes are
// what the attribution tests pin: real game outcomes would couple the
// assertions to what the bot happens to play, which later bot work is
// allowed to change. Both sides are named "bot" in every synthetic run,
// which is exactly the collision the attribution fix is about.
func runSynthetic(t *testing.T, games, seats int, play matchPlayer) []string {
	t.Helper()
	var buf bytes.Buffer
	if err := bench(0, games, seats, "bot", "bot", play, &buf); err != nil {
		t.Fatalf("bench: %v", err)
	}
	m := summaryRe.FindStringSubmatch(buf.String())
	if m == nil {
		t.Fatalf("report block missing or malformed:\n%s", buf.String())
	}
	return m
}

// TestSamePolicyDoesNotCreditOneSideWithEverything is the regression for
// the by-name attribution defect: while both sides were named "bot", every
// non-draw winner matched the -a case first and A was credited with all of
// them -- a confident 100% with a zero-width interval on the only run that
// existed. Attribution must follow the winning seat: A holds seat 0 on even
// games and seat 1 on odd games, so a fixed seat-0 winner over an even N
// splits the run exactly in half. A pass here means the tally reads the
// seat, not the name.
func TestSamePolicyDoesNotCreditOneSideWithEverything(t *testing.T) {
	const games, seats = 8, 2
	m := runSynthetic(t, games, seats, func(_ uint64, _ []string) (gameOutcome, error) {
		return gameOutcome{winner: "bot", winnerSeat: 0, turns: 10, intents: 50}, nil
	})
	aWins, bWins := atoi(m[2]), atoi(m[3])
	if aWins != games/2 || bWins != games/2 {
		t.Errorf("same-policy run, seat 0 won all %d games: A = %d, B = %d, want %d/%d (attribution must follow the seat, not the name)",
			games, aWins, bWins, games/2, games/2)
	}
	// The degenerate line must also say it is degenerate, not read like a
	// policy comparison.
	if !strings.Contains(strings.Join(m, "\n"), "same policy") {
		t.Errorf("same-policy run: A/B line carries no same-policy marker:\n%s", strings.Join(m, "\n"))
	}
}

// TestWinsAreAttributedBySeatNotName pins the direction the tally reads:
// a win from a seat B holds is credited to B even when both sides carry the
// same name. Both games of the run end with seat 1 winning; A holds seat 1
// only on odd games, so exactly one of the two wins is A's -- the other is
// B's. Under the old by-name attribution every winner (named "bot") matched
// the -a case first and A took both.
func TestWinsAreAttributedBySeatNotName(t *testing.T) {
	m := runSynthetic(t, 2, 2, func(_ uint64, _ []string) (gameOutcome, error) {
		return gameOutcome{winner: "bot", winnerSeat: 1, turns: 10, intents: 50}, nil
	})
	aWins, bWins := atoi(m[2]), atoi(m[3])
	if aWins != 1 || bWins != 1 {
		t.Errorf("two seat-1 wins, A held seat 1 once: A = %d, B = %d, want 1/1", aWins, bWins)
	}
	seat0, seat1 := atoi(m[8]), atoi(m[9])
	if seat0 != 0 || seat1 != 2 {
		t.Errorf("seat wins = %d/%d, want 0/2 (both winners sat in seat 1)", seat0, seat1)
	}
}

// TestTheSummaryReportsSeatWins: the seat split is part of the summary, not
// something a reader must reconstruct by grepping the per-game lines. Every
// game's winner sits in seat 1 here, so the seat line must report 0/2 with
// the seat-0 rate at 0% beside its (degenerate, Wald-edge) CI -- the CI
// behaviour itself is TestTheIntervalWidensOnASmallSample's job; this test
// only pins that the line exists in the summary and adds up.
func TestTheSummaryReportsSeatWins(t *testing.T) {
	const games = 2
	m := runSynthetic(t, games, 2, func(_ uint64, _ []string) (gameOutcome, error) {
		return gameOutcome{winner: "bot", winnerSeat: 1, turns: 10, intents: 50}, nil
	})
	seat0, seat1 := atoi(m[8]), atoi(m[9])
	if seat0 != 0 || seat1 != games {
		t.Errorf("seat wins = %d/%d, want 0/%d", seat0, seat1, games)
	}
	if rate := atof(m[10]); rate != 0 {
		t.Errorf("seat 0 win rate = %.1f%%, want 0.0%%", rate)
	}
	// The per-seat counts partition the non-draw wins.
	if seat0+seat1 != atoi(m[2])+atoi(m[3]) {
		t.Errorf("seat wins %d+%d do not match A+B %s+%s", seat0, seat1, m[2], m[3])
	}
}

// TestShortEndToEndRun is the required short end-to-end check: a small N
// actually completes and reports. It is deliberately two games -- the
// whole point of the interval test above is that you do not need many to
// prove the harness works, and every game here is a full 2-seat engine
// play, measured at ~60ms each against the warm corpus cache on this box
// (TEST_HISTORY.md records what the whole suite costs at commit time).
// It asserts the report's arithmetic: the win/draw counts partition the
// games played, the stated rate is the wins over games, the CI brackets
// that rate, the seat split partitions the non-draw wins, and the mean
// turns is over the same denominator.
func TestShortEndToEndRun(t *testing.T) {
	dir := corpusDirOrSkip(t)
	var buf bytes.Buffer
	if err := run(0, 2, 2, 0, 0, "bot", "bot", dir, 200, 0, false, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()

	gameLines := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "game ") {
			gameLines++
		}
	}
	if gameLines != 2 {
		t.Errorf("expected 2 per-game lines, got %d:\n%s", gameLines, out)
	}

	// With both sides "bot" the A/B line must be marked same-policy, not
	// read like a policy comparison.
	if !strings.Contains(out, "same policy") {
		t.Errorf("same-policy run: A/B line is not marked:\n%s", out)
	}

	m := summaryRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("report block missing or malformed:\n%s", out)
	}
	played, aWins, bWins, draws := atoi(m[1]), atoi(m[2]), atoi(m[3]), atoi(m[4])
	rate, lo, hi := atof(m[5]), atof(m[6]), atof(m[7])
	seat0, seat1 := atoi(m[8]), atoi(m[9])
	seatRate := atof(m[10])
	meanTurns := atof(m[13])

	if played != 2 {
		t.Errorf("games played = %d, want 2", played)
	}
	if aWins+bWins+draws != played {
		t.Errorf("A+B+draws = %d+%d+%d = %d, does not partition games played %d",
			aWins, bWins, draws, aWins+bWins+draws, played)
	}
	ratePct := float64(aWins) / float64(played) * 100
	if diff := rate - ratePct; diff > 0.05 || diff < -0.05 {
		t.Errorf("reported rate %.1f%% does not match %d/%d games", rate, aWins, played)
	}
	if lo > rate || hi < rate {
		t.Errorf("CI [%.1f%%, %.1f%%] does not bracket the reported rate %.1f%%", lo, hi, rate)
	}
	if seat0+seat1 != aWins+bWins {
		t.Errorf("seat wins %d+%d do not partition the A+B wins %d+%d", seat0, seat1, aWins, bWins)
	}
	seatRatePct := float64(seat0) / float64(played) * 100
	if diff := seatRate - seatRatePct; diff > 0.05 || diff < -0.05 {
		t.Errorf("seat 0 rate %.1f%% does not match %d/%d games", seatRate, seat0, played)
	}
	if meanTurns <= 0 {
		t.Errorf("mean turns per game = %.1f, want > 0", meanTurns)
	}
}

// ---- deck-pair matrix mode ----

// matrixText runs runPairs + writeMatrixText over the synthetic play and
// returns the text report. It keeps the matrix report tests away from the
// engine (the attribution and CI assertions are testing the report logic,
// not the bot), the same separation the single-pair synthetic tests use.
func matrixText(t *testing.T, games, workers int, pairs []pairDef, play pairPlayer) string {
	t.Helper()
	results, err := runPairs(0, games, "a", "b", pairs, play, workers, nil)
	if err != nil {
		t.Fatalf("runPairs: %v", err)
	}
	var buf bytes.Buffer
	if err := writeMatrixText(&buf, "a", "b", 0, games, results, false); err != nil {
		t.Fatalf("writeMatrixText: %v", err)
	}
	return buf.String()
}

// TestFullPairsIteratesSorted pins the pair generator: N decks must yield
// the N*(N-1)/2 unordered pairs, strictly sorted (a < b and lexicographically
// ascending sequence), no duplicates, and the FIRST pair must be the default
// single-pair run's decks -- names[0]:names[1] of the same sorted deck list
// `run` seats -- so a matrix row reproduces it. The pair is DERIVED from the
// list, never hard-coded: which deck sorts first changes whenever a deck is
// added (avengers-assemble displaced death-n-taxes once already). N is the repo deck directory's size, whatever it is now
// (12 Legacy decks plus the m38 commander decks: the pair count is a
// property of the list, not a pinned constant). Sorting is the whole
// guarantee that pair order never depends on map iteration -- the mutation
// TestMatrixReportOrderIsSorted checks the report, this checks the
// generator.
func TestFullPairsIteratesSorted(t *testing.T) {
	names := testutil.RepoDeckNames()
	if len(names) < 2 {
		t.Fatalf("expected at least 2 repo decks, got %d: %v", len(names), names)
	}
	ps := fullPairs(names)
	want := len(names) * (len(names) - 1) / 2
	if len(ps) != want {
		t.Fatalf("fullPairs(%d decks) = %d pairs, want %d (= N*(N-1)/2)", len(names), len(ps), want)
	}
	if want := (pairDef{names[0], names[1]}).String(); ps[0].String() != want {
		t.Errorf("first pair = %s, want %s (the default single-pair run seats names[0] and names[1]; a matrix row must reproduce it)", ps[0], want)
	}
	seen := map[string]bool{}
	prev := ""
	for _, p := range ps {
		if p.a >= p.b {
			t.Errorf("pair %s not in a<b order", p)
		}
		s := p.String()
		if seen[s] {
			t.Errorf("duplicate pair %s", s)
		}
		seen[s] = true
		if prev != "" && s <= prev {
			t.Errorf("pairs not strictly ascending after %s: %s (order must be sorted, not map order)", prev, s)
		}
		prev = s
	}
}

// TestParsePairs covers the -pairs spec: "all" expands to every unordered
// pair over the current deck list (N*(N-1)/2), the named "a:b,c:d" form
// parses, and an unknown deck name is rejected.
func TestParsePairs(t *testing.T) {
	names := testutil.RepoDeckNames()

	ps, err := parsePairs("all", names)
	if err != nil {
		t.Fatalf("parsePairs(all): %v", err)
	}
	if want := len(names) * (len(names) - 1) / 2; len(ps) != want {
		t.Errorf("parsePairs(all) = %d pairs, want %d", len(ps), want)
	}

	ps, err = parsePairs("death-n-taxes:dimir-tempo,tron:ur-delver", names)
	if err != nil {
		t.Fatalf("parsePairs(named): %v", err)
	}
	if len(ps) != 2 || ps[0].String() != "death-n-taxes:dimir-tempo" || ps[1].String() != "tron:ur-delver" {
		t.Errorf("parsePairs(named) = %v, want [death-n-taxes:dimir-tempo tron:ur-delver]", ps)
	}

	if _, err := parsePairs("death-n-taxes:not-a-deck", names); err == nil {
		t.Error("parsePairs with an unknown deck name returned no error")
	}
}

// TestMatrixTradesPoliciesEveryGame pins the property that makes a matrix a
// policy measurement rather than a deck measurement: within each pair, seats
// still trade policies every game, so each policy holds each seat in exactly
// half of an even run. If policies stopped trading (e.g. A always on seat 0),
// the deck list a seat holds would masquerade as a policy advantage.
func TestMatrixTradesPoliciesEveryGame(t *testing.T) {
	const games = 6
	byPolicy := map[string]int{} // policy*2+seat -> count
	var mu sync.Mutex
	play := func(pos int, seed uint64, pols []string) (gameOutcome, error) {
		mu.Lock()
		for s := 0; s < 2; s++ {
			byPolicy[pols[s]+"/"+strconv.Itoa(s)]++
		}
		mu.Unlock()
		return gameOutcome{winner: "a", winnerSeat: 0, turns: 5, intents: 10}, nil
	}
	if _, err := runPairs(0, games, "a", "b", []pairDef{{a: "dnt", b: "dim"}}, play, 1, nil); err != nil {
		t.Fatalf("runPairs: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, pol := range []string{"a", "b"} {
		for s := 0; s < 2; s++ {
			want := games / 2
			if got := byPolicy[pol+"/"+strconv.Itoa(s)]; got != want {
				t.Errorf("policy %q held seat %d in %d games, want %d (policies must trade seats within a pair)",
					pol, s, got, want)
			}
		}
	}
}

// TestMatrixGamesArePerPair pins that -games means games PER PAIR, not the
// total: each pair tallies exactly `games` games (A+B+draws == games, not
// the total), the report header states it unambiguously, and the pooled
// total
// is games x the pair count. This is the regression that catches a "make
// -games mean the total" mutation, which a reader of a per-pair row would
// silently misread.
func TestMatrixGamesArePerPair(t *testing.T) {
	const games = 7
	play := func(pos int, seed uint64, pols []string) (gameOutcome, error) {
		return gameOutcome{winner: "a", winnerSeat: 0, turns: 9, intents: 20}, nil
	}
	pairs := []pairDef{{a: "d1", b: "d2"}, {a: "d3", b: "d4"}}
	results, err := runPairs(0, games, "a", "b", pairs, play, 1, nil)
	if err != nil {
		t.Fatalf("runPairs: %v", err)
	}
	for _, r := range results {
		if got := r.aWins + r.bWins + r.draws; got != games {
			t.Errorf("pair %s tallied %d games, want %d per pair (-games is PER PAIR, not total)", r.pd, got, games)
		}
	}
	out := matrixText(t, games, 1, pairs, play)
	if !strings.Contains(out, "games per pair: 7") {
		t.Errorf("report must state games-per-pair explicitly:\n%s", out)
	}
	if !strings.Contains(out, "7 games PER PAIR (total 14 games)") {
		t.Errorf("header must state per-pair count unambiguously:\n%s", out)
	}
}

// TestPooledCIPoolsCountsNotRates pins that the pooled CI is computed over
// pooled COUNTS (sum of wins over sum of games), not by averaging the
// per-pair win rates. With unequal pair sizes the two disagree (0.0 and 0.10
// average to 0.05; 100 in 1010 is 0.099), so a mean-rate pooling mutation is
// caught. Real matrices use equal per-pair N where the two coincide
// numerically -- exactly why this guard needs unequal denominators to prove
// the arithmetic it relies on.
func TestPooledCIPoolsCountsNotRates(t *testing.T) {
	results := []pairResult{
		{pd: pairDef{a: "d1", b: "d2"}, games: 10, aWins: 0, bWins: 10, seatWins: [2]int{0, 10}, totalTurns: 100},
		{pd: pairDef{a: "d3", b: "d4"}, games: 1000, aWins: 100, bWins: 900, seatWins: [2]int{100, 900}, totalTurns: 9000},
	}
	wantRate := 100.0 / 1010.0 // pooled counts, NOT (0.00+0.10)/2 = 0.05
	lo, hi := ci95(100, 1010)
	var buf bytes.Buffer
	// games param only drives the header's per-pair line; the pooled math
	// reads each pair's own game count.
	if err := writeMatrixText(&buf, "a", "b", 0, 10, results, false); err != nil {
		t.Fatalf("writeMatrixText: %v", err)
	}
	out := buf.String()
	wantPct := wantRate * 100
	wantPctStr := strconv.FormatFloat(wantPct, 'f', 1, 64) // "9.9"
	if !strings.Contains(out, "pooled a win rate: "+wantPctStr+"%") {
		t.Errorf("pooled rate must pool counts (100/1010 = 9.9%%), not average rates (5.0%%):\n%s", out)
	}
	wantLo, wantHi := strconv.FormatFloat(lo*100, 'f', 1, 64), strconv.FormatFloat(hi*100, 'f', 1, 64)
	if !strings.Contains(out, "["+wantLo+"%, "+wantHi+"%]") {
		t.Errorf("pooled CI must be the Wald interval on pooled counts (%s..%s):\n%s", wantLo, wantHi, out)
	}
}

// TestMatrixReportOrderIsSorted pins that the report prints pairs in the
// deterministic sorted generation order (never map iteration) by asserting
// each tabular row's deck pair equals fullPairs' output element for element.
func TestMatrixReportOrderIsSorted(t *testing.T) {
	names := testutil.RepoDeckNames()
	ps := fullPairs(names)
	play := func(pos int, seed uint64, pols []string) (gameOutcome, error) {
		return gameOutcome{winner: "a", winnerSeat: 0, turns: 3, intents: 5}, nil
	}
	results, err := runPairs(0, 1, "a", "b", ps, play, 4, nil)
	if err != nil {
		t.Fatalf("runPairs: %v", err)
	}
	var buf bytes.Buffer
	if err := writeMatrixText(&buf, "a", "b", 0, 1, results, false); err != nil {
		t.Fatalf("writeMatrixText: %v", err)
	}
	// Table rows are aligned by tabwriter into space padding, so a row's deck
	// pair is its first whitespace field. Match each row against the known
	// pair list (by position) to prove the report follows sorted generation
	// order rather than any map iteration.
	order := make(map[string]int, len(ps))
	for i, p := range ps {
		order[p.String()] = i
	}
	row := 0
	for _, ln := range strings.Split(buf.String(), "\n") {
		fields := strings.Fields(ln)
		if len(fields) == 0 {
			continue
		}
		if idx, ok := order[fields[0]]; ok {
			if idx != row {
				t.Fatalf("pair %q sits at report row %d but its sorted index is %d (report must follow sorted pair order, not map order)",
					fields[0], row, idx)
			}
			row++
		}
	}
	if row != len(ps) {
		t.Errorf("expected %d pair rows in the report, got %d", len(ps), row)
	}
}

// TestMatrixDeterministicUnderWorkers pins that the matrix result and its
// report are byte-identical regardless of how many parallel workers run the
// pairs -- the concurrency the matrix mode adds must not leak into the
// deterministic report. Every pair's tally is written to its own slice
// position and read back in pair order, so worker count is orthogonal to
// the output.
func TestMatrixDeterministicUnderWorkers(t *testing.T) {
	play := func(pos int, seed uint64, pols []string) (gameOutcome, error) {
		if seed%2 == 0 {
			return gameOutcome{winner: "a", winnerSeat: 0, turns: int32(pos), intents: int(seed)}, nil
		}
		return gameOutcome{winner: "b", winnerSeat: 1, turns: int32(seed), intents: int(seed)}, nil
	}
	pairs := fullPairs(testutil.RepoDeckNames())
	r1, err1 := runPairs(3, 50, "a", "b", pairs, play, 1, nil)
	r8, err8 := runPairs(3, 50, "a", "b", pairs, play, 8, nil)
	if err1 != nil || err8 != nil {
		t.Fatalf("runPairs: %v / %v", err1, err8)
	}
	if len(r1) != len(r8) {
		t.Fatalf("result counts differ: %d vs %d", len(r1), len(r8))
	}
	for i := range r1 {
		if r1[i] != r8[i] {
			t.Errorf("result %d differs between workers=1 and workers=8: %+v vs %+v", i, r1[i], r8[i])
		}
	}
	var b1, b2 bytes.Buffer
	if err := writeMatrixText(&b1, "a", "b", 3, 50, r1, false); err != nil {
		t.Fatal(err)
	}
	if err := writeMatrixText(&b2, "a", "b", 3, 50, r8, false); err != nil {
		t.Fatal(err)
	}
	if b1.String() != b2.String() {
		t.Errorf("text reports differ between workers=1 and workers=8")
	}
}

// TestMatrixEndToEnd runs the matrix path through the real engine (two
// named pairs, 2 games each) and asserts the report both deterministically
// repeats and contains a row per pair -- the smallest possible matrix is
// still a real two-pair measurement, not a formatting exercise.
func TestMatrixEndToEnd(t *testing.T) {
	dir := corpusDirOrSkip(t)
	pairs, err := parsePairs("death-n-taxes:dimir-tempo,mono-red-goblins:tron", testutil.RepoDeckNames())
	if err != nil {
		t.Fatalf("parsePairs: %v", err)
	}
	var b1, b2 bytes.Buffer
	if err := runMatrix(0, 2, 2, "bot", "bot", dir, "text", pairs, 2, 200, 0, false, nil, &b1, io.Discard); err != nil {
		t.Fatalf("runMatrix: %v", err)
	}
	out := b1.String()
	for _, want := range []string{"death-n-taxes:dimir-tempo", "mono-red-goblins:tron"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing pair %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "games per pair: 2") {
		t.Errorf("report must state games-per-pair:\n%s", out)
	}
	// Deterministic: a second identical run is byte-identical.
	if err := runMatrix(0, 2, 2, "bot", "bot", dir, "text", pairs, 2, 200, 0, false, nil, &b2, io.Discard); err != nil {
		t.Fatalf("runMatrix(second): %v", err)
	}
	if b1.String() != b2.String() {
		t.Errorf("matrix end-to-end report not deterministic across identical runs")
	}
}

// ---- task op1: commander construction format and the turn watchdog ----

// stallRe captures the loud stall line bench/run and the matrix writers emit
// when any game hit either watchdog cap. Group 1 is the number that hit
// -max-turns, group 2 the number that hit -max-intents, group 3 the
// non-stalled denominator. It is extra to summaryRe (the normal block still
// prints when stalls occurred), and it proves the run said so loudly and
// distinguished the two causes rather than silently dropping games.
var stallRe = regexp.MustCompile(`@@ STALLED: (\d+) game\(s\) hit the -max-turns cap, (\d+) hit the -max-intents cap; win rates are over (\d+) non-stalled game\(s\) @@`)

// seatLineRe captures the per-game line's winner token so a stall's
// winner=stalled marker is assertable directly.
var winRe = regexp.MustCompile(`winner=([^\s]+)`)

// TestConstructedDefaultIsByteIdentical pins Part A's non-negotiable: the
// default constructed path must produce today's exact numbers for a fixed
// seed at games 20, and the Config the default builds must carry NO
// commander settings -- a mutation that made the default path apply
// commander life/command-zone settings would move the seat split and fail
// here. The split is asserted directly (not just determinism) because this
// bench's numbers are quoted as a golden. The split belongs to the DEFAULT
// PAIR -- the first two sorted repo decks, whatever they are at the time
// (run() seats names[:seats]); which decks that is changes whenever a deck
// is added, so the pair is re-derived here and the pinned number is
// re-measured against the real run when the pair moves.
func TestConstructedDefaultIsByteIdentical(t *testing.T) {
	dir := corpusDirOrSkip(t)

	// buildGameConfig with commander=false leaves the zero value untouched.
	con := buildGameConfig(0, []string{testutil.RepoDeckNames()[0], testutil.RepoDeckNames()[1]}, nil, nil, false)
	if con.Format != rules.FormatConstructed || con.StartingLife != 0 || con.Commanders != nil {
		t.Errorf("constructed default Config must carry no commander settings: %+v", con)
	}

	var buf bytes.Buffer
	if err := run(0, 20, 2, 0, 0, "bot", "legacy", dir, 200, 0, false, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	m := summaryRe.FindStringSubmatch(buf.String())
	if m == nil {
		t.Fatalf("summary block missing:\n%s", buf.String())
	}
	// Seat 0 wins: 5, seat 1 wins: 15 at this fixed seed, for the default
	// pair avengers-assemble:death-n-taxes (the first two sorted repo decks
	// at the 2026-09-17 avengers-assemble import; the prior 16/4 belonged to
	// death-n-taxes:dimir-tempo). This is a command golden, not a claim about
	// policy strength: it catches a change to the default constructed bench's
	// deck order, seed use, or bot path. The 16/4 was re-measured by
	// inbox-botbench-stability-run's bot fix: the KChoose mana-payment arm now
	// prefers a phyrexian pip's life payment over its pool colour while the
	// seat has life to spare (measured by reverting the arm: the old 15/5
	// returns), which changed dimir-tempo's Dismember ({1}{B/P}{B/P}) pip
	// answers in that run. The 7/13 was itself re-measured by the
	// ETBReplacement zone-spec fix (ticket agent-20260918T200326Z-f4542478):
	// Metallic Mimic — avengers-assemble's one ETBReplacement carrier — now
	// pumps each OTHER creature you control of the chosen type entering from
	// the battlefield (its oracle) instead of pumping only itself, which
	// moved 1 game across the 20 (reverting cards/kw_etbreplacement.go
	// returns 7/13; the fix reproduces 6/14 deterministically). The 8/12 was
	// an ARTIFACT of the OptionalCost round's generic dotted-count-head split
	// (effects/count.go): it silently truncated every no-space dotted head
	// (Count$CardCounters.CHARGE, Count$Kicked.4.0, Count$Foretold.1.0) to an
	// unresolved zero, and restoring the real reads — restricting the split to
	// OptionalGenericCostPaid and adding the CastSA> ref — returns the
	// original 5/15 (ticket agent-20260919T060203Z-702ca6ba round 2; reverting
	// the dot-split fix alone reproduces 8/12). The OptionalCost OFFERS
	// themselves do not move this split: neither deck carries an OptionalCost
	// carrier. The 5/15 was re-measured to 6/14 by VoteCard$'s per-voter
	// decision (branch vote_card1, merged 2026-09-22): Council's Judgment in
	// death-n-taxes now asks each voter and the bot selects the highest-worth
	// opposing permanent instead of ballot order's first (on the branch tree,
	// reverting effects/misc.go returns 5/15). It is also covered by the
	// ConditionDefined$ Targeted gate (ticket
	// agent-20260918T210307Z-25a7b039): Rescue, Pepper Potts --
	// avengers-assemble's one ConditionDefined$ Targeted carrier -- now takes
	// its +1/+1 counter only when the card its ETB returned was an artifact
	// (its oracle; pinned by rules/rescue_pepper_potts_test.go) instead of on
	// every resolution, including a bounced plain creature or no target at
	// all, where the gate used to fail open (reverting effects/conditions.go
	// returns 6/14; the fix reproduces 5/15 deterministically). The Any-mana
	// projection change in cards/mana_production.go then re-measured this
	// same pair from 6/14 to 5/15: the bot can now see an untapped Any source
	// as a real WUBRG alternative instead of the old colourless stand-in, so
	// its tap choice changes in one game (reverting that file reproduces 6/14).
	//
	// The 6/14 was re-measured to 7/13 by the api:ManaReflected collector fix
	// (ticket cli-20260922T225137Z): cards.ManaProduction now folds a face's
	// AB$ ManaReflected abilities (Face.ManaReflectedAbilities) and the
	// botpolicy tap gate aims a needed coloured pip at a Reflected source,
	// so avengers-assemble's three reflected mana rocks -- Exotic Orchard,
	// Fellwar Stone and Plaza of Heroes -- are tapped for a needed pip where
	// before they projected as producing nothing and the gate passed over
	// them. Measured: reverting ONLY the six reflected files (leaving the
	// bench's own legacy-menace guard in place) returns 6/14 deterministically;
	// the guard alone on HEAD also returns 6/14, so the one-game move is
	// entirely the reflected-mana change. (The legacy guard is the reason this
	// measurement exists at all: the fact-free legacy block policy coined an
	// illegal lone block onto a Menace attacker and the engine rejected the
	// intent, aborting the bench at HEAD seed 22 and at this change's seed 18;
	// see legacySeat.Decide.)
	//
	// MERGE (Any-mana projection x ManaReflected collector, both present):
	// measured at that merged tip as 6/14 -- neither parent's value (5/15 with
	// the projection change alone, 7/13 with the collector fix alone); the two
	// cards/mana_production.go changes interact on the shared bench pair.
	//
	// The MOC Cavalry Charge deck import for Battle cry (agent-20260920T064028Z-e215debd)
	// changes the sorted default pair to avengers-assemble:cavalry-charge.
	// Hero of Bladehold contributes its real CR 702.33 Battle cry trigger,
	// and the new pair measures 7/13. Removing only the deck fixture restores
	// the previous 6/14 default pair, attributing the movement to adding
	// Cavalry Charge to the default deck pool.
	//
	// bot-x1 re-measured 7/13 to 6/14: the production bot's A1 attachment
	// no-op rule is scoped to attach abilities (botpolicy equipNoOp, the
	// explore policy's X1 promoted), so an attached Equipment's own
	// non-attach abilities and a creatureless seat's non-attach abilities are
	// activated. The base commit measures 7/13; this change alone moves it.
	// fdn-dig-rest-random-order then re-measured 6/14 to 5/15: in the
	// avengers-assemble deck Squad Rallier's RestRandomOrder$ Dig now consumes
	// the seeded RNG to randomize the three unchosen cards at the bottom of
	// the library, changing the later deterministic game path. Reverting
	// effects/cardflow.go alone restores 6/14; the new Dig regression test
	// independently pins the random bottom order and replay.
	// The Winter Soldier entry-counter fix (ticket
	// agent-20260927T212826Z-4ef285af) then re-measured 5/15 to 6/14: the
	// Effect root now pre-captures its ChangeZone sub's target and remembers
	// it, so avengers-assemble's Winter Soldier, Reborn Avenger returns a Hero
	// from the graveyard WITH its additional +1/+1 counter instead of none.
	// Measured on the fix merged with main at a58faab6c: reverting ONLY the
	// fix's effects/registry.go, effects/targets_ask.go, effects/zone.go and
	// rules/stack.go restores 5/15; with them the split is 6/14.
	// The Boros Moxite Burn Moxfield import (Pauper burn) sorts before
	// cavalry-charge, so the sorted default pair becomes
	// avengers-assemble:boros-moxite-burn, which measures 7/13. Removing only
	// the deck fixture restores 6/14, attributing the movement to adding the
	// deck to the default pool.
	const wantSeat0, wantSeat1 = 7, 13
	if seat0, seat1 := atoi(m[8]), atoi(m[9]); seat0 != wantSeat0 || seat1 != wantSeat1 {
		t.Errorf("constructed default split = %d/%d, want %d/%d after the Boros Moxite Burn deck import (%s vs %s at seed 0, games 20)", seat0, seat1, wantSeat0, wantSeat1, testutil.RepoDeckNames()[0], testutil.RepoDeckNames()[1])
	}
	if strings.Contains(buf.String(), "STALLED") {
		t.Errorf("constructed default (no stalls) must not print a stall line")
	}
}

// TestCommanderConfig pins Part A's commander Config: FormatCommander, 40
// starting life (CR 903.6), and the command-zone indices equal to each
// seat's deck.File.CommanderIndex() -- asserted against CommanderIndex(),
// never a hard-coded integer, so a deck authoring change that moves the
// commander's position re-flags this. The engine's own genesis is what
// these settings drive; here we pin that the command built the Config the
// brief demands.
func TestCommanderConfig(t *testing.T) {
	name := "foundations-calling-all-angels"
	f, err := testutil.LoadRepoDeckFile(name)
	if err != nil {
		t.Fatalf("LoadRepoDeckFile: %v", err)
	}

	con := buildGameConfig(7, []string{name}, nil, nil, false)
	if con.Format != rules.FormatConstructed || con.StartingLife != 0 {
		t.Errorf("constructed config carries a commander setting: format=%v life=%d", con.Format, con.StartingLife)
	}

	cmd := buildGameConfig(7, []string{name}, nil, [][]int{{f.CommanderIndex()}}, true)
	if cmd.StartingLife != 40 {
		t.Errorf("commander starting life = %d, want 40 (CR 903.6)", cmd.StartingLife)
	}
	if cmd.Format != rules.FormatCommander {
		t.Errorf("commander format = %d, want FormatCommander", cmd.Format)
	}
	if len(cmd.Commanders) != 1 || len(cmd.Commanders[0]) != 1 || cmd.Commanders[0][0] != f.CommanderIndex() {
		t.Errorf("commander indices = %v, want %q's CommanderIndex", cmd.Commanders, name)
	}

	// A partner-pair deck file (the CR 903.13 two-commander shape) carries
	// BOTH commanders: File.CommanderIndices lists every commander in order,
	// and buildGameConfig threads the list through untouched — the engine's
	// legalCommandersFor is what validates the pair, not this layer.
	pair := deck.File{
		Name:       "pair",
		Commanders: []string{"Amalia", "Ember Dragon"},
		Cards: []deck.Entry{
			{Name: "Amalia", Count: 1},
			{Name: "Ember Dragon", Count: 1},
			{Name: "Plains", Count: 98},
		},
	}
	cmdPair := buildGameConfig(7, []string{"pair"}, nil, [][]int{pair.CommanderIndices()}, true)
	if len(cmdPair.Commanders) != 1 || len(cmdPair.Commanders[0]) != 2 || cmdPair.Commanders[0][0] != 0 || cmdPair.Commanders[0][1] != 1 {
		t.Errorf("partner command-zone indices = %v, want [0 1]", cmdPair.Commanders)
	}
}

// TestCommanderDeckNamesGateIncludesPluralFiles pins the deck-pool gate on
// the CommanderNames accessor, not the legacy singular field: a plural-only
// partner-pair file (exactly the on-disk shape the deckimport writer produces
// for a pair — no legacy "commander" key) is a commander deck, and a file
// with neither designation is not. The pre-fix gate read f.Commander != "",
// which silently classified every plural-only file constructed and dropped it
// from the whole commander pool.
func TestCommanderDeckNamesGateIncludesPluralFiles(t *testing.T) {
	load := func(n string) (deck.File, error) {
		switch n {
		case "pair":
			return deck.File{Commanders: []string{"Hobbit Warden", "Garden Keeper"}}, nil
		case "single":
			return deck.File{Commander: "Amalia"}, nil
		default:
			return deck.File{}, nil
		}
	}
	got, err := commanderDeckNamesFrom([]string{"constructed", "pair", "single"}, load)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "pair,single" {
		t.Fatalf("commander deck pool = %v, want [pair single] (input order preserved)", got)
	}
}

// commanderSet is a sorted name set for builder tests.
type commanderSet map[string]bool

func (c commanderSet) has(n string) bool { return c[n] }

// TestSeatedDeckNamesRotatePinsTheRotation pins the -rotate seating rule:
// seat s holds names[(s+rotate)%len(names)], so rotate=0 is the historical
// fixed assignment and the rotate=0..seats-1 cycle deals every deck to every
// seat exactly once (the property the op5 four-seat experiment relies on: a
// win share that follows the seat index shows up in every rotation, one that
// follows the deck follows each deck around the table). It also pins the
// concrete first rotation:
//
//	rotate 0: A B C D    rotate 1: B C D A    rotate 2: C D A B    rotate 3: D A B C
//
// each a permutation of the input order.
func TestSeatedDeckNamesRotatePinsTheRotation(t *testing.T) {
	names := []string{"a", "b", "c", "d"}
	seat := func(rotate int) []string { return seatedDeckNames(names, rotate) }
	if got := strings.Join(seat(0), " "); got != "a b c d" {
		t.Errorf("rotate 0 must be the input order verbatim, got %q", got)
	}
	want := [][]string{
		{"a", "b", "c", "d"},
		{"b", "c", "d", "a"},
		{"c", "d", "a", "b"},
		{"d", "a", "b", "c"},
	}
	for r := 0; r < len(names); r++ {
		got := seat(r)
		same := len(got) == len(want[r])
		for i := range got {
			same = same && got[i] == want[r][i]
		}
		if !same {
			t.Errorf("rotate %d = %v, want %v", r, got, want[r])
		}
	}
	// Each rotation is a permutation (no deck duplicated, none dropped), and
	// across the cycle every deck sits at every seat exactly once -- the
	// bijection the whole experiment's interpretation depends on.
	seen := map[string]int{}
	for r := 0; r < len(names); r++ {
		seen[seat(r)[0]]++
	}
	for _, n := range names {
		if seen[n] != 1 {
			t.Errorf("deck %q sits at seat 0 in %d rotations, want exactly 1", n, seen[n])
		}
	}
}

// TestRotateIsValidatedPins the -rotate bounds: a rotation index outside
// [0, seats) is a clear flag error, never a silently truncated or wrapped
// seating.
func TestRotateIsValidated(t *testing.T) {
	for _, tc := range []struct {
		rotate, seats int
	}{
		{-1, 4},
		{4, 4},
		{5, 4},
		{2, 2},
	} {
		err := run(1, 1, tc.seats, tc.rotate, 0, "bot", "bot", ".", 200, 0, false, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "-rotate must be in") {
			t.Errorf("rotate=%d seats=%d: err = %v, want the -rotate bounds error", tc.rotate, tc.seats, err)
		}
	}
}

// TestCommanderRunRotateSeatsTheRotatedOrder drives a real 2-game commander
// run with -rotate 1 and pins the header and summary shape: the header must
// name the rotated deck order (so the per-game seat tallies below it are
// attributable to the right deck lists), and the run must still produce one
// winner per game. It is the end-to-end companion of the pure-function test
// above: it proves the rotation reaches the games, not just the helper.
func TestCommanderRunRotateSeatsTheRotatedOrder(t *testing.T) {
	dir := corpusDirOrSkip(t)
	var buf bytes.Buffer
	if err := run(1, 2, 4, 1, 0, "bot", "bot", dir, 200, 0, true, &buf); err != nil {
		t.Fatalf("run(rotate=1): %v", err)
	}
	out := buf.String()
	// The four youngest commander decks in sorted order, rotated by one: seat
	// s holds names[(s+1)%4] of the four decks a 4-seat run seats. Built
	// from the same source the run uses, never a hand-typed string.
	cmd, err := commanderDeckNames()
	if err != nil {
		t.Fatalf("commanderDeckNames: %v", err)
	}
	seated := seatedDeckNames(cmd[:4], 1)
	hdr := "decks " + strings.Join(seated, ",")
	if !strings.Contains(out, hdr) {
		t.Errorf("header must name the rotated order %q:\n%s", hdr, out)
	}
	hdrLine := out[:strings.Index(out, "\n")]
	if !strings.Contains(hdrLine, "(commander format)") {
		t.Errorf("rotated commander run must still mark the header: %q", hdrLine)
	}
	gameLines := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "game ") {
			gameLines++
			if !strings.Contains(line, "winner=bot@") && !strings.Contains(line, "winner=stalled") {
				t.Errorf("game line must record a winner seat or a stall: %s", line)
			}
		}
	}
	if gameLines != 2 {
		t.Errorf("expected 2 per-game lines, got %d", gameLines)
	}
	// The seat-tally block (4-seat shape: bare counts, no rate) must sum to
	// the winner count (2 games, no stalls at these caps): the rotated
	// seating still credits wins to real seats.
	seatRe := regexp.MustCompile(`(?m)^seat 0 wins: (\d+)  seat 1 wins: (\d+)  seat 2 wins: (\d+)  seat 3 wins: (\d+)$`)
	m := seatRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("4-seat seat-tally block missing:\n%s", out)
	}
	seat0, seat1 := atoi(m[1]), atoi(m[2])
	seat2, seat3 := atoi(m[3]), atoi(m[4])
	ab := regexp.MustCompile(`(?m)^A wins: (\d+)  B wins: (\d+).*$`).FindStringSubmatch(out)
	if ab == nil {
		t.Fatalf("A/B line missing:\n%s", out)
	}
	if aWins, bWins := atoi(ab[1]), atoi(ab[2]); seat0+seat1+seat2+seat3 != aWins+bWins {
		t.Errorf("seat wins %d+%d+%d+%d do not partition the A+B wins %d+%d",
			seat0, seat1, seat2, seat3, aWins, bWins)
	}
}

// TestCommanderAllExpandsToCommanderDecks pins Part A's deck selection: in
// commander mode -pairs all expands over ONLY the Commander decks, giving
// N choose 2 unordered pairs for the current Commander pool, and never names
// a constructed deck -- dealing a 100-card Commander list as a constructed
// pile is the defect the whole task exists to end. The pool deliberately
// grows with deck fixtures, so this test derives N rather than pinning an
// obsolete file count.
func TestCommanderAllExpandsToCommanderDecks(t *testing.T) {
	cmd, err := commanderDeckNames()
	if err != nil {
		t.Fatalf("commanderDeckNames: %v", err)
	}
	if len(cmd) < 2 {
		t.Fatalf("need at least two Commander decks, got %v", cmd)
	}
	set := commanderSet{}
	for _, n := range cmd {
		set[n] = true
	}
	ps, err := parsePairsForMode("all", cmd, true)
	if err != nil {
		t.Fatalf("parsePairsForMode(all, commander): %v", err)
	}
	if want := len(cmd) * (len(cmd) - 1) / 2; len(ps) != want {
		t.Errorf("commander -pairs all = %d pairs, want %d (%d choose 2)", len(ps), want, len(cmd))
	}
	for _, p := range ps {
		if !set.has(p.a) || !set.has(p.b) {
			t.Errorf("pair %s names a non-commander deck", p)
		}
	}
}

// TestCommanderNamedNonCommanderIsError pins Part A's flag-validation rule:
// an explicitly named -pairs a:b) naming a repo deck with no commander is a
// clear flag error in commander mode, never a silently constructed game.
func TestCommanderNamedNonCommanderIsError(t *testing.T) {
	cmd, err := commanderDeckNames()
	if err != nil {
		t.Fatalf("commanderDeckNames: %v", err)
	}
	_, err = parsePairsForMode("tron:mono-red-goblins", cmd, true)
	if err == nil {
		t.Fatal("naming a constructed deck in commander mode returned no error")
	}
	if !strings.Contains(err.Error(), "commander") {
		t.Errorf("error must say the deck names no commander, got: %v", err)
	}
}

// TestTurnWatchdogEndsGameAtMaxTurns pins Part B: a real engine game whose
// turn count reaches -max-turns ends as a stall (not a win, not a draw). A
// seed-0 constructed game at default life lasts ~18 turns, so capping at 2
// forces the watchdog to fire immediately, and the per-game line must say
// winner=stalled while the summary reports it as a stall.
func TestTurnWatchdogEndsGameAtMaxTurns(t *testing.T) {
	dir := corpusDirOrSkip(t)
	var buf bytes.Buffer
	if err := run(0, 1, 2, 0, 0, "bot", "bot", dir, 2, 0, false, &buf); err != nil {
		t.Fatalf("run(maxTurns=2): %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "winner=stalled") {
		t.Errorf("per-game line must call the outcome a stall:\n%s", out)
	}
	m := stallRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no stall line:\n%s", out)
	}
	if atoi(m[1]) != 1 {
		t.Errorf("stall count = %s, want 1", m[1])
	}
}

// TestStalledExcludedFromWinRate pins Part B's sampling rule: a stalled
// game is excluded from the win-rate denominator. Here 2 wins and 2 stalls
// over 4 games must report A win rate 100%% (2/2 non-stalled), not 50%%
// (2/4) -- silently counting deterministic drops would be the biased-sample
// defect the watchdog exists to expose. The STALLED line must carry the
// dropped count.
func TestStalledExcludedFromWinRate(t *testing.T) {
	play := func(seed uint64, _ []string) (gameOutcome, error) {
		// even seeds (games 0, 2) are seat-0 wins for A; odd seeds are intent
		// stalls (a frozen turn that keeps answering intents).
		if seed%2 == 0 {
			return gameOutcome{winner: "bot", winnerSeat: 0, turns: 10, intents: 50}, nil
		}
		return gameOutcome{stallOn: "intents", turns: 3, intents: 20000}, nil
	}
	var buf bytes.Buffer
	if err := bench(0, 4, 2, "bot", "bot", play, &buf); err != nil {
		t.Fatalf("bench: %v", err)
	}
	out := buf.String()
	m := summaryRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("summary block missing:\n%s", out)
	}
	if rate := atof(m[5]); rate != 100 {
		t.Errorf("A win rate = %.1f%%, want 100%% (2 wins over 4-2=2 non-stalled games)", rate)
	}
	sm := stallRe.FindStringSubmatch(out)
	if sm == nil || atoi(sm[2]) != 2 {
		t.Errorf("stall line must name 2 intent-stalled drops:\n%s", out)
	}
}

// TestCommanderMatrixJSONReproducible pins Part B's determinism gate for the
// commander matrix: two runs at the same (base seed, pairs, games, max-turns)
// produce identical JSON. It runs a commander subset that does not involve
// foundations-keen-engineering -- the deck whose pathological games hang the
// bench (op2's lane); the set of pairs is part of the determinism input.
func TestCommanderMatrixJSONReproducible(t *testing.T) {
	dir := corpusDirOrSkip(t)
	cmdNames, err := commanderDeckNames()
	if err != nil {
		t.Fatalf("commanderDeckNames: %v", err)
	}
	pairs, err := parsePairsForMode("foundations-calling-all-angels:foundations-reign-of-dragons,foundations-reign-of-dragons:foundations-tramplesaurus-rex", cmdNames, true)
	if err != nil {
		t.Fatalf("parsePairsForMode: %v", err)
	}
	var b1, b2 bytes.Buffer
	if err := runMatrix(0, 2, 2, "bot", "bot", dir, "json", pairs, 2, 200, 0, true, nil, &b1, io.Discard); err != nil {
		t.Fatalf("runMatrix(json): %v", err)
	}
	if b1.String() == "" || !strings.Contains(b1.String(), `"format": "commander"`) {
		t.Fatalf("commander JSON must carry the format:\n%s", b1.String())
	}
	if err := runMatrix(0, 2, 2, "bot", "bot", dir, "json", pairs, 2, 200, 0, true, nil, &b2, io.Discard); err != nil {
		t.Fatalf("runMatrix(json, second): %v", err)
	}
	if b1.String() != b2.String() {
		t.Errorf("commander matrix JSON not deterministic across identical runs")
	}
}

// ---- fix round 1: the intent watchdog (stalled, not error) ----

// TestMaxIntentsEndsNonTerminatingGameAsStall pins that -max-intents ends a
// game that does not finish inside the cap, recorded as a stall (not an
// error). It drives a real engine game (bot vs bot) with a trivially small
// cap of 5 intents and no turn cap: a healthy game is never over in 5
// intents, so the intent cap fires immediately and the watchdog records it
// as a stall -- the synthetic-shaped loop the fix exists for, without
// depending on any specific pathological deck.
func TestMaxIntentsEndsNonTerminatingGameAsStall(t *testing.T) {
	dir := corpusDirOrSkip(t)
	var buf bytes.Buffer
	if err := run(0, 1, 2, 0, 0, "bot", "bot", dir, 0, 5, false, &buf); err != nil {
		t.Fatalf("run(maxIntents=5): %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "winner=stalled") {
		t.Errorf("per-game line must call the intent-capped game a stall:\n%s", out)
	}
	m := stallRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no stall notice:\n%s", out)
	}
	if atoi(m[2]) != 1 {
		t.Errorf("intent stall count = %s, want 1", m[2])
	}
}

// TestIntentStalledExcludedFromWinRate pins the win-rate sampling rule for
// intent-capped games, mirroring what TestStalledExcludedFromWinRate proves
// for the turn cap: 2 wins + 2 intent stalls over 4 games report A win rate
// 100%% (2/2 non-stalled), never 50%% (2/4).
func TestIntentStalledExcludedFromWinRate(t *testing.T) {
	play := func(seed uint64, _ []string) (gameOutcome, error) {
		// even seeds are seat-0 wins for A; odd seeds are intent stalls.
		if seed%2 == 0 {
			return gameOutcome{winner: "bot", winnerSeat: 0, turns: 10, intents: 100}, nil
		}
		return gameOutcome{stallOn: "intents", turns: 5, intents: 20000}, nil
	}
	var buf bytes.Buffer
	if err := bench(0, 4, 2, "bot", "bot", play, &buf); err != nil {
		t.Fatalf("bench: %v", err)
	}
	out := buf.String()
	m := summaryRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("summary block missing:\n%s", out)
	}
	if rate := atof(m[5]); rate != 100 {
		t.Errorf("A win rate = %.1f%%, want 100%% (2 wins / 2 non-stalled)", rate)
	}
}

// TestIntentCapIsNotAnError pins the whole point of the fix: reaching the
// intent cap is a stalled outcome, not a fatal error, so the run continues
// and the games/pairs after the hung one still report. The synthetic player
// stalls game 0 on the intent cap (returns a stall, nil error) and wins the
// rest; bench must not fail, and both the stalled game and the finished ones
// appear in the report.
func TestIntentCapIsNotAnError(t *testing.T) {
	// bench plays its games in parallel on a pool, so several goroutines run
	// this synthetic player at once: the two branch flags are atomic because
	// the assertion below is that BOTH branches were exercised, and a plain
	// bool written from two games at once is a data race even when every
	// writer stores the same value.
	var caused, continued atomic.Bool
	play := func(seed uint64, _ []string) (gameOutcome, error) {
		if seed == 0 {
			caused.Store(true)
			// A stall is returned as a normal outcome, not an error: a single
			// hung game must not abort the whole run.
			return gameOutcome{stallOn: "intents", turns: 3, intents: 20000}, nil
		}
		continued.Store(true)
		return gameOutcome{winner: "bot", winnerSeat: 0, turns: 12, intents: 200}, nil
	}
	var buf bytes.Buffer
	if err := bench(0, 3, 2, "bot", "bot", play, &buf); err != nil {
		t.Fatalf("bench must not error on an intent-capped game (one hung game should be stepped over): %v", err)
	}
	out := buf.String()
	if !caused.Load() || !continued.Load() {
		t.Fatalf("synthetic player did not exercise both the stall and the continuation branches")
	}
	if !strings.Contains(out, "winner=stalled") || !strings.Contains(out, "winner=bot@0") {
		t.Errorf("both the intent-stalled game and the later win must be reported:\n%s", out)
	}
}

// TestStallNoticeDistinguishesCauses pins that the stall notice names the
// turn cap and the intent cap separately, and counts each -- a reader must
// be able to tell a game that ran long (turns) from one whose turn froze
// (intents) to act on either.
func TestStallNoticeDistinguishesCauses(t *testing.T) {
	play := func(seed uint64, _ []string) (gameOutcome, error) {
		switch seed % 3 {
		case 0:
			return gameOutcome{winner: "bot", winnerSeat: 0, turns: 10, intents: 100}, nil
		case 1:
			return gameOutcome{stallOn: "turns", turns: 200, intents: 300}, nil
		default:
			return gameOutcome{stallOn: "intents", turns: 5, intents: 20000}, nil
		}
	}
	var buf bytes.Buffer
	if err := bench(0, 3, 2, "bot", "bot", play, &buf); err != nil {
		t.Fatalf("bench: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "-max-turns cap") || !strings.Contains(out, "-max-intents cap") {
		t.Errorf("notice must name the turn cap and intent cap separately:\n%s", out)
	}
	m := stallRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no stall notice:\n%s", out)
	}
	if atoi(m[1]) != 1 || atoi(m[2]) != 1 {
		t.Errorf("turn/intent stall counts = %s/%s, want 1/1", m[1], m[2])
	}
}

// TestAllStalledReportsNoRate pins the all-stalled denominator fix: a run
// where every game stalled has no non-stalled games, so a reported "0.0%%"
// would be a rate over nothing -- a lie that gets quoted as a real win
// rate. The report must instead say the rate is unavailable.
func TestAllStalledReportsNoRate(t *testing.T) {
	play := func(seed uint64, _ []string) (gameOutcome, error) {
		return gameOutcome{stallOn: "intents", turns: 4, intents: 20000}, nil
	}
	var buf bytes.Buffer
	if err := bench(0, 3, 2, "bot", "bot", play, &buf); err != nil {
		t.Fatalf("bench: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "A win rate: 0.0%") {
		t.Errorf("all-stalled run must not print 0.0%% as a win rate:\n%s", out)
	}
	if !strings.Contains(out, "no rate") {
		t.Errorf("all-stalled run must say the rate is unavailable, not 0:\n%s", out)
	}
}

// TestTheSummaryReportsTheStartingPlayerSplit (rv2a): since the CR 103.1
// toss the seat that plays first is drawn per game -- the seat-index split
// no longer measures play/draw, so the summary carries a starting-player
// split taken from each game's first TurnChange instead. The synthetic
// schedule: game 0 starts at seat 0 and seat 0 wins it; game 1 starts at
// seat 1 and seat 1 wins it; game 2 starts at seat 1 and draws. The line
// must read 1/1 and 2/1 -- starts and wins-when-starting, not seat wins.
func TestTheSummaryReportsTheStartingPlayerSplit(t *testing.T) {
	var buf bytes.Buffer
	starters := []int{0, 1, 1}
	winners := []int{0, 1, -1} // -1: a draw
	if err := bench(0, 3, 2, "bot", "bot", func(g uint64, _ []string) (gameOutcome, error) {
		o := gameOutcome{turns: 10, intents: 50, starter: starters[g], starterSet: true}
		if winners[g] >= 0 {
			o.winner, o.winnerSeat = "bot", winners[g]
		}
		return o, nil
	}, &buf); err != nil {
		t.Fatalf("bench: %v", err)
	}
	out := buf.String()
	startRe := regexp.MustCompile(`(?m)^starting player: seat 0 started (\d+) and won (\d+)  seat 1 started (\d+) and won (\d+)$`)
	m := startRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("starting-player line missing from the summary:\n%s", out)
	}
	if s0, w0, s1, w1 := atoi(m[1]), atoi(m[2]), atoi(m[3]), atoi(m[4]); s0 != 1 || w0 != 1 || s1 != 2 || w1 != 1 {
		t.Errorf("starting split = seat 0 %d/%d, seat 1 %d/%d, want 1/1 and 2/1", s0, w0, s1, w1)
	}
}

// TestTheStartingPlayerLineIsSuppressedWithoutStarters (rv2a): an outcome
// source that records no starter (a synthetic player predating the field)
// must not grow a zeroed line -- the report stays byte-identical to the
// pre-rv2a shape for every caller that does not carry starters.
func TestTheStartingPlayerLineIsSuppressedWithoutStarters(t *testing.T) {
	var buf bytes.Buffer
	if err := bench(0, 3, 2, "bot", "bot", func(_ uint64, _ []string) (gameOutcome, error) {
		// starterSet stays false: a hand-built outcome that leaves the field
		// at its zero value must not invent a seat-0 start.
		return gameOutcome{winner: "bot", winnerSeat: 0, turns: 10, intents: 50}, nil
	}, &buf); err != nil {
		t.Fatalf("bench: %v", err)
	}
	if strings.Contains(buf.String(), "starting player:") {
		t.Errorf("starting-player line printed with no starters recorded:\n%s", buf.String())
	}
}

// TestStartingPlayerRejectsAnInvalidRecordedSeat keeps the fold defensive:
// only a present, in-range first TurnChange can index the per-seat split.
func TestStartingPlayerRejectsAnInvalidRecordedSeat(t *testing.T) {
	var buf bytes.Buffer
	err := bench(0, 1, 2, "bot", "bot", func(_ uint64, _ []string) (gameOutcome, error) {
		return gameOutcome{turns: 1, starter: 2, starterSet: true}, nil
	}, &buf)
	if err == nil || !strings.Contains(err.Error(), "starter seat 2 out of range [0,2)") {
		t.Fatalf("bench invalid starter error = %v, want range error", err)
	}
}
