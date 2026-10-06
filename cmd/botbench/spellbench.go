package main

// -spellbench: a gorge-native SpellBench workup. It plays a round robin of
// bot policies on a SpellBench catalog's decks (-spellbench-catalog:
// pauper-kernel, the default, or fdn-limited) exactly the way SpellBench's
// arena schedules its benchmark (python/spellbench/arena/runner.py,
// "spellbench-arena-seed-v1"):
//
//   - matchups are the unordered bot pairs (i < j) in -spellbench order,
//     self-play excluded (the benchmark's include_self_play false);
//   - each matchup plays pairs_per_matchup = -spellbench-pairs x len(decks)
//     seat-swapped PAIRS; pair p plays deck pool[p % len(pool)] in BOTH
//     seats (a mirror, the benchmark's deck_pool form);
//   - both games of a pair share one game seed (common random numbers):
//     game 0 seats bot i as p0, game 1 seats bot j as p0;
//   - game_seed(m, p) = SplitMix64(base ^ 0x5350_5f47_414d_4553 ^ m*golden ^
//     p*0xd1b54a32d192ed03).next() & (2^53-1), the arena's derivation, and
//     the base seed defaults to the benchmark's 20260926.
//
// The gorge engine is seeded with the game seed, so the CR 103.1 toss hands
// the first turn to the same SEAT in both games of a pair and each bot
// starts once. A seat's policy seed is (game seed ^ seat+1), bench's
// per-seat derivation; the sb-uniform constructor XORs in the benchmark's
// uniform seed (11), v2's (agent_seed ^ seed).
//
// Results are written as a SpellBench match ledger (matches.jsonl, schema
// spellbench-match-ledger/v1) in schedule order, so SpellBench's own
// leaderboard code rates them (scripts/spellbench-rate.py): natural wins and
// draws are rated; a game ended by -max-turns / -max-intents /
// -max-turn-intents is "truncated" and one ended by an engine panic,
// livelock or a refused answer is "halted" -- both recorded, both excluded,
// exactly as SpellBench excludes them. games.jsonl carries the gorge-side
// extras (turns, wall time, stall kind, fallbacks) per game.
//
// A builtin (sb-*) seat whose answer the engine refuses (a whole-declaration
// constraint the wire does not publish, e.g. a lone blocker on a menace
// attacker) is asked again first (builtins.Seat.Refused: at priority the
// policy's next choice with the refused option withdrawn, never a bare
// pass); only if that is refused too does a fallback answer: the minimal
// answer (pass, or the clamped empty answer), else the default bot's
// answer. Every refusal and fallback is counted per policy and reported.
// Any other seat's refused answer halts the game.
//
// Builtin seats get the engine as their potential-play planner
// (builtins.Seat.SetPlanner, rules.Engine.PotentialPaymentPlans): a pure
// read of the seat's own pool and sources at the decision it answers. The
// summary reports, per policy, the plays lost (chosen, payable, not taken),
// the lowering recoveries (re-plans, yields, stack waits) and the plays the
// planner proved unpayable.

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/internal/azmcts"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/spellbench/registry"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// sbOpts are the -spellbench-* flags.
type sbOpts struct {
	bots          string
	pairs         int
	decks         string
	out           string
	baseSeed      uint64
	with, without string
	engineVersion string
	catalog       string
	format        string // the catalog's ledger format, set from catalog
	// mulligans is -spellbench-mulligans: the London round size for every
	// game of the run. 0 (the default) is SpellBench's behaviour today: no
	// mulligan ask is ever posed. >0 turns the round on so a mulligan policy
	// A/B can run on this bench.
	mulligans int
	// tacticalWeights / tacticalAlt name JSON weight files for sb-tactical
	// and sb-tactical-alt.
	tacticalWeights, tacticalAlt string
	// trace names a directory where every sb-tactical seat writes its
	// scored decisions, one file per game and seat (debugging).
	trace string
}

var sbFlags sbOpts

