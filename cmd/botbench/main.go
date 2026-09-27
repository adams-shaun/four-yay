// Command botbench plays N matches between two named bot policies and
// reports the result: wins each way, draws, policy A's win rate with a 95%
// confidence interval, and the mean number of turns per game.
//
// The whole run is reproducible from (base seed, policies, N) alone:
//
//   - game i is seeded from base+i, so no game in a run is a repeat of
//     another (and a run of 500 at a base is a strict superset of a run of
//     100 at the same base);
//   - deck lists are spread equally over the run, so a deck-list advantage
//     (which pile a seat holds) is spread equally over the seats instead of
//     masquerading as a policy advantage; the FIRST TURN is not a seating
//     fact at all since the CR 103.1 toss -- rules.New draws the starting
//     seat per game, so play/draw is spread by the toss and the report's
//     starting-player split (below) is what measures its advantage;
//   - nothing outside those inputs reaches the output -- no wall clock, no
//     map-range order, no global rand.
//
// Wins are attributed by seat, not by policy name: each win is credited to
// whichever side held the winning seat that game. That matters most for the
// baseline run `-a bot -b bot`, where both sides run the same policy and
// every non-draw winner carries the same name -- the seat is the only thing
// that can split the run. The summary therefore always reports the wins per
// seat (for the two-seat case, with a rate and confidence interval) beside
// the A/B split, and marks the A/B line when both sides are the same policy:
// that split is ~50% by construction. The seat split no longer measures a
// play/draw advantage (the toss decides who plays first, not the seat) -- it
// is the deck-list control; the STARTING-PLAYER split below it, taken from
// each game's first TurnChange, is the number that measures play/draw.
//
// `-a bot -b bot` is the same-policy baseline that measures harness bias:
// seat.NewBot is the production policy, and pitting it against itself
// shows how big a run artifact is before any real comparison is read (the
// split is ~50% by construction, and it moves with the seed
// and game count, so no single figure is quoted here); since the toss the
// play/draw figure to read beside it is the starting-player split, not the
// seat split. The head-to-head
// that credits a policy: -a bot -b legacy, where legacy is the pre-B2
// fuzz-driver heuristic in botpolicy.LegacyDecide, with blocker legality
// facts supplied to that arm. Registering a third policy is one entry in the
// policies map. Same names on both sides
// is a valid and expected run.
//
// -pairs switches the bench to a deck-pair matrix. The default run (no
// -pairs) plays one deck list per seat and is unchanged; the defect it
// fixes is that with the repo's 12 decks it always and only played the
// first two sorted names (death-n-taxes vs dimir-tempo). -pairs all plays
// every unordered deck pair (66 for 12 decks at 2 seats) and -pairs
// a:b,c:d plays named pairs, so which deck list a seat holds is varied
// across the run instead of pinned forever to one matchup. Three
// properties make a matrix the same measurement the single-pair run is:
//
//   - -games means games PER PAIR (the header and pooled line both say
//     that), so the historical N=4000 on death-n-taxes:dimir-tempo is the
//     matrix's first row and reproduces it exactly;
//   - seats still trade policies within each pair every game (aPlaysSeat),
//     so a deck list can no more masquerade as a policy advantage in a
//     matrix than it can in the single-pair run; each deck pair is instead
//     played at a distinct seed block (pair p uses base+p*games ..
//     base+(p+1)*games), so the whole matrix is a pure function of (base
//     seed, games per pair, pair list) and pairs are independent and
//     playable in parallel;
//   - pair order is the sorted i<j order over RepoDeckNames(), never map
//     iteration, so a matrix is byte-identical run to run and allows an
//     exact -out json diff between two builds.
//
// -profiles: -cpuprofile and -memprofile capture the run for `go tool
// pprof` without touching the report: -cpuprofile streams a CPU profile
// over the whole run, -memprofile writes one heap profile after the last
// game (pprof reads both alloc_space and inuse_space from that file). A
// profiled run's report is byte-identical to an unprofiled one.
//
// -action-coverage: the completeness counterpart of -decision-stats --
// which decision kinds and option rows were never asked, which offered
// option shapes were never chosen, which cast shapes (kicked/surged/
// flashback/miracle/{X}/plain), cards, ability slots and registered card
// primitives were never exercised at runtime. It is derived from the same
// Decision/Intent observations plus each finished game's event log (a
// complete description of the match), so it changes no engine behaviour;
// the report appends after the run and is deterministic. See
// actioncoverage.go for the honest signal classes (execution / presence /
// none).
//
// -grind: the throughput/profiling mode -- one repo deck pinned to one
// goroutine playing itself until a wall-clock (-grind-seconds) or iteration
// (-grind-iters) budget runs out; "all" grinds every deck in the format's
// pool, one goroutine each. Deliberately NOT the -pairs worker-pool shape:
// pool scheduling mixes every deck's work into one profile, while a pinned
// goroutine per deck gives each deck its own measurement window (same
// wall-clock budget, not the same iteration count -- a fixed iteration cap
// gives a slow deck proportionally more profile samples). Every game's
// outcome is still seed-pure; only the iteration count and iters/sec column
// read the clock. See grind.go.
//
// The report is a per-pair table (pair, A/B wins, A win rate with its 95%
// CI, the seat split with its CI, mean turns) plus a pooled line over all
// pairs with its own CI and the count of pairs whose A-win interval
// excludes 50% in each direction -- the honest summary a single pooled
// percentage hides. Both reports end with the starting-player split (each
// seat's starts and wins when it played first, from the first TurnChange),
// the play/draw measurement the seat split replaced.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/host"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// policies registers every bot policy the bench can pit against another,
// keyed by the -a/-b name. Each entry constructs a fresh Seat for one seat
// of one game from that seat's per-game seed, so a policy's decisions are a
// pure function of (base seed, game index, seat) and nothing else. The map
// is read by name only, never ranged over in the reporting path, so its
// iteration order can never reach the output.
var policies = map[string]func(seed uint64) seat.Seat{
	// seat.NewBot's *Bot result is wrapped because Go has no return-type
	// covariance: a func returning *Bot is not assignable to one returning
	// seat.Seat, and the wrapper keeps a future policy free to return any
	// Seat implementation.
	"bot": hostedPolicy(host.BotPolicy),
	// bot-auto-pay is the hosted default policy with the payment-plan
	// adapter enabled.  It is deliberately a separate bench name: comparing
	// it with "bot" measures the distribution change from atomic mana
	// payment without pretending the existing cast ranker was retrained.
	"bot-auto-pay": func(seed uint64) seat.Seat {
		return seat.NewBot(seed).EnableAutoPayMana()
	},
	"lethal-pressure": hostedPolicy(host.LethalPressurePolicy),
	// ar8 is the combined-attacker lethal-pressure experiment (AR7's
	// per-attacker test plus the attacking-SET subset search). It is
	// deliberately NOT a hosted policy -- host.NormalizeBotPolicy does not
	// know the name -- so it can only be benched here, never hosted.
	"ar8": func(seed uint64) seat.Seat {
		return seat.NewCombinedLethalBot(seed)
	},
	// blocks is the BLK whole-assignment defender experiment (B0-B3): the
	// production bot with KBlockers answered by the whole-assignment
	// heuristic instead of the per-blocker one. Deliberately NOT a hosted
	// policy -- host.NormalizeBotPolicy does not know the name -- so it can
	// only be benched here, never hosted.
	"blocks": func(seed uint64) seat.Seat {
		return seat.NewBlocksBot(seed)
	},
	// cast-profile plays the production bot with a learned cast profile
	// (botpolicy.CastWeights): with the embedded default profile it is
	// intent-identical to "bot" (the baseline equality the L2 tests pin),
	// and -profile <path> swaps the weights for a candidate file so a
	// profile is benched without a rebuild.
	"cast-profile": func(seed uint64) seat.Seat {
		w, err := castProfileWeightsForRun()
		if err != nil {
			panic(err) // the embedded default profile is pinned valid by botpolicy's tests.
		}
		return seat.NewCastProfileBotWithWeights(seed, w)
	},
	// legacy is the benchmark's historical heuristic, with blocker legality
	// facts supplied by the shared view adapter. It is not a production
	// policy -- nothing but the bench drives it.
	"legacy": func(seed uint64) seat.Seat {
		return &legacySeat{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
	},
	// policynet is the L9c learned per-option policy (internal/policynet's
	// trained scorer over the search-teacher label corpus): it answers the
	// two kinds the teacher labelled (attackers, priority cast choice) and
	// delegates everything else to the default bot (seat.PolicyNetBot's own
	// delegation). Deliberately NOT a hosted policy -- host.NormalizeBotPolicy
	// does not know the name -- so it can only be benched here, never hosted.
	// The weights come from -checkpoint (no embedded checkpoint: this task
	// commits no trained weights), loaded once before any game starts; the
	// Model is read-only after load, so every seat of every game wraps it in
	// its own single-threaded Scorer and no two games share scratch.
	"policynet": func(seed uint64) seat.Seat {
		if policynetModel == nil {
			panic("botbench: policy policynet needs -checkpoint <path>")
		}
		b := seat.NewPolicyNetBotKinds(seed, policynet.NewScorer(policynetModel), policynetKinds)
		if err := b.SetAdmission(policynetAdmission); err != nil {
			panic("botbench: " + err.Error()) // validated by mainExit before any game
		}
		// -policynet-temperature (ticket pn14): collection-only sampling,
		// seeded from this seat's per-game seed.
		b.SetSampling(policynetTemperature, seed)
		return b
	},
	// explore is the coverage-exploration bot (seat.NewExploreBot,
	// botpolicy.ExploreDecide): the production bot with uniform picks among
	// sibling abilities and early-game ability detours. Bench-only; ticket
	// pn14 mixes it in as a collection opponent (-opp-mix explore:0.3).
	"explore": func(seed uint64) seat.Seat {
		return seat.NewExploreBot(seed)
	},
	// search is the PIMC search teacher as a playable seat
	// (searchseat.SearchBot): the default bot wrapped with the teacher
	// answering the covered kinds (attackers, priority cast with >=2 distinct
	// castable objects) and delegating everything else -- Choose returns the
	// bot's own intent on every failure path, so an unsearchable decision is
	// the bot's decision, never a dropped one. The knobs are the -search-*
	// flags over searchseat.Defaults() (the teacher's measured values); the
	// observation feed the teacher's sampler needs is maintained by the
	// driver (internal/bench.PlayGame's search-seat branch, the seat.
	// BoardSeat idiom one level up) and the seat never reads hidden state
	// beyond what the actor-scoped Collector exposes. Deliberately NOT a
	// hosted policy, like policynet before it: a live table has no driver
	// to feed the seat a history yet.
	"search": func(seed uint64) seat.Seat {
		return searchseat.NewSearchBot(seed, searchKnobs)
	},
}

func hostedPolicy(name string) func(seed uint64) seat.Seat {
	return func(seed uint64) seat.Seat {
		s, err := host.NewBotPolicySeat(name, seed)
		if err != nil {
			panic(err) // constants above are the closed hosted-policy vocabulary.
		}
		return s
	}
}

// castProfileOverride is the weights -profile names: nil (the zero value)
// means the embedded default profile, and mainExit sets it once after
// parsing the file. Package scope rather than a run parameter so the
// policies map's cast-profile entry can read it without threading a
// weights argument through run/runMatrix/playMatch; it is write-once
// before any game starts and read-only afterwards.
var castProfileOverride *botpolicy.CastWeights

// policynetModel is the trained model -checkpoint names, loaded once by
// mainExit before any game starts (nil = no checkpoint given). Same
// write-once/read-only package-scope pattern as castProfileOverride: the
// policies map's policynet entry reads it, and the Model itself is treated
// as immutable by every seat built over it.
var policynetModel *policynet.Model

// policynetAdmission is the -policynet-admission value (seat.AdmissionAuto by
// default), validated by mainExit and applied to every policynet seat.
var policynetAdmission = seat.AdmissionAuto

// policynetTemperature is -policynet-temperature: 0 (the default) is the
// deterministic argmax/vote seat; > 0 samples every scored answer at that
// temperature (seat.PolicyNetBot.SetSampling; ticket pn14, collection only).
var policynetTemperature float64

// oppMixArg / oppMix are -opp-mix (ticket pn14): per game, with the listed
// probabilities, side B's seat is replaced by another built-in policy (the
// draw is a pure function of the game seed). The report still names side B;
// the mix only changes who sits there.
var (
	oppMixArg string
	oppMix    []oppMixEntry
)

type oppMixEntry struct {
	name string
	frac float64
}

// parseOppMix parses "name:frac[,name:frac]" (fractions in (0,1], summing to
// at most 1; names must be built-in policies).
func parseOppMix(spec string) ([]oppMixEntry, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	var out []oppMixEntry
	total := 0.0
	for _, part := range strings.Split(spec, ",") {
		name, fs, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			return nil, fmt.Errorf("-opp-mix entry %q: want name:fraction", part)
		}
		f, err := strconv.ParseFloat(fs, 64)
		if err != nil || f <= 0 || f > 1 {
			return nil, fmt.Errorf("-opp-mix entry %q: fraction must be in (0,1]", part)
		}
		if _, ok := policies[name]; !ok || name == "policynet" || name == "search" {
			return nil, fmt.Errorf("-opp-mix entry %q: %q is not a mixable built-in policy", part, name)
		}
		total += f
		out = append(out, oppMixEntry{name: name, frac: f})
	}
	if total > 1+1e-9 {
		return nil, fmt.Errorf("-opp-mix fractions sum to %g > 1", total)
	}
	return out, nil
}

