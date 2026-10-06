// Command policytune fits a botpolicy.CastWeights profile by simultaneous-
// perturbation stochastic approximation (SPSA) with common random numbers.
//
// Each iteration perturbs the current weights in both directions (w+cΔ and
// w-cΔ, Rademacher Δ from a PRNG seeded by the run seed and the iteration),
// plays the two profiles head-to-head on the SAME per-game seeds with seats
// traded, and steps along the paired win-rate difference. Head-to-head with
// shared seeds is the variance reduction: no absolute baseline run is needed
// per step. The result is a profile JSON loadable by the L2 loader
// (botpolicy.ParseCastProfile) and a CSV trajectory.
//
// The game-playing itself is internal/bench (gbench), the same policy-
// agnostic pair-matrix runner cmd/botbench uses, so the fit and the bench
// cannot drift apart. The two sides are built DIRECTLY with
// seat.NewCastProfileBotWithWeights(seed, w) -- one constructor per side --
// rather than through cmd/botbench's package-level cast-profile override,
// which both sides would share; the fit needs w+ on one side and w- on the
// other in the same game.
//
// Determinism: a fit is a pure function of its flags, independent of
// -workers. Every game is seeded from the per-iteration block, the scheduler
// folds pair results in ascending order, and nothing reads the wall clock or
// the global math/rand.
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// Held-out seed range, reserved for gated evaluation (L4) and never used by
// a development fit: [heldOutLo, heldOutHi). A fit whose seed block touches
// this range is refused, so a tuner cannot accidentally fit on the held-out
// suite and then "validate" on it.
const (
	heldOutLo uint64 = 1000000
	heldOutHi uint64 = 2000000
)

// defaultMonoDecks are the five approved mono decks. The default pair list is
// every unordered pair of them (the ten approved mono-deck pairs), sorted and
// deterministic.
var defaultMonoDecks = []string{
	"mono-black-aggro",
	"mono-blue-tempo",
	"mono-green-stompy",
	"mono-red-prowess",
	"mono-white-equipment",
}

// sparePool recycles finished games' storage (rules.Spare) between the games
// a fit plays back to back, the way every other batch runner does; the
// outcome is a pure function of the seeds, so reuse is invisible to the fit.
var sparePool bench.SparePool

// committer converts one game's seat assignment into a rules.Config and runs
// it. It is the gbench.PairPlayer used by both the fit evaluation and the
// bot bench.
type devSuite struct {
	reg          *cards.Registry
	deckByName   map[string][]*cards.Card
	pairs        []bench.PairDef
	gamesPerPair int
	workers      int
	maxTurns     int
	maxIntents   int
	// ctor builds one seat from its seed and a profile. Defaults to
	// seat.NewCastProfileBotWithWeights; a test overrides it to observe which
	// weights reach which side.
	ctor func(seed uint64, w Weights) seat.Seat
	// autoPay (the -auto-pay flag) fits and benches under the hosted
	// auto-pay adapter: both fitted sides AND the BenchVsBot baseline pay
	// mana through offered payment plans (seat.Bot.EnableAutoPayMana), the
	// shape gorged hosts by default (-auto-mana and -bot-auto-mana both
	// default true). Off keeps the historical manual-tapping fit.
	autoPay bool
}

// setAutoPay switches both fitted sides and the bench baseline to the
// auto-pay adapter. It replaces ctor, so call it before any test override.
func (d *devSuite) setAutoPay(on bool) {
	d.autoPay = on
	if on {
		d.ctor = func(seed uint64, w Weights) seat.Seat {
			return seat.NewCastProfileBotWithWeights(seed, w).EnableAutoPayMana()
		}
	}
}

// baselineCtor is the production bot BenchVsBot measures against: seat.NewBot,
// with the auto-pay adapter when the fit runs under -auto-pay, so a fitted
// auto-pay profile is measured against the auto-pay default it would replace.
func (d *devSuite) baselineCtor() func(seed uint64) seat.Seat {
	if d.autoPay {
		return func(seed uint64) seat.Seat { return seat.NewBot(seed).EnableAutoPayMana() }
	}
	return func(seed uint64) seat.Seat { return seat.NewBot(seed) }
}