func registerSpellbenchFlags(fs *flag.FlagSet) {
	fs.StringVar(&sbFlags.bots, "spellbench", "", "SpellBench workup mode: comma list of policies to round-robin (e.g. sb-uniform,sb-heuristic,sb-first,bot,az) on the -spellbench-catalog decks as mirrors; writes a SpellBench match ledger to -spellbench-out")
	fs.IntVar(&sbFlags.pairs, "spellbench-pairs", 4, "spellbench: seat-swapped pairs per deck per matchup (the benchmark's pairs_per_deck)")
	fs.StringVar(&sbFlags.decks, "spellbench-decks", "", "spellbench: comma list of catalog deck ids (default: the catalog's benchmark pool)")
	fs.StringVar(&sbFlags.catalog, "spellbench-catalog", "pauper-kernel", "spellbench: deck catalog, pauper-kernel, fdn-limited (alias fdn) or repo-constructed")
	fs.StringVar(&sbFlags.out, "spellbench-out", "", "spellbench: output directory (created; matches.jsonl, games.jsonl, run.json are written there)")
	fs.Uint64Var(&sbFlags.baseSeed, "spellbench-base-seed", 20260926, "spellbench: tournament base seed (the benchmark's base_seed)")
	fs.StringVar(&sbFlags.with, "spellbench-with", "", "spellbench: play only the matchups that include this policy (indices and seeds stay those of the full round robin)")
	fs.StringVar(&sbFlags.without, "spellbench-without", "", "spellbench: skip the matchups that include this policy")
	fs.StringVar(&sbFlags.tacticalWeights, "spellbench-tactical-weights", "", "spellbench: JSON file of builtins.TacticalWeights for the sb-tactical arms (default: the built-in weights; absent fields keep their defaults)")
	fs.StringVar(&sbFlags.tacticalAlt, "spellbench-tactical-alt-weights", "", "spellbench: comma list of JSON files of builtins.TacticalWeights for sb-tactical-alt, -alt2 ... -alt8 (weight-tuning A/B)")
	fs.StringVar(&sbFlags.trace, "spellbench-trace", "", "spellbench: directory for sb-tactical decision traces (one file per game and seat)")
	fs.StringVar(&sbFlags.engineVersion, "spellbench-engine-version", "dev", "spellbench: engine_version recorded in the ledger (e.g. the git commit)")
	fs.IntVar(&sbFlags.mulligans, "spellbench-mulligans", 0, "spellbench: London mulligans per player per game (0 = none posed, the benchmark default; 0..6 as host/httpapi clamps it)")
}

const (
	sbMask64       = ^uint64(0)
	sbGolden       = uint64(0x9E3779B97F4A7C15)
	sbGameDomain   = uint64(0x53505F47414D4553) // "SP_GAMES"
	sbPairMixer    = uint64(0xD1B54A32D192ED03)
	sbMaxJSONInt   = uint64(1)<<53 - 1
	sbLedgerSchema = "spellbench-match-ledger/v1"
)

// sbGameSeed is the arena's derive_game_seed.
func sbGameSeed(base uint64, matchup, pair int) uint64 {
	mixed := base ^ sbGameDomain ^ (uint64(matchup) * sbGolden) ^ (uint64(pair) * sbPairMixer)
	s := builtins.NewSplitMix64(mixed & sbMask64)
	return s.Next() & sbMaxJSONInt
}

// sbGame is one scheduled game.
type sbGame struct {
	id      string
	matchup int
	pair    int
	game    int
	seed    uint64
	deck    string
	seats   [2]string // policy name at p0, p1
	ordinal int       // position in the full schedule
}

// sbSchedule is the arena's _schedule over bots and pool.
func sbSchedule(bots, pool []string, pairsPerDeck int, base uint64) []sbGame {
	var out []sbGame
	m := 0
	for i := 0; i < len(bots); i++ {
		for j := i + 1; j < len(bots); j++ {
			for p := 0; p < pairsPerDeck*len(pool); p++ {
				seed := sbGameSeed(base, m, p)
				for g := 0; g < 2; g++ {
					seats := [2]string{bots[i], bots[j]}
					if g == 1 {
						seats = [2]string{bots[j], bots[i]}
					}
					out = append(out, sbGame{
						id: fmt.Sprintf("m%04dp%04dg%d", m, p, g), matchup: m, pair: p, game: g,
						seed: seed, deck: pool[p%len(pool)], seats: seats, ordinal: len(out),
					})
				}
			}
			m++
		}
	}
	return out
}

// sbResult is one played game.
type sbResult struct {
	outcome   gbench.Outcome
	err       error
	wall      time.Duration
	fallbacks [2]int
	rejects   [2]string // first refused answer per seat
	stats     [2]builtins.Stats
	// mullAsks / mullTaken count each seat's keep/mulligan asks and the
	// "mulligan" answers among them (bottoming answers are hand-retention,
	// not a mulligan taken). Mechanism evidence for a mulligan policy A/B.
	mullAsks  [2]int
	mullTaken [2]int
	// corpus is this game's -az-corpus records as one gzip member (nil when
	// off or when no az seat searched): members concatenate into the file.
	corpus []byte
	// visits counts the records in corpus.
	visits int
}

