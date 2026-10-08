// searchteacher is the 2026-09-19 search-teacher spike: a "search seat" that
// answers selected decision kinds with ensemble determinization (the
// internal/searchprobe history-conditioned sampler + default-bot rollouts on
// every sampled world) and the default bot everywhere else, benched against
// the default bot on the ten approved mono-deck pairs. Every game is also
// replayed bot-vs-bot on the same seed, so the paired difference isolates the
// teacher's overrides. Not a production seat; development seeds only.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/internal/traceboard"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var approvedDecks = []string{"mono-white-equipment", "mono-blue-tempo", "mono-black-aggro", "mono-red-prowess", "mono-green-stompy"}

type config struct {
	kinds                    map[string]bool
	worlds, attempts, limit  int
	minESS, margin           float64
	horizon                  int32
	maxSubmits, sampleBudget int
	sampleSeed               uint64
	maxTurn                  int32
	oracle, audit            bool
	decisionWorkers          int
	noLandExclusion          bool
	comparePotential         bool
	labelsPath               string
	// labelExtras (pn12) writes schema 3 label records carrying
	// policynet.LabelExtras; off, the corpus is schema 2 byte for byte.
	labelExtras bool
	rescore     int
	// value is the -value-checkpoint model (nil: the heuristic leaf). It is
	// loaded once before any game and shared read-only by every worker.
	value *policynet.Model
	// prior is the -prior-checkpoint policy head (nil = off, today's
	// candidate lists); priorTopK / priorWiden are searchseat.Options'
	// PriorTopK / PriorWiden (0 = that field's default).
	prior                 *policynet.Model
	priorTopK, priorWiden int
}

// DecisionRecord is one search-seat decision the teacher was asked about.
type DecisionRecord struct {
	Kind                  string
	Turn                  int32
	Frames                int
	Candidates            int
	Attempts, Accepted    int
	PrefixRejected        int
	ESS                   float64
	Duplicates            int
	Covered               bool
	Fallback              string
	Index                 int
	Values                []float64
	Rollouts, Submits     int
	Terminal, Capped      int
	SampleMS, SearchMS    float64
	TopRejection          string
	OracleValues          []float64 `json:",omitempty"`
	HandToStack           searchprobe.HandToStackCauses
	CompetitionExclusions int
	CompetitionResidual   int
	CompetitionUnguided   int
	BoardPAOnly           int
	IncompatibleProposals int
	ChosenDiffersFromBot  bool
	// Prior diagnostics (searchseat.Trace's), zero with no -prior-checkpoint.
	PriorEnumerated int  `json:",omitempty"`
	PriorKept       int  `json:",omitempty"`
	PriorRanked     bool `json:",omitempty"`
	PriorChanged    bool `json:",omitempty"`
}

// GameRecord is one seed: the search game and its bot-vs-bot twin.
type GameRecord struct {
	Pair                  string
	Seed                  uint64
	SearchSeat            int
	SearchDeck            string
	SearchOver, BaseOver  bool
	SearchDraw, BaseDraw  bool
	SearchWon, BaseWon    bool
	SearchTurns, BaseTurn int32
	Overrides             int
	Unsupported, Error    string
	WallMS                float64
	GameIndex             int
	Decisions             []DecisionRecord
	// Labels is the covered decisions' label corpus, published through
	// -labels; it is deliberately excluded from the GameRecord JSON (the
	// -out diagnostics and the training corpus are different files).
	Labels []LabelRecord `json:"-"`
}