// oppMixPick returns the mixed-in policy for the game with this seed, or ""
// to keep side B's own policy: one splitmix64 draw of the seed, compared
// against the cumulative fractions in listed order.
func oppMixPick(seed uint64) string {
	if len(oppMix) == 0 {
		return ""
	}
	z := seed ^ 0x6f70702d6d697821
	z += 0x9e3779b97f4a7c15
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	u := float64(z>>11) / (1 << 53)
	acc := 0.0
	for _, e := range oppMix {
		acc += e.frac
		if u < acc {
			return e.name
		}
	}
	return ""
}

// policynetKindsArg / policynetKindsGiven are the raw -policynet-kinds value
// and whether the flag was given at all (main sets both after flag.Parse);
// mainExit validates them into policynetKinds before any game starts. The
// default is attackers only -- seat.NewPolicyNetBot's own selection, so a
// run that never names the flag benches exactly the default seat.
var (
	policynetKindsArg   = "attackers"
	policynetKindsGiven bool
	policynetKinds      = []decision.Kind{decision.KAttackers}
)

// castProfileWeightsForRun resolves the run's cast-profile weights: the
// -profile file when one was given, else the embedded default profile.
// LoadCastProfile for the default is infallible for a committed file, but
// the error is propagated anyway so the caller decides how loudly to fail.
func castProfileWeightsForRun() (botpolicy.CastWeights, error) {
	if castProfileOverride != nil {
		return *castProfileOverride, nil
	}
	return botpolicy.LoadCastProfile(botpolicy.DefaultCastProfileName)
}

// legacySeat runs the benchmark's historical heuristic with the shared
// view-to-board adapter and a seed-deterministic RNG.
type legacySeat struct {
	r *rand.Rand
}

func (s *legacySeat) Decide(_ context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	in := botpolicy.LegacyDecide(seat.BoardFromView(v), &d, s.r)
	return in, nil
}

// gameSeed returns the seed game i of a run at base seed b plays. The
// engine's log and rng and every seat bot's PCG all derive from this one
// number, seeded base+i -- so the run as a whole is reproducible from
// (base, policies, N) and the first k games of any run are exactly the run
// at the same base with N=k. The "same game N times" failure mode the bench
// exists to rule out is literally this function returning a constant.
func gameSeed(base uint64, game int) uint64 { return base + uint64(game) }

// decisionStatsEnabled is switched on by the -decision-stats flag when main
// starts a run. It lives at package scope (not as a run/runMatrix parameter)
// so the dozens of test call sites that exercise the normal path keep their
// current signature and behaviour: when disabled, no collector is created,
// nothing is recorded and nothing is appended, so the normal report is
// byte-identical to a pre-flag build. main sets it once; it is read-only
// thereafter.
var decisionStatsEnabled bool

// actionCoverageEnabled is switched on by the -action-coverage flag when
// main starts a run, the same package-scope pattern as decisionStatsEnabled:
// when disabled no collector is created, nothing is recorded and nothing is
// appended, so the normal report is byte-identical to a pre-flag build.
var actionCoverageEnabled bool

// aPlaysSeat reports whether policy A (the -a side) holds seat s in game i.
// A holds a seat when (game+seat) is even: with two seats the assignment
// flips every game, and for any seat count a seat sees A in exactly half
// the games of an even run -- the property TestSeatAssignmentAlternates
// pins. Every (policy, seat) pair therefore plays the same number of games,
// so a deck-list advantage cannot masquerade as a policy advantage (the
// first turn is no longer a seating fact -- the CR 103.1 toss draws it per
// game, which is why the report carries the starting-player split) regardless
// of how many seats the run uses. This function is the single source of
// truth for both the seat assignment AND the win attribution: the tally
// credits each win to the side aPlaysSeat says held the winning seat, so
// the two cannot drift apart.
func aPlaysSeat(game, seat int) bool { return gbench.PlaysSeat(game, seat) }