// sbDisplayName is the ledger name for a policy. A spec resolves through
// internal/spellbench/registry and the spec string is itself the ledger
// name (a composed candidate shows up under its composed name), except az:
// it carries its world and simulation count so a clairvoyant number can
// never be read as a fair one -- and a COMPOSED az spec keeps that marker
// ahead of its decorators ("az+passguard" is
// "az-clairvoyant-sims<N>+passguard"), so scripts/spellbench-rate.py's
// name tag (a leading "az-") still classifies it as a search agent and no
// composed clairvoyant spec can be rated as a fair one.
func sbDisplayName(policy string) string {
	base := basePolicy(policy)
	if !isAZPolicy(base) {
		return policy
	}
	if azLabel != "" {
		return azLabel
	}
	cfg := azSeatConfig(base)
	if cfg.PriorOnly && base == "az" {
		return "az-prior" + strings.TrimPrefix(policy, base)
	}
	world := cfg.World
	if world == "" {
		// azFrontDoor always sets azCfg.World before a run; the empty case
		// is a bare sbDisplayName call (tests), where az's historical world
		// is clairvoyant.
		world = "clairvoyant"
	}
	name := fmt.Sprintf("az-%s-sims%d", world, cfg.Search.Sims)
	if world == "redeal" && cfg.Worlds > 0 {
		name += fmt.Sprintf("-k%d", cfg.Worlds)
	}
	if rest := strings.TrimPrefix(policy, base); rest != "" {
		name += rest
	}
	return name
}

// basePolicy is a registry spec's base policy, dropping its decorators
// ("az+passguard" -> "az"); az classification uses it so a decorated az
// seat is still recognised as az.
func basePolicy(spec string) string {
	base, _, _ := strings.Cut(spec, "+")
	return base
}

func sbBotID(name string) string {
	h := sha256.Sum256([]byte("gorge-botbench-policy/v1:" + name))
	return hex.EncodeToString(h[:])
}

// sbIntentPicksKind reports whether in's first choice selects an option of
// the given kind on d.
func sbIntentPicksKind(d *decision.Decision, in decision.Intent, kind string) bool {
	if len(in.Choices) == 0 {
		return false
	}
	for _, o := range d.Options {
		if o.Index == in.Choices[0] {
			return o.Kind == kind
		}
	}
	return false
}

// sbSubmitWithFallback is the Hooks.Submit that keeps a builtin seat's
// refused answer from halting the game (file comment). The builtin is
// found through the decoration (registry.UnwrapSeat), so a decorated sb-*
// spec keeps the fallback exactly as the bare name has it.
func sbSubmitWithFallback(seats []seat.Seat, res *sbResult) func(*rules.Engine, int, *decision.Decision, decision.Intent) (bool, error) {
	return func(e *rules.Engine, seatIdx int, d *decision.Decision, in decision.Intent) (bool, error) {
		err := e.Submit(in)
		if err == nil {
			return true, nil
		}
		b, ok := registry.UnwrapSeat(seats[seatIdx]).(*builtins.Seat)
		if !ok {
			return true, err
		}
		if res.rejects[seatIdx] == "" {
			res.rejects[seatIdx] = fmt.Sprintf("%s: %v", d.Kind, err)
		}
		v := view.Project(e.G, e, d.Player, d)
		v.Round = view.RoundOf(e.G, e.L.Events)
		if e.Submit(b.Refused(v, *d, in)) == nil {
			return true, nil
		}
		res.fallbacks[seatIdx]++
		brd := botpolicy.BoardFromGame(e.G, e, d.Player)
		fallbacks := bots.Fallbacks(d, brd, seatIdx)
		var err3 error
		for _, fb := range fallbacks {
			if err3 = e.Submit(fb); err3 == nil {
				return true, nil
			}
		}
		return true, fmt.Errorf("answer refused (%v); fallbacks refused (%v)", err, err3)
	}
}