func run(args []string, stdout, progress io.Writer) error {
	fs := flag.NewFlagSet("searchteacher", flag.ContinueOnError)
	fs.SetOutput(progress)
	games := fs.Int("games", 10, "games per approved pair")
	seed := fs.Uint64("seed", 30_000_000, "base development seed (the held-out range [1000000,2000000) is refused)")
	kinds := fs.String("kinds", "attackers", "comma list of decision kinds the teacher answers: attackers, blockers, cast, target")
	worlds := fs.Int("worlds", 8, "K sampled worlds per decision")
	attempts := fs.Int("attempts", 64, "sampler proposal attempts per decision")
	minESS := fs.Float64("min-ess", 0, "ESS gate for resampling K worlds (0 = K, the calibration contract)")
	margin := fs.Float64("margin", 0, "mean-value margin a candidate must beat the bot's answer by")
	limit := fs.Int("candidates", 6, "max candidates per decision (bot answer first)")
	horizon := fs.Int("horizon", 0, "rollout horizon in engine turns after the root (0 = game end)")
	maxSubmits := fs.Int("max-submits", 5000, "per-rollout and per-sample-attempt submit cap")
	sampleSeed := fs.Uint64("sample-seed", 54321, "fixed sampler seed base")
	maxTurn := fs.Int("max-turn", 200, "only search decisions at engine turn <= this")
	workers := fs.Int("workers", 16, "parallel games")
	decisionWorkers := fs.Int("decision-workers", 1, "goroutines WITHIN one searched decision (sampling attempts and rollouts); latency only, never changes a label. Keep 1 when -workers already fills the cores")
	audit := fs.Bool("audit", false, "measurement only: at every covered decision also score the candidates on one clone of the actual engine (never used to choose)")
	oracle := fs.Bool("oracle", false, "CHEATING ceiling: search one clone of the actual engine (true hidden zones and future chance) instead of sampled worlds")
	comparePotential := fs.Bool("compare-potential-actions", false, "measurement only: replay with the seat's potential-action walk (the pre-2026-09-21 capture) and report the rejections it alone decides; the labels are byte-identical either way")
	noLandExclusion := fs.Bool("no-land-exclusion", false, "measurement only: sample without the declined-land-drop exclusion (the pre-2026-09-21 proposal; reproduces that sampler's label corpus byte for byte)")
	pairsFlag := fs.String("pairs", "", "restrict to comma list of a:b pairs (default: the ten approved pairs)")
	outPath := fs.String("out", "", "JSONL of GameRecords (new file)")
	valueCheckpoint := fs.String("value-checkpoint", "", "score non-terminal rollout leaves with this policynet checkpoint's value head instead of the material heuristic (needs -horizon > 0 and a checkpoint trained with -value-weight > 0)")
	labelsPath := fs.String("labels", "", "JSONL label corpus of covered decisions (new file only, atomic publish; gzip-compressed when the path ends in .gz)")
	rescoreFlag := fs.Int("rescore-worlds", 0, "measurement only: at every covered decision also run an independent search with this many worlds and a different sample seed, recorded as rescore_values in the label (never used to choose; 0 = off)")
	labelExtrasFlag := fs.Bool("label-extras", false, "pn12: write schema 3 label records with the extras object (the opponent's hand and next draws, flagged diagnostic; every cast/activate option's follow-up target decision; the in-game target answer)")
	corpus := fs.String("cards", ".cards", "compiled corpus directory")
	cpuprofile := fs.String("cpuprofile", "", "write a CPU profile to this pprof file over the whole run (empty = off)")
	priorPath := fs.String("prior-checkpoint", "", "policynet checkpoint whose policy head ranks the attackers/cast candidates: enumerate -prior-widen, keep the bot answer plus the top -prior-topk (empty = off, today's fixed candidate list)")
	priorTopK := fs.Int("prior-topk", 0, "with -prior-checkpoint: non-bot candidates kept (0 = -candidates minus 1)")
	priorWiden := fs.Int("prior-widen", 0, "with -prior-checkpoint: candidate enumeration cap, bot answer included (0 = max(-candidates, 16))")
	memprofile := fs.String("memprofile", "", "write a heap profile to this pprof file after the last game (empty = off)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	prof := &profiler{cpuPath: *cpuprofile, memPath: *memprofile}
	if err := prof.start(); err != nil {
		return err
	}
	defer func() {
		if err := prof.finish(); err != nil {
			fmt.Fprintln(progress, err)
		}
	}()
	if *games < 1 || *worlds < 1 || *attempts < 1 || *workers < 1 || *limit < 2 {
		return fmt.Errorf("games, worlds, attempts, workers must be >0 and candidates >=2")
	}
	if *priorTopK < 0 || *priorWiden < 0 {
		return fmt.Errorf("prior-topk and prior-widen must be >= 0")
	}
	if *priorPath == "" && (*priorTopK != 0 || *priorWiden != 0) {
		return fmt.Errorf("-prior-topk / -prior-widen need -prior-checkpoint")
	}
	last := *seed + uint64(10**games)
	if *seed < 2_000_000 && last > 1_000_000 {
		return fmt.Errorf("seed range [%d,%d) overlaps held-out [1000000,2000000)", *seed, last)
	}
	cfg := config{kinds: map[string]bool{}, worlds: *worlds, attempts: *attempts, limit: *limit, minESS: *minESS, margin: *margin,
		horizon: int32(*horizon), maxSubmits: *maxSubmits, sampleSeed: *sampleSeed, maxTurn: int32(*maxTurn), oracle: *oracle, audit: *audit, labelsPath: *labelsPath, decisionWorkers: *decisionWorkers, noLandExclusion: *noLandExclusion, comparePotential: *comparePotential}
	cfg.priorTopK, cfg.priorWiden = *priorTopK, *priorWiden
	cfg.labelExtras = *labelExtrasFlag
	cfg.rescore = *rescoreFlag
	if *priorPath != "" {
		m, err := policynet.LoadCheckpointFile(*priorPath)
		if err != nil {
			return fmt.Errorf("prior checkpoint: %w", err)
		}
		cfg.prior = m
	}
	for _, k := range strings.Split(*kinds, ",") {
		k = strings.TrimSpace(k)
		if k != "attackers" && k != "blockers" && k != "cast" && k != "target" && k != "" {
			return fmt.Errorf("unknown kind %q", k)
		}
		if k != "" {
			cfg.kinds[k] = true
		}
	}
	if *valueCheckpoint != "" {
		// A game-end rollout (-horizon 0) ends at game over, which is scored
		// 1 / 0 / 0.5 and never reaches the leaf evaluator; only a MaxSubmits
		// cap would, so a value checkpoint there would silently do (almost)
		// nothing.
		if cfg.horizon <= 0 {
			return fmt.Errorf("-value-checkpoint needs -horizon > 0: a game-end rollout (-horizon 0) never stops at a non-terminal leaf, so the value head would never be read")
		}
		m, err := policynet.LoadCheckpointFile(*valueCheckpoint)
		if err != nil {
			return fmt.Errorf("-value-checkpoint: %w", err)
		}
		if !m.HasValue() {
			return fmt.Errorf("-value-checkpoint %s has no value head (train it with policytrain -value-weight > 0)", *valueCheckpoint)
		}
		cfg.value = m
	}
	if cfg.labelsPath != "" {
		if err := checkLabelsDestination(cfg.labelsPath); err != nil {
			return err
		}
	}
	// Create -out only after every fail-fast check has passed: creating it
	// earlier would leave an empty file behind on a run that refuses a
	// -labels destination (or any later pre-flight failure).
	var out *os.File
	if *outPath != "" {
		f, err := os.OpenFile(*outPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}
	reg, err := cards.SharedCorpus(*corpus)
	if err != nil {
		return err
	}
	type pair struct{ a, b string }
	var pairs []pair
	if *pairsFlag != "" {
		for _, p := range strings.Split(*pairsFlag, ",") {
			a, b, ok := strings.Cut(p, ":")
			if !ok {
				return fmt.Errorf("bad pair %q", p)
			}
			pairs = append(pairs, pair{a, b})
		}
	} else {
		for i := range approvedDecks {
			for j := i + 1; j < len(approvedDecks); j++ {
				pairs = append(pairs, pair{approvedDecks[i], approvedDecks[j]})
			}
		}
	}
	setups := make([]searchprobe.PublicGame, len(pairs))
	for i, p := range pairs {
		da, err := testutil.LoadRepoDeck(reg, p.a)
		if err != nil {
			return err
		}
		db, err := testutil.LoadRepoDeck(reg, p.b)
		if err != nil {
			return err
		}
		setups[i] = searchprobe.PublicGame{Names: []string{p.a, p.b}, Decks: [][]*cards.Card{da, db}, Tokens: reg.Tokens}
	}
	type job struct{ pi, g int }
	jobs := make(chan job)
	results := make(chan GameRecord)
	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				s := *seed + uint64(j.pi**games+j.g)
				rec := playPaired(setups[j.pi], s, j.g%2, cfg, pairs[j.pi].a+":"+pairs[j.pi].b, j.g)
				results <- rec
			}
		}()
	}
	go func() {
		for pi := range pairs {
			for g := 0; g < *games; g++ {
				jobs <- job{pi, g}
			}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	start := time.Now()
	var all []GameRecord
	for r := range results {
		all = append(all, r)
		if len(all)%10 == 0 || len(all) == len(pairs)**games {
			fmt.Fprintf(progress, "%d/%d games  %.0fs\n", len(all), len(pairs)**games, time.Since(start).Seconds())
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Seed < all[j].Seed })
	if out != nil {
		enc := json.NewEncoder(out)
		for _, r := range all {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
	}
	if cfg.labelsPath != "" {
		if err := writeLabels(cfg.labelsPath, collectLabels(all)); err != nil {
			return err
		}
	}
	summarize(stdout, all, cfg, *seed, *games, time.Since(start))
	return nil
}

// playPaired plays the seed once with the search seat and once bot-vs-bot.
func playPaired(setup searchprobe.PublicGame, seed uint64, searchSeat int, cfg config, pair string, gameIndex int) GameRecord {
	t0 := time.Now()
	rec := GameRecord{Pair: pair, Seed: seed, GameIndex: gameIndex, SearchSeat: searchSeat, SearchDeck: setup.Names[searchSeat]}
	base, err := playGame(setup, seed, searchSeat, cfg, false, nil)
	if err != nil {
		rec.Error = "baseline: " + err.Error()
		return rec
	}
	rec.BaseOver, rec.BaseDraw, rec.BaseTurn = base.G.Over, base.G.Draw, base.G.Turn
	rec.BaseWon = base.G.Over && !base.G.Draw && int(base.G.Winner) == searchSeat
	e, err := playGame(setup, seed, searchSeat, cfg, true, &rec)
	if err != nil {
		rec.Error = "search: " + err.Error()
		return rec
	}
	rec.SearchOver, rec.SearchDraw, rec.SearchTurns = e.G.Over, e.G.Draw, e.G.Turn
	rec.SearchWon = e.G.Over && !e.G.Draw && int(e.G.Winner) == searchSeat
	rec.WallMS = float64(time.Since(t0).Microseconds()) / 1000
	return rec
}

func playGame(setup searchprobe.PublicGame, seed uint64, searchSeat int, cfg config, search bool, rec *GameRecord) (*rules.Engine, error) {
	rcfg := rules.Config{Seed: seed, Names: setup.Names, Decks: setup.Decks, Tokens: setup.Tokens, StartingLife: setup.StartingLife}
	e := rules.New(rcfg)
	e.Advance()
	rngs := searchprobe.BotRandoms(seed, len(setup.Names))
	board := botpolicy.NewBoard(len(rngs))
	actor := state.PlayerID(searchSeat)
	feed := searchseat.NewFeed(actor)
	observing := search
	track := targetTracker{label: -1}
	for steps := 0; !e.G.Over; steps++ {
		if steps >= 20000 || e.G.Turn >= 200 {
			return e, nil // stall: Over=false
		}
		d := e.Pending()
		if d == nil {
			return e, fmt.Errorf("missing decision")
		}
		b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &board)
		in := botpolicy.Decide(b, d, rngs[d.Player])
		if observing {
			f, ok := feed.Observe(e)
			if !ok {
				observing = false
				rec.Unsupported = feed.StopReason()
			} else if d.Player == actor {
				before := len(rec.Labels)
				if e.G.Turn <= cfg.maxTurn {
					if chosen, ok := teach(setup, feed.HistoryRef(), feed.Collector(), e, d, in, f, cfg, rec, &b); ok {
						in = chosen
					}
				}
				if cfg.labelExtras {
					track.observe(rec, d, in, actor)
					track.afterLabel(rec, before, d, in)
				}
				if err := feed.RecordAnswer(d, in); err != nil {
					return e, err
				}
			}
		}
		if err := e.Submit(in); err != nil {
			return e, err
		}
	}
	return e, nil
}

// teach runs the teacher at an eligible decision. It returns the intent to
// play and true when the teacher overrode the bot. b is the deciding seat's
// board snapshot source (BoardFromGameInto built it for this decision and
// BoardFromGameInto will overwrite it at the next one, so every board read
// here is a synchronous copy).
// teach runs the teacher at an eligible decision and emits this command's
// label and diagnostics. It returns the intent to play and true when the
// teacher overrode the bot.
//
// The DECISION itself lives in internal/searchseat: candidate building, world
// sampling, teacher scoring and root matching are shared with any seat that
// plays the teacher's answer, so the corpus a student learns from and the
// policy that plays cannot drift. What stays here is label emission -- the
// LabelRecord, the DecisionRecord, the seat view and the audit pass -- none of
// which a playing seat needs.
//
// b is the deciding seat's board snapshot source (BoardFromGameInto built it
// for this decision and will overwrite it at the next one, so every board read
// here is a synchronous copy).
func teach(setup searchprobe.PublicGame, h *searchprobe.History, collector *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, f searchprobe.Frame, cfg config, rec *GameRecord, b *botpolicy.Board) (decision.Intent, bool) {
	opts := searchseat.Options{
		Kinds:        cfg.kinds,
		Worlds:       cfg.worlds,
		Attempts:     cfg.attempts,
		MinESS:       cfg.minESS,
		Limit:        cfg.limit,
		Margin:       cfg.margin,
		HorizonTurns: cfg.horizon,
		MaxSubmits:   cfg.maxSubmits,
		SampleSeed:   cfg.sampleSeed,
		Clairvoyant:  cfg.oracle,
		Parallelism:  cfg.decisionWorkers,
		Value:        cfg.value,

		NoLandExclusion:         cfg.noLandExclusion,
		ComparePotentialActions: cfg.comparePotential,

		Prior:      cfg.prior,
		PriorTopK:  cfg.priorTopK,
		PriorWiden: cfg.priorWiden,
	}
	// The phase split is timed HERE, not in searchseat: internal/archtest
	// allows the time import in host, host/httpapi and cmd/gorged only, so the
	// search helper takes callbacks and this command, which already owns a
	// clock, does the measuring.
	var sampleMS, searchMS float64
	t0 := time.Now()
	opts.AfterSample = func() {
		sampleMS = float64(time.Since(t0).Microseconds()) / 1000
		t0 = time.Now()
	}
	opts.AfterSearch = func() {
		searchMS = float64(time.Since(t0).Microseconds()) / 1000
	}

	// An ineligible decision, or one the candidate builder cannot open, emits
	// nothing at all -- not even a DecisionRecord. That is the pre-extraction
	// behaviour: dr was constructed only after the candidate check passed, so
	// a decision the teacher never looked at leaves no trace in the corpus.
	if !searchseat.Eligible(d, opts) {
		return bot, false
	}

	in, overrode, tr := searchseat.Choose(setup, *h, collector, e, d, bot, f, opts)
	if len(tr.Candidates) < 2 {
		return bot, false
	}

	dr := DecisionRecord{Kind: tr.Kind, Turn: e.G.Turn, Frames: len(h.Frames), Candidates: len(tr.Candidates)}
	defer func() { rec.Decisions = append(rec.Decisions, dr) }()
	dr.SampleMS, dr.SearchMS = sampleMS, searchMS
	dr.Attempts, dr.Accepted, dr.PrefixRejected, dr.ESS, dr.Duplicates = tr.Attempts, tr.Accepted, tr.PrefixRejected, tr.ESS, tr.Duplicates
	dr.HandToStack = tr.HandToStack
	dr.CompetitionExclusions, dr.CompetitionResidual, dr.CompetitionUnguided = tr.CompetitionExclusions, tr.CompetitionResidual, tr.CompetitionUnguided
	dr.IncompatibleProposals = tr.IncompatibleProposals
	dr.BoardPAOnly = tr.BoardPotentialActionsOnly
	dr.TopRejection = tr.TopRejection
	dr.PriorEnumerated, dr.PriorKept = tr.PriorEnumerated, tr.PriorKept
	dr.PriorRanked, dr.PriorChanged = tr.PriorRanked, tr.PriorChanged

	if !tr.Covered {
		dr.Fallback = tr.Fallback
		return bot, false
	}

	// The seat's redacted view must serialize before a label can exist; a
	// failure is a real (if currently unreachable) drop path, so record it on
	// the DecisionRecord and keep the bot's answer -- never build a partial
	// LabelRecord and then dereference it (that would panic the whole
	// multi-worker run where this branch means to degrade gracefully).
	raw, err := seatView(e, d)
	if err != nil {
		dr.Fallback = "seat view: " + err.Error()
		return bot, false
	}
	lbl := &LabelRecord{RecordType: "label-v1", SchemaVersion: labelSchemaVersion, Pair: rec.Pair, GameIndex: rec.GameIndex, Seed: rec.Seed,
		Sequence: d.Seq, Seat: d.Player, Kind: d.Kind, Turn: e.G.Turn, Horizon: cfg.horizon, BotIndex: 0,
		Board: traceboard.Project(b), View: raw, Options: append([]decision.Option(nil), d.Options...)}
	lbl.Worlds = tr.Worlds
	if cfg.labelExtras {
		lbl.SchemaVersion = labelSchemaExtras
		lbl.Extras = labelExtras(e, d)
	}
	lbl.Attempts, lbl.Accepted = tr.Attempts, tr.Accepted
	lbl.Candidates = make([]LabelCandidate, len(tr.Candidates))
	for i, cand := range tr.Candidates {
		lc := LabelCandidate{Bot: i == 0}
		if m, err := collector.Match(d, cand); err == nil {
			lc.Choices = append([]int(nil), m.Choices...)
		}
		lbl.Candidates[i] = lc
	}

	if cfg.audit && !cfg.oracle {
		// Audit only: the clairvoyant values are recorded after the label is
		// fixed and never reach the choice above.
		if or, err := searchprobe.TeacherChoice([]searchprobe.World{{Engine: e.Clone(), Observer: collector}}, tr.Candidates, searchprobe.TeacherOptions{Seed: cfg.sampleSeed ^ 0x5eed ^ uint64(e.G.Turn), MaxSubmits: cfg.maxSubmits, Clairvoyant: true}); err == nil {
			dr.OracleValues = or.Values
		}
	}

	dr.Covered = true
	dr.Index, dr.Values, dr.Rollouts, dr.Submits, dr.Terminal, dr.Capped = tr.Index, tr.Values, tr.Rollouts, tr.Submits, tr.Terminal, tr.Capped
	best := 0
	for i := 1; i < len(tr.Values); i++ {
		if tr.Values[i] > tr.Values[best] {
			best = i
		}
	}
	lbl.TeacherChoice = tr.Index
	lbl.Margin = tr.Values[best] - tr.Values[0]
	for i := range lbl.Candidates {
		lbl.Candidates[i].Value = tr.Values[i]
		lbl.Candidates[i].Worlds = lbl.Worlds
	}
	if cfg.rescore > 0 && !cfg.oracle {
		// Measurement only: the label and the move played are already fixed.
		ro := opts
		// K worlds need far more proposals than the default 64 attempts.
		ro.Worlds, ro.SampleSeed = cfg.rescore, cfg.sampleSeed^0x7e5c07e
		ro.Attempts = max(opts.Attempts, 16*cfg.rescore)
		ro.AfterSample, ro.AfterSearch = nil, nil
		if _, _, rt := searchseat.Choose(setup, *h, collector, e, d, bot, f, ro); rt.Covered && len(rt.Values) == len(tr.Values) {
			lbl.RescoreValues = append([]float64(nil), rt.Values...)
			lbl.RescoreWorlds = rt.Worlds
		}
	}
	rec.Labels = append(rec.Labels, *lbl)

	if !overrode {
		if tr.Fallback != "" {
			dr.Fallback = tr.Fallback
		}
		return bot, false
	}
	dr.ChosenDiffersFromBot = true
	rec.Overrides++
	return in, true
}

func wald(p float64, n int) float64 {
	if n == 0 {
		return 0
	}
	return 1.96 * math.Sqrt(p*(1-p)/float64(n))
}

func score(over, draw, won bool) float64 {
	switch {
	case won:
		return 1
	case over && draw:
		return 0.5
	}
	return 0
}

func summarize(w io.Writer, all []GameRecord, cfg config, seed uint64, games int, wall time.Duration) {
	var kinds []string
	for k := range cfg.kinds {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	leaf := "heuristic"
	if cfg.value != nil {
		leaf = "value"
	}
	fmt.Fprintf(w, "search-teacher spike: oracle=%v kinds=%s K=%d attempts=%d minESS=%g horizon=%d leaf=%s margin=%g candidates<=%d seed=%d games/pair=%d wall=%.0fs %s\n",
		cfg.oracle, strings.Join(kinds, ","), cfg.worlds, cfg.attempts, cfg.minESS, cfg.horizon, leaf, cfg.margin, cfg.limit, seed, games, wall.Seconds(), priorHeader(cfg))
	var n, errs, stalls, unsupported int
	var sWins, bWins float64
	var diffs []float64
	byPair := map[string][3]float64{}
	var nd, covered, overrides, accepted, attemptsN int
	var priorCovered, priorChanged int
	var compExclusions, compResidual, compUnguided, boardPAOnly int
	var sampleMS, searchMS, coveredSampleMS, coveredSearchMS []float64
	fallbacks := map[string]int{}
	turnBuckets := map[string][2]int{}
	perKind := map[string]*kindStats{}
	for _, r := range all {
		if r.Error != "" {
			errs++
			continue
		}
		if r.Unsupported != "" {
			unsupported++
		}
		if !r.SearchOver || !r.BaseOver {
			stalls++
		}
		n++
		s, b := score(r.SearchOver, r.SearchDraw, r.SearchWon), score(r.BaseOver, r.BaseDraw, r.BaseWon)
		sWins += s
		bWins += b
		diffs = append(diffs, s-b)
		pp := byPair[r.Pair]
		pp[0]++
		pp[1] += s
		pp[2] += b
		byPair[r.Pair] = pp
		for _, d := range r.Decisions {
			nd++
			ks := perKind[d.Kind]
			if ks == nil {
				ks = &kindStats{fallbacks: map[string]int{}}
				perKind[d.Kind] = ks
			}
			ks.asked++
			if d.Covered {
				ks.covered++
				if d.ChosenDiffersFromBot {
					ks.overrides++
				}
			} else {
				ks.fallbacks[d.Fallback]++
			}
			accepted += d.Accepted
			attemptsN += d.Attempts
			compExclusions += d.CompetitionExclusions
			compResidual += d.CompetitionResidual
			compUnguided += d.CompetitionUnguided
			boardPAOnly += d.BoardPAOnly
			sampleMS = append(sampleMS, d.SampleMS)
			searchMS = append(searchMS, d.SearchMS)
			bucket := "t01-06"
			switch {
			case d.Turn > 12:
				bucket = "t13+"
			case d.Turn > 6:
				bucket = "t07-12"
			}
			tb := turnBuckets[bucket]
			tb[0]++
			if d.Covered {
				covered++
				tb[1]++
				coveredSampleMS = append(coveredSampleMS, d.SampleMS)
				coveredSearchMS = append(coveredSearchMS, d.SearchMS)
				if d.ChosenDiffersFromBot {
					overrides++
				}
				if d.PriorEnumerated > 0 {
					priorCovered++
					if d.PriorChanged {
						priorChanged++
					}
				}
			} else {
				fallbacks[d.Fallback]++
			}
			turnBuckets[bucket] = tb
		}
	}
	fmt.Fprintf(w, "games %d (errors %d, stalls %d, observation-unsupported %d)\n", n, errs, stalls, unsupported)
	if n > 0 {
		sp, bp := sWins/float64(n), bWins/float64(n)
		fmt.Fprintf(w, "search seat vs bot:   %.1f%% ± %.1f (Wald 95%%)\n", 100*sp, 100*wald(sp, n))
		fmt.Fprintf(w, "bot twin (same seeds): %.1f%% ± %.1f\n", 100*bp, 100*wald(bp, n))
		mean, sd := meanSD(diffs)
		changed := 0
		for _, d := range diffs {
			if d != 0 {
				changed++
			}
		}
		fmt.Fprintf(w, "paired delta:          %+.2fpp ± %.2f (95%%, n=%d; %d games changed outcome)\n", 100*mean, 196*sd/math.Sqrt(float64(n)), n, changed)
	}
	fmt.Fprintf(w, "decisions asked %d, covered %d (%.1f%%), overrides %d (%.1f%% of covered)\n", nd, covered, pct(covered, nd), overrides, pct(overrides, covered))
	if cfg.prior != nil {
		fmt.Fprintf(w, "prior changed the candidate set on %d of %d covered decisions\n", priorChanged, priorCovered)
	}
	fmt.Fprintf(w, "sampler acceptance %d/%d (%.2f%%)\n", accepted, attemptsN, pct(accepted, attemptsN))
	fmt.Fprintf(w, "  competition: exclusions taught %d, residual rejections %d, unguided %d\n", compExclusions, compResidual, compUnguided)
	if cfg.comparePotential {
		fmt.Fprintf(w, "  rejections decided by potential_actions alone: %d\n", boardPAOnly)
	}
	for _, b := range []string{"t01-06", "t07-12", "t13+"} {
		tb := turnBuckets[b]
		fmt.Fprintf(w, "  coverage %s: %d/%d (%.1f%%)\n", b, tb[1], tb[0], pct(tb[1], tb[0]))
	}
	fmt.Fprintf(w, "ms/decision sample: mean %.1f p50 %.1f p95 %.1f | search: mean %.1f p50 %.1f p95 %.1f (all asked)\n",
		mean(sampleMS), quant(sampleMS, .5), quant(sampleMS, .95), mean(searchMS), quant(searchMS, .5), quant(searchMS, .95))
	fmt.Fprintf(w, "ms/labelled decision (covered only): sample %.1f + search %.1f = %.1f\n", mean(coveredSampleMS), mean(coveredSearchMS), mean(coveredSampleMS)+mean(coveredSearchMS))
	var fk []string
	for k := range fallbacks {
		fk = append(fk, k)
	}
	sort.Strings(fk)
	for _, k := range fk {
		fmt.Fprintf(w, "  fallback %q: %d\n", k, fallbacks[k])
	}
	var pk []string
	for k := range byPair {
		pk = append(pk, k)
	}
	sort.Strings(pk)
	for _, k := range pk {
		p := byPair[k]
		fmt.Fprintf(w, "  %-40s n=%3.0f search %5.1f%%  twin %5.1f%%\n", k, p[0], 100*p[1]/p[0], 100*p[2]/p[0])
	}
	// Per-kind split, appended after every pre-existing line so an old log
	// still compares line for line up to here.
	var kk []string
	for k := range perKind {
		kk = append(kk, k)
	}
	sort.Strings(kk)
	for _, k := range kk {
		ks := perKind[k]
		fmt.Fprintf(w, "kind %s: asked %d, covered %d (%.1f%%), overrides %d (%.1f%% of covered)\n", k, ks.asked, ks.covered, pct(ks.covered, ks.asked), ks.overrides, pct(ks.overrides, ks.covered))
		var fb []string
		for f := range ks.fallbacks {
			fb = append(fb, f)
		}
		sort.Strings(fb)
		for _, f := range fb {
			fmt.Fprintf(w, "  kind %s fallback %q: %d\n", k, f, ks.fallbacks[f])
		}
	}
}

// priorHeader is the summary header's prior field: "prior=off", or
// "prior=on topk=K widen=W" with the EFFECTIVE values (searchseat's
// defaults applied), so a log names the budget it actually ran.
func priorHeader(cfg config) string {
	if cfg.prior == nil {
		return "prior=off"
	}
	topK, widen := searchseat.Options{Limit: cfg.limit, PriorTopK: cfg.priorTopK, PriorWiden: cfg.priorWiden}.PriorBudget()
	return fmt.Sprintf("prior=on topk=%d widen=%d", topK, widen)
}

// kindStats is summarize's per-decision-kind census.
type kindStats struct {
	asked, covered, overrides int
	fallbacks                 map[string]int
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return 100 * float64(a) / float64(b)
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func meanSD(xs []float64) (float64, float64) {
	m := mean(xs)
	if len(xs) < 2 {
		return m, 0
	}
	v := 0.0
	for _, x := range xs {
		v += (x - m) * (x - m)
	}
	return m, math.Sqrt(v / float64(len(xs)-1))
}

func quant(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[int(q*float64(len(s)-1))]
}