// builtinPolicyNames is the sorted built-in policy vocabulary, for an error
// message. Sorted because the message reaches an operator and a map range
// would reorder it between runs.
func builtinPolicyNames() []string {
	out := make([]string, 0, len(policies))
	for k := range policies {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// seatCtorForDeck resolves ONE SEAT's policy name against the deck that seat
// plays, which is what makes a policy a property of the deck rather than of
// the bench invocation.
//
// A built-in name always wins. That ordering is deliberate and not merely
// convenient: every existing invocation (-a bot -b policynet, the ten
// approved mono pairs, the acceptance runs) must keep resolving to exactly
// the seat it resolves to today, or the measured baselines stop comparing.
// deckPolicyShadowCheck rejects a deck that tries to redefine a built-in
// name, so the precedence can never silently discard a deck author's policy.
//
// Anything else must be a policy the SEAT'S OWN deck declares. A missing name
// is a hard error (deck.File.Policy builds it, listing what the deck does
// have) rather than a fall back to the default bot: a silent fallback would
// make side A identical to side B for exactly the decks missing the policy
// under test, and the run would report "no effect" when the truth is "the
// policy was never loaded".
func seatCtorForDeck(name string, f deck.File) (func(seed uint64) seat.Seat, error) {
	if c, ok := policies[name]; ok {
		return c, nil
	}
	raw, err := f.Policy(name)
	if err != nil {
		return nil, fmt.Errorf("%w; built-in policies: %s", err, strings.Join(builtinPolicyNames(), ", "))
	}
	ps, err := botpolicy.ParseDeckPolicy(raw)
	if err != nil {
		return nil, fmt.Errorf("deck %q policy %q: %w", f.Name, name, err)
	}
	if !ps.CastSet {
		// The document parsed, but every class it sets is one this build
		// cannot read. Saying so is the point: silently seating the default
		// bot would look like the policy ran.
		return nil, fmt.Errorf("deck %q policy %q sets no decision class this build reads (unread classes: %s)",
			f.Name, name, strings.Join(ps.UnknownClasses, ", "))
	}
	w := ps.Cast
	return func(seed uint64) seat.Seat { return seat.NewCastProfileBotWithWeights(seed, w) }, nil
}

// deckPolicyShadowCheck rejects a deck whose declared policy name collides
// with a built-in. Built-ins take precedence (see seatCtorForDeck), so
// without this check the deck's policy would be silently unreachable -- the
// failure mode where a bench run looks like it exercised a tuned profile and
// actually re-ran the stock bot.
func deckPolicyShadowCheck(f deck.File) error {
	for _, name := range f.PolicyNames() {
		if _, ok := policies[name]; ok {
			return fmt.Errorf("deck %q declares a policy named %q, which is a built-in policy name; rename it (built-ins take precedence and the deck's would never be selected)", f.Name, name)
		}
	}
	return nil
}

// gameOutcome is one bench game's result: the policy that won and the seat
// it won from, how many turns the game lasted (state.Game.Turn at the
// end), and how many intents the seats answered. The seat is what the tally
// attributes by: each win is credited to whichever side held the winning
// seat that game (aPlaysSeat), so the split is correct whether or not the
// two policy names collide. With aName == bName the seat is the only thing
// that can tell the sides apart -- with both names "bot", the A/B split
// would read as "both sides won everything" without it. Intents and turns
// are the two numbers every later bot task reads: an intent count reaching
// the -max-intents cap is a policy that stopped terminating, and mean turns
// is the metric the report averages. winnerSeat is only meaningful when
// winner is non-empty.
type gameOutcome struct {
	winner     string // policy name of the winning seat; "" for a draw
	winnerSeat int    // the seat the winner sat in (valid when winner != "")
	turns      int32
	intents    int
	// starter is the seat that took the first turn, from the game log's
	// first TurnChange (the CR 103.1 toss winner, resolved over the
	// survivors) -- the seat the starting-player split attributes by.
	// starterSet distinguishes a real seat 0 from an outcome source (notably
	// an older synthetic test player) that supplied no starter at all; an int
	// zero value cannot do that because seat 0 is real.
	starter    int
	starterSet bool
	// stallOn names the watchdog cap that ended the game before it could
	// finish, distinguishing the three failure modes a reader must tell
	// apart: "turns" (the turn watchdog fired at -max-turns -- the game ran
	// long), "intents" (the intent watchdog fired at -max-intents -- the
	// turn number stopped advancing while intents kept coming) and
	// "livelock" (the engine's own livelock watcher fired inside
	// Submit/Advance -- the event stream was provably non-terminating; see
	// rules/livelock.go). "" means the game is not stalled. A stalled game
	// is a distinct outcome -- NOT a win and NOT a draw -- excluded from the
	// win-rate denominator, so a runner cannot mistake the rate of whatever
	// happened to terminate for an honest win rate.
	stallOn string
	// livelock carries the watcher's diagnostic for a "livelock" stall: the
	// repeating event shape, its object, the repeat count and cycle length
	// (or the runaway span). "" for every other outcome. Recorded against
	// the deck pair and seed so a batch with one stuck game still reports
	// the rest -- and the run exits non-zero at the end, because a livelock
	// is an engine bug, not a slow game.
	livelock string
}

// isStalled reports whether the game was ended by either watchdog cap.
func (o gameOutcome) isStalled() bool { return o.stallOn != "" }

// playMatch plays one game between the given per-seat seats to completion, or
// ends it as a stall at whichever watchdog cap fires first. pols is the
// policy name sitting at each seat, used only to map the winner's seat back
// to a policy. The seats each own their RNG (seeded by the caller), the
// engine replays from its own Config.Seed, and nothing reads the wall clock,
// so the outcome is a pure function of the inputs.
//
// The two caps catch two different pathologies and are both harneess props,
// not engine rules:
//   - maxTurns (the -max-turns flag): a game that runs long in turn count.
//     0 means no cap. It cannot catch a frozen-turn loop, because the turn
//     number never advances to it.
//   - maxIntents (the -max-intents flag): a game whose turn number stops
//     advancing but that keeps submitting intents forever (the re-arming a
//     {0} Equip onto its own target loop). 0 means no cap.
//
// Reaching either cap is a stalled outcome, never an error: one hung game
// records a stall and the rest of the run keeps going instead of aborting
// the whole matrix. The engine's own livelock watcher (rules/livelock.go) is
// a third stall cause ("livelock"): a game whose event stream provably never
// terminates is aborted inside Submit and recorded the same way, with the
// watcher's diagnostic carried in the outcome -- and the fold-level reporters
// fail the run at the end, because a livelock is an engine bug, not a slow
// game.
func playMatch(cfg rules.Config, pols []string, seats []seat.Seat, maxTurns, maxIntents int, collect *decisionStats, cov *actionCoverage) (gameOutcome, error) {
	return playMatchTraced(cfg, pols, seats, maxTurns, maxIntents, collect, cov, nil, traceDecisionMeta{})
}

func playMatchTraced(cfg rules.Config, pols []string, seats []seat.Seat, maxTurns, maxIntents int, collect *decisionStats, cov *actionCoverage, trace *gameTrace, meta traceDecisionMeta) (gameOutcome, error) {
	if collect != nil {
		collect.game()
	}
	if cov != nil {
		cov.game()
	}
	spare, _ := sparePool.Get().(*rules.Spare)
	if spare == nil {
		spare = new(rules.Spare)
	}
	cfg.Spare = spare
	o, e, err := playMatchOnceTraced(cfg, pols, seats, maxTurns, maxIntents, collect, cov, trace, meta)
	if err == nil && cov != nil {
		// A finished game (win, draw OR stall) attributes its runtime
		// exercise from the log; a game that ERRORED has no trustworthy log
		// shape to walk, so it is skipped.
		cov.walkGame(cfg, e)
	}
	if err == nil && trace != nil {
		trace.finish(o, meta)
	}
	if err == nil && e != nil {
		// This is the finished engine's last use: its seats, trace and
		// coverage walk are done with it, so its log and object arrays go
		// back for the next game (rules.Spare -- reuse never changes a game).
		*spare = e.Release()
		sparePool.Put(spare)
	}
	return o, err
}

// sparePool recycles finished games' log and object arrays (rules.Spare)
// between the games a worker plays back to back. Which spare a game draws
// is scheduling-dependent, but invisible (rules.Spare's contract), so the
// run's output is byte-identical with or without it.
var sparePool sync.Pool

// playMatchOnce is playMatch's game loop; it returns the engine so the
// action-coverage walk can read the finished log and state.
func playMatchOnce(cfg rules.Config, pols []string, seats []seat.Seat, maxTurns, maxIntents int, collect *decisionStats, cov *actionCoverage) (gameOutcome, *rules.Engine, error) {
	return playMatchOnceTraced(cfg, pols, seats, maxTurns, maxIntents, collect, cov, nil, traceDecisionMeta{})
}

// playMatchOnceTraced is the opt-in observational form of playMatchOnce.
// trace is game-local, so concurrent workers never share or write it. The
// ordinary wrapper passes nil and retains its pre-trace allocation and output
// path.
//
// The engine drive loop itself lives in internal/bench (bench.PlayGame), the
// SAME loop cmd/policytune runs: this function only translates the optional
// observers (decision stats, action coverage, decision trace) into bench's
// Hooks and the policy-name-free bench.Outcome back into a gameOutcome. There
// is no second copy of the watchdog/livelock loop to keep in step.
func playMatchOnceTraced(cfg rules.Config, pols []string, seats []seat.Seat, maxTurns, maxIntents int, collect *decisionStats, cov *actionCoverage, trace *gameTrace, meta traceDecisionMeta) (gameOutcome, *rules.Engine, error) {
	hooks := gbench.Hooks{NeedBoard: trace != nil}
	if collect != nil || cov != nil || trace != nil {
		// game() already ran in playMatchTraced (this function's only
		// collector-carrying caller) before the loop started, so the hook
		// records decisions only; a Start callback here would double-count the
		// game in every collector's header.
		hooks.Decision = func(seatIdx int, d *decision.Decision, in decision.Intent, brd *botpolicy.Board) error {
			if collect != nil {
				collect.record(d, in)
			}
			if cov != nil {
				cov.record(d, in)
			}
			if trace != nil {
				decisionMeta := meta
				decisionMeta.Policy = pols[seatIdx]
				if err := trace.record(d, in, brd, decisionMeta); err != nil {
					return fmt.Errorf("seed %d: recording decision trace: %w", cfg.Seed, err)
				}
			}
			return nil
		}
	}
	o, e, err := gbench.PlayGame(cfg, seats, maxTurns, maxIntents, hooks)
	if err != nil {
		return gameOutcome{}, e, err
	}
	return gameOutcomeFrom(o, pols), e, nil
}

// gameOutcomeFrom converts bench's policy-name-free Outcome back into the
// bench command's gameOutcome, resolving the winning seat to the policy name
// sitting there. Ruling P14: Draw must be read before any winner, and a
// stalled game has neither.
func gameOutcomeFrom(o gbench.Outcome, pols []string) gameOutcome {
	g := gameOutcome{
		turns:      o.Turns,
		intents:    o.Intents,
		stallOn:    o.StallOn,
		livelock:   o.Livelock,
		starter:    o.Starter,
		starterSet: o.StarterSet,
	}
	if !o.Draw && !o.IsStalled() {
		g.winner = pols[o.WinnerSeat]
		g.winnerSeat = o.WinnerSeat
	}
	return g
}

// stallNotice is the loud, unmistakable summary line a run with any stalled
// games prints beside its win rates. It exists so a run that threw games
// away to either watchdog cap cannot be mistaken for a clean run -- the very
// mistake the win-rate-over-non-stalled-games change exists to prevent. It
// names the two caps separately because they mean different things: a game
// that hit -max-turns ran long, while one that hit -max-intents froze its
// turn count -- a reader who cannot tell them apart cannot act on either. A
// run with no stalls prints nothing, so the constructed default report is
// byte-identical to a run that never saw a watchdog.
func stallNotice(turnStalls, intentStalls, livelocks, eff int) string {
	if turnStalls == 0 && intentStalls == 0 && livelocks == 0 {
		return ""
	}
	s := fmt.Sprintf("\n@@ STALLED: %d game(s) hit the -max-turns cap, %d hit the -max-intents cap", turnStalls, intentStalls)
	if livelocks > 0 {
		s += fmt.Sprintf(", %d aborted with a LIVELOCK or engine PANIC (engine bug; the diagnostic names the repeating event shape or the panic and its stack)", livelocks)
	}
	return s + fmt.Sprintf("; win rates are over %d non-stalled game(s) @@\n", eff)
}

// ci95 returns the 95% confidence interval on a success rate using the
// normal approximation to the binomial (the Wald interval):
//
//	p̂ ± z·√(p̂(1−p̂)/n)   with z = 1.96
//
// clamped to [0, 1]. It is explicitly an approximation: accurate in the
// large-sample centre of the binomial, it degrades at the edges -- p̂ of 0
// or 1 collapses the interval to a point, a known Wald pathology the report
// defends against by always printing the game count beside the interval,
// which is what actually makes a thin sample look thin. This is the
// simplest interval that widens honestly as n shrinks, and the width is
// exactly what TestTheIntervalWidensOnASmallSample pins.
func ci95(successes, trials int) (lo, hi float64) {
	if trials < 1 {
		return 0, 0
	}
	p := float64(successes) / float64(trials)
	se := math.Sqrt(p * (1 - p) / float64(trials))
	lo = p - 1.96*se
	hi = p + 1.96*se
	if lo < 0 {
		lo = 0
	}
	if hi > 1 {
		hi = 1
	}
	return lo, hi
}

// seatLabels renders the per-game seat map ("A@0 B@1") in seat order. It is
// built from the per-seat slice -- the seat-to-policy assignment lives in
// that slice only, never in a map that could range in some other order.
func seatLabels(pols []string) string {
	parts := make([]string, len(pols))
	for i, p := range pols {
		parts[i] = fmt.Sprintf("%s@%d", p, i)
	}
	return strings.Join(parts, " ")
}

// winnerLabel renders the per-game winner: "policy@seat" for a real win,
// "draw" for CR 104.4a's no-surviving-seats ending. The seat is why the
// per-game lines can show a same-policy run's seat bias at all -- with both
// policies named "bot" the raw name alone is identical on both sides.
func winnerLabel(o gameOutcome) string {
	if o.isStalled() {
		if o.stallOn == "livelock" {
			return "LIVELOCK"
		}
		if o.stallOn == "panic" {
			return "PANIC"
		}
		return "stalled"
	}
	if o.winner == "" {
		return "draw"
	}
	return fmt.Sprintf("%s@%d", o.winner, o.winnerSeat)
}

// polsFor returns the policy name sitting at each seat of game g: B sits
// everywhere and A takes the seats aPlaysSeat gives it, so seats trade
// policies every game. It is the single builder both the single-pair bench
// and the matrix pair worker use, so the two can never disagree about which
// policy a seat holds in a given game.
func polsFor(g, seats int, aName, bName string) []string {
	return gbench.PolsFor(g, seats, aName, bName)
}

// matchPlayer plays one game of a bench run: given a game's seed and the
// policy name sitting at each seat, it returns that game's outcome. run
// wires the real engine into this shape (playMatch with per-seat bots);
// tests inject synthetic players to pin the attribution and the report
// without paying an engine game each.
type matchPlayer func(seed uint64, pols []string) (gameOutcome, error)

// outcomeKind is the shared classification of one finished bench game,
// produced by classifyOutcome and applied by both the single-pair fold and
// the matrix pair fold, so the two tallies can never drift apart. A real win
// credits whichever side held the winning seat in that game (aPlaysSeat),
// which is the one source of truth for both the seat assignment and the win
// attribution.
type outcomeKind struct {
	stall      bool
	stallOn    string
	draw       bool
	winnerSeat int
	aWin       bool
	bWin       bool
	starter    int
	starterSet bool
}

// classifyOutcome splits one gameOutcome into the classification the singles
// and matrix folds share. The game index g is needed only because attribution
// reads it; seats bounds the winner-seat check. It returns a BARE
// winner-seat-out-of-range error that the callers wrap with their own
// "game N:" / "pair P, game N:" context, so the folded error messages stay
// exactly as they were before this helper existed.
func classifyOutcome(g int, oc gameOutcome, seats int) (outcomeKind, error) {
	var k outcomeKind
	if oc.starterSet {
		if oc.starter < 0 || oc.starter >= seats {
			return outcomeKind{}, fmt.Errorf("starter seat %d out of range [0,%d)", oc.starter, seats)
		}
		k.starter, k.starterSet = oc.starter, true
	}
	switch {
	case oc.isStalled():
		k.stall = true
		k.stallOn = oc.stallOn
	case oc.winner == "":
		k.draw = true
	default:
		if oc.winnerSeat < 0 || oc.winnerSeat >= seats {
			return outcomeKind{}, fmt.Errorf("winner seat %d out of range [0,%d)", oc.winnerSeat, seats)
		}
		k.winnerSeat = oc.winnerSeat
		if aPlaysSeat(g, oc.winnerSeat) {
			k.aWin = true
		} else {
			k.bWin = true
		}
	}
	return k, nil
}

// bench is the testable entry of the bench: it validates `games`/`seats`,
// plays `games` matches using the default worker budget (all cores), and
// writes the per-game lines and the summary to out. Every match is seeded
// from base+game (gameSeed) and the seat assignment alternates (aPlaysSeat),
// so the output is byte-identical across runs with the same
// (base, policies, games). run() resolves the corpus and decks, wires the
// real engine in, and passes a caller-sized -workers budget through
// benchWithPool; the tests drive bench directly with a synthetic
// matchPlayer, exactly like the tests of cmd/mtgsim drive its run. The
// parallel body and the full semantics of the fold live on benchWithPool.
//
// Attribution is by seat, not by name: with distinct policies the two agree
// (pols[seat] is the policy at that seat), and with aName == bName the seat
// is the only thing that can split the run. aPlaysSeat decides both the
// assignment and the attribution, so they cannot drift apart.
//
// bench never touches a *state.Game or a *rules.Engine field: it drives
// engines the way hosts and sims do -- view.Project, Seat.Decide,
// Engine.Submit -- so a bench run exercises the same seat-facing path a
// real match does, and the engine and seat packages stay untouched by this
// command.
func bench(baseSeed uint64, games, seats int, aName, bName string, play matchPlayer, out io.Writer) error {
	pool := newGamePool(runtime.NumCPU())
	defer pool.close()
	return benchWithPool(baseSeed, games, seats, aName, bName, play, out, pool)
}

// benchWithPool is bench's parallel body. It plays every game into a
// per-game slot across `pool` (games are seeded base+i, so each is
// independent exactly as the matrix's are), waits for all of them, then
// folds the slots in ascending game order. The fold is the only place that
// mutates the running tally or writes a per-game line, so the output and the
// first error are identical to the old sequential loop regardless of
// completion order: the per-game lines stay in ascending game order, and the
// first error returned is the LOWEST failing game index, not whichever
// goroutine happened to finish first.
func benchWithPool(baseSeed uint64, games, seats int, aName, bName string, play matchPlayer, out io.Writer, pool *gamePool) error {
	if games < 1 {
		return fmt.Errorf("-games must be at least 1, got %d", games)
	}
	if seats < 2 {
		return fmt.Errorf("-seats must be at least 2 (a bench pits two policies), got %d", seats)
	}

	slots := make([]gameResult, games)
	var done sync.WaitGroup
	done.Add(games)
	for g := 0; g < games; g++ {
		g := g
		s := gameSeed(baseSeed, g)
		pols := polsFor(g, seats, aName, bName)
		pool.submit(func() {
			defer done.Done()
			slots[g].outcome, slots[g].err = play(s, pols)
		})
	}
	done.Wait()

	var aWins, bWins, draws, stallTurns, stallIntents, livelocks int
	seatWins := make([]int, seats)
	starts := make([]int, seats)
	startWins := make([]int, seats)
	anyStarts := false
	var totalTurns int64
	for g, result := range slots {
		if result.err != nil {
			// All games have completed, so the first error picked from the
			// ordered slots is the sequential loop's lowest failing game
			// index, not whichever game finished first.
			fmt.Fprintf(out, "game %d: %v\n", g, result.err)
			return result.err
		}
		oc := result.outcome
		s := gameSeed(baseSeed, g)
		pols := polsFor(g, seats, aName, bName)
		// The per-game line carries the seed next to the result, so a run
		// that ever played the same game twice would show it -- the seed
		// column stops grinding forward. That is the symptom the bench
		// exists to make impossible, and it is visible in the report if it
		// ever regresses.
		fmt.Fprintf(out, "game %d: seed %d, %s, %6d intents, %3d turns, winner=%s\n",
			g, s, seatLabels(pols), oc.intents, oc.turns, winnerLabel(oc))
		if oc.livelock != "" {
			// The structured record of WHY the game was aborted: the repeating
			// event shape, its object, the repeat count and cycle length. A
			// parseable field on the line above (winner=LIVELOCK) points here.
			fmt.Fprintf(out, "  game %d: LIVELOCK: %s\n", g, oc.livelock)
		}
		kind, err := classifyOutcome(g, oc, seats)
		if err != nil {
			return fmt.Errorf("game %d: %w", g, err)
		}
		switch {
		case kind.stall:
			// A stalled game is neither a win nor a draw: it credits no seat
			// and is excluded from the win-rate denominator below. The three
			// caps are tallied apart so the stall notice can say which
			// pathology ended the game; a livelock additionally fails the run
			// at the end, because it is an engine bug, not a slow game.
			if kind.stallOn == "intents" {
				stallIntents++
			} else if gbench.IsAbort(kind.stallOn) {
				livelocks++
			} else {
				stallTurns++
			}
		case kind.draw:
			draws++
		default:
			// A real win: credit the side that held the winning seat this
			// game. classifyOutcome resolved attribution via aPlaysSeat, the
			// same predicate that assigned the seats, so the tally agrees
			// with the assignment by construction.
			seatWins[kind.winnerSeat]++
			if kind.aWin {
				aWins++
			} else {
				bWins++
			}
		}
		// The starting-player tally, over every game that reached turn 1
		// (stalled games with a starter still count as starts; a genesis
		// stall and an older synthetic outcome leave starterSet false). A draw
		// counts as a start with no win, like the seat tally.
		if kind.starterSet {
			anyStarts = true
			starts[kind.starter]++
			if kind.winnerSeat == kind.starter && !kind.stall && !kind.draw {
				startWins[kind.starter]++
			}
		}
		totalTurns += int64(oc.turns)
	}

	// The win-rate denominator excludes stalled games: a rate over "whatever
	// happened to terminate" is a biased sample, which is the exact defect
	// the turn watchdog exists to expose. With no stalls this is `games`, so
	// the constructed default report stays byte-identical to today.
	// A livelock/panic abort is a stall too (classifyOutcome credits it to no
	// seat), so it leaves the denominator exactly like the two watchdog caps.
	eff := games - (stallTurns + stallIntents + livelocks)
	lo, hi := ci95(aWins, eff)
	fmt.Fprintf(out, "\ngames played: %d\n", games)
	ab := fmt.Sprintf("A wins: %d  B wins: %d  draws: %d", aWins, bWins, draws)
	if aName == bName {
		// Same policy on both sides: every non-draw winner carries the same
		// name, so the A/B split is whichever side happened to hold the
		// winning seat -- ~50% by construction, not a comparison. Say so
		// openly; the seat split below is the informative number.
		ab += fmt.Sprintf("  (same policy %q on both sides: the split is ~50%% by construction; read the starting-player split below)", aName)
	}
	fmt.Fprintln(out, ab)
	fmt.Fprintf(out, "A win rate: %s\n", rateFmt(aWins, eff, lo, hi, "(normal approximation to the binomial)"))
	if seats == 2 {
		// The two-seat baseline: seat 0's rate and interval (over the
		// non-stalled games, so it shares the win rate's denominator and a
		// stalled game is not silently credited to a seat). Drawn games win
		// no seat, so the per-seat rates sum to less than 100% when draws
		// occur. This is the number the -a -b baseline run exists to
		// produce.
		sLo, sHi := ci95(seatWins[0], eff)
		fmt.Fprintf(out, "seat 0 wins: %d  seat 1 wins: %d  seat 0 win rate: %s\n",
			seatWins[0], seatWins[1], rateFmt(seatWins[0], eff, sLo, sHi, "(normal approximation to the binomial)"))
	} else {
		// More than two seats: counts per seat, no rate -- the two-seat
		// case is the one the bench's consumers compare policies through,
		// and a bare rate per seat across many seats would be noise without
		// the paired policy result.
		parts := make([]string, seats)
		for s := 0; s < seats; s++ {
			parts[s] = fmt.Sprintf("seat %d wins: %d", s, seatWins[s])
		}
		fmt.Fprintln(out, strings.Join(parts, "  "))
	}
	fmt.Fprintf(out, "mean turns per game: %.1f\n", float64(totalTurns)/float64(games))
	if anyStarts {
		// The play/draw measurement the seat split above replaced: since the
		// CR 103.1 toss the seat that plays first is drawn per game, not the
		// seat index, so the first-turn advantage lives here -- each seat's
		// starts beside its wins when it played first. Suppressed when no
		// game recorded a starter (a synthetic-player run), so the report is
		// byte-identical to before for every caller that does not carry one.
		parts := make([]string, seats)
		for s := 0; s < seats; s++ {
			parts[s] = fmt.Sprintf("seat %d started %d and won %d", s, starts[s], startWins[s])
		}
		fmt.Fprintf(out, "starting player: %s\n", strings.Join(parts, "  "))
	}
	if notice := stallNotice(stallTurns, stallIntents, livelocks, eff); notice != "" {
		// A run with any stalls must say so loudly -- a line that cannot be
		// mistaken for a clean run. Nothing is printed when there are no
		// stalls, so the constructed default report is unchanged.
		fmt.Fprint(out, notice)
	}
	if livelocks > 0 {
		// The full report is printed first -- the other games' results are
		// real and must reach the reader -- and then the run fails: a
		// livelocked game is an engine bug, and a zero exit would read as a
		// clean pass to whatever automated caller ran the bench.
		return fmt.Errorf("%d of %d game(s) aborted with a livelock or engine panic (see the LIVELOCK diagnostics above)", livelocks, games)
	}
	return nil
}

// pairDef is one deck pair in a matrix run: the repo decks that sit in seat
// 0 (a) and seat 1 (b) for every game of that pair. The pair is ordered so
// the historical default assignment -- seat 0 = death-n-taxes, seat 1 =
// dimir-tempo -- is the matrix's first row and reproduces the single-pair
// run byte-for-byte (same decks, same seed base, same seat assignment).
type pairDef struct {
	a, b string
}

func (p pairDef) String() string { return p.a + ":" + p.b }

// fullPairs returns every unordered pair of names in sorted i<j order, so
// the order is a pure function of the sorted deck list and never depends on
// map iteration -- the invariant TestMatrixReportOrderIsSorted pins. For the
// 12 repo decks that is 66 pairs, and the first pair -- (death-n-taxes,
// dimir-tempo), because death-n-taxes sorts first -- is exactly the pair the
// default single-pair run has always played, so a matrix row reproduces it.
func fullPairs(names []string) []pairDef {
	bps := gbench.FullPairs(names)
	ps := make([]pairDef, len(bps))
	for i, b := range bps {
		ps[i] = pairDef{a: b.A, b: b.B}
	}
	return ps
}

// commanderDeckNames returns the repo decks whose deck.File names a
// commander, in RepoDeckNames' sorted order (filtering preserves it). A
// Commander bench only seats these: a Commander game needs commander decks,
// so an -pairs all or -seats run over the full list would otherwise deal a
// 100-card Commander list as a constructed pile -- exactly the defect part A
// fixes. There are five (the foundations-* precon lists).
func commanderDeckNames() ([]string, error) {
	return commanderDeckNamesFrom(testutil.RepoDeckNames(), testutil.LoadRepoDeckFile)
}

// commanderDeckNamesFrom is the gate commanderDeckNames applies, separated
// from the repo-deck loading so a test can exercise it on synthetic deck
// files (no plural repo deck file exists yet — the partner-pair ticket's
// brief excluded adding one).
func commanderDeckNamesFrom(names []string, load func(string) (deck.File, error)) ([]string, error) {
	var cmd []string
	for _, n := range names {
		f, err := load(n)
		if err != nil {
			return nil, err
		}
		if len(f.CommanderNames()) == 0 {
			continue
		}
		cmd = append(cmd, n)
	}
	return cmd, nil
}

// parsePairsForMode parses a -pairs spec against the deck pool the run's
// format selects: "all" expands over the pool, named pairs are validated
// against it. In commander mode an explicitly named deck that is a repo deck
// but names no commander is a clear flag error -- silently turning it into a
// constructed game would misrepresent the run. Every named deck in the pool
// is validated as a commander deck (with its command-zone index and the
// deck.ValidateCommander CR 903.4/903.5 gate) when the games are set up.
func parsePairsForMode(spec string, pool []string, commander bool) ([]pairDef, error) {
	if spec == "all" {
		return parsePairs("all", pool)
	}
	if commander {
		inPool := make(map[string]bool, len(pool))
		for _, n := range pool {
			inPool[n] = true
		}
		repo := testutil.RepoDeckNames()
		inRepo := make(map[string]bool, len(repo))
		for _, n := range repo {
			inRepo[n] = true
		}
		for _, tok := range strings.Split(spec, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			parts := strings.SplitN(tok, ":", 2)
			if len(parts) != 2 {
				continue // parsePairs reports the shape error
			}
			for _, name := range []string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])} {
				if inRepo[name] && !inPool[name] {
					return nil, fmt.Errorf("-format commander: deck %q is a repo deck but names no commander -- pick a Commander deck (foundations-*)", name)
				}
			}
		}
	}
	return parsePairs(spec, pool)
}