func sbPlay(g sbGame, deck []*cards.Card, reg *cards.Registry, maxTurns, maxIntents int) sbResult {
	var res sbResult
	seats := make([]seat.Seat, 2)
	for s := 0; s < 2; s++ {
		// The names were validated by spellbenchExit; a build failure here
		// is a programming error, so it panics like the old nil map entry.
		s0, err := registry.Build(g.seats[s], g.seed^uint64(s+1))
		if err != nil {
			panic("spellbench: " + err.Error())
		}
		seats[s] = s0
		if b, ok := registry.UnwrapSeat(seats[s]).(*builtins.Seat); ok && sbFlags.trace != "" && b.Policy() == builtins.Tactical {
			f, err := os.Create(filepath.Join(sbFlags.trace, fmt.Sprintf("%s-%s-p%d.txt", g.id, g.deck, s)))
			if err == nil {
				defer f.Close()
				b.SetTrace(f)
			}
		}
	}
	cfg := rules.Config{
		Seed: g.seed, Names: []string{"p0", "p1"}, Decks: [][]*cards.Card{deck, deck},
		Tokens: reg.Tokens, NameUniverse: reg.Cards, Mulligans: sbFlags.mulligans,
	}
	// A finished game's storage backs the next game this worker plays
	// (rules.Spare; reuse never changes a game -- the same contract
	// playMatch's sparePool relies on).
	spare := sbSparePool.Get()
	cfg.Spare = spare
	// The decision arena backs the game's priority decisions, resolution
	// contexts and LKI copies with chunks the Spare recycles. It is sound
	// only when nothing read from the engine outlives its Release below: a
	// non-search seat answers each decision before the next is posed and the
	// seats die with this call. A search seat reads the live engine as its
	// root, so a game with one keeps the arena off (decision_arena.go).
	arena := true
	for s := 0; s < 2; s++ {
		if _, ok := seats[s].(searchseat.SearchSeat); ok {
			arena = false
		}
		if _, ok := registry.UnwrapSeat(seats[s]).(searchseat.SearchSeat); ok {
			arena = false
		}
	}
	var recs []policynet.VisitRecord
	if azCorpusPath != "" {
		for s := 0; s < 2; s++ {
			az, ok := registry.UnwrapSeat(seats[s]).(*azmcts.Seat)
			if !ok {
				continue
			}
			opp := g.seats[1-s]
			az.SetRecorder(func(r policynet.VisitRecord) {
				r.GameID, r.Deck, r.Seed, r.Opponent = g.id, g.deck, g.seed, opp
				recs = append(recs, r)
			})
		}
	}
	hooks := gbench.Hooks{Submit: sbSubmitWithFallback(seats, &res),
		Decision: func(seatIdx int, d *decision.Decision, in decision.Intent, _ *botpolicy.Board) error {
			if d.Kind != decision.KMulligan || len(d.Options) == 0 || d.Options[0].Kind == "bottom" {
				return nil
			}
			res.mullAsks[seatIdx]++
			if sbIntentPicksKind(d, in, "mulligan") {
				res.mullTaken[seatIdx]++
			}
			return nil
		}, Setup: func(e *rules.Engine) {
			if arena {
				e.SetDecisionArena(true)
			}
			for _, st := range seats {
				if b, ok := registry.UnwrapSeat(st).(*builtins.Seat); ok {
					b.SetPlanner(e)
				}
			}
		}}
	if maxTurnIntents > 0 {
		hooks.Guard = turnIntentGuard(maxTurnIntents)
	}
	t0 := time.Now()
	o, e, err := gbench.PlayGame(cfg, seats, maxTurns, maxIntents, hooks)
	res.wall = time.Since(t0)
	res.outcome, res.err = o, err
	if len(recs) > 0 {
		res.corpus, res.visits = sbCorpusMember(recs, o, err)
	}
	for s := 0; s < 2; s++ {
		if b, ok := registry.UnwrapSeat(seats[s]).(*builtins.Seat); ok {
			res.stats[s] = b.Stats
		}
	}
	if err == nil && e != nil && !gbench.IsAbort(o.StallOn) {
		// The engine's last use: the outcome, corpus and stats above are
		// plain values, and the seats that held it die with this call.
		sbSparePool.Put(spare, e)
	}
	return res
}

// sbSparePool recycles finished spellbench games' storage (rules.Spare)
// between the games a worker plays back to back. Which spare a game draws is
// scheduling-dependent but invisible (rules.Spare's contract).
var sbSparePool gbench.SparePool

// sbCorpusMember stamps each record with the recording seat's outcome and
// encodes the game's records as one gzip member. A halted or truncated game's
// records keep outcome_known false.
func sbCorpusMember(recs []policynet.VisitRecord, o gbench.Outcome, err error) ([]byte, int) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	enc := json.NewEncoder(zw)
	for i := range recs {
		r := &recs[i]
		switch {
		case err != nil || gbench.IsAbort(o.StallOn) || o.IsStalled():
		case o.Draw:
			r.Outcome, r.OutcomeKnown = 0.5, true
		case o.WinnerSeat == r.Seat:
			r.Outcome, r.OutcomeKnown = 1, true
		default:
			r.Outcome, r.OutcomeKnown = 0, true
		}
		if e := enc.Encode(r); e != nil {
			panic("botbench: encoding a visit record: " + e.Error()) // plain data; cannot fail
		}
	}
	if e := zw.Close(); e != nil {
		panic("botbench: closing a visit corpus member: " + e.Error())
	}
	return buf.Bytes(), len(recs)
}