// openDevSuite resolves the corpus and every deck the pairs name once.
func openDevSuite(dir string, pairs []bench.PairDef, games, workers, maxTurns, maxIntents int) (*devSuite, error) {
	reg, err := testutil.OpenCorpusRegistry(dir)
	if err != nil {
		return nil, fmt.Errorf("opening corpus at %s: %w (run `make fetch-cards compile-cards` first)", dir, err)
	}
	deckByName := make(map[string][]*cards.Card, len(pairs)*2)
	for _, pd := range pairs {
		for _, name := range []string{pd.A, pd.B} {
			if _, ok := deckByName[name]; ok {
				continue
			}
			d, err := testutil.LoadRepoDeck(reg, name)
			if err != nil {
				return nil, err
			}
			deckByName[name] = d
		}
	}
	return &devSuite{
		reg:          reg,
		deckByName:   deckByName,
		pairs:        pairs,
		gamesPerPair: games,
		workers:      workers,
		maxTurns:     maxTurns,
		maxIntents:   maxIntents,
		ctor:         func(seed uint64, w Weights) seat.Seat { return seat.NewCastProfileBotWithWeights(seed, w) },
	}, nil
}

// player returns the gbench.PairPlayer that builds one game's Config from the
// pair's decks and runs it.
func (d *devSuite) player() bench.PairPlayer {
	return func(pos int, seed uint64, g int, seats [2]seat.Seat) (bench.Outcome, error) {
		pd := d.pairs[pos]
		cfg := rules.Config{
			Seed:  seed,
			Names: []string{pd.A, pd.B},
			Decks: [][]*cards.Card{d.deckByName[pd.A], d.deckByName[pd.B]},
		}
		cfg.Tokens = d.reg.Tokens
		o, err := sparePool.PlayGameRecycled(cfg, seats[:], d.maxTurns, d.maxIntents, bench.Hooks{}, nil)
		return o, err
	}
}

// evalWith runs one head-to-head evaluation of plus vs minus on the dev suite
// and totals the plus side's wins. It is the single place both Eval and
// BenchVsBot build their side constructors, so the two cannot disagree about
// how a side reaches a seat.
func (d *devSuite) evalWith(plus, minus Weights, baseSeed uint64) (bench.PairResult, error) {
	aCtor := func(seed uint64) seat.Seat { return d.ctor(seed, plus) }
	bCtor := func(seed uint64) seat.Seat { return d.ctor(seed, minus) }
	results, err := bench.RunPairs(baseSeed, d.gamesPerPair, d.pairs, aCtor, bCtor, d.player(), d.workers, nil)
	if err != nil {
		return bench.PairResult{}, err
	}
	var total bench.PairResult
	for _, r := range results {
		total.AWins += r.AWins
		total.BWins += r.BWins
		total.Draws += r.Draws
		total.Stalls += r.Stalls
		total.Games += r.Games
	}
	return total, nil
}

// Eval implements Evaluator: plus vs minus head-to-head, plus's win rate.
func (d *devSuite) Eval(plus, minus Weights, baseSeed uint64) (EvalResult, error) {
	total, err := d.evalWith(plus, minus, baseSeed)
	if err != nil {
		return EvalResult{}, err
	}
	effective := total.Games - total.Stalls
	if effective <= 0 {
		return EvalResult{Games: 0}, nil
	}
	return EvalResult{PlusWins: total.AWins, Games: effective}, nil
}

// BenchVsBot benches w (side A) against the production bot (side B) on the
// same dev suite. The bot is seat.NewBot, exactly the production policy (with
// the auto-pay adapter under -auto-pay, see baselineCtor), so a fitted
// profile is measured against the thing L4 will gate it against.
func (d *devSuite) BenchVsBot(w Weights, baseSeed uint64) (float64, error) {
	aCtor := func(seed uint64) seat.Seat { return d.ctor(seed, w) }
	bCtor := d.baselineCtor()
	results, err := bench.RunPairs(baseSeed, d.gamesPerPair, d.pairs, aCtor, bCtor, d.player(), d.workers, nil)
	if err != nil {
		return 0, err
	}
	var wins, eff int
	for _, r := range results {
		wins += r.AWins
		eff += r.Games - r.Stalls
	}
	if eff <= 0 {
		return 0.5, nil
	}
	return float64(wins) / float64(eff), nil
}

// seedRangeOverlapsHeldOut reports whether [base, base+span) intersects the
// reserved held-out range. The span is the fit's whole development seed
// block, so a run that starts below the range but spills into it is refused
// too. An empty span (no iterations/games) never overlaps.
func seedRangeOverlapsHeldOut(base, span uint64) bool {
	if span == 0 {
		return false
	}
	end := base + span
	if end < base {
		// uint64 overflow: the block is effectively unbounded.
		return true
	}
	return base < heldOutHi && end > heldOutLo
}

// csvTrace writes one trajectory row per iteration. The column order is
// fixed (iter, a, c, winrate, games, plus_wins, then every fitted weight
// value in struct order), so two fits diff column-wise.
type csvTrace struct {
	w      *csv.Writer
	fields []string
}

func newCSVTrace(out io.Writer, fields []string) *csvTrace {
	c := &csvTrace{w: csv.NewWriter(out), fields: fields}
	hdr := []string{"iter", "a", "c", "winrate", "games", "plus_wins"}
	hdr = append(hdr, fields...)
	_ = c.w.Write(hdr)
	return c
}