// parsePairs parses a -pairs spec: "all" for every unordered pair of the
// given (sorted) deck names, or a comma-separated list of colon-separated
// deck pairs "a:b,c:d". Deck names must exist in names. The lookup map is
// only probed by key -- never ranged over in the report path -- so iteration
// order cannot reach the output. names is the pool for the run's format
// (commander mode passes the commander-only list).
func parsePairs(spec string, names []string) ([]pairDef, error) {
	bps, err := gbench.ParsePairs(spec, names)
	if err != nil {
		return nil, err
	}
	ps := make([]pairDef, len(bps))
	for i, b := range bps {
		ps[i] = pairDef{a: b.A, b: b.B}
	}
	return ps, nil
}

// pairPlayer plays one game of a matrix pair. pos indexes into the pairs
// slice (so the closure can resolve that pair's two decks); seed is the
// game's seed and pols is the policy name sitting at each seat, built by
// playOnePair exactly the way bench builds it, so a pair is the single-pair
// run with a different deck list. It is matrix-mode's analogue of run's
// `play` closure.
type pairPlayer func(pos int, seed uint64, pols []string) (gameOutcome, error)

// pairResult tallies one deck pair: the raw A/B and seat counts plus mean
// turns, exactly the numbers a single-pair run reports, so a matrix row is
// directly comparable to the historical single-pair number. stalls counts
// the games either watchdog ended -- they are neither A/B wins nor draws,
// credit no seat, and are excluded from the pair's win-rate denominator --
// split into stallTurns (the -max-turns cap: the game ran long) and
// stallIntents (the -max-intents cap: the turn count froze), so the pooled
// notice can say which pathology each dropped game hit.
type pairResult struct {
	pd           pairDef
	games        int
	aWins        int
	bWins        int
	draws        int
	stalls       int
	stallTurns   int
	stallIntents int
	// livelocks counts the games the engine's own livelock watcher aborted,
	// and firstLivelock records the first game's diagnostic ("game N: ...")
	// so the pooled report can name one concretely. A livelocked game is a
	// stall for every rate denominator here AND fails the run at the end.
	livelocks     int
	firstLivelock string
	seatWins      [2]int
	// The play/draw tally, from each game's first TurnChange (the CR 103.1
	// toss winner resolved over the survivors): starts[s] games where seat s
	// played first, startWins[s] of them won by that seat. A draw counts as
	// a start with no win; a genesis stall (no turn 1) counts as neither.
	starts     [2]int
	startWins  [2]int
	totalTurns int64
}

// commanderIndices returns the command-zone indices a deck names -- its
// File's CommanderIndices, one per commander (a CR 903.13 partner pair
// lists two) -- validating the deck against the Commander deck rules first
// (deck.ValidateCommander, the m35 CR 903.4/903.5 gate cmd/gorged applies
// up front) so a commander bench never half-starts on an illegal deck. A
// deck with no commander is an error, the same gate the deck-pool selection
// enforces: a Commander game must seat commander decks.
func commanderIndices(reg *cards.Registry, name string) ([]int, error) {
	f, err := testutil.LoadRepoDeckFile(name)
	if err != nil {
		return nil, err
	}
	if len(f.CommanderNames()) == 0 {
		return nil, fmt.Errorf("-format commander: deck %q is a repo deck but names no commander", name)
	}
	if verr := f.ValidateCommander(reg); verr != nil {
		return nil, fmt.Errorf("commander deck %q: %w", name, verr)
	}
	return f.CommanderIndices(), nil
}

// parseGameFormat turns the -format flag's string into the commander flag
// that threads through run/runMatrix. "constructed" is the zero value.
func parseGameFormat(s string) (bool, error) {
	switch s {
	case "constructed", "":
		return false, nil
	case "commander":
		return true, nil
	}
	return false, fmt.Errorf("-format must be \"constructed\" or \"commander\", got %q", s)
}

// gameSeedPair returns the seed game g of pair number pos in a matrix run at
// base baseSeed, where every pair plays exactly `games` games. Game i of a
// run plays at base+i, so a pair's games are distinct from every other
// pair's because the offset pos*games shifts the whole block; and pair 0 is
// base+0..base+games-1, byte-identical to the single-pair run of the same
// len games at the same base -- the property that lets a matrix row
// reproduce today's number (and that cmd/botbench's own liveness probe
// would catch if it ever stopped holding).
func gameSeedPair(baseSeed uint64, pos, games, g int) uint64 {
	return gbench.GameSeedPair(baseSeed, pos, games, g)
}

// pdToBench converts a bench command pair into the shared scheduler's
// PairDef.
func pdToBench(pd pairDef) gbench.PairDef { return gbench.PairDef{A: pd.a, B: pd.b} }

// seatCtorFor resolves a policy name to a shared-scheduler seat constructor.
// A name in the policies table uses that policy's real constructor; any other
// name (a synthetic test player) gets a nil-returning ctor, because the
// synthetic player reads the policy names, not the built seats. The real run
// path resolves every (policy, seated deck) pair through seatCtorForDeck
// before any game starts, so a typo never reaches here.
func seatCtorFor(name string) gbench.SeatCtor {
	if c, ok := policies[name]; ok {
		return gbench.SeatCtor(c)
	}
	return func(uint64) seat.Seat { return nil }
}