// sbWriteCorpus writes the games' corpus members in schedule order to a
// temporary file renamed onto -az-corpus, so the file is a pure function of
// the run's flags and a failed run leaves no partial corpus.
func sbWriteCorpus(path string, results []sbResult) (int, error) {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range results {
		if _, err := f.Write(r.corpus); err != nil {
			f.Close()
			return 0, err
		}
		n += r.visits
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	return n, os.Rename(tmp, path)
}

// sbLedgerRow renders one game as a spellbench-match-ledger/v1 row.
func sbLedgerRow(g sbGame, r sbResult, engine map[string]string, format string) map[string]any {
	seatRows := make([]map[string]any, 2)
	ids := [2]string{}
	for s := 0; s < 2; s++ {
		name := sbDisplayName(g.seats[s])
		ids[s] = sbBotID(name)
		seatRows[s] = map[string]any{"seat": fmt.Sprintf("p%d", s), "bot_id": ids[s], "name": name, "version": "1.0.0"}
	}
	row := map[string]any{
		"schema": sbLedgerSchema, "game_id": g.id, "matchup_index": g.matchup, "pair_index": g.pair,
		"game_index": g.game, "format": format, "game_seed": g.seed, "seats": seatRows,
		"decks":      []map[string]string{{"catalog_id": g.deck}, {"catalog_id": g.deck}},
		"step_count": r.outcome.Intents, "decision_count": r.outcome.Intents, "engine": engine,
		"winner": nil, "winner_bot_id": nil, "adjudication": nil,
	}
	o := r.outcome
	switch {
	case r.err != nil:
		row["outcome"], row["classification"], row["reason"] = "halted", "halted", "engine_error"
		row["adjudication"] = map[string]string{"kind": "engine_halt", "detail": sbTrim(r.err.Error())}
	case gbench.IsAbort(o.StallOn):
		row["outcome"], row["classification"], row["reason"] = "halted", "halted", o.StallOn
		row["adjudication"] = map[string]string{"kind": "engine_halt", "detail": sbTrim(o.Livelock)}
	case o.IsStalled():
		row["outcome"], row["classification"], row["reason"] = "truncated", "truncated", "max_"+o.StallOn
	case o.Draw:
		row["outcome"], row["classification"], row["reason"] = "draw", "natural", "game_over"
	default:
		w := o.WinnerSeat
		row["outcome"], row["classification"], row["reason"] = fmt.Sprintf("p%d_win", w), "natural", "game_over"
		row["winner"], row["winner_bot_id"] = fmt.Sprintf("p%d", w), ids[w]
	}
	return row
}

func sbTrim(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	if s == "" {
		s = "unspecified"
	}
	return s
}

// sbCardPool reads the corpus pin for the ledger's card_pool_identity.
func sbCardPool(dir, catalog string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "cards.lock"))
	if err == nil {
		var lock struct {
			Commit string `json:"commit"`
		}
		if json.Unmarshal(raw, &lock) == nil && lock.Commit != "" {
			return "spellbench-" + catalog + "-catalog/forge@" + lock.Commit
		}
	}
	return "spellbench-" + catalog + "-catalog/forge@unknown"
}