func (c *csvTrace) WriteRow(iter int, a, cc float64, res EvalResult, w Weights) error {
	row := []string{
		strconv.Itoa(iter),
		strconv.FormatFloat(a, 'g', -1, 64),
		strconv.FormatFloat(cc, 'g', -1, 64),
		strconv.FormatFloat(res.WinRate(), 'g', -1, 64),
		strconv.Itoa(res.Games),
		strconv.Itoa(res.PlusWins),
	}
	for _, name := range c.fields {
		row = append(row, strconv.FormatInt(int64(getWeight(w, name)), 10))
	}
	if err := c.w.Write(row); err != nil {
		return err
	}
	c.w.Flush()
	return c.w.Error()
}

func (c *csvTrace) Close() error {
	c.w.Flush()
	return c.w.Error()
}

// writeProfile writes w as an L2-loadable profile document. The schema is
// exactly {"version":1,"cast":{...}} with CastWeights' Go field names as the
// cast keys (botpolicy's loader has no tags and rejects unknown keys), and
// the result is round-tripped through the loader before it reaches disk so a
// file that cannot be read back is never written.
func writeProfile(path string, w Weights) error {
	doc := struct {
		Version int     `json:"version"`
		Cast    Weights `json:"cast"`
	}{Version: 1, Cast: w}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := botpolicy.ParseCastProfile(data); err != nil {
		return fmt.Errorf("internal error: fitted profile does not load back: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// loadInit resolves -init: a file path when non-empty, else the embedded
// default profile.
func loadInit(path string) (Weights, error) {
	if path == "" {
		return botpolicy.LoadCastProfile(botpolicy.DefaultCastProfileName)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Weights{}, fmt.Errorf("reading -init %s: %w", path, err)
	}
	w, err := botpolicy.ParseCastProfile(data)
	if err != nil {
		return Weights{}, fmt.Errorf("-init %s: %w", path, err)
	}
	return w, nil
}

// parseFit splits the -fit flag's comma list.
func parseFit(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// resolvePairs resolves the -pairs spec: empty means the ten approved
// mono-deck pairs (every unordered pair of defaultMonoDecks); otherwise
// gbench.ParsePairs against the repo deck names, the same syntax and errors
// cmd/botbench's -pairs uses.
func resolvePairs(spec string) ([]bench.PairDef, error) {
	if strings.TrimSpace(spec) == "" {
		names := append([]string(nil), defaultMonoDecks...)
		sort.Strings(names)
		return bench.FullPairs(names), nil
	}
	return bench.ParsePairs(spec, testutil.RepoDeckNames())
}

func main() {
	if code := mainExit(os.Args[1:], os.Stdout, os.Stderr); code != 0 {
		os.Exit(code)
	}
}

// mainExit is main's body with the exit code as its return, so the whole
// flag-driven path stays testable.
func mainExit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("policytune", flag.ContinueOnError)
	fs.SetOutput(stderr)
	initPath := fs.String("init", "", "cast-profile weights JSON to start from; empty = the embedded default profile")
	iters := fs.Int("iters", 20, "number of SPSA iterations")
	games := fs.Int("games", 4, "games per deck pair per evaluation")
	pairsSpec := fs.String("pairs", "", "deck-pair spec (same syntax as botbench: \"all\", \"a:b,c:d\", or \"coverage\" is NOT supported here); empty = the ten approved mono-deck pairs")
	seed := fs.Uint64("seed", 1, "development seed base; the seed block must not overlap the reserved held-out range [1000000, 2000000)")
	aGain := fs.Float64("a", DefaultSchedule.A, "SPSA a numerator: a_k = a/(k+1)^alpha")
	alpha := fs.Float64("alpha", DefaultSchedule.Alpha, "SPSA a decay exponent")
	cGain := fs.Float64("c", DefaultSchedule.C, "SPSA c numerator: c_k = max(1, round(c/(k+1)^gamma))")
	gamma := fs.Float64("gamma", DefaultSchedule.Gamma, "SPSA c decay exponent")
	workers := fs.Int("workers", 0, "parallelism budget; 0 = all cores (the result is deterministic regardless)")
	out := fs.String("out", "", "write the fitted profile JSON here (/dev/null is allowed)")
	tracePath := fs.String("trace", "", "write the per-iteration CSV trajectory here")
	fitSpec := fs.String("fit", "", "comma list of CastWeights field names to tune; others are frozen; empty = every field")
	benchEvery := fs.Int("bench-every", 0, "every K iterations, also bench the current weights vs the production bot and log the rate; 0 = off")
	dir := fs.String("dir", ".cards", "corpus directory (holds ir.gob.gz / cardsfolder)")
	maxTurns := fs.Int("max-turns", 200, "maximum turns per game before it ends as a stall (excluded from rates); 0 = no cap")
	maxIntents := fs.Int("max-intents", 20000, "maximum intents per game before it ends as a stall; 0 = no cap")
	quiet := fs.Bool("quiet", false, "suppress the per-iteration log lines")
	autoPay := fs.Bool("auto-pay", false, "fit under the hosted auto-pay adapter: both sides and the -bench-every baseline cast through offered payment plans (seat.Bot.EnableAutoPayMana)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	logw := stdout
	if *quiet {
		logw = io.Discard
	}

	if *iters < 1 {
		fmt.Fprintf(stderr, "policytune: -iters must be at least 1, got %d\n", *iters)
		return 1
	}
	if *games < 1 {
		fmt.Fprintf(stderr, "policytune: -games must be at least 1, got %d\n", *games)
		return 1
	}
	if *benchEvery < 0 {
		fmt.Fprintf(stderr, "policytune: -bench-every must be >= 0, got %d\n", *benchEvery)
		return 1
	}

	init, err := loadInit(*initPath)
	if err != nil {
		fmt.Fprintf(stderr, "policytune: %v\n", err)
		return 1
	}
	pairs, err := resolvePairs(*pairsSpec)
	if err != nil {
		fmt.Fprintf(stderr, "policytune: %v\n", err)
		return 1
	}
	// The development seed block: each iteration consumes Stride seeds
	// (pairs*games), so the whole fit spans Iters*Stride seeds from the base.
	stride := uint64(len(pairs) * *games)
	span := uint64(*iters) * stride
	if seedRangeOverlapsHeldOut(*seed, span) {
		fmt.Fprintf(stderr, "policytune: -seed %d with span %d overlaps the reserved held-out range [%d, %d); pick a development seed outside it\n",
			*seed, span, heldOutLo, heldOutHi)
		return 1
	}

	suite, err := openDevSuite(*dir, pairs, *games, *workers, *maxTurns, *maxIntents)
	if err != nil {
		fmt.Fprintf(stderr, "policytune: %v\n", err)
		return 1
	}
	suite.setAutoPay(*autoPay)

	// The trace names the fields BEFORE the fit resolves them, so both use
	// the same list: resolve here, pass to RunSPSA via Config.Fit.
	fields, err := resolveFitFields(parseFit(*fitSpec))
	if err != nil {
		fmt.Fprintf(stderr, "policytune: %v\n", err)
		return 1
	}

	var trace TraceWriter
	var traceFile *os.File
	if *tracePath != "" {
		f, err := os.Create(*tracePath)
		if err != nil {
			fmt.Fprintf(stderr, "policytune: creating -trace %s: %v\n", *tracePath, err)
			return 1
		}
		traceFile = f
		trace = newCSVTrace(f, fields)
	}

	cfg := Config{
		Iters:      *iters,
		Seed:       *seed,
		Stride:     stride,
		Schedule:   SPSASchedule{A: *aGain, Alpha: *alpha, C: *cGain, Gamma: *gamma},
		Fit:        parseFit(*fitSpec),
		BenchEvery: *benchEvery,
		Trace:      trace,
		Log:        logw,
	}

	var benchFn BenchFn
	if *benchEvery > 0 {
		// Bench uses a DIFFERENT seed block, disjoint from the fit's, so the
		// measurement cannot reuse the fit's games. It is offset by the whole
		// fit span (plus a gap) and still must miss held-out.
		benchBase := *seed + span + 1
		if seedRangeOverlapsHeldOut(benchBase, stride) {
			fmt.Fprintf(stderr, "policytune: the -bench-every block at seed %d overlaps the held-out range; raise -seed or drop -bench-every\n", benchBase)
			return 1
		}
		benchFn = func(iter int, w Weights) (float64, error) {
			return suite.BenchVsBot(w, benchBase+uint64(iter)*stride)
		}
	}

	res, err := RunSPSA(cfg, init, suite, benchFn)
	if traceFile != nil {
		if cerr := trace.Close(); cerr != nil && err == nil {
			err = cerr
		}
		if cerr := traceFile.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "policytune: %v\n", err)
		return 1
	}

	if *out != "" {
		if err := writeProfile(*out, res.Weights); err != nil {
			fmt.Fprintf(stderr, "policytune: writing -out %s: %v\n", *out, err)
			return 1
		}
	}
	fmt.Fprintf(logw, "policytune: %d iterations over %d pairs x %d games; final weights written to %s\n",
		*iters, len(pairs), *games, outputName(*out))
	return 0
}

func outputName(path string) string {
	if path == "" {
		return "(no -out; not written)"
	}
	return path
}