// pairFromBench converts a shared scheduler PairResult back into the bench
// command's pairResult (same fields, unexported spelling).
func pairFromBench(r gbench.PairResult) pairResult {
	return pairResult{
		pd:            pairDef{a: r.PD.A, b: r.PD.B},
		games:         r.Games,
		aWins:         r.AWins,
		bWins:         r.BWins,
		draws:         r.Draws,
		stalls:        r.Stalls,
		stallTurns:    r.StallTurns,
		stallIntents:  r.StallIntents,
		livelocks:     r.Livelocks,
		firstLivelock: r.FirstLivelock,
		seatWins:      r.SeatWins,
		starts:        r.Starts,
		startWins:     r.StartWins,
		totalTurns:    r.TotalTurns,
	}
}

// outcomeToBench converts the bench command's gameOutcome into the shared
// scheduler's policy-name-free Outcome. The winner's SEAT is what the shared
// fold attributes by; the name stays command-side.
func outcomeToBench(o gameOutcome) gbench.Outcome {
	return gbench.Outcome{
		WinnerSeat: o.winnerSeat,
		Draw:       o.winner == "" && !o.isStalled(),
		StallOn:    o.stallOn,
		Livelock:   o.livelock,
		Turns:      o.turns,
		Intents:    o.intents,
		Starter:    o.starter,
		StarterSet: o.starterSet,
	}
}

// gamePool is the one shared execution budget for the single-pair bench
// (benchWithPool) and the -rotate run. The matrix scheduler moved to
// internal/bench (gbench.RunPairs); this remains the single-pair pool.
type gamePool struct {
	jobs chan func()
	wg   sync.WaitGroup
}

func newGamePool(workers int) *gamePool {
	if workers < 1 {
		workers = 1
	}
	p := &gamePool{jobs: make(chan func())}
	p.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer p.wg.Done()
			for job := range p.jobs {
				job()
			}
		}()
	}
	return p
}

func (p *gamePool) submit(job func()) {
	p.jobs <- job
}

func (p *gamePool) close() {
	close(p.jobs)
	p.wg.Wait()
}

type gameResult struct {
	outcome gameOutcome
	err     error
}

// progressWriter serialises live progress lines from parallel workers onto a
// single writer. It is deliberately NOT the report writer: a matrix run's
// report is printed once, serially and in pair order, after every pair
// finishes, so the report is byte-identical across runs regardless of how
// the workers interleaved. Progress is only so an hour's run is not a black
// box; it never reaches the deterministic report.
type progressWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (p *progressWriter) line(format string, a ...any) {
	if p == nil || p.w == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.w, format+"\n", a...)
}

// runPairs plays every pair in `pairs` over `games` games each and returns
// the per-pair tallies, in pair order. Pairs are independent games -- each is
// a distinct seed block -- so they can be played in parallel (workers) and
// the result is byte-deterministic regardless of how many workers run: each
// result is written to results[pos] by disjunct position and read back in
// slice order, never through a map. A worker that hits an error (an intent
// cap, a policy failure) records it, closes stop, and the whole run returns
// that error -- the same abort-on-error behaviour as the single-pair bench.
func runPairs(baseSeed uint64, games int, aName, bName string, pairs []pairDef, play pairPlayer, workers int, prog *progressWriter) ([]pairResult, error) {
	// The scheduler itself lives in internal/bench (gbench.RunPairs), the SAME
	// pair-matrix runner cmd/policytune calls: it owns the worker pool, the
	// per-pair seed block, the side-to-seat trading and the ascending-order
	// fold. This function only translates the bench command's policy-name
	// player into the shared two-seat-constructor shape and the shared tallies
	// back into pairResult.
	bPairs := make([]gbench.PairDef, len(pairs))
	for i, pd := range pairs {
		bPairs[i] = pdToBench(pd)
	}
	// The side constructors: side A is the -a policy, side B the -b policy.
	// gbench's scheduler builds that game's two seats from them (PlaysSeat
	// decides which side sits where). A synthetic `play` (the tests inject
	// one with fake names like "a"/"b") never reads the seats, so an unknown
	// name maps to a nil-returning ctor rather than an error: the real run
	// resolves every (policy, seated deck) pair through seatCtorForDeck
	// before any game starts.
	aCtor := seatCtorFor(aName)
	bCtor := seatCtorFor(bName)
	var progress gbench.Progress
	if prog != nil {
		progress = prog.line
	}
	var adapter gbench.PairPlayer = func(pos int, seed uint64, g int, seats [2]seat.Seat) (gbench.Outcome, error) {
		// Reconstruct the per-seat policy-name assignment the trace and the
		// per-game lines report, then call the caller's player. The seats the
		// shared scheduler built are the same two the caller would build from
		// those names (same constructors, same per-seat seed), so passing the
		// names is equivalent and keeps the bench's player signature stable.
		_ = seats
		pols := polsFor(g, 2, aName, bName)
		oc, err := play(pos, seed, pols)
		if err != nil {
			return gbench.Outcome{}, err
		}
		return outcomeToBench(oc), nil
	}
	results, err := gbench.RunPairs(baseSeed, games, bPairs, aCtor, bCtor, adapter, workers, progress)
	if err != nil {
		return nil, err
	}
	out := make([]pairResult, len(results))
	for i, r := range results {
		out[i] = pairFromBench(r)
	}
	return out, nil
}

// mergedResult pools every pair's counts into the single set of numbers the
// pooled line and the JSON summary report. Pooling is over COUNTS, not rates:
// the pooled win rate is (sum of A wins)/(sum of all games), which is what a
// single run of all the games would have produced, and its CI is the Wald
// interval on that pooled count. Averaging the per-pair rates instead would
// over-weight the small pairs -- the mutation TestPooledCIPoolsCountsNotRates
// exists to catch.
type mergedResult struct {
	pairs         int
	games         int
	aWins         int
	bWins         int
	draws         int
	stalls        int
	stallTurns    int
	stallIntents  int
	livelocks     int
	firstLivelock string
	seatWins      [2]int
	starts        [2]int
	startWins     [2]int
	totalTurns    int64
}

func mergeResults(results []pairResult) mergedResult {
	var m mergedResult
	m.pairs = len(results)
	for _, r := range results {
		m.games += r.games
		m.aWins += r.aWins
		m.bWins += r.bWins
		m.draws += r.draws
		m.stalls += r.stalls
		m.stallTurns += r.stallTurns
		m.stallIntents += r.stallIntents
		m.livelocks += r.livelocks
		if m.firstLivelock == "" {
			m.firstLivelock = r.firstLivelock
		}
		m.seatWins[0] += r.seatWins[0]
		m.seatWins[1] += r.seatWins[1]
		m.starts[0] += r.starts[0]
		m.starts[1] += r.starts[1]
		m.startWins[0] += r.startWins[0]
		m.startWins[1] += r.startWins[1]
		m.totalTurns += r.totalTurns
	}
	return m
}

// excludeCounts classifies the per-pair intervals against 50%: a pair whose
// whole A-win interval sits below 50% is a pair where policy A loses; above
// is a pair where A wins; the rest are undecided. The interval is over the
// pair's NON-stalled games (a stalled game is not a win for either side), so
// the honest summary a single pooled percentage hides stays honest when the
// watchdog dropped games -- one that reads "beats B on 41 of 66, loses on 3,
// undecided on 22" says more than "53% overall".
func excludeCounts(results []pairResult) (below, above, undecided int) {
	for _, r := range results {
		lo, hi := ci95(r.aWins, r.games-r.stalls)
		if hi < 0.5 {
			below++
		} else if lo > 0.5 {
			above++
		} else {
			undecided++
		}
	}
	return below, above, undecided
}

// writeMatrixText prints the per-pair table and the pooled line. The header
// states plainly that -games is PER PAIR, because a reader who mistakes the
// total for the per-pair N draws wrong conclusions from every row -- that
// the number is per-pair is the whole point of the flag.
func writeMatrixText(out io.Writer, aName, bName string, baseSeed uint64, games int, results []pairResult, commander bool) error {
	m := mergeResults(results)
	// staleHeader is the header's per-row arithmetic denominator: a pair's
	// A win rate and seat 0 rate are over ITS non-stalled games, not over
	// the byte N, so a stalled game is never distorted into a win or a loss
	// for either side. mEff is the pooled analogue.
	hdr := fmt.Sprintf("bot bench matrix: base seed %d, %s vs %s, %d deck pairs, %d games PER PAIR (total %d games)",
		baseSeed, aName, bName, len(results), games, m.games)
	if commander {
		// The header must say which format the run used, so a Commander
		// number cannot be mistaken for a constructed baseline in a
		// scrollback.
		hdr += " (commander format)"
	}
	fmt.Fprintln(out, hdr)
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "deck pair\tA wins\tB wins\tdraws\tstalls\tA win rate (95% CI)\tseat 0 rate (95% CI)\tmean turns")
	for _, r := range results {
		eff := r.games - r.stalls
		lo, hi := ci95(r.aWins, eff)
		sLo, sHi := ci95(r.seatWins[0], eff)
		// rateCell renders "no rate" for an all-stalled pair, which a bare
		// "0.0%" would otherwise mis-represent as a real rate over no games.
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\t%s\t%.1f\n",
			r.pd, r.aWins, r.bWins, r.draws, r.stalls,
			rateCell(r.aWins, eff, lo, hi),
			rateCell(r.seatWins[0], eff, sLo, sHi),
			float64(r.totalTurns)/float64(r.games))
	}
	tw.Flush()

	mEff := m.games - m.stalls
	blo, bhi := ci95(m.aWins, mEff)
	sLo, sHi := ci95(m.seatWins[0], mEff)
	below, above, undecided := excludeCounts(results)
	fmt.Fprintf(out, "\ngames per pair: %d  (total %d games across %d pairs)\n", games, m.games, m.pairs)
	fmt.Fprintf(out, "pooled across %d pairs: %s wins %d, B wins %d, draws %d", m.pairs, aName, m.aWins, m.bWins, m.draws)
	if m.stalls > 0 {
		// The pooled line names the thrown-away games so a reader sees how
		// much of the run the stalls cost.
		fmt.Fprintf(out, ", %d stalled", m.stalls)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "pooled %s win rate: %s\n", aName,
		rateFmt(m.aWins, mEff, blo, bhi, "(normal approximation to the binomial, pooled over counts)"))
	fmt.Fprintf(out, "pooled seat 0 win rate: %s\n",
		rateFmt(m.seatWins[0], mEff, sLo, sHi, "(normal approximation to the binomial)"))
	fmt.Fprintf(out, "mean turns per game (pooled): %.1f\n", float64(m.totalTurns)/float64(m.games))
	if m.starts[0]+m.starts[1] > 0 {
		// The pooled play/draw measurement, same shape as the single-pair
		// report's line: since the CR 103.1 toss the seat that plays first
		// is drawn per game, not the seat index.
		fmt.Fprintf(out, "pooled starting player: seat 0 started %d and won %d  seat 1 started %d and won %d\n",
			m.starts[0], m.startWins[0], m.starts[1], m.startWins[1])
	}
	fmt.Fprintf(out, "pairs whose A-win interval excludes 50%%: A loses on %d, A wins on %d, undecided on %d\n", below, above, undecided)
	if notice := stallNotice(m.stallTurns, m.stallIntents, m.livelocks, mEff); notice != "" {
		// A matrix with any stalls says so loudly, like the single-pair run.
		fmt.Fprint(out, notice)
	}
	if m.firstLivelock != "" {
		fmt.Fprintf(out, "first livelock: %s\n", m.firstLivelock)
	}
	if m.livelocks > 0 {
		// After the full report: a livelocked game is an engine bug, and a
		// zero exit would read as a clean pass to the automated caller.
		return fmt.Errorf("%d of %d game(s) aborted with a livelock or engine panic (see the LIVELOCK diagnostics above)", m.livelocks, m.games)
	}
	return nil
}

// rateFmt renders a rate with its CI and a trailing suffix for the long-form
// report lines ("A win rate:", "pooled ... win rate:"). Over zero
// non-stalled games a rate is not 0%, it is undefined -- a "0.0%" over no
// games is a number that gets quoted as if it meant something -- so it prints
// a phrase that cannot be parsed as a win rate.
func rateFmt(wins, eff int, lo, hi float64, suffix string) string {
	if eff <= 0 {
		return "no rate (all games stalled)"
	}
	s := fmt.Sprintf("%.1f%%  95%% CI [%.1f%%, %.1f%%]", float64(wins)/float64(eff)*100, lo*100, hi*100)
	if suffix != "" {
		s += " " + suffix
	}
	return s
}

// rateCell is the tabwriter column variant of rateFmt: the terse
// "12.3% [1.2%, 23.4%]" shape a matrix row's width needs, with the same
// all-stalled guard.
func rateCell(wins, eff int, lo, hi float64) string {
	if eff <= 0 {
		return "no rate"
	}
	return fmt.Sprintf("%.1f%% [%.1f%%, %.1f%%]", float64(wins)/float64(eff)*100, lo*100, hi*100)
}

// jsonPair and jsonPooled are the machine-readable report, one element per
// pair plus the pooled summary. Rates are fractions (0..1) so a future task
// can diff two runs numerically without re-parsing % strings. Field order is
// struct order, so the JSON is byte-deterministic for identical inputs.
type jsonPair struct {
	Pair  string `json:"pair"`
	AWins int    `json:"a_wins"`
	BWins int    `json:"b_wins"`
	Draws int    `json:"draws"`
	// Stalls is every watchdog/aborted game; Livelocks is the subset the
	// engine's livelock watcher aborted -- the machine-readable count an
	// agent parses instead of tailing raw output.
	Stalls     int       `json:"stalls"`
	Livelocks  int       `json:"livelocks"`
	AWinRate   float64   `json:"a_win_rate"`
	AWinRateCI []float64 `json:"a_win_rate_ci"`
	Seat0Wins  int       `json:"seat0_wins"`
	Seat1Wins  int       `json:"seat1_wins"`
	Seat0Rate  float64   `json:"seat0_rate"`
	Seat0CI    []float64 `json:"seat0_ci"`
	MeanTurns  float64   `json:"mean_turns"`
}