func sbSplit(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// spellbenchExit runs the -spellbench mode and returns the exit code.
func spellbenchExit(o sbOpts, dir string, workers, maxTurns, maxIntents int, checkpoint string, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintln(stderr, "botbench -spellbench:", err)
		return 1
	}
	bots := sbSplit(o.bots)
	if len(bots) < 2 {
		return fail(fmt.Errorf("need at least two policies, got %q", o.bots))
	}
	seen := map[string]bool{}
	azSide := false
	azBases := map[string]bool{}
	for _, b := range bots {
		// Every name is a registry spec ("bot", "bot+passguard"); the
		// error names the registered policies for an unknown base.
		if err := registry.CheckSpec(b); err != nil {
			return fail(err)
		}
		if seen[b] {
			return fail(fmt.Errorf("policy %q listed twice", b))
		}
		seen[b] = true
		if base := basePolicy(b); isAZPolicy(base) {
			azSide = true
			azBases[base] = true
		}
	}
	if azBases["az"] && azBases["az-redeal"] && azWorldArg == "redeal" {
		return fail(fmt.Errorf("az with -az-world redeal and az-redeal are the same policy; list one"))
	}
	for _, f := range []string{o.with, o.without} {
		if f != "" && !seen[f] {
			return fail(fmt.Errorf("-spellbench-with/-without %q is not in the policy list", f))
		}
	}
	type wfile struct {
		path string
		dst  *builtins.TacticalWeights
	}
	wfiles := []wfile{{o.tacticalWeights, &tacticalWeights}}
	for i, pth := range sbSplit(o.tacticalAlt) {
		if i >= len(tacticalAltWeights) {
			return fail(fmt.Errorf("-spellbench-tactical-alt-weights: at most %d files", len(tacticalAltWeights)))
		}
		wfiles = append(wfiles, wfile{pth, &tacticalAltWeights[i]})
	}
	for _, f := range wfiles {
		if f.path == "" {
			continue
		}
		raw, err := os.ReadFile(f.path)
		if err != nil {
			return fail(err)
		}
		w := builtins.DefaultTacticalWeights()
		if err := json.Unmarshal(raw, &w); err != nil {
			return fail(fmt.Errorf("%s: %w", f.path, err))
		}
		*f.dst = w
	}
	if o.pairs < 1 {
		return fail(fmt.Errorf("-spellbench-pairs must be at least 1"))
	}
	if o.mulligans < 0 || o.mulligans > 6 {
		return fail(fmt.Errorf("-spellbench-mulligans must be between 0 and 6"))
	}
	if o.out == "" {
		return fail(fmt.Errorf("-spellbench-out is required"))
	}
	if azCorpusPath != "" {
		if !azSide {
			return fail(fmt.Errorf("-az-corpus needs an az policy"))
		}
		if _, err := os.Stat(azCorpusPath); err == nil {
			return fail(fmt.Errorf("-az-corpus %s already exists", azCorpusPath))
		}
	}
	var ckModel *policynet.Model
	if checkpoint != "" {
		if !azSide {
			return fail(fmt.Errorf("-checkpoint in -spellbench mode feeds az only, and az is not listed"))
		}
		m, err := policynet.LoadCheckpointFile(checkpoint)
		if err != nil {
			return fail(fmt.Errorf("-checkpoint %s: %w", checkpoint, err))
		}
		ckModel = m
	}
	azA, azB := "", ""
	if azBases["az"] {
		azA = "az"
	}
	if azBases["az-redeal"] {
		azB = "az-redeal"
	}
	// The -spellbench az-redeal registry entry passes azNet, so a
	// -checkpoint is applied here and the front door must not refuse it
	// (azSeatsFromSpellbench); the -pairs path is azSeatsFromHosted.
	if err := azFrontDoor(azA, azB, ckModel, azSeatsFromSpellbench); err != nil {
		return fail(err)
	}
	if azSide {
		installAZCostStats()
	}
	sbSearchSide := false
	for _, b := range bots {
		sbSearchSide = sbSearchSide || isSBSearchPolicy(b)
	}
	if sbSearchSide {
		installSBSearchCostStats()
	}
	cat, err := spellbench.CatalogByID(o.catalog)
	if err != nil {
		return fail(err)
	}
	o.catalog, o.format = cat.ID, cat.Format
	pool := cat.Pool
	if o.decks != "" {
		pool = sbSplit(o.decks)
	}
	files := make([]deck.File, len(pool))
	for i, id := range pool {
		f, err := spellbench.File(cat.Dir, id)
		if err != nil {
			return fail(err)
		}
		files[i] = f
	}
	reg, err := openCorpusForDecks(dir, files, stderr)
	if err != nil {
		return fail(fmt.Errorf("opening corpus at %s: %w (run `make fetch-cards compile-cards` first)", dir, err))
	}
	setTacticalRegistry(reg)
	decks := make(map[string][]*cards.Card, len(pool)) // lookup only
	for i, id := range pool {
		d, err := files[i].Resolve(reg)
		if err != nil {
			return fail(err)
		}
		decks[id] = d
	}
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return fail(err)
	}
	if o.trace != "" {
		if err := os.MkdirAll(o.trace, 0o755); err != nil {
			return fail(err)
		}
	}

	full := sbSchedule(bots, pool, o.pairs, o.baseSeed)
	var sched []sbGame
	for _, g := range full {
		has := func(p string) bool { return g.seats[0] == p || g.seats[1] == p }
		if (o.with != "" && !has(o.with)) || (o.without != "" && has(o.without)) {
			continue
		}
		sched = append(sched, g)
	}
	if len(sched) == 0 {
		return fail(fmt.Errorf("no games scheduled"))
	}
	if workers <= 0 {
		workers = 1
	}
	engine := map[string]string{
		"engine_name": "gorge", "engine_version": o.engineVersion,
		"rules_snapshot_id": "gorge/" + o.engineVersion, "card_pool_identity": sbCardPool(dir, cat.ID),
	}

	fmt.Fprintf(stderr, "spellbench: %d policies, %d decks, %d pairs/deck -> %d games (%d scheduled here), %d workers\n",
		len(bots), len(pool), o.pairs, len(full), len(sched), workers)
	results := make([]sbResult, len(sched))
	var done atomic.Int64
	var mu sync.Mutex
	start := time.Now()
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				g := sched[i]
				results[i] = sbPlay(g, decks[g.deck], reg, maxTurns, maxIntents)
				n := done.Add(1)
				r := results[i]
				mu.Lock()
				fmt.Fprintf(stderr, "[%d/%d %s] %s %-8s %s vs %s: %s turns=%d intents=%d %.1fs\n",
					n, len(sched), time.Since(start).Round(time.Second), g.id, g.deck, g.seats[0], g.seats[1],
					sbResultLabel(r), r.outcome.Turns, r.outcome.Intents, r.wall.Seconds())
				mu.Unlock()
			}
		}()
	}
	for i := range sched {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	elapsed := time.Since(start)

	if err := sbWriteOutputs(o, bots, pool, sched, results, engine, elapsed, workers); err != nil {
		return fail(err)
	}
	sbWriteSummary(stdout, bots, sched, results, elapsed)
	if azSide {
		// Both az policies feed one cost report; its game count is the
		// games either seated.
		fmt.Fprint(stdout, azCostReport(sbCountBase(sched, "az")+sbCountBase(sched, "az-redeal")))
	}
	if sbSearchSide {
		var ps []string
		var ns []int
		for _, b := range bots {
			if isSBSearchPolicy(b) {
				ps = append(ps, b)
				ns = append(ns, sbCount(sched, b))
			}
		}
		fmt.Fprint(stdout, sbSearchCostReports(ps, ns))
	}
	if azCorpusPath != "" {
		n, err := sbWriteCorpus(azCorpusPath, results)
		if err != nil {
			return fail(fmt.Errorf("-az-corpus: %w", err))
		}
		fmt.Fprintf(stdout, "az visit corpus %s: %d records\n", azCorpusPath, n)
	}
	return 0
}

