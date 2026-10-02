// Command mzselfplay is the game engine of DraftZero's training loop with
// gorge in place of the XMage JVM.
//
// draft-zero's loop launches the engine as
//
//	[MZ_JAVA, <jvm flags...>, -jar, lib/mage-magezero-1.4.58.jar, <game.yml>]
//
// (engine.launch_jvm). scripts/mzrepro/mzjava is the MZ_JAVA to set: it
// drops the JVM arguments and runs this command on the game.yml. The command
// then does what MageZeroMain / ParallelDataGenerator.generateData do: plays
// training.games games between the two configured players, writes each
// player's training records to its output_file as HDF5, and prints one
// GAME_SUMMARY line per finished game. The loop, MageZero's train.py,
// test.py and server.py run unchanged around it.
//
// Usage:
//
//	mzselfplay [flags] <game.yml>
//
// Environment (each also a flag):
//
//	MZ_ACTION_VOCAB    the action vocabulary (draft-zero assets/vocab/FDN_SPG.tsv);
//	                   required, and the same file the trainer and server read
//	MZ_GORGE_CARDS     the compiled card corpus directory (default .cards)
//	MZ_PYTHON          the Python that has numpy and h5py (default python3)
//	MZ_NPY2H5          scripts/mzrepro/npy2h5.py (the shim sets it)
//	MZ_SEED            the run seed (default: FNV-1a of the game.yml bytes)
//	MZ_EVAL_TIMEOUT_MS one inference request's timeout (default 30000)
//	MZ_TRACE           1 logs every searched decision
//
// Exit status: 0 when every requested game was attempted and both shards
// were written (a failed game is logged and skipped, as upstream skips it);
// 2 for a configuration this engine refuses or cannot run; 3 when a network
// player's inference server stopped answering and leaves were evaluated by
// the offline evaluator instead (the shards are still written).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/azmcts/clairvoyant"
	"github.com/adams-shaun/gorge/internal/mzbridge"
	"github.com/adams-shaun/gorge/internal/mzbridge/mzclient"
	"github.com/adams-shaun/gorge/internal/mzplay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// logger writes lines in the XMage bundle's log4j layout, the one
// draft-zero's metrics.LINE_RE parses:
//
//	LEVEL date time,ms message =>[thread] class
type logger struct {
	mu  sync.Mutex
	out io.Writer
}

func (l *logger) logf(level, thread, format string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	fmt.Fprintf(l.out, "%-5s %s,%03d %s =>[%s] MzSelfPlay\n", level, now.Format("2006-01-02 15:04:05"), now.Nanosecond()/1e6,
		fmt.Sprintf(format, a...), thread)
}

// netEval is one inference server as the leaves see it: mzclient behind a
// request timeout, with the failure accounting shared by every game.
type netEval struct {
	url     string
	client  *mzclient.Client
	timeout time.Duration

	evals, errs atomic.Int64
	nanos       atomic.Int64
	streak      atomic.Int32
	dead        atomic.Bool
}

var errServerDead = errors.New("mzselfplay: the inference server stopped answering; not asked again")

// deadAfter is how many requests in a row may fail before a server is taken
// for dead by every game.
const deadAfter = 3

func (n *netEval) Evaluate(ids []int32) (mzbridge.Evaluation, error) {
	if n.dead.Load() {
		return mzbridge.Evaluation{}, errServerDead
	}
	ctx, cancel := context.WithTimeout(context.Background(), n.timeout)
	defer cancel()
	t0 := time.Now()
	ev, err := n.client.Evaluate(ctx, ids)
	n.nanos.Add(int64(time.Since(t0)))
	n.evals.Add(1)
	if err != nil {
		n.errs.Add(1)
		if n.streak.Add(1) >= deadAfter {
			n.dead.Store(true)
		}
		return ev, err
	}
	n.streak.Store(0)
	return ev, nil
}