type jsonPooled struct {
	Pairs        int       `json:"pairs"`
	Games        int       `json:"games"`
	GamesPerPair int       `json:"games_per_pair"`
	AWins        int       `json:"a_wins"`
	BWins        int       `json:"b_wins"`
	Draws        int       `json:"draws"`
	Stalls       int       `json:"stalls"`
	Livelocks    int       `json:"livelocks"`
	AWinRate     float64   `json:"a_win_rate"`
	AWinRateCI   []float64 `json:"a_win_rate_ci"`
	Seat0Rate    float64   `json:"seat0_rate"`
	Seat0CI      []float64 `json:"seat0_ci"`
	MeanTurns    float64   `json:"mean_turns"`
	Seat0Starts  int       `json:"seat0_starts"`
	Seat1Starts  int       `json:"seat1_starts"`
	Seat0StartW  int       `json:"seat0_start_wins"`
	Seat1StartW  int       `json:"seat1_start_wins"`
	ExcludeBelow int       `json:"pairs_excluding_50_below"`
	ExcludeAbove int       `json:"pairs_excluding_50_above"`
	Undecided    int       `json:"pairs_undecided"`
}

type jsonDoc struct {
	PolicyA      string     `json:"policy_a"`
	PolicyB      string     `json:"policy_b"`
	BaseSeed     uint64     `json:"base_seed"`
	Format       string     `json:"format"`
	GamesPerPair int        `json:"games_per_pair"`
	Pairs        []jsonPair `json:"pairs"`
	Pooled       jsonPooled `json:"pooled"`
}