func sbCount(sched []sbGame, policy string) int {
	n := 0
	for _, g := range sched {
		if g.seats[0] == policy || g.seats[1] == policy {
			n++
		}
	}
	return n
}

// sbCountBase counts games either seat played a spec whose base policy is
// the given name, so a decorated az spec ("az+passguard") still feeds the
// az cost report.
func sbCountBase(sched []sbGame, policy string) int {
	n := 0
	for _, g := range sched {
		if basePolicy(g.seats[0]) == policy || basePolicy(g.seats[1]) == policy {
			n++
		}
	}
	return n
}

func sbResultLabel(r sbResult) string {
	switch {
	case r.err != nil:
		return "HALTED(" + sbTrim(r.err.Error()) + ")"
	case gbench.IsAbort(r.outcome.StallOn):
		return "HALTED(" + r.outcome.StallOn + ")"
	case r.outcome.IsStalled():
		return "truncated(" + r.outcome.StallOn + ")"
	case r.outcome.Draw:
		return "draw"
	}
	return fmt.Sprintf("p%d wins", r.outcome.WinnerSeat)
}

func sbWriteOutputs(o sbOpts, bots, pool []string, sched []sbGame, results []sbResult, engine map[string]string, elapsed time.Duration, workers int) error {
	lf, err := os.Create(filepath.Join(o.out, "matches.jsonl"))
	if err != nil {
		return err
	}
	defer lf.Close()
	gf, err := os.Create(filepath.Join(o.out, "games.jsonl"))
	if err != nil {
		return err
	}
	defer gf.Close()
	le, ge := json.NewEncoder(lf), json.NewEncoder(gf)
	for i, g := range sched {
		r := results[i]
		if err := le.Encode(sbLedgerRow(g, r, engine, o.format)); err != nil {
			return err
		}
		extra := map[string]any{
			"game_id": g.id, "deck": g.deck, "seed": g.seed, "p0": g.seats[0], "p1": g.seats[1],
			"result": sbResultLabel(r), "turns": r.outcome.Turns, "intents": r.outcome.Intents,
			"wall_ms": r.wall.Milliseconds(), "stall_on": r.outcome.StallOn,
			"fallbacks": r.fallbacks, "first_reject": r.rejects, "pursuit_stats": r.stats,
			"mulligans": map[string][2]int{"asks": r.mullAsks, "taken": r.mullTaken},
		}
		if r.outcome.StarterSet {
			extra["starter"] = r.outcome.Starter
		}
		if err := ge.Encode(extra); err != nil {
			return err
		}
	}
	names := make([]string, len(bots))
	for i, b := range bots {
		names[i] = sbDisplayName(b)
	}
	run := map[string]any{
		"policies": bots, "display_names": names, "catalog": o.catalog, "decks": pool, "pairs_per_deck": o.pairs,
		"base_seed": o.baseSeed, "with": o.with, "without": o.without, "games": len(sched),
		"mulligans":    o.mulligans,
		"wall_seconds": elapsed.Seconds(), "workers": workers, "engine": engine,
		"az":               map[string]any{"sims": azCfg.Search.Sims, "world": azWorldArg, "worlds": azCfg.Worlds},
		"max_turn_intents": maxTurnIntents,
	}
	raw, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(o.out, "run.json"), append(raw, '\n'), 0o644)
}