// runStats is the run's totals, printed as one MZSELFPLAY_STATS line.
type runStats struct {
	Games         int            `json:"games"`
	Failed        int            `json:"failed"`
	WallSeconds   float64        `json:"wall_seconds"`
	PlaySeconds   float64        `json:"play_seconds"`
	Threads       int            `json:"threads"`
	Seed          uint64         `json:"seed"`
	RowsA         int            `json:"rows_a"`
	RowsB         int            `json:"rows_b"`
	RowsPriority  int            `json:"rows_priority"`
	RowsTarget    int            `json:"rows_target"`
	RowsUse       int            `json:"rows_use"`
	Turns         int            `json:"turns"`
	Submits       int            `json:"submits"`
	Searches      int            `json:"searches"`
	BotAnswers    int            `json:"bot_answers"`
	BotByKind     map[string]int `json:"bot_answers_by_kind"`
	Trivial       int            `json:"trivial_passes"`
	Timeouts      int            `json:"search_timeouts"`
	MacroFailed   int            `json:"macro_failed"`
	Simulations   int            `json:"simulations"`
	SimFailures   int            `json:"simulation_failures"`
	SimPanics     int            `json:"simulation_panics"`
	SimSubmitErr  int            `json:"simulation_submit_errors"`
	SimChance     int            `json:"simulation_chance_failures"`
	SimBadWorlds  int            `json:"simulation_bad_worlds"`
	LeafEvals     int64          `json:"leaf_evals"`
	LeafPerSecond float64        `json:"leaf_evals_per_second"`
	NetEvals      int64          `json:"net_evals"`
	NetErrors     int64          `json:"net_errors"`
	NetFallbacks  int64          `json:"net_fallbacks"`
	NetMeanMS     float64        `json:"net_mean_ms"`
	ActionCands   int            `json:"action_candidates"`
	ActionHits    int            `json:"action_vocab_hits"`
	ActionVisits  int            `json:"action_visits"`
	ActionHitV    int            `json:"action_vocab_hit_visits"`
	TargetCands   int            `json:"target_candidates"`
	TargetHits    int            `json:"target_vocab_hits"`
	TargetVisits  int            `json:"target_visits"`
	TargetHitV    int            `json:"target_vocab_hit_visits"`
	MissedActions []string       `json:"missed_actions,omitempty"`
	MissedTargets []string       `json:"missed_targets,omitempty"`
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mzselfplay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cardsDir := fs.String("cards", envOr("MZ_GORGE_CARDS", ".cards"), "compiled card corpus directory")
	vocabPath := fs.String("vocab", os.Getenv(mzbridge.MZActionVocabEnv), "action vocabulary file (MZ_ACTION_VOCAB)")
	python := fs.String("python", envOr("MZ_PYTHON", "python3"), "Python with numpy and h5py, for the HDF5 conversion")
	converter := fs.String("npy2h5", os.Getenv("MZ_NPY2H5"), "scripts/mzrepro/npy2h5.py")
	seedText := fs.String("seed", os.Getenv("MZ_SEED"), "run seed (default: FNV-1a of the game.yml bytes)")
	evalTimeout := fs.Int("eval-timeout-ms", envInt("MZ_EVAL_TIMEOUT_MS", 30000), "one inference request's timeout")
	trace := fs.Bool("trace", os.Getenv("MZ_TRACE") == "1", "log every searched decision")
	keepNPY := fs.Bool("keep-npy", false, "write the .npy shard files only, without converting to HDF5")
	cpuprofile := fs.String("cpuprofile", "", "write a CPU profile")
	checkPool := fs.Bool("check-pool", false, "resolve every deck of the pool files given as arguments against the corpus and report; play nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *checkPool {
		return checkPools(fs.Args(), *cardsDir, stdout)
	}
	log := &logger{out: stdout}
	fail := func(format string, a ...any) int {
		log.logf("FATAL", "main", format, a...)
		fmt.Fprintf(stderr, "mzselfplay: "+format+"\n", a...)
		return 2
	}
	if fs.NArg() < 1 {
		return fail("usage: mzselfplay [flags] <game.yml>")
	}
	// The last argument is the config: the MZ_JAVA shim passes the JVM's.
	ymlPath := fs.Arg(fs.NArg() - 1)
	raw, err := os.ReadFile(ymlPath)
	if err != nil {
		return fail("%v", err)
	}
	cfg, err := mzplay.ParseConfig(string(raw))
	if err != nil {
		return fail("%s: %v", ymlPath, err)
	}
	if err := cfg.Validate(); err != nil {
		return fail("%s: %v", ymlPath, err)
	}
	if cfg.A.OutputFile == "" || cfg.B.OutputFile == "" {
		return fail("%s: both players need an output_file", ymlPath)
	}
	if *vocabPath == "" {
		return fail("no action vocabulary: set %s to draft-zero's assets/vocab/FDN_SPG.tsv (the file the trainer and the server read)", mzbridge.MZActionVocabEnv)
	}
	vocab, err := mzbridge.LoadVocab(*vocabPath)
	if err != nil {
		return fail("%v", err)
	}
	if !*keepNPY && *converter == "" {
		return fail("no HDF5 converter: set MZ_NPY2H5 to scripts/mzrepro/npy2h5.py (the mzjava shim does)")
	}
	var runSeed uint64
	if *seedText != "" {
		if runSeed, err = strconv.ParseUint(*seedText, 10, 64); err != nil {
			return fail("seed %q: %v", *seedText, err)
		}
	} else {
		h := fnv.New64a()
		h.Write(raw)
		runSeed = h.Sum64()
	}
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return fail("%v", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return fail("%v", err)
		}
		defer pprof.StopCPUProfile()
	}
	t0 := time.Now()

	// The plan: every game's seed and decks, then the corpus for exactly
	// those decks' cards.
	pool := func(p mzplay.PlayerConfig) ([]string, error) {
		if p.DeckPool != "" {
			return mzplay.ReadPool(p.DeckPool)
		}
		return []string{p.DeckPath}, nil
	}
	poolA, err := pool(cfg.A)
	if err != nil {
		return fail("player_a: %v", err)
	}
	poolB, err := pool(cfg.B)
	if err != nil {
		return fail("player_b: %v", err)
	}
	plans, err := mzplay.PlanGames(cfg, runSeed, poolA, poolB)
	if err != nil {
		return fail("%v", err)
	}
	lists := map[string]mzplay.DeckList{} // lookup only
	var names []string
	for _, pl := range plans {
		for _, path := range []string{pl.DeckA, pl.DeckB} {
			if _, ok := lists[path]; ok {
				continue
			}
			list, err := mzplay.LoadDCK(path)
			if err != nil {
				return fail("deck %v", err)
			}
			lists[path] = list
			names = append(names, list.Names()...)
		}
	}
	reg, err := cards.OpenCorpusFor(*cardsDir, names)
	if err != nil {
		return fail("card corpus %s: %v", *cardsDir, err)
	}
	decks := map[string]mzplay.ResolvedDeck{} // lookup only
	badDecks := map[string]error{}            // lookup only
	for _, pl := range plans {
		for _, path := range []string{pl.DeckA, pl.DeckB} {
			if _, ok := decks[path]; ok {
				continue
			}
			if _, bad := badDecks[path]; bad {
				continue
			}
			d, err := mzplay.LoadDeck(path, reg)
			if err != nil {
				badDecks[path] = err
				log.logf("ERROR", "main", "deck refused: %v", err)
				continue
			}
			decks[path] = d
		}
	}

	// The inference servers of the network players.
	clairvoyant.AllowClairvoyant()
	threads := max(1, min(cfg.Threads, max(1, cfg.Games)))
	servers := map[int]*netEval{} // lookup only
	seatServer := [2]*netEval{}
	for i, p := range []struct {
		pc   mzplay.PlayerConfig
		port int
	}{{cfg.A, cfg.Port}, {cfg.B, cfg.OpponentPort}} {
		if p.pc.OfflineMode {
			continue
		}
		srv, ok := servers[p.port]
		if !ok {
			url := fmt.Sprintf("http://%s:%d", hostOf(cfg.Host), p.port)
			// One game thread asks one leaf at a time, so at most `threads`
			// bags are ever waiting: a batch of that many goes at once, and
			// a smaller one after a short wait for company.
			timeout := time.Duration(*evalTimeout) * time.Millisecond
			srv = &netEval{url: url, timeout: timeout, client: mzclient.New(url, mzclient.Options{
				MaxBatch: threads, FlushInterval: 500 * time.Microsecond, MaxInFlight: 2 * threads,
				HTTPClient: &http.Client{Timeout: timeout},
			})}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			herr := srv.client.Healthz(ctx)
			cancel()
			if herr != nil {
				return fail("player %s plays with the network (offline_mode false) but its inference server %s does not answer: %v", "ab"[i:i+1], url, herr)
			}
			servers[p.port] = srv
			defer srv.client.Close()
		}
		seatServer[i] = srv
	}
	log.logf("INFO", "main", "gorge self-play: %d games, %d threads, run seed %d, budget A %d (%s) B %d (%s), corpus %d cards (subset %v), vocab A=%d",
		cfg.Games, threads, runSeed, cfg.A.SearchBudget, leafName(cfg.A), cfg.B.SearchBudget, leafName(cfg.B), len(reg.Cards), reg.IsSubset(), vocab.Dim())

	// The games.
	results := make([]*mzplay.GameResult, len(plans))
	var gameCount, winCount, failed atomic.Int64
	var leafEvals, netFallbacks atomic.Int64
	var playNanos atomic.Int64
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < threads; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			thread := fmt.Sprintf("pool-1-thread-%d", w+1)
			for i := range jobs {
				pl := plans[i]
				da, okA := decks[pl.DeckA]
				db, okB := decks[pl.DeckB]
				if !okA || !okB {
					failed.Add(1)
					log.logf("ERROR", thread, "A game simulation failed and its result will be ignored. Cause: deck refused (%s vs %s)", pl.DeckA, pl.DeckB)
					continue
				}
				start := time.Now()
				var leaves [2]*mzplay.NetLeaf
				var offline [2]int64
				seat := func(s int, pc mzplay.PlayerConfig, d mzplay.ResolvedDeck) mzplay.SeatSetup {
					out := mzplay.SeatSetup{Deck: d.Cards, DeckName: d.Stem, Budget: pc.SearchBudget, BackpropDiscount: pc.BackpropDiscount,
						Lambda: pc.TDDiscount, SeeOpponentHand: pc.SeeOpponentHand}
					if srv := seatServer[s]; srv != nil {
						leaves[s] = mzplay.NewNetLeaf(srv, pc.SeeOpponentHand)
						out.Leaf = leaves[s].Leaf
					} else {
						n := &offline[s]
						out.Leaf = func(e *rules.Engine, actor state.PlayerID) float64 {
							*n++
							return mzplay.OfflineLeaf(e, actor)
						}
					}
					return out
				}
				gs := mzplay.GameSetup{
					Index: pl.Index, Seed: pl.Seed, GoesFirst: cfg.GoesFirst, MaxTurns: cfg.MaxTurns,
					Seats:  [2]mzplay.SeatSetup{seat(0, cfg.A, da), seat(1, cfg.B, db)},
					Tokens: reg.Tokens, NameUniverse: reg.Cards, Vocab: vocab,
				}
				timeouts := [2]time.Duration{time.Duration(cfg.A.TimeoutMS) * time.Millisecond, time.Duration(cfg.B.TimeoutMS) * time.Millisecond}
				gs.DecisionContext = func(s int) (context.Context, context.CancelFunc) {
					if timeouts[s] <= 0 {
						return context.Background(), func() {}
					}
					return context.WithTimeout(context.Background(), timeouts[s])
				}
				if cfg.MaxMinutes > 0 {
					deadline := start.Add(time.Duration(cfg.MaxMinutes) * time.Minute)
					gs.Abort = func() bool { return time.Now().After(deadline) }
				}
				if *trace {
					gs.Trace = func(line string) { log.logf("INFO", thread, "%s", line) }
				}
				log.logf("INFO", thread, "Using seed: %d (game %d: %s vs %s)", int64(pl.Seed), pl.Index, da.Stem, db.Stem)
				r, err := mzplay.PlayGame(gs)
				playNanos.Add(int64(time.Since(start)))
				for s := range leaves {
					leafEvals.Add(offline[s])
					if leaves[s] != nil {
						leafEvals.Add(int64(leaves[s].Calls))
						netFallbacks.Add(int64(leaves[s].Fallbacks))
						if leaves[s].Fallbacks > 0 {
							log.logf("ERROR", thread, "REMOTE EVAL FAILURE: game %d seat %d: %d of %d leaves fell back to the offline evaluator (last error: %v)",
								pl.Index, s, leaves[s].Fallbacks, leaves[s].Calls, leaves[s].LastErr)
						}
					}
				}
				if err != nil {
					failed.Add(1)
					if errors.Is(err, mzplay.ErrAborted) {
						log.logf("ERROR", thread, "Game timed out after %d minutes, cancelling.", cfg.MaxMinutes)
					} else {
						log.logf("ERROR", thread, "A game simulation failed and its result will be ignored. Cause: %v", err)
					}
					continue
				}
				results[i] = &r
				if r.Winner == 0 {
					winCount.Add(1)
				}
				n := gameCount.Add(1)
				log.logf("INFO", thread, "Game #%d completed successfully (%.1fs, %d turns, %d+%d records, %d searches, %d simulations)",
					n, time.Since(start).Seconds(), r.Turns, len(r.Rows[0]), len(r.Rows[1]), r.Stats.Searches, r.Stats.Simulations)
				log.logf("INFO", thread, "%s", mzplay.SummaryOf(int(n), r, da.Stem, db.Stem).Line())
				log.logf("INFO", thread, "Current WR: %v", float64(winCount.Load())/float64(n))
			}
		}(w)
	}
	for i := range plans {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	// The shards, in game-index order whatever order the threads finished in.
	shards := [2]*mzbridge.Shard{mzbridge.NewShard(vocab.Dim()), mzbridge.NewShard(vocab.Dim())}
	st := runStats{Failed: int(failed.Load()), Threads: threads, Seed: runSeed}
	var total mzplay.GameStats
	for _, r := range results {
		if r == nil {
			continue
		}
		st.Games++
		st.Turns += r.Turns
		total.Add(r.Stats)
		for s := range shards {
			for _, row := range r.Rows[s] {
				if err := shards[s].Append(mzbridge.Record{IDs: row.IDs, Policy: row.Policy, Value: float32(row.Value), Score: float32(row.Score),
					IsPlayer: true, Type: row.Type}); err != nil {
					return fail("game %d seat %d: %v", r.Index, s, err)
				}
			}
			shards[s].EndGame()
		}
	}
	for s, out := range []string{cfg.A.OutputFile, cfg.B.OutputFile} {
		if err := writeShard(shards[s], out, *python, *converter, *keepNPY, log); err != nil {
			return fail("%v", err)
		}
	}

	st.RowsA, st.RowsB = shards[0].Rows(), shards[1].Rows()
	st.RowsPriority, st.RowsTarget, st.RowsUse = total.RowsPriority, total.RowsTarget, total.RowsUse
	st.Submits, st.Searches, st.BotAnswers, st.Trivial = total.Submits, total.Searches, total.Bot, total.Trivial
	st.BotByKind = map[string]int{}
	for i, n := range total.BotByKind {
		if n > 0 {
			st.BotByKind[mzplay.BotKindNames[i]] = n
		}
	}
	st.Timeouts, st.MacroFailed, st.Simulations, st.SimFailures = total.Timeouts, total.MacroFailed, total.Simulations, total.SimFailures
	st.ActionCands, st.ActionHits, st.ActionVisits, st.ActionHitV = total.ActionCands, total.ActionHits, total.ActionVisits, total.ActionHitV
	st.TargetCands, st.TargetHits, st.TargetVisits, st.TargetHitV = total.TargetCands, total.TargetHits, total.TargetVisits, total.TargetHitV
	st.MissedActions, st.MissedTargets = total.MissedActions, total.MissedTargets
	st.SimPanics, st.SimSubmitErr, st.SimChance, st.SimBadWorlds = total.SimPanics, total.SimSubmitErrors, total.SimChance, total.SimBadWorlds
	st.LeafEvals, st.NetFallbacks = leafEvals.Load(), netFallbacks.Load()
	st.WallSeconds = time.Since(t0).Seconds()
	st.PlaySeconds = float64(playNanos.Load()) / 1e9
	if st.PlaySeconds > 0 {
		st.LeafPerSecond = float64(st.LeafEvals) / st.PlaySeconds
	}
	var netNanos int64
	for _, srv := range servers {
		st.NetEvals += srv.evals.Load()
		st.NetErrors += srv.errs.Load()
		netNanos += srv.nanos.Load()
	}
	if st.NetEvals > 0 {
		st.NetMeanMS = float64(netNanos) / 1e6 / float64(st.NetEvals)
	}
	if b, err := json.Marshal(st); err == nil {
		log.logf("INFO", "main", "MZSELFPLAY_STATS %s", b)
	}
	log.logf("INFO", "main", "--- Simulation Summary ---")
	log.logf("INFO", "main", "Total requested: %d games", cfg.Games)
	log.logf("INFO", "main", "Successful: %d", st.Games)
	log.logf("INFO", "main", "Failed: %d", st.Failed)
	if st.Games > 0 {
		log.logf("INFO", "main", "Player A win rate: %.2f%% (%d/%d)", 100*float64(winCount.Load())/float64(st.Games), winCount.Load(), st.Games)
	}
	if st.NetFallbacks > 0 {
		log.logf("ERROR", "main", "%d of %d leaf evaluations of a network player were answered by the OFFLINE evaluator because its inference server failed", st.NetFallbacks, st.LeafEvals)
		fmt.Fprintf(stderr, "mzselfplay: %d leaf evaluations fell back to the offline evaluator (inference server failure)\n", st.NetFallbacks)
		return 3
	}
	return 0
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

// hostOf maps "localhost" to the IPv4 loopback: server.py binds 127.0.0.1,
// and "localhost" may resolve to ::1 first.
func hostOf(h string) string {
	if h == "" || h == "localhost" {
		return "127.0.0.1"
	}
	return h
}

func leafName(p mzplay.PlayerConfig) string {
	if p.OfflineMode {
		return "offline evaluator"
	}
	return "network"
}

// writeShard writes one seat's shard at out: the four .npy files under
// out's stem, then the HDF5 file MageZero reads (scripts/mzrepro/npy2h5.py).
func writeShard(s *mzbridge.Shard, out, python, converter string, keepNPY bool, log *logger) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	stem := out + ".part"
	if keepNPY {
		stem = out
	}
	if err := s.WriteFiles(stem); err != nil {
		return err
	}
	if keepNPY {
		log.logf("INFO", "lz-writer", "wrote %s.*.npy: %d states, %d games", stem, s.Rows(), s.Games())
		return nil
	}
	cmd := exec.Command(python, converter, "--dim", strconv.Itoa(s.Dim()), "--remove", stem, out)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("HDF5 conversion of %s failed: %v: %s", out, err, b)
	}
	log.logf("INFO", "lz-writer", "Processing %d states. %s", s.Rows(), trimNL(b))
	return nil
}

func trimNL(b []byte) string {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return string(b)
}