func writeMatrixJSON(out io.Writer, aName, bName string, baseSeed uint64, games int, results []pairResult, commander bool) error {
	m := mergeResults(results)
	format := "constructed"
	if commander {
		format = "commander"
	}
	doc := jsonDoc{PolicyA: aName, PolicyB: bName, BaseSeed: baseSeed, Format: format, GamesPerPair: games}
	for _, r := range results {
		eff := r.games - r.stalls
		lo, hi := ci95(r.aWins, eff)
		sLo, sHi := ci95(r.seatWins[0], eff)
		doc.Pairs = append(doc.Pairs, jsonPair{
			Pair:       r.pd.String(),
			AWins:      r.aWins,
			BWins:      r.bWins,
			Draws:      r.draws,
			Stalls:     r.stalls,
			Livelocks:  r.livelocks,
			AWinRate:   winRateFrac(r.aWins, eff),
			AWinRateCI: []float64{lo, hi},
			Seat0Wins:  r.seatWins[0],
			Seat1Wins:  r.seatWins[1],
			Seat0Rate:  winRateFrac(r.seatWins[0], eff),
			Seat0CI:    []float64{sLo, sHi},
			MeanTurns:  float64(r.totalTurns) / float64(r.games),
		})
	}
	mEff := m.games - m.stalls
	blo, bhi := ci95(m.aWins, mEff)
	sLo, sHi := ci95(m.seatWins[0], mEff)
	below, above, undecided := excludeCounts(results)
	doc.Pooled = jsonPooled{
		Pairs:        m.pairs,
		Games:        m.games,
		GamesPerPair: games,
		AWins:        m.aWins,
		BWins:        m.bWins,
		Draws:        m.draws,
		Stalls:       m.stalls,
		Livelocks:    m.livelocks,
		AWinRate:     winRateFrac(m.aWins, mEff),
		AWinRateCI:   []float64{blo, bhi},
		Seat0Rate:    winRateFrac(m.seatWins[0], mEff),
		Seat0CI:      []float64{sLo, sHi},
		MeanTurns:    float64(m.totalTurns) / float64(m.games),
		Seat0Starts:  m.starts[0],
		Seat1Starts:  m.starts[1],
		Seat0StartW:  m.startWins[0],
		Seat1StartW:  m.startWins[1],
		ExcludeBelow: below,
		ExcludeAbove: above,
		Undecided:    undecided,
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// winRateFrac is a 0..1 rate with a guarded denominator, the machine-readable
// counterpart of rateFmt/rateCell; it stays 0 when eff <= 0 (the JSON
// consumers diff runs numerically, where a 0 for a no-rate pair is an honest
// less-than-any-real-rate rather than prose that might get quoted).
func winRateFrac(wins, eff int) float64 {
	if eff <= 0 {
		return 0
	}
	return float64(wins) / float64(eff)
}

// runMatrix is the matrix mode's entry through the real engine: it opens the
// corpus, resolves every distinct deck the pairs name, plays each pair for
// `games` matches (parallelised across pairs by runPairs), and prints either
// the text table+pooled line or the JSON document. It shares playMatch, the
// policies table and ci95 with run(), so the seat-trades-policies and
// seed-determinism properties are the same two seats a single-pair run has.
func runMatrix(baseSeed uint64, games, seats int, aName, bName, dir, format string, pairs []pairDef, workers, maxTurns, maxIntents int, commander bool, coverage *coveragePlan, out, prog io.Writer) error {
	return runMatrixTraced(baseSeed, games, seats, aName, bName, dir, format, pairs, workers, maxTurns, maxIntents, commander, coverage, "", out, prog)
}

func runMatrixTraced(baseSeed uint64, games, seats int, aName, bName, dir, format string, pairs []pairDef, workers, maxTurns, maxIntents int, commander bool, coverage *coveragePlan, tracePath string, out, prog io.Writer) error {
	if seats != 2 {
		return fmt.Errorf("-pairs requires -seats 2 (a matrix pits one deck pair against another), got %d", seats)
	}
	if games < 1 {
		return fmt.Errorf("-games must be at least 1 per pair, got %d", games)
	}
	if format != "text" && format != "json" {
		return fmt.Errorf("-out must be \"text\" or \"json\", got %q", format)
	}
	// Policy names are NOT validated here any more: a name may be a built-in
	// or a policy one of the seated decks declares, and which decks are
	// seated is not known until they are loaded below. Both paths resolve
	// every (policy, seated deck) pair through seatCtorForDeck before any
	// game starts, and its error names the deck and the policies that deck
	// does declare -- strictly more validation than this check, not less.

	reg, err := testutil.OpenCorpusRegistry(dir)
	if err != nil {
		return fmt.Errorf("opening corpus at %s: %w (run `make fetch-cards compile-cards` first)", dir, err)
	}

	// Resolve each distinct deck once; pairs share decks so one deck list
	// maps to many pairs. The map is only looked up by key during play --
	// never ranged over in the reporting path -- so its iteration order
	// cannot reach the output. commander mode also resolves each deck's
	// command-zone index and validates the deck against the Commander deck
	// rules (deck.ValidateCommander) up front, so a commander bench never
	// half-starts on an illegal deck or a deck with no commander (which an
	// explicit pair or the -seats path could otherwise slip past).
	deckByName := make(map[string][]*cards.Card, len(pairs)*2)
	// deckFileByName carries the parsed deck FILE beside its resolved cards,
	// because a deck's named bot policies (deck.File.Policies) are read from
	// it at seat construction. Keyed like deckByName and likewise never
	// ranged in the reporting path, so its iteration order cannot reach the
	// output.
	deckFileByName := make(map[string]deck.File, len(pairs)*2)
	cmdrByName := make(map[string][]int, len(pairs)*2) // commander mode only
	for _, pd := range pairs {
		for _, name := range []string{pd.a, pd.b} {
			if _, ok := deckByName[name]; ok {
				continue
			}
			d, err := testutil.LoadRepoDeck(reg, name)
			if err != nil {
				return err
			}
			deckByName[name] = d
			df, err := testutil.LoadRepoDeckFile(name)
			if err != nil {
				return err
			}
			if err := deckPolicyShadowCheck(df); err != nil {
				return err
			}
			deckFileByName[name] = df
			if commander {
				cis, err := commanderIndices(reg, name)
				if err != nil {
					return err
				}
				cmdrByName[name] = cis
			}
		}
	}

	// collect is the -decision-stats histogram and cov the -action-coverage
	// collector for this run; both nil when their flags are off, so a default
	// run records nothing and appends nothing.
	var collect *decisionStats
	if decisionStatsEnabled {
		collect = newDecisionStats()
	}
	var cov *actionCoverage
	if actionCoverageEnabled {
		cov = newActionCoverage()
	}
	var traces []*gameTrace
	if tracePath != "" {
		traces = make([]*gameTrace, len(pairs)*games)
	}
	var onpol *onpolicyCollector
	if onpolicyCorpusPath != "" {
		onpol = newOnPolicyCollector(len(pairs), games)
	}

	// Resolve every (policy name, seated deck) pair ONCE, before any game
	// starts, so a name no deck declares fails the run immediately instead of
	// panicking inside a worker mid-matrix. The map is keyed by the pair the
	// resolution depends on; a built-in name resolves identically for every
	// deck, a deck policy only for the deck that declares it.
	seatCtors := make(map[[2]string]func(seed uint64) seat.Seat, len(pairs)*4)
	polNames := []string{aName, bName}
	for _, e := range oppMix {
		polNames = append(polNames, e.name)
	}
	for _, pd := range pairs {
		for _, deckName := range []string{pd.a, pd.b} {
			for _, polName := range polNames {
				key := [2]string{polName, deckName}
				if _, ok := seatCtors[key]; ok {
					continue
				}
				c, err := seatCtorForDeck(polName, deckFileByName[deckName])
				if err != nil {
					return err
				}
				seatCtors[key] = c
			}
		}
	}

	play := func(pos int, seed uint64, pols []string) (gameOutcome, error) {
		pd := pairs[pos]
		deckNames := [2]string{pd.a, pd.b}
		botSeats := make([]seat.Seat, 2)
		mixed := oppMixPick(seed)
		for seat := 0; seat < 2; seat++ {
			// Same per-seat seed derivation as run(): policy RNG is distinct
			// from the engine's and from every other seat's.
			//
			// The policy is resolved against the deck THIS SEAT plays, which
			// is what per-deck policy means: policies still trade seats every
			// game (aPlaysSeat) while decks stay pinned to seats, so side A
			// picks up deck 0's policy in the games it sits at seat 0 and deck
			// 1's in the games it sits at seat 1. The pre-resolved table above
			// means this hot path does no parsing.
			pol := pols[seat]
			if mixed != "" && pol == bName {
				pol = mixed // -opp-mix: this game's side B sits another policy
			}
			botSeats[seat] = seatCtors[[2]string{pol, deckNames[seat]}](seed ^ uint64(seat+1))
		}
		var commanders [][]int
		if commander {
			commanders = [][]int{cmdrByName[pd.a], cmdrByName[pd.b]}
		}
		cfg := buildGameConfig(seed, []string{pd.a, pd.b},
			[][]*cards.Card{deckByName[pd.a], deckByName[pd.b]}, commanders, commander)
		cfg.Tokens = reg.Tokens
		cfg.NameUniverse = reg.Cards
		if onpol != nil {
			// The on-policy recorder only observes the policynet seats'
			// scored decisions; the game itself is the plain playMatch.
			gameIndex := int(seed - gameSeedPair(baseSeed, pos, games, 0))
			g := onpol.attach(pos, gameIndex, pd.String(), seed, deckNames, botSeats)
			o, err := playMatch(cfg, pols, botSeats, maxTurns, maxIntents, collect, cov)
			if err == nil {
				onpol.finish(g, o)
			}
			return o, err
		}
		if traces == nil {
			return playMatch(cfg, pols, botSeats, maxTurns, maxIntents, collect, cov)
		}
		gameIndex := int(seed - gameSeedPair(baseSeed, pos, games, 0))
		trace := newGameTrace()
		traces[pos*games+gameIndex] = trace
		meta := traceDecisionMeta{PairIndex: pos, Pair: pd.String(), GameIndex: gameIndex, Seed: seed}
		return playMatchTraced(cfg, pols, botSeats, maxTurns, maxIntents, collect, cov, trace, meta)
	}

	results, err := runPairs(baseSeed, games, aName, bName, pairs, play, workers, &progressWriter{w: prog})
	if err != nil {
		return err
	}
	if onpol != nil {
		if err := onpol.write(onpolicyCorpusPath); err != nil {
			return err
		}
	}
	if format == "json" {
		if err := writeMatrixJSON(out, aName, bName, baseSeed, games, results, commander); err != nil {
			return err
		}
		collect.write(out)
		cov.write(out)
		m := mergeResults(results)
		if m.livelocks > 0 {
			// Same contract as the text report: the JSON document is complete
			// (the per-pair livelocks counts are in it), then the run fails.
			return fmt.Errorf("%d of %d game(s) aborted with a livelock or engine panic (see the JSON livelocks fields)", m.livelocks, m.games)
		}
		if tracePath != "" {
			return writeDecisionTrace(tracePath, newTraceRunV1(baseSeed, games, aName, bName, pairs, maxTurns, maxIntents, commander), traces)
		}
		return nil
	}
	if coverage != nil {
		fmt.Fprintf(out, "coverage selection (static registered primitive metric): decks %s; historical default %d/%d, selected %d/%d\n",
			strings.Join(coverage.Names, ","), coverage.Baseline, coverage.Available, coverage.Covered, coverage.Available)
	}
	if err := writeMatrixText(out, aName, bName, baseSeed, games, results, commander); err != nil {
		return err
	}
	collect.write(out)
	cov.write(out)
	if tracePath != "" {
		return writeDecisionTrace(tracePath, newTraceRunV1(baseSeed, games, aName, bName, pairs, maxTurns, maxIntents, commander), traces)
	}
	return nil
}

// seatedDeckNames returns the deck-list order a run seats, with seat s
// holding names[(s+rotate)%len(names)] -- rotate=0 is the historical fixed
// assignment (seat 0 = names[0] etc.), and over the rotate=0..seats-1 cycle
// every deck sits at every seat exactly once, which is the property a
// rotate experiment relies on: if a four-seat imbalance follows the seat
// index it cannot be a fixed deck sitting there, and vice versa.
func seatedDeckNames(names []string, rotate int) []string {
	n := len(names)
	seated := make([]string, n)
	for s := 0; s < n; s++ {
		seated[s] = names[(s+rotate)%n]
	}
	return seated
}

// run is main's entry through the real engine: it validates the flags,
// opens the corpus and the per-seat repo decks, and plays `games` matches
// between the named policies via benchWithPool, sized by the -workers budget
// (0 uses all cores). It exists so the whole flag-driven path stays open to
// tests that need it (determinism, end-to-end); the loop and the report live
// in benchWithPool, which tests can also drive directly with a synthetic
// matchPlayer.
//
// rotate shifts which deck-list a seat holds (seatDeckIndex: seat s holds
// the pool's (s+rotate)%seats-th deck), so a four-seat commander run can
// be repeated with each deck at each seat -- the experiment that tells a
// seat-index artifact from a deck-strength difference. rotate must be in
// [0, seats); 0 is today's assignment, so a run without the flag is
// byte-identical to the pre-flag bench.
func run(baseSeed uint64, games, seats, rotate, workers int, aName, bName, dir string, maxTurns, maxIntents int, commander bool, out io.Writer) error {
	if games < 1 {
		return fmt.Errorf("-games must be at least 1, got %d", games)
	}
	if seats < 2 {
		return fmt.Errorf("-seats must be at least 2 (a bench pits two policies), got %d", seats)
	}
	if rotate < 0 || rotate >= seats {
		return fmt.Errorf("-rotate must be in [0,%d) with %d seats, got %d", seats, seats, rotate)
	}
	// Policy names are NOT validated here any more: a name may be a built-in
	// or a policy one of the seated decks declares, and which decks are
	// seated is not known until they are loaded below. Both paths resolve
	// every (policy, seated deck) pair through seatCtorForDeck before any
	// game starts, and its error names the deck and the policies that deck
	// does declare -- strictly more validation than this check, not less.

	reg, err := testutil.OpenCorpusRegistry(dir)
	if err != nil {
		return fmt.Errorf("opening corpus at %s: %w (run `make fetch-cards compile-cards` first)", dir, err)
	}

	// Decks are tied to seats for the whole run (seat 0 always holds the
	// first deck of the pool), and seats trade policies every game, so each
	// policy plays each deck-list the same number of times -- the deck can
	// no more masquerade as a policy advantage than seat order can. The pool
	// is the full deck list for a constructed run and the commander-only
	// list for a commander run (a Commander game must seat commander decks;
	// dealing a 100-card Commander list as a constructed pile is exactly the
	// defect this command fixes). Both lists stay sorted, so the assignment
	// is identical on every machine.
	var names []string
	if commander {
		names, err = commanderDeckNames()
		if err != nil {
			return err
		}
	} else {
		names = testutil.RepoDeckNames()
	}
	if seats > len(names) {
		return fmt.Errorf("-seats %d exceeds the %d %s decks available", seats, len(names), deckKind(commander))
	}
	// The deck a seat holds is the pool's (s+rotate)%seats-th entry, so
	// rotate=0 is the historical assignment and the header, the Config and
	// the loaded lists all describe the same seated order. The deck list
	// stays sorted (names is sorted); only which seat holds which entry
	// changes.
	seated := seatedDeckNames(names[:seats], rotate)
	decks := make([][]*cards.Card, seats)
	// seatCtor[s] is the constructor for the policy sitting at seat s,
	// resolved against the deck seat s holds. Built per seat rather than per
	// policy name because a deck policy is a property of the deck: two seats
	// answering to the same -a name can hold different weights.
	seatCtor := make([]map[string]func(seed uint64) seat.Seat, seats)
	commanders := make([][]int, seats) // commander mode only
	for s := 0; s < seats; s++ {
		d, err := testutil.LoadRepoDeck(reg, seated[s])
		if err != nil {
			return err
		}
		decks[s] = d
		df, err := testutil.LoadRepoDeckFile(seated[s])
		if err != nil {
			return err
		}
		if err := deckPolicyShadowCheck(df); err != nil {
			return err
		}
		// Resolve both policy names against this seat's deck up front, so a
		// name no deck declares fails the run before any game is played.
		seatCtor[s] = make(map[string]func(seed uint64) seat.Seat, 2)
		for _, polName := range []string{aName, bName} {
			if _, ok := seatCtor[s][polName]; ok {
				continue
			}
			c, err := seatCtorForDeck(polName, df)
			if err != nil {
				return err
			}
			seatCtor[s][polName] = c
		}
		if commander {
			cis, err := commanderIndices(reg, seated[s])
			if err != nil {
				return err
			}
			commanders[s] = cis
		}
	}

	hdr := fmt.Sprintf("bot bench: base seed %d, %s vs %s, %d games, %d seats, decks %s",
		baseSeed, aName, bName, games, seats, strings.Join(seated, ","))
	if commander {
		// The header names the format so a Commander number cannot be quoted
		// as a constructed baseline -- the mix-up this task exists to end.
		hdr += " (commander format)"
	}
	fmt.Fprintln(out, hdr)

	// collect is the -decision-stats histogram and cov the -action-coverage
	// collector for this run; both nil when their flags are off, so a default
	// run records nothing and appends nothing.
	var collect *decisionStats
	if decisionStatsEnabled {
		collect = newDecisionStats()
	}
	var cov *actionCoverage
	if actionCoverageEnabled {
		cov = newActionCoverage()
	}

	play := func(s uint64, pols []string) (gameOutcome, error) {
		botSeats := make([]seat.Seat, seats)
		for seat := 0; seat < seats; seat++ {
			// One bot per seat, each seeded from the game's seed the same
			// way host/defaultSeats does (seed ^ seat+1) so a policy's RNG
			// is distinct from the engine's and from every other seat's.
			// The constructor was resolved against the deck THIS seat holds,
			// so a deck's own named policy is what plays it.
			botSeats[seat] = seatCtor[seat][pols[seat]](s ^ uint64(seat+1))
		}
		cfg := buildGameConfig(s, seated, decks, commanders, commander)
		cfg.Tokens = reg.Tokens
		cfg.NameUniverse = reg.Cards
		return playMatch(cfg, pols, botSeats, maxTurns, maxIntents, collect, cov)
	}
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	pool := newGamePool(workers)
	defer pool.close()
	if err := benchWithPool(baseSeed, games, seats, aName, bName, play, out, pool); err != nil {
		return err
	}
	collect.write(out)
	cov.write(out)
	return nil
}

// buildGameConfig turns one game's seat assignment into its rules.Config,
// applying commander mode's life and command-zone settings. Extracted from
// run/runMatrix so the commander Config is unit-testable without paying an
// engine game: commander mode sets FormatCommander, exactly 40 starting life
// (CR 903.6) and each seat's command-zone commander index, matching how
// cmd/gorged's deckLoader seats a commander table.
func buildGameConfig(seed uint64, names []string, decks [][]*cards.Card, commanders [][]int, commander bool) rules.Config {
	cfg := rules.Config{Seed: seed, Names: names, Decks: decks}
	if commander {
		cfg.Format = rules.FormatCommander
		cfg.StartingLife = 40
		cfg.Commanders = commanders
	}
	return cfg
}

// deckKind names the deck pool a run selects, for an error message that
// says why a -seats count is too high.
func deckKind(commander bool) string {
	if commander {
		return "commander"
	}
	return "repo"
}

func main() {
	a := flag.String("a", "bot", "policy name on side A")
	b := flag.String("b", "bot", "policy name on side B (same as -a is a valid, expected run)")
	games := flag.Int("games", 100, "number of games to play (matrix mode: per pair)")
	seed := flag.Uint64("seed", 0, "base seed; game i plays at seed+i")
	seats := flag.Int("seats", 2, "number of seats")
	rotate := flag.Int("rotate", 0, "rotate the seat-to-deck assignment by N positions (seat s holds the (s+N)%%seats-th deck); 0 is today's fixed assignment")
	pairs := flag.String("pairs", "", "deck-pair matrix: \"all\" for every unordered repo-deck pair, \"coverage\" for the deterministic primitive-covering deck subset, or a comma-separated \"a:b,c:d\" list; empty keeps today's single-pair behaviour")
	format := flag.String("format", "constructed", "construction format: constructed or commander (commander deals commander decks into the command zone, starts at 40 life, and plays for -max-turns before a game is recorded as a stall)")
	out := flag.String("out", "text", "matrix output format: text or json (json is machine-readable for diffing runs)")
	workers := flag.Int("workers", 0, "parallelism budget for bench games, single-pair and matrix, across pairs and games; 0 = use all cores (result is deterministic regardless)")
	// The two watchdog caps catch different pathologies and must not be
	// conflated: -max-turns ends a game that runs long in turn count (a cap
	// a frozen-turn loop never reaches, because its turn number stops);
	// -max-intents ends a game whose turn count freezes but that keeps
	// submitting intents forever. Both end the game as a stall -- not a win,
	// not a draw, excluded from the win-rate denominator. Healthy constructed
	// games measure 171-1136 intents, so the 20000 default is ~17x the worst
	// of those with room for Commander's longer games.
	maxTurns := flag.Int("max-turns", 200, "maximum turns per game before it ends as a stall (not a win, not a draw); catches a game that runs long in turn count; 0 = no cap")
	maxIntents := flag.Int("max-intents", 20000, "maximum intents per game before it ends as a stall (not a win, not a draw); catches a game whose turn count never advances but that keeps submitting intents; 0 = no cap")
	dir := flag.String("dir", ".cards", "corpus directory (holds ir.gob.gz / cardsfolder)")
	profile := flag.String("profile", "", "path to a cast-profile weights JSON (schema {\"version\":1,\"cast\":{...}}) applied to any side named cast-profile; empty = the embedded default profile")
	checkpoint := flag.String("checkpoint", "", "path to a trained policynet checkpoint (L9c binary format), required by any side named policynet; no embedded checkpoint exists")
	flag.StringVar(&policynetKindsArg, "policynet-kinds", "attackers", "comma list of the decision kinds the policynet seat scores (attackers, priority, blockers, target); every other kind delegates to the default bot. Refused unless a side is policynet. priority and target are scored only on a ResidualW > 0 checkpoint and only in the trained distribution; blockers follows the attackers contract (seat.PolicyNetBot)")
	flag.StringVar(&policynetAdmission, "policynet-admission", seat.AdmissionAuto, "the policynet seat's subset (attackers, blockers) admission vote: auto (the default: the calibrated sign when a decision's scores straddle 0, else the decision's mean, refusing an exact tie) or sign (score > 0 alone, the BCE head's calibrated vote; ticket pn13)")
	flag.Float64Var(&policynetTemperature, "policynet-temperature", 0, "ticket pn14, collection only: > 0 makes every policynet seat SAMPLE its scored answers (softmax of score/T for priority and target; a per-option Bernoulli of sigmoid(score/T) for attackers and blockers) from a stream seeded by the game seed; 0 (default) keeps the deterministic argmax/vote seat")
	flag.StringVar(&oppMixArg, "opp-mix", "", "ticket pn14, matrix mode only: name:fraction[,name:fraction] -- per game (a pure function of the game seed), with these probabilities side B's seat plays another built-in policy (e.g. explore:0.3) instead of -b")
	flag.StringVar(&onpolicyCorpusPath, "onpolicy-corpus", "", "write every decision a policynet seat SCORED (encoded state and options, the scores, its answer and the bot's, the game outcome for that seat) as the on-policy PPO corpus JSONL to this new file (matrix mode only; parent must exist, destination must not). Observational: the bench result is unchanged")
	decisionStats := flag.Bool("decision-stats", false, "append a per-decision-kind histogram (count, mean per game, mean option count, singleton share, first-option share) at the end of a run; default off so the normal report is unchanged")
	actionCoverage := flag.Bool("action-coverage", false, "append the action-coverage completeness report (decision kinds / option rows never asked, offered-but-never-chosen shapes, cast shapes, cards and ability slots never fired, primitives never exercised) at the end of a run; default off so the normal report is unchanged")
	decisionTrace := flag.String("decision-trace", "", "write an opt-in atomic JSONL decision trace to a new file (matrix mode only; parent must exist and destination must not)")
	analyzeTrace := flag.String("analyze-trace", "", "read a decision trace and write deterministic diagnostic-proxy JSON; no games are played")
	grind := flag.String("grind", "", "grind mode: pin one repo deck to one goroutine and play it against itself as many games as the budget allows; a deck name, or \"all\" for every deck in the format's pool (one goroutine each); mutually exclusive with -pairs; -workers is ignored (the one-goroutine-per-deck shape IS the mode)")
	grindSeconds := flag.Float64("grind-seconds", 0, "grind wall-clock budget in seconds (checked between games, so at least one game always plays); 0 with -grind-iters 0 means the 30s default")
	grindIters := flag.Int("grind-iters", 0, "grind iteration cap per deck; 0 = wall-clock only")
	// The search seat's knobs: the teacher's own measured values are the flag
	// defaults (searchseat.Defaults pins the pairing), so `-a search` with no
	// -search-* flag plays exactly the teacher that was measured.
	searchWorlds := flag.Int("search-worlds", 8, "search policy: sampled worlds per searched decision (teacher default 8)")
	searchAttempts := flag.Int("search-attempts", 64, "search policy: sampler proposal attempts per searched decision (teacher default 64)")
	searchMinESS := flag.Float64("search-min-ess", 0, "search policy: ESS gate for resampling (0 = the calibration contract, teacher default)")
	searchMargin := flag.Float64("search-margin", 0, "search policy: mean-value margin a candidate must beat the bot by (teacher default 0)")
	searchHorizon := flag.Int("search-horizon", 0, "search policy: rollout horizon in engine turns (0 = game end, teacher default)")
	searchMaxSubmits := flag.Int("search-max-submits", 5000, "search policy: per-rollout and per-sample-attempt submit cap (teacher default 5000)")
	searchCandidates := flag.Int("search-candidates", 6, "search policy: max candidates per decision, bot answer first (teacher default 6)")
	searchParallelism := flag.Int("search-parallelism", 1, "search policy: goroutines WITHIN one searched decision; latency only, never changes an answer. Keep 1 when -workers already fills the cores; a live gorged seat may set 4")
	searchMana := flag.Bool("search-mana", false, "PN22: compare alternative bare mana-source taps when the bot taps at priority (opt-in)")
	searchRedeal := flag.Bool("search-redeal", false, "use the known-card-preserving redeal fallback when search's history sampler starves (opt-in)")
	flag.StringVar(&searchOracleCheckpoint, "search-oracle-checkpoint", "", "search policy, ticket pn17-a1: score non-terminal rollout leaves with this ORACLE value checkpoint (a mz-opphand model with a value head) read from the omniscient projection of each sampled world; needs -search-horizon > 0 and a search side. Empty (default) = the heuristic leaf, byte for byte")
	cpuprofile := flag.String("cpuprofile", "", "write a CPU profile to this pprof file over the whole run (empty = off)")
	memprofile := flag.String("memprofile", "", "write a heap profile to this pprof file after the last game finishes (pprof reads both alloc_space and inuse_space from it; empty = off)")
	flag.Parse()
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "policynet-kinds" {
			policynetKindsGiven = true
		}
	})
	decisionStatsEnabled = *decisionStats
	actionCoverageEnabled = *actionCoverage
	searchKnobs = searchseat.Options{
		Kinds:        map[string]bool{"attackers": true, "cast": true, "mana": *searchMana},
		Worlds:       *searchWorlds,
		Attempts:     *searchAttempts,
		MinESS:       *searchMinESS,
		Limit:        *searchCandidates,
		Margin:       *searchMargin,
		HorizonTurns: int32(*searchHorizon),
		MaxSubmits:   *searchMaxSubmits,
		SampleSeed:   54321,
		Parallelism:  *searchParallelism,
		Redeal:       *searchRedeal,
	}

	os.Exit(mainExit(*a, *b, *games, *seed, *seats, *rotate, *pairs, *format, *out, *workers,
		*maxTurns, *maxIntents, *dir, *profile, *decisionStats, *actionCoverage, *grind, *grindSeconds, *grindIters, *cpuprofile, *memprofile, *decisionTrace, *analyzeTrace, *checkpoint))
}