// sbWriteSummary prints per-matchup W/D/L (bot A = the earlier listed),
// excluded games, fallbacks and wall time.
func sbWriteSummary(w io.Writer, bots []string, sched []sbGame, results []sbResult, elapsed time.Duration) {
	type tally struct {
		games, aw, bw, dr, trunc, halt int
		wall                           time.Duration
		turns                          int64
	}
	order := []string{}
	tallies := map[string]*tally{} // keyed lookups; printed in schedule order
	fallbacks := map[string]int{}
	seatGames := map[string]int{}
	pursuits := map[string]*builtins.Stats{}
	seatIntents := map[string]int64{} // both seats' intents, summed over the policy's games
	for i, g := range sched {
		r := results[i]
		a, b := g.seats[0], g.seats[1]
		if g.game == 1 {
			a, b = b, a
		}
		key := a + " vs " + b
		t := tallies[key]
		if t == nil {
			t = &tally{}
			tallies[key] = t
			order = append(order, key)
		}
		t.games++
		t.wall += r.wall
		t.turns += int64(r.outcome.Turns)
		switch {
		case r.err != nil || gbench.IsAbort(r.outcome.StallOn):
			t.halt++
		case r.outcome.IsStalled():
			t.trunc++
		case r.outcome.Draw:
			t.dr++
		case g.seats[r.outcome.WinnerSeat] == a:
			t.aw++
		default:
			t.bw++
		}
		for s := 0; s < 2; s++ {
			fallbacks[g.seats[s]] += r.fallbacks[s]
			seatGames[g.seats[s]]++
			if pursuits[g.seats[s]] == nil {
				pursuits[g.seats[s]] = &builtins.Stats{}
			}
			pursuits[g.seats[s]].Add(r.stats[s])
			seatIntents[g.seats[s]] += int64(r.outcome.Intents)
		}
	}
	fmt.Fprintf(w, "spellbench workup: %d games in %s wall\n", len(sched), elapsed.Round(time.Second))
	fmt.Fprintf(w, "%-44s %5s %5s %5s %5s %6s %5s %8s %9s\n", "matchup (A vs B)", "games", "A", "B", "draw", "trunc", "halt", "turns", "s/game")
	for _, k := range order {
		t := tallies[k]
		fmt.Fprintf(w, "%-44s %5d %5d %5d %5d %6d %5d %8.1f %9.2f\n", k, t.games, t.aw, t.bw, t.dr, t.trunc, t.halt,
			float64(t.turns)/float64(t.games), t.wall.Seconds()/float64(t.games))
	}
	names := append([]string(nil), bots...)
	sort.Strings(names)
	for _, n := range names {
		if seatGames[n] == 0 {
			continue
		}
		ps := pursuits[n]
		fmt.Fprintf(w, "policy %-22s seat-games %4d  refused-answer fallbacks %4d  pursuits %5d (taps %5d, failed %4d)\n",
			n, seatGames[n], fallbacks[n], ps.Pursuits, ps.PursuitTaps, ps.PursuitFailures)
		sg := float64(seatGames[n])
		fmt.Fprintf(w, "       %-22s game intents/game %7.1f  own decisions/game %7.1f\n", "", float64(seatIntents[n])/sg, float64(ps.Decisions)/sg)
		if ps.Decisions > 0 {
			fmt.Fprintf(w, "       %-22s LEGAL ACTIONS LOST %4d  (recovered %d; pursuit failures priced %d / unpriced %d; proven-unpayable plays skipped %d; refusals %d; autopay fallbacks %d, auto-filled %d, window taps %d)\n", "",
				ps.LostPlays, ps.RecoveredPlays, ps.PursuitFailuresPriced, ps.PursuitFailures-ps.PursuitFailuresPriced,
				ps.ExcludedUnpayable, ps.Refusals, ps.AutoPayFallbacks, ps.AutoFills, ps.WindowTaps)
		}
		if len(ps.PursuitFailuresByVerdict) > 0 {
			var vs []string
			for k := range ps.PursuitFailuresByVerdict {
				vs = append(vs, k)
			}
			sort.Strings(vs)
			var parts []string
			for _, k := range vs {
				parts = append(parts, fmt.Sprintf("%s=%d", k, ps.PursuitFailuresByVerdict[k]))
			}
			fmt.Fprintf(w, "       %-22s pursuit failures by play/verdict: %s\n", "", strings.Join(parts, ", "))
		}
		if ps.Lowerings > 0 {
			var causes []string
			for k := range ps.AbortsByCause {
				causes = append(causes, k)
			}
			sort.Strings(causes)
			var cs []string
			for _, k := range causes {
				cs = append(cs, fmt.Sprintf("%s=%d", k, ps.AbortsByCause[k]))
			}
			fmt.Fprintf(w, "       %-22s lowerings %5d (abilities %d)  cast %5d  abilities %4d  aborted %4d (re-planned %d, passes %4d) [%s]  taps %5d  mana asks %4d  yields %d  stack waits %d\n", "",
				ps.Lowerings, ps.AbilityLowerings, ps.LoweredCasts, ps.LoweredAbilities, ps.Aborts, ps.Replans, ps.AbortPasses, strings.Join(cs, " "),
				ps.LoweringTaps, ps.LoweringAsks, ps.LoweringYields, ps.LoweringWaits)
			for _, a := range ps.AbortSamples {
				fmt.Fprintf(w, "       %-22s   abort: %s\n", "", a)
			}
		}
		if ps.Scripts+ps.ExactProofs+ps.ExactLimited > 0 {
			fmt.Fprintf(w, "       %-22s scripts %4d (exact %d)  played %4d  aborted %3d  steps %5d  exact proofs %d  exact over budget %d\n", "",
				ps.Scripts, ps.ExactScripts, ps.ScriptedPlays, ps.ScriptAborts, ps.ScriptSteps, ps.ExactProofs, ps.ExactLimited)
		}
	}
}