// mainExit is main's body with the exit code as its return, so the profiler
// (a defer) runs on EVERY exit path -- including a failed run, whose partial
// profile is still readable evidence -- instead of being skipped by the
// os.Exit calls a flag-error path used to make.
func mainExit(aName, bName string, games int, seed uint64, seats, rotate int, pairs, format, out string, workers,
	maxTurns, maxIntents int, dir, profile string, decisionStats, actionCoverage bool, grind string, grindSeconds float64, grindIters int, cpuprofile, memprofile, decisionTrace, analyzeTrace, checkpoint string) int {
	fail := func(err error) int {
		fmt.Fprintln(os.Stderr, "botbench:", err)
		return 1
	}
	// The -profile file is parsed before any game starts so a bad candidate
	// fails the run at the front door instead of mid-game: it applies to any
	// side named cast-profile (single-pair, matrix or grind), and an empty
	// path leaves the embedded default in place.
	if profile != "" {
		data, err := os.ReadFile(profile)
		if err != nil {
			return fail(fmt.Errorf("reading -profile %s: %w", profile, err))
		}
		w, err := botpolicy.ParseCastProfile(data)
		if err != nil {
			return fail(fmt.Errorf("-profile %s: %w", profile, err))
		}
		castProfileOverride = &w
	}
	if analyzeTrace != "" {
		if decisionTrace != "" || pairs != "" || grind != "" {
			return fail(fmt.Errorf("-analyze-trace cannot be combined with -decision-trace, -pairs, or -grind"))
		}
		if err := analyzeTraceFile(analyzeTrace, os.Stdout); err != nil {
			return fail(err)
		}
		return 0
	}

	// The -checkpoint weights are loaded once before any game starts -- the
	// full checkpoint gate (magic, schema version, encoder hash, geometry)
	// runs here, so a drifted checkpoint fails the run at the front door
	// instead of mid-game. A checkpoint is policynet's flag: required by
	// any side named policynet, refused for a run that names none.
	policynetSide := aName == "policynet" || bName == "policynet"
	if checkpoint != "" {
		m, err := policynet.LoadCheckpointFile(checkpoint)
		if err != nil {
			return fail(fmt.Errorf("-checkpoint %s: %w", checkpoint, err))
		}
		policynetModel = m
	}
	if policynetSide && checkpoint == "" {
		return fail(fmt.Errorf("policy policynet requires -checkpoint <path> (there is no embedded checkpoint)"))
	}
	if !policynetSide && checkpoint != "" {
		return fail(fmt.Errorf("-checkpoint was given but neither side is policynet"))
	}
	oracleKnobs, err := withSearchOracle(searchOracleCheckpoint, aName, bName, searchKnobs)
	if err != nil {
		return fail(err)
	}
	searchKnobs = oracleKnobs
	// -policynet-kinds is policynet's flag too: validated against the seat's
	// scored-kind vocabulary before any game starts, refused for a run that
	// names no policynet side.
	if policynetKindsGiven && !policynetSide {
		return fail(fmt.Errorf("-policynet-kinds was given but neither side is policynet"))
	}
	kinds, err := seat.ParsePolicyNetKinds(policynetKindsArg)
	if err != nil {
		return fail(fmt.Errorf("-policynet-kinds: %w", err))
	}
	policynetKinds = kinds
	if policynetAdmission != seat.AdmissionAuto && !policynetSide {
		return fail(fmt.Errorf("-policynet-admission was given but neither side is policynet"))
	}
	if err := (&seat.PolicyNetBot{}).SetAdmission(policynetAdmission); err != nil {
		return fail(fmt.Errorf("-policynet-admission: %w", err))
	}
	if policynetTemperature < 0 {
		return fail(fmt.Errorf("-policynet-temperature %g < 0", policynetTemperature))
	}
	if policynetTemperature > 0 && !policynetSide {
		return fail(fmt.Errorf("-policynet-temperature was given but neither side is policynet"))
	}
	if oppMixArg != "" {
		mix, err := parseOppMix(oppMixArg)
		if err != nil {
			return fail(err)
		}
		switch {
		case pairs == "" || grind != "":
			return fail(fmt.Errorf("-opp-mix requires -pairs"))
		case aName == bName:
			return fail(fmt.Errorf("-opp-mix needs distinct -a and -b (it replaces side B)"))
		}
		oppMix = mix
	}
	// -onpolicy-corpus records the policynet seats' scored decisions: it
	// needs a policynet side, the -pairs matrix and no decision trace (one
	// observer per run), and a fresh destination, checked before any game.
	if onpolicyCorpusPath != "" {
		switch {
		case !policynetSide:
			return fail(fmt.Errorf("-onpolicy-corpus was given but neither side is policynet"))
		case pairs == "" || grind != "":
			return fail(fmt.Errorf("-onpolicy-corpus requires -pairs"))
		case decisionTrace != "":
			return fail(fmt.Errorf("-onpolicy-corpus cannot be combined with -decision-trace"))
		}
		if err := checkOnPolicyDestination(onpolicyCorpusPath); err != nil {
			return fail(err)
		}
	}

	// The search seat's timing and diagnostic hooks are installed once, here,
	// before any game starts -- the clock is this command's (internal/
	// archtest allows it to cmd/botbench), the seat never reads one. A run
	// without a search side leaves the hooks nil: no capture-time cost and
	// no diagnostics.
	searchSide := aName == "search" || bName == "search"
	if searchSide {
		installSearchCostStats()
	}

	prof := &profiler{cpuPath: cpuprofile, memPath: memprofile}
	if err := prof.start(); err != nil {
		return fail(err)
	}
	// finish runs on every return below; a run that returns an error still
	// leaves its partial profiles on disk. A write failure is reported but
	// never masks the run's own error.
	defer func() {
		if err := prof.finish(); err != nil {
			fmt.Fprintln(os.Stderr, "botbench:", err)
		}
	}()

	decisionStatsEnabled = decisionStats
	actionCoverageEnabled = actionCoverage

	// Grind mode first: it is its own shape (one deck, one goroutine, budget
	// driven) and refuses to mix with the pair matrix.
	if grind != "" {
		if decisionTrace != "" {
			return fail(fmt.Errorf("-decision-trace requires -pairs and cannot be used with -grind"))
		}
		if pairs != "" {
			return fail(fmt.Errorf("-grind and -pairs are mutually exclusive (grind pins one deck to one goroutine; the matrix fans pairs over the pool)"))
		}
		if workers != 0 {
			// Say it loudly rather than silently swallowing the flag: the
			// one-goroutine-per-deck shape IS the mode, and a profile that
			// mixed pool-scheduled games would not be a grind profile.
			fmt.Fprintln(os.Stderr, "botbench: -workers is ignored in -grind mode (one goroutine per deck is the point of the mode)")
		}
		if err := runGrind(seed, grind, grindSeconds, grindIters, dir, format, maxTurns, maxIntents, os.Stdout, os.Stderr); err != nil {
			return fail(err)
		}
		return 0
	}

	commander, err := parseGameFormat(format)
	if err != nil {
		return fail(err)
	}

	// The deck pool is the format's: a Commander run seats only commander
	// decks, so -pairs all and -seats both expand over that pool rather than
	// dealing a 100-card Commander list as a constructed pile.
	var deckPool []string
	if commander {
		deckPool, err = commanderDeckNames()
		if err != nil {
			return fail(err)
		}
	} else {
		deckPool = testutil.RepoDeckNames()
	}

	if pairs != "" {
		if rotate != 0 {
			return fail(fmt.Errorf("-rotate applies to the single-run bench only, not -pairs (a 2-seat pair already plays both seatings)"))
		}
		var coverage *coveragePlan
		var ps []pairDef
		if pairs == "coverage" {
			if out != "text" {
				return fail(fmt.Errorf("-pairs coverage requires -out text so its static coverage metric is present"))
			}
			reg, err := testutil.OpenCorpusRegistry(dir)
			if err != nil {
				return fail(fmt.Errorf("opening corpus: %w", err))
			}
			plan, err := coverageDeckPlan(reg, deckPool)
			if err != nil {
				return fail(err)
			}
			plan.Baseline, err = deckPrimitiveCoverage(reg, deckPool[:2])
			if err != nil {
				return fail(err)
			}
			coverage = &plan
			ps = fullPairs(plan.Names)
		} else {
			ps, err = parsePairsForMode(pairs, deckPool, commander)
			if err != nil {
				return fail(err)
			}
		}
		if err := runMatrixTraced(seed, games, seats, aName, bName, dir, out, ps, workers, maxTurns, maxIntents, commander, coverage, decisionTrace, os.Stdout, os.Stderr); err != nil {
			return fail(err)
		}
		if searchSide {
			fmt.Fprint(os.Stdout, searchCostReport(games*len(ps)))
		}
		return 0
	}
	if decisionTrace != "" {
		return fail(fmt.Errorf("-decision-trace requires -pairs"))
	}
	if err := run(seed, games, seats, rotate, workers, aName, bName, dir, maxTurns, maxIntents, commander, os.Stdout); err != nil {
		return fail(err)
	}
	if searchSide {
		fmt.Fprint(os.Stdout, searchCostReport(games))
	}
	return 0
}
