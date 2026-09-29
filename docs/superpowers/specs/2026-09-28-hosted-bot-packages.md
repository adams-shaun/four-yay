# Hosted bot packages and live-table search seats — design and ticket plan (2026-09-28)

**Status:** design of record for the "hosted bot packages" series. Nothing here
is implemented.
**Base:** `origin/spellbench-prep` at `f297a3123dde0f51a5214e8518508165dc3b2332`.
Every `path:line` below was read at that SHA. It is about to land on `main` via
agentctl ticket `cli-20260928T193741Z-5822d0cd`, and every ticket in §11
depends on that ticket.
**Notation:** a claim not read from code or a report is marked **INFERRED**.
A ticket must measure or test an INFERRED claim before it builds on it.

Related documents: the first policy-selection design
(`docs/superpowers/specs/2026-09-19-hosted-bot-policy-selection-design.md`)
and its report (`docs/superpowers/reports/2026-09-19-hosted-bot-policy-selection.md`),
the AZ spec (`docs/superpowers/specs/2026-09-27-alphazero-mcts-design.md`),
the SpellBench agent design (`docs/superpowers/specs/2026-09-28-spellbench-agent-design.md`,
cited below as **SB**), and the training summary
(`docs/superpowers/reports/2026-09-24-training-approaches-summary.md`, cited as **TS**).

---

## 0. Operator decisions (settled; restated, not reopened)

1. **Lineup.** Every honest bot is selectable. Each entry is labelled
   production or experimental and carries its measured strength and think
   time. Clairvoyant policies (`az` with `-az-world clairvoyant`) are never
   hostable.
2. **Packaging.** Each bot is one public package under `bots/<name>/` in the
   gorge module (one `go.mod`), and it self-registers with a small `bots`
   registry. gorged links the set, and a table picks an entry by name.
   `host.NormalizeBotPolicy`'s closed vocabulary becomes the registry's
   closed set. botbench consumes the same registry.
3. **Sequencing.** Every ticket builds on `main` after spellbench-prep lands.

## 1. The key design calls (summary)

1. **A `bots` registry** (`bots/registry.go`) holds `Entry{Info, New}`.
   `Info` carries the name, label, description, tier, measured strength with
   its setting and source, cost, formats, max seats and the caretaker policy.
   `host.NormalizeBotPolicy` delegates to `bots.Normalize`. **Registered**
   (a build-time closed set) is kept separate from **offered** (gorged's
   `-bot-policies` subset). Only offered names gate new vs-bot tables.
2. **Honest root.** The host never hands a bot the live engine, or a clone of
   it. A seat that needs engine access (search, and the SpellBench payment
   planner) gets an *honest root* instead. This is one `searchprobe.Redealer`
   deal of the live position, built under the match lock from the seat's own
   observation feed, so the only engine a hosted bot ever touches has its
   hidden cards redealt. If the redeal refuses, the seat gets no root and
   plays its fallback, and the refusal is counted.
3. **The host owns the observation feed.** It keeps one `searchseat.Feed` per
   engine-aware bot seat. It captures a frame at every decision of every
   player inside `projectNext` (under `m.mu`), and records the *accepted*
   intent after a successful `Submit`. The feed is derived state and is never
   persisted. Undo rebuilds it with a new `replay.Walk`, and a future resume
   (M5) would rebuild it the same way.
4. **The known-card tracker moves into the feed** (`Feed.Known()`), so it has
   one owner that the host can rebuild after undo. azmcts and sbsearch stop
   keeping their own.
5. **azmcts is split.** The clairvoyant world moves to
   `internal/azmcts/clairvoyant`, which bench and training still link and
   which stays banned from host, httpapi, gorged and `bots/...`. The honest
   core and `RedealSource` become linkable. `SeatConfig.Source` injects the
   clairvoyant factory, and only botbench passes it.
6. **Refusal ladder.** SpellBench builtins can produce an answer the engine
   refuses. The ladder that botbench's SpellBench runner uses today (the
   seat's retry, then the minimal answer, then the default bot) moves into
   `bots` and the host runs it. Today any bot refusal crashes a hosted match.
7. **CPU.** A FIFO search-slot semaphore (`-bot-search-slots`) bounds
   concurrent searched decisions, and `-max-search-tables` bounds live search
   tables at creation. When saturated, decisions queue. The host never
   substitutes another policy and never cuts a search on the clock: budgets
   are simulation, world and step counts only.
8. **Determinism.** A hosted policy's intents are a pure function of (entry,
   per-seat seed, the table's intent stream). Replay never consults a bot,
   because the intent log is the record. SB §12.6 recommends a wall-clock
   per-decision cap for a live seat, and that recommendation is rejected for
   hosting.
9. **Labels say what was measured.** Almost every number comes from gorge
   botbench against `bot`, either on the five-deck mono suite or on the Pauper
   kernel / FDN mirror pools. None was measured in hosted Legacy or Commander
   play. `lethal-pressure` and `cast-profile` are labelled as what they now
   are: aliases of the manual bot, and weaker than the hosted auto-pay bot.
10. **The listing is wire data.** `GET /api/bot-policies` returns
    `protocol.BotPolicyList`, generated to TypeScript by `cmd/gentypes`. The
    landing page gets a policy picker (a UI ticket, for codex or by hand).

---

## 2. Current state (verified at `f297a3123`)

- **Hosted vocabulary.** `host/bot_policy.go:24-37`: `NormalizeBotPolicy`
  maps `""` to `bot`, accepts `bot`, `lethal-pressure` and `cast-profile`,
  and rejects everything else. `NewBotPolicySeatWithAutoPayMana`
  (`host/bot_policy.go:67-98`) builds `seat.NewAttackSimBot(...)`
  `.EnableAutoPayMana()` for `bot` on auto-pay tables, the arm promoted in
  `66aa546dd`. `host/bot_policy_az_test.go:10`
  (`TestHostedVocabularyRefusesSearchPolicies`) pins that `az`, `search` and
  `policynet` are refused.
- **Seat construction in the host.**
  - `host/match.go:472` builds every bot seat through
    `defaultSeatsWithAutoPayMana(t.cfg.BotPolicy, ...)`.
  - `host/match.go:500` builds each human seat's timeout caretaker from the
    same policy (`newCaretakerSeat`, `host/bot_policy.go:51-60`).
  - `projectNext` (`host/match.go:366`) runs under `m.mu`. It builds either a
    `botpolicy.Board` (for a `seat.BoardSeat`) or a `view.View`.
  - `parkSeat` (`host/match.go:429`) runs the seat outside the lock.
  - A refused intent crashes the match (`host/match.go:630`).
- **Undo.** `rewindToLastIntent` (`host/undo.go:241-285`) rebuilds the engine
  with `replay.ReplayTo` and swaps it in. Bot seats keep their state. The
  comment at `host/undo.go:32-35` says bots "re-decide against the rewound
  state".
- **Restart.** Restart aborts live matches ("resume is M5",
  `host/restart.go:27-30`). On-demand vs-bot tables are dropped at load
  (`host/restart.go:62-70`). Only perpetual startup tables reload, and their
  `BotPolicy` is unset and normalizes to `bot` (`cmd/gorged/main.go:544-546`).
- **vs-bot create.**
  - httpapi normalizes `bot_policy` *before* the builder
    (`host/httpapi/rest.go:424`), so an omitted field reaches gorged as `bot`.
  - gorged normalizes again (`cmd/gorged/game.go:37`) and builds a 2-seat
    table with `Humans: []int{0}` (`cmd/gorged/game.go:96-101`).
  - `TestCreateGameBotPolicyDecodeAndRejectsDiagnostic`
    (`host/httpapi/game_test.go:119`) pins that `{}` reaches the builder as
    `bot`.
- **Search seats need a driver feed.** `searchseat.SearchSeat` and `Env`
  (`internal/searchseat/searchbot.go:61-84`) take `Engine *rules.Engine` plus
  a driver-owned `*Feed`. `internal/bench/bench.go:156-169` builds one feed
  per search seat. `internal/bench/bench.go:212-251` captures through every
  feed at every decision, calls `DecideSearch` with `Env{Engine: e}` (the
  **real** engine), and records the answer. botbench's `search` entry
  comment (`cmd/botbench/main.go:248-263`) records that "a live table has no
  driver to feed the seat a history yet".
- **The real engine inside the honest paths.**
  - `searchseat.Choose` reads `e` for the turn (`teacherSeed`,
    `internal/searchseat/searchseat.go:432`), for the actor's blockers board,
    and, with `Options.Redeal`, as the redeal base
    (`internal/searchseat/searchseat.go:549`).
  - `azmcts.Root.Engine` "is the real engine" (`internal/azmcts/search.go:15-19`).
  - `RedealInput.Base` (`internal/azmcts/redeal.go:36`) and
    `searchprobe.RedealBase` (`internal/searchprobe/redeal.go:40`) read the
    base engine for public state, zone sizes and the hidden objects to permute.
  - `searchprobe.(*Redealer).Deal` (`internal/searchprobe/redeal.go:310`)
    clones the base with `CloneHypotheticalInto` and moves objects between
    hand and library (`redealPlayer`, `internal/searchprobe/redeal.go:331-394`).
  - `TestRedealWorldsIgnoreTheRealHiddenCards`
    (`internal/azmcts/redeal_test.go:208`) is the leak test: swap the hidden
    cards, reverse both libraries, and the same seeds must deal name-identical
    worlds.
- **Clairvoyant ban.**
  - `internal/archtest/arch_test.go:152-158` forbids `host`, `host/httpapi`
    and `cmd/gorged` from depending on `internal/azmcts`.
  - `azmcts.NewClairvoyant` / `AllowClairvoyant` (`internal/azmcts/world.go:29-72`)
    gate the clone at runtime.
  - `azmcts.Seat.DecideSearch` itself calls `NewClairvoyant` when
    `SeatConfig.World` is `""` or `clairvoyant` (`internal/azmcts/seat.go:170-182`).
  - botbench's `azFrontDoor` is the only non-test `AllowClairvoyant` caller
    (`cmd/botbench/azcost.go:114-116`).
- **The SpellBench builtins need the runner's help.**
  - `builtins.Seat.SetPlanner` takes a `Planner`, which `*rules.Engine`
    implements (`internal/spellbench/builtins/builtins.go:348-354, 420-422`).
    The runner hands it the live engine (`cmd/botbench/spellbench.go:327`).
  - `PotentialPaymentPlans` and `PotentialPlayScript` open derived-memo
    scopes on the engine (`rules/potential_plan.go:88-97`,
    `rules/potential_script.go:473-494`). They must not run on the live
    engine outside `m.mu`.
  - The runner's `sbSubmitWithFallback` (`cmd/botbench/spellbench.go:243-285`)
    retries a refused builtin answer: `Seat.Refused`, then pass or `Clamp`,
    then `botpolicy.Decide`.
  - A refused `Submit` preserves the pending decision and records nothing
    (`rules/engine.go:3754-3820`, "preserve-and-reject").
  - `sbsearch.Seat.DecideBoard` type-asserts its planner to `*rules.Engine`
    and projects a view from it (`internal/spellbench/sbsearch/sbsearch.go:219-231`).
- **Process-global diagnostic hooks.** `searchseat.Millis/Watch`
  (`internal/searchseat/searchbot.go:102,109`), `azmcts.Millis/Watch`
  (`internal/azmcts/seat.go:72,78`) and `sbsearch.Millis/Watch`
  (`internal/spellbench/sbsearch/sbsearch.go:141,146`) are nil unless a
  command installs them. gorged must never install them.
- **Clock rule.** `TestTimeIsImportedOnlyByTheHost`
  (`internal/archtest/arch_test.go:109`) forbids `time` in any `bots/...`
  package. D6's compile-time half, `TestNoExportLeaksAnEngineGame`
  (`internal/archtest/arch_test.go:188`), scans the host and httpapi exported
  signatures. `TestNoRegistryDataResultIsAnEngineGame`
  (`host/leak_test.go:65`) is D6's runtime half.

---

## 3. The lineup and the honesty census (D)

### 3.1 Hostable entries

"Measured" is always *gorge botbench*, never hosted play. Two deck settings
recur below:

- **"mono suite"** is the five `internal/testutil/decks/mono-*` decks: ten
  unordered pairs, held-out seed 1,000,000, 400 games per pair, seats traded
  (TS header; `docs/superpowers/reports/2026-09-19-bot-policy-decision-trace-and-ar7.md`).
- **"Pauper kernel"** is SB's eight-deck seat-swapped mirror pool on the gorge
  engine (`botbench -spellbench`), rated by SpellBench's Bradley-Terry code.

The hosted constructed pool (every non-commander deck under `-decks`) and
every Commander deck are unmeasured for every entry below.

| name | tier | measured strength (what, against whom, where) | cost (think time) | caretaker | formats | seats |
|---|---|---|---|---|---|---|
| `bot` | production | The baseline every other row is measured against. On auto-pay tables (gorged's `-bot-auto-mana` default) it plays the attack-sim arm: 53.17% [52.68, 53.65] constructed and 53.69% [52.73, 54.64] commander vs the plain auto-pay bot, held-out (commit `66aa546dd` message; `host/bot_policy.go:88-96`). | Not measured per decision. `bot` vs `bot` plays ~550k games/h on 8 workers (SB §12.5 table). **INFERRED** < 1 ms/decision. | self | both | any |
| `lethal-pressure` | experimental | AR7: 51.65% [50.10, 53.20] vs the pre-AR7 bot, mono suite, manual mana (AR7 report "Held-out gate"). Promoted into the default Decide in `9be522522`, so on manual tables it plays as `bot`. On auto-pay tables it lacks the attack-sim arm, which makes it the `bot-auto-pay` arm that hosted `bot` beat by +3.17pp. | as `bot` | `bot` | both | any |
| `cast-profile` | experimental | With the embedded default profile it is intent-identical to the manual bot (`cmd/botbench/main.go:188-192`; `seat.NewCastProfileBot`, `seat/bot.go:163`). The fitted L4 profile failed its gate at 50.92% [49.38, 52.47] (TS §2) and is not embedded (**INFERRED**: `botpolicy.DefaultCastProfileName` is the baseline). Same auto-pay caveat as `lethal-pressure`. | as `bot` | `bot` | both | any |
| `search` (L10 PIMC teacher) | experimental | 53.7% [52.2, 55.2] vs a 50.4% same-seed control, +3.3pp, held-out, mono suite, manual mana. The edge is early-game only: coverage is 64.5% on turns 1-6 and 4.8% on turns 13+ (TS §4). | 664 ms mean, p95 2.2 s per searched decision at parallelism 1. With `-decision-workers`, latency is 171 ms (TS §4). | `bot` | constructed | 2 |
| `az-redeal` (gen 0: uniform prior, heuristic leaf, 100 sims, fresh deal per sim) | experimental | az100 redeal 70.5% [68.5, 72.5] vs `bot`, +20.5pp over the control, 2,000 games, `uw-tempo` vs the 5 mono decks, true lists visible (SB §12.5). On the Pauper kernel, `az-redeal-sims25` rated 1382 vs `bot` 1264 (SB §12.5 round robin). | 216-278 ms mean uncontended, p95 380-550 per searched decision at 100 sims (SB §12.5) | `bot` | constructed | 2 |
| `sb-tactical` | experimental | vs `bot`: 110-18 on the Pauper kernel (benchmark seed), 106-22 (seed 99991), 87-41 on FDN mirrors. Elo 1507 vs `bot` 1242 (SB §3.4 "Measured" table). | ~80 ms per *game* vs `bot` on 8 workers (SB §3.4). **INFERRED** ~1 ms/decision. | `bot` | constructed | 2 |
| `sb-search-lite-atk` | experimental | 328-184 (64.1%) vs `sb-tactical`, Pauper kernel, seed 777, 512 games (SB §12.6). Never measured vs `bot` directly. | 219 ms mean, p90 448, p99 1,108 per searched decision, box at load 14-20 (SB §12.6) | `bot` | constructed | 2 |
| `sb-heuristic` | experimental | Weaker than `bot`: Pauper kernel 1078 vs 1242 (194-446), FDN 1103 vs 1327 (SB §3.4; `docs/superpowers/reports/2026-09-28-spellbench-fdn-limited.md` "First FDN baseline") | negligible | `bot` | constructed | 2 |
| `sb-uniform` | experimental | Anchor 1000 vs `bot` 1238 on the Pauper kernel (SB §3.1 W1 table) | negligible | `bot` | constructed | 2 |
| `sb-first` | experimental | 708 vs `bot` 1238 on the Pauper kernel (SB §3.1 W1 table) | negligible | `bot` | constructed | 2 |

Notes:

- **Why only `bot` is production.** L10's promotion is an operator decision
  not yet taken (TS §4). Every other search or SpellBench number was
  measured in a different setting from the one hosting will expose.
- **Why constructed only and 2 seats for the new entries.** Their
  measurements are all 1v1 constructed. `sbsearch` searches only 2-player
  games (`internal/spellbench/sbsearch/sbsearch.go:300`, `len(e.G.Players) != 2`).
  `searchprobe.PublicGame` has no format or commander field
  (`internal/searchprobe/sample.go:24-29`), so L10's sampler cannot rebuild a
  Commander genesis. **INFERRED** it would fail every sample and fall back
  to the bot. See open question Q3.
- **The config is exactly botbench's.** Each search entry's hosted config
  equals botbench's default flag config for the same name:
  - `search` = `searchseat.Defaults()` (`internal/searchseat/searchseat.go:174-186`).
  - `az-redeal` = `azmcts.DefaultSeatConfig()` with `World=redeal`,
    `Sims=100` and `Worlds=0` (`cmd/botbench/azcost.go:26,44,53,125-131`).
  - `sb-search-lite-atk` = the `sbSearchVariants` row
    (`cmd/botbench/spellbench_registry.go`, Worlds 4, Horizon 2, Attack).

  "Hosted `X`" therefore means "`botbench -a X` with default flags", apart
  from the honest-root difference in §5.3.
- **Labels are refreshed by measurement.** BP-20 re-measures every search
  entry through the hosted path (`-hosted-root`) and updates the labels.

### 3.2 Not hostable, and why

| name(s) | where | reason |
|---|---|---|
| `az` with `-az-world clairvoyant`, and `az` with no world | `cmd/botbench/main.go:274`, `cmd/botbench/spellbench_registry.go` | Clones the real engine. Never hostable (operator decision 1). |
| `az` with `-az-world prior` (PriorOnly), and `policynet` | `internal/azmcts/seat.go:32-38`, `cmd/botbench/main.go:228` | Need a `-checkpoint`, and none is embedded. At gen 0 PriorOnly *is* the bot. Measured flat: TS §5 (46-47% vs 50.1%) and SB §16. |
| `legacy`, `explore`, `ar8`, `blocks`, `attack-sim`, `bot-auto-pay`, `attack-sim-auto-pay` | `cmd/botbench/main.go:144-260` | Diagnostics or collection bots, or arms inside noise (TS §1). The two auto-pay arms are what `bot` already plays. |
| `sb-tactical-{planned,noearly,notiming,norace,arch,alt,alt2..alt8}`, the `sb-*-manual` and `sb-*-planned` arms, the other `sb-search*` budgets | `cmd/botbench/spellbench_registry.go`, `internal/spellbench/registry/sb.go` | Tuning, ablation or budget arms of a hosted entry, not distinct bots. They are honest, so the operator can add one later as a new registry entry. |
| decorators `burn`, `curve`, `lethal`, `mull`, `passguard` | `internal/spellbench/registry/*.go` | Honest, but they are wrappers rather than bots, and all four heuristic decorators measured neutral on `sb-tactical` (SB §12.6). |
| `sbv1-tactical` (tac9), `shadow-tactical`, `shadow-az`, `shadow-roll`, **the arbiter** (`-roll-arbiter`), `shadow-route`, `shadow-sbsearch` / `-arbsearch`, and the v2 protocol agent | `cmd/sbv1agent/main.go:60-100`, `internal/spellbench/kshadow/policy.go` (imports `v1agent`), `internal/spellbench/v2agent` | **Kernel-shadow only.** They answer mtg-kernel or SpellBench protocol decisions from the kernel's `x_kernel_v5` observation. The arbiter compares `v1agent.Tactical`'s kernel pick with `sb-tactical`'s (SB §3.6 table). None of them is a gorge `seat.Seat`. |

---

## 4. The registry API

### 4.1 Package `bots` (`bots/registry.go`, `bots/env.go`, `bots/refusal.go`)

```go
package bots

// Tier is an entry's release status.
type Tier string

const (
	Production   Tier = "production"
	Experimental Tier = "experimental"
)

// Default is the policy an omitted or empty name normalizes to. It is fixed
// at "bot" forever, because tables persisted before policy selection existed
// must keep their behaviour. gorged's -bot-policy default is applied by the
// vs-bot builder, never here.
const Default = "bot"

// Measurement is one measured claim and where it came from. The UI shows
// Claim and makes Setting and Source reachable.
type Measurement struct {
	Claim   string // "53.7% [52.2, 55.2] vs bot (+3.3pp over a 50.4% control)"
	Versus  string // "bot"
	Setting string // "gorge botbench, mono suite, held-out seed 1,000,000, 400 games/pair, manual mana"
	Source  string // repo-relative doc path plus section: "docs/superpowers/reports/...md §4"
}

// Cost is the measured think time.
type Cost struct {
	MeanMS, P95MS float64 // 0 = not measured (Note says why)
	Scope         string  // "per searched decision" | "per decision"
	Note          string  // "~85 searched decisions per game per seat", "not measured; INFERRED <1 ms"
	Source        string
}

type Info struct {
	Name        string // registry key and wire name, [a-z0-9-]+
	Label       string // short UI name
	Description string // one or two sentences
	Tier        Tier
	Strength    []Measurement
	Cost        Cost
	// Env: the host keeps an observation feed for this seat and builds an
	// Env for its decisions (§5). Search: its decisions take a search slot,
	// and its tables count toward -max-search-tables (§7).
	Env, Search bool
	// Formats lists the host.Format names it may be offered in
	// ("constructed", "commander"). MaxSeats is the largest table it may sit
	// at (0 = any).
	Formats  []string
	MaxSeats int
	// Caretaker is the entry that answers a timed-out human seat's decision
	// on a table playing this policy. "" means the entry itself. It must
	// name an entry whose Env is false.
	Caretaker string
}

// Options are what a factory receives per seat.
type Options struct {
	Seed        uint64 // the host's per-seat seed (match seed ^ (slot+1))
	AutoPayMana bool   // the table's auto-pay setting (host: cfg.autoPayManaEnabled())
	// SearchParallelism is the number of goroutines within one searched
	// decision. It changes latency only, never an answer
	// (searchseat.Options.Parallelism). 0 and 1 mean sequential.
	SearchParallelism int
	Deps              Deps
}

// Deps are the process-wide read-only inputs a factory may need.
type Deps struct {
	Cards *cards.Registry // the corpus (sb-* need a card lookup); nil = refuse
}

type Factory func(Options) (seat.Seat, error)

type Entry struct {
	Info
	New Factory
}

func Register(e Entry)                          // from init; panics on a duplicate, an empty name, a bad Tier or no factory
func Lookup(name string) (Entry, bool)
func Normalize(name string) (string, error)     // "" -> Default; unregistered -> error listing Names()
func New(name string, o Options) (seat.Seat, error)
func Names() []string                           // sorted
func Entries() []Entry                          // production first, then by Name
func Validate() error                           // each Caretaker resolves to a registered non-Env entry
```

`bots` imports `seat`, `cards`, `decision`, `view`, `botpolicy`, `rules` and
`internal/searchseat` (for `Env`). It must never import `host`, `protocol`,
`time`, `internal/testutil` or `internal/azmcts/clairvoyant` (§6.2).

### 4.2 Engine-aware seats and the refusal ladder

```go
// bots/env.go

// EnvSeat is a hosted seat that answers some decisions from host-built
// inputs richer than a View or Board. The host detects the interface; an
// Entry with Env set must build one.
type EnvSeat interface {
	seat.Seat
	// WantsEnv reports whether d should get an Env. It must be a pure
	// function of d; false costs the host no redeal.
	WantsEnv(d *decision.Decision) bool
	// DecideEnv answers from env. It must not retain env.Search.Engine or
	// env.Search.Feed past its return.
	DecideEnv(ctx context.Context, env Env, d decision.Decision) (decision.Intent, error)
}

// Env is one decision's host-built input.
type Env struct {
	View  view.View       // the actor's projection, exactly what a plain Seat gets
	Board botpolicy.Board // the actor's board (BoardFromGameInto)
	// Search.Setup is the declared public game. Search.Engine is the HONEST
	// ROOT (a hypothetical redeal of the live position), or nil when the
	// redeal refused. Search.Board equals Board. Search.Feed is the actor's
	// feed, read-only.
	Search      searchseat.Env
	RootRefused string // why Search.Engine is nil ("" when it is set)
}

// RootSeed derives the honest root's deal seed from the per-seat seed and
// the decision sequence number.
func RootSeed(seatSeed, seq uint64) [2]uint64
```

```go
// bots/refusal.go

// RefusalAnswerer is a seat whose answers the engine may refuse (a
// whole-declaration constraint the options do not publish). The host asks it
// once again before the shared fallbacks.
type RefusalAnswerer interface {
	AnswerRefused(v view.View, d decision.Decision, refused decision.Intent) decision.Intent
}

// Fallbacks are the ladder's non-seat rungs, in order: the minimal answer
// (pass at priority, else botpolicy.Clamp of the empty answer), then
// botpolicy.Decide on brd with PCG(d.Seq, seatIdx+1). It is exactly
// cmd/botbench/spellbench.go:262-283 lifted out, and deterministic.
func Fallbacks(d *decision.Decision, brd botpolicy.Board, seatIdx int) []decision.Intent
```

### 4.3 Packages

| package | registers | wraps |
|---|---|---|
| `bots/bot` | `bot` | `seat.NewBot`, or `seat.NewAttackSimBot(...).EnableAutoPayMana()` on auto-pay tables (today's `host/bot_policy.go:88-98`) |
| `bots/lethalpressure` | `lethal-pressure` | `seat.NewLethalPressureBot` (+`EnableAutoPayMana`) |
| `bots/castprofile` | `cast-profile` | `seat.NewCastProfileBot` (+`EnableAutoPayMana`) |
| `bots/search` | `search` | `searchseat.NewSearchBot(seed, Hosted())`, with `Hosted()` equal to `searchseat.Defaults()` plus `Parallelism` |
| `bots/azredeal` | `az-redeal` | `azmcts.NewSeat(seed, nil, Hosted())` |
| `bots/sbtactical` | `sb-tactical` | `builtins.NewTactical(AutoPay, seed, NewRegistryLookup(Deps.Cards), DefaultTacticalWeights())` |
| `bots/sbsearch` | `sb-search-lite-atk` | `sbsearch.New(<sb-tactical>, seed, LiteAtk())` |
| `bots/sbheuristic`, `bots/sbuniform`, `bots/sbfirst` | `sb-heuristic`, `sb-uniform`, `sb-first` | `builtins.New(<policy>, AutoPay, seed[^UniformSeed])` |
| `bots/all` | — | blank-imports every package above. gorged and botbench import it. |

Every search adapter exports `Hosted() <Config>` and
`New(o bots.Options, cfg <Config>) seat.Seat`, so botbench can overlay its
flags on the hosted config (§8.3). `host` imports only `bots` and `bots/bot`,
which guarantees the default always resolves. host tests that need other
entries blank-import them in a `_test.go` file.

---

## 5. A — the live-table search feed

### 5.1 What the seat receives

At an `EnvSeat`'s decision, only if `WantsEnv(d)` is true, the host builds
`bots.Env` under `m.mu`:

- **`View`** is `view.Project(m.e.G, m.e, d.Player, &dc)` with `Round` set.
  This is exactly what a plain seat gets from `projectNext`
  (`host/match.go:410-415`).
- **`Board`** is `botpolicy.BoardFromGameInto(m.e.G, m.e, d.Player, brd)`
  (`host/match.go:406`).
- **`Search.Setup`** is `searchprobe.PublicGame{Names: m.cfg.Names, Decks: m.cfg.Decks,
  Tokens: m.cfg.Tokens, StartingLife: m.cfg.StartingLife}`, the same value
  `internal/bench/bench.go:164-166` builds. This is **open decklist**: each
  seat's deck id is already public on the wire (`protocol.SeatInfo.Deck`,
  `host/match.go:142`), and every hosted deck is a repo file. Q6 covers
  user-imported lists.
- **`Search.Feed`** is the actor's feed, captured through this decision (§5.2).
- **`Search.Engine`** is the **honest root**:
  `searchseat.HonestRoot(setup, feed, m.e, bots.RootSeed(seatSeed, d.Seq))`.
  It is a new helper (BP-04) that prepares
  `searchprobe.NewRedealer(setup, feed.History(), feed.Known(), RedealBase{m.e, feed.Collector()})`
  and returns one `Deal`. `Deal` itself checks that the dealt world projects
  to the same board and decision as the actor's last frame
  (`internal/searchprobe/redeal.go:318-324`). A refused or failed deal
  returns `nil` plus a reason, which becomes `RootRefused`.

The seat runs `DecideEnv` outside `m.mu`, as `parkSeat` runs every seat today.
It owns the honest root for the duration of the call, with no aliasing with
the live engine and no race with fan-out projections. The planner calls
(`PotentialPaymentPlans` / `PotentialPlayScript`) mutate the root's derived
memo, and that is safe because nothing else holds the root.

Adapter rules:

- **`search`, `az-redeal`, `sb-search-lite-atk`** forward `env.Search` to the
  wrapped `DecideSearch` when `env.Search.Engine != nil &&
  env.Search.Feed.Live()`.
  - Otherwise `search` and `az-redeal` play `DecideBoard(env.Board)`: the
    wrapped bot, exactly the bench's fallback (`internal/bench/bench.go:246-248`).
  - `sb-search-lite-atk` instead plays the *inner* `sb-tactical` on
    `env.View` with the planner set to nil. It does **not** call
    `sbsearch.Seat.DecideBoard`, which would project a view from a stale
    planner engine (`internal/spellbench/sbsearch/sbsearch.go:219-227`).
- **`sb-tactical`, `sb-heuristic`, `sb-uniform`, `sb-first`** call
  `SetPlanner(env.Search.Engine)` when the root is non-nil and
  `SetPlanner(nil)` otherwise. The nil must be explicit, because a nil
  `*rules.Engine` inside the `Planner` interface is non-nil. They then call
  `Decide(ctx, env.View, d)`.
- Every adapter implements `seat.BoardSeat` (or leaves the plain View path
  in place) for decisions where `WantsEnv` is false, so those cost what they
  cost today.
- `WantsEnv` per adapter:
  - `search`: `searchseat.Eligible(d, opts)`.
  - `az-redeal`: the searched kinds (priority, attackers, blockers, target).
  - `sb-search-lite-atk`: priority or attackers.
  - the sb-* builtins: priority (the planner is read at priority only;
    **INFERRED** from `internal/spellbench/builtins/builtins.go:72-90`, and
    BP-13 verifies it).

### 5.2 The feed's lifecycle (host-owned, derived, never persisted)

- **Create.** At the top of `play`, after `seats` is final, the host creates
  one `searchseat.NewFeed(slot)` for each non-human seat that implements
  `bots.EnvSeat`. A match with no such seat builds no feed and pays nothing,
  mirroring `internal/bench/bench.go:152-156`.
- **Observe.** At the start of `projectNext`, for every decision of every
  player (the human's included), the host calls `feed.Observe(m.e)` on every
  live feed while holding `m.mu`. The sampler's epoch constraints need every
  player's bursts (`internal/searchseat/feed.go:10-25`). A failed capture
  stops that feed (`Feed.Live()` goes false) and the seat plays its fallback
  from then on. A capture error never crashes the match.
- **Record.** Inside the Submit critical section, after a *successful*
  `m.e.Submit(in)`, the host calls `feed.RecordAnswer(&dc, in)` if the
  decision belonged to that feed's actor and this decision's capture
  succeeded. It must be the accepted intent, which may be a refusal-ladder
  rung (§5.5), not the seat's first answer. A `RecordAnswer` error stops the
  feed. The bench crashes instead (`internal/bench/bench.go:238-245`), but a
  live table plays on with the fallback, and the stop is counted.
- **Known cards.** `Feed.Known()` (BP-04) folds new frames lazily with the
  tracker moved from `azmcts.KnownTracker` (`internal/azmcts/redeal.go:162-200`).
  The fold semantics are unchanged: the answer to frame i is folded before
  frame i+1. `azmcts.KnownTracker` becomes a type alias.
  `azmcts.Seat.redealSource` (`internal/azmcts/seat.go:250-263`) and
  `sbsearch.Seat.redealer` (`internal/spellbench/sbsearch/sbsearch.go:670-679`)
  read `env.Feed.Known()` in place of their own trackers. The values are
  identical, so the bench numbers do not move.
- **Undo.** Right after `rewindToLastIntent` swaps in the replayed engine,
  under the same lock, the host rebuilds every feed:
  `searchseat.RebuildFeed(m.cfg, m.e.L, m.intents, actor)`.
  - `RebuildFeed` (BP-03) is `replay.Walk` (a new exported stepping form of
    `replay.run`, `replay/replay.go:196-264`). It calls `visit(e, i)` at every
    intent boundary before submitting intent i, and once at n.
  - Each visit Observes, and after the actor's own intents it records them.
  - The result equals the live feed truncated to that boundary. BP-03 pins
    this with `TestRebuildFeedMatchesTheLiveFeed`.
  - Cost is one full capture pass over the kept prefix while `m.mu` is held.
    **INFERRED** it is acceptable because undo is rare; BP-09 measures it.
  - A rebuild error crashes the match (D15), because the feed and the log
    must never disagree.
- **Persist and resume.** Nothing new is persisted. The feed is a pure
  function of (`rules.Config`, intent log prefix, actor). Today a restart
  aborts live matches (`host/restart.go:27-30`) and drops on-demand tables
  (`host/restart.go:62-70`), so no feed ever needs restoring. If M5 resume
  lands, it rebuilds feeds with the same `RebuildFeed`. It must also
  re-derive each bot seat's internal state by re-deciding the recorded prefix
  and checking that each re-decided intent equals the logged one; a mismatch
  aborts the match, never continues it. §8 is what makes that check possible.
- **Replay.** `replay.Replay` / `ReplayTo` re-submit the logged intents and
  never construct a seat. Feeds and roots play no part in replay.

### 5.3 Why the honest root, and what it does not cover

Handing the seat an `e.Clone()` would satisfy the lock discipline but give a
bot a structurally clairvoyant object. The honest root makes the host
boundary the enforcement point: whatever a hosted bot reads out of `G`, the
hidden cards in it are a uniform redeal from the actor's derivable pool.

The redeal permutes *objects* between hand and library. The permuted objects
keep their real card (`redealPlayer`, `internal/searchprobe/redeal.go:331-394`).
The root's **event log is a clone of the real log**, and its old `Secret`
events name object ids. So a consumer that read a past secret `MoveZone`,
dereferenced its `Obj` in `G`, and read the name would recover real hidden
history, for example what the opponent drew. Two facts bound this channel:

- No bot in the lineup reads the log for identities. The engine's own log
  reads are counts and public provenance.
- The bench, which produced every measured number, hands these same bots the
  **real** engine, so the hosted exposure is strictly smaller than the
  measured one.

The spec does not close the channel. It tests for its use: BP-08 adds the
seat-level leak test `TestHostedEnvSeatsIgnoreTheRealHiddenCards`, and
BP-11 to BP-15 add each entry to it. The test builds two live engines that
differ only in hidden cards, using the swap fixture of
`TestRedealWorldsIgnoreTheRealHiddenCards` (whose swap emits extra secret
events into the alternate log). It drives one decision of each hosted entry
through the host's Env path and requires identical intents. Redacting the
root's secret log is follow-up work (Q2).

**Hosted `search` and `az-redeal` vs bench numbers.** The bench roots its
search at the real engine and the host at the honest root:

- For `search` (Sample path, `Redeal` off), the engine is read only for the
  turn, the actor's board and the decision. **INFERRED** exact intent parity.
- For `sb-tactical`'s planner (the actor's own pool and sources),
  **INFERRED** exact parity.
- For `az-redeal` and `sb-search-lite-atk`, the nested redeal starts from a
  base whose hidden objects already sit in redealt positions. The deals are
  name-identical by the leak test's argument, but object ids at each position
  may differ, and rollout tie-breaks can depend on option order.
  **INFERRED**: parity is not guaranteed.

BP-05 therefore adds a `-hosted-root` botbench mode and measures intent
agreement. If the INFERRED-exact entries are not exact, BP-05 stops and
reports. For the other entries it reports the agreement rate, and BP-20
re-measures strength through `-hosted-root` so the labels describe the
hosted configuration. It also checks that a redeal from a redealt root
prepares at all (**INFERRED**; `NewRedealer`'s checks at
`internal/searchprobe/redeal.go:121-136` are the same ones `Deal` already
passed).

### 5.4 Enforcement

- **Compile-time.** `TestNoExportLeaksAnEngineGame` is unchanged: every new
  host export (`Options.SearchSlots`, `Options.MaxSearchTables`,
  `Options.BotDeps`) carries no engine type. The dependency rows in §6.2 also
  apply.
- **Runtime (BP-08): `TestHostedEnvSeatsNeverSeeTheLiveEngine`.** A test-only
  spy `EnvSeat` records `env.Search.Engine` at each decision. The test asserts:
  - the pointer is never `m.e` and never aliases `m.e.G`;
  - across a fixed-seed game, the root's opponent hand differs from the live
    opponent hand at least once, which proves a redeal happened;
  - no root is reused after `DecideEnv` returns.
- **Behavioural (BP-08, then each adapter).**
  `TestHostedEnvSeatsIgnoreTheRealHiddenCards`, described in §5.3.
- **Configuration (each adapter).**
  - `search`: `TestHostedSearchConfigIsHonest` asserts `Options.Clairvoyant`
    is false, `OracleValue` and `Value` are nil, and `Validate()` returns nil.
  - `az-redeal`: `TestHostedAZIsRedeal` asserts `World == WorldRedeal`, a nil
    net and `Explore` false.

### 5.5 The refusal ladder in the host (BP-06)

Today `host/match.go:626-634` returns `intent %d rejected` and crashes. New
behaviour applies only when the deciding seat implements
`bots.RefusalAnswerer`. Every other seat keeps today's crash, unchanged. The
ladder runs in order, the first rung to submit wins, and every attempt is its
own `m.locked` Submit:

1. **The seat's retry.** Re-project the View under `m.mu`, call
   `AnswerRefused(v, dc, in)` *outside* the lock, and Submit.
2. **The fallback rungs.** Submit each element of
   `bots.Fallbacks(&dc, BoardFromGame(...), slot)` in order.
3. **Crash.** If every rung is refused, crash with all the errors, as today.

Refusals leave no events (preserve-and-reject), so only the accepted intent
is logged, and the ladder is deterministic. A per-match counter
(`refusals`, `fallbacks`) goes on the crash report and the sidecar, as
diagnostics only. The fallback logic moves out of
`cmd/botbench/spellbench.go:262-283` into `bots.Fallbacks`, which botbench
calls in BP-16, so the bench and the host cannot drift.

---

## 6. B — splitting the azmcts clairvoyant world

### 6.1 The move (BP-02)

- **New package `internal/azmcts/clairvoyant`.** `NewClairvoyant`,
  `AllowClairvoyant`, `ErrClairvoyantRefused` and the `clairvoyant` source
  type move here from `internal/azmcts/world.go:29-72`.
- **It exports** `Source(env searchseat.Env, obs *searchprobe.Collector)
  (azmcts.WorldSource, error)`, which is `NewClairvoyant(env.Engine, obs)`.
- **`azmcts` keeps** `World`, `WorldSource`, `ErrNoWorld`, `RedealSource`,
  `WorldRedeal` and `WorldClairvoyant` (a string constant only).
- **`SeatConfig` gains** `Source func(searchseat.Env, *searchprobe.Collector) (WorldSource, error)`.
- **`NewSeat`** (`internal/azmcts/seat.go:117-127`):
  - `World == WorldRedeal` builds the redeal source as today.
  - `World` of `""` or `WorldClairvoyant` requires `cfg.Source != nil`, and
    otherwise returns `azmcts: the clairvoyant world is not linked
    (internal/azmcts/clairvoyant)`.
  - `DecideSearch` (`internal/azmcts/seat.go:170-182`) calls `cfg.Source` in
    place of `NewClairvoyant`.
- **The `AllowClairvoyant` runtime gate stays, inside the new package.** A
  clairvoyant arm therefore needs both a link-time import and a runtime
  opt-in.
- **botbench** (`cmd/botbench/azcost.go:114-116`, `azSeatConfig` at
  `:125-131`, the `az` entries at `cmd/botbench/main.go:274-280` and
  `cmd/botbench/spellbench_registry.go`) sets `cfg.Source = clairvoyant.Source`
  for `az` when the world is clairvoyant. It never does so for `az-redeal`.
- **azmcts tests.** Package-internal tests that build clairvoyant worlds
  (`internal/azmcts/bench_test.go:39`, `search_test.go:77`, `seat_test.go`,
  `world_test.go:19-31`) cannot import the new package, because the internal
  test package would import a package that imports azmcts. They get a
  test-only copy in `internal/azmcts/clairvoyant_helper_test.go`, about 20
  lines. Tests do not reach binaries, and archtest scans non-test imports
  only (`internal/archtest/arch_test.go:25-27`).
  `TestClairvoyantRefusedUnlessAllowed` moves to the new package.
  `TestSeatRefusesClairvoyantByDefault` becomes "refuses when Source is nil".
- **`cmd/botbench/azcorpus_test.go:25-32`** imports the new package.

### 6.2 archtest rows (`internal/archtest/arch_test.go`)

`TestDependencyOrderHolds` changes as follows. Where a row names a package
that does not yet exist, `continue` already skips it
(`internal/archtest/arch_test.go:161-164`).

| change | from | to | why |
|---|---|---|---|
| replace `:156-158` | `host`, `host/httpapi`, `cmd/gorged` | `internal/azmcts/clairvoyant` | Keeps the ban's intent: nothing that seats a non-bench opponent links the real-engine clone. The honest core becomes linkable. |
| add | `internal/azmcts` | `internal/azmcts/clairvoyant` | The core must never pull the clone back in. |
| add | `internal/spellbench/sbsearch`, `internal/searchseat` | `internal/azmcts/clairvoyant` | Both are linked by hosted entries. |
| add (loop) | every package with prefix `module+"/bots"` | `internal/azmcts/clairvoyant`, `host`, `internal/testutil` | Bots are hostable and host-independent (host imports bots, so the reverse arrow would also be a cycle). |
| add | `protocol` | `module+"/bots"` | The wire stays a leaf. `bots` imports rules, and `{protocol, rules}` is already forbidden. |

`TestTimeIsImportedOnlyByTheHost` needs no edit: `bots/...` are not on its
allow-list, so a clock in a bot fails the build. The comment at
`internal/archtest/arch_test.go:152-155` is rewritten to name the new package.

---

## 7. C — server CPU

- **Search slots.** `host.Options.SearchSlots int` defaults to 0, which means
  unbounded, so embedders and tests are unchanged. gorged sets it from
  `-bot-search-slots`, whose default is `max(1, runtime.GOMAXPROCS(0)/2)`.
  - `parkSeat` acquires a slot around `DecideEnv` for a bot seat whose
    table-policy entry has `Search` set.
  - The slot is a **FIFO** queue: a mutex plus a slice of waiter channels.
    Go does not specify FIFO for a buffered-channel semaphore.
  - The wait selects on `ctx.Done()`. On cancellation, `parkSeat` returns
    `ctx.Err()`, and the play loop, seeing `ctx.Err() != nil`, records
    `r.abort(m)` and does not crash. A table being closed is not a crash.
  - Slots are released on every path, panics included (`defer`).
- **When saturated, decisions queue.** The host never degrades to another
  policy, never shortens a search, and never times a search out. Wall time
  varies with load. Intents do not, because every budget is a count:
  - `searchseat`: Worlds, Attempts, MaxSubmits;
  - `azmcts`: Sims, MaxSteps, Limit;
  - `sbsearch`: Worlds, Horizon (turns), MaxSteps, and the value-driven
    MinWorlds / MaxWorlds.

  `searchseat.Options.Parallelism` folds its parallel work in sequential
  order (`internal/searchseat/searchseat.go:129-134`), so
  `-bot-search-parallelism` also changes latency only.
- **Admission.** `host.Options.MaxSearchTables int` (0 = unbounded; gorged
  sets it from `-max-search-tables`, default 8). `AddTable` refuses an
  `OnDemand` table whose policy entry has `Search` set when that many
  on-demand search tables are already live, not finished. It mirrors the
  existing `MaxOnDemandTables` check (`host/registry.go:243-252`). The
  builder's error becomes a 400 carrying the message "search bots are at
  capacity; choose a non-search policy", which is an explicit refusal and not
  a silent substitution.
- **Memory.** The az100 bench peaked at 0.9 GB RSS per 8-worker run (SB
  §12.5), **INFERRED** about 110 MB per concurrent search. Each feed holds one
  JSON board frame per decision for the whole game, **INFERRED** a few MB per
  match. BP-07 and BP-10 measure both and record them in the ticket report.
  Slots bound the first, and `-max-search-tables` the second.
- **Pace.** The host's `-pace` sleep (`host/match.go:686`) is unaffected.
  vs-bot tables set no pace (`cmd/gorged/game.go:96-101`).
- **Rejected alternatives.**
  1. A wall-clock budget. It makes intents a function of load, which breaks
     §8 and the no-clock rule.
  2. "Degrade to `bot` when busy". It silently changes the opponent the
     player chose.
  3. Per-table goroutine pools. They don't bound the box.

---

## 8. Determinism and replay (the invariant, and how each bot meets it)

**Invariant.** For a fixed registry build, a hosted policy's intent at every
decision is a pure function of four things:

- the entry name;
- the per-seat seed (`m.seed ^ (slot+1)`, `host/match.go:291`);
- the table's configuration (`rules.Config`: decks, format, tokens, seed);
- the sequence of intents before that decision (the human's included).

It never depends on wall time, scheduling, slot contention, parallelism,
GOMAXPROCS or map order. **Replay never runs a bot**: `replay.Replay` and
`ReplayTo` resubmit logged intents. So a recorded table replays even after
its policy is removed from the registry. Only building new seats needs the
name registered.

Two known limits:

1. **Undo.** An undo is serviced at a loop boundary chosen by wall time
   (`host/undo.go:45-54`), and bot seats keep their internal state (RNG
   position, builtin pursuits) across a rewind (`host/undo.go:32-35`). A game
   with undos therefore replays exactly from its log, but re-deriving it by
   re-running the bots is guaranteed only for undo-free prefixes. This is
   unchanged from today. Feeds are rebuilt exactly (§5.2), so they add no new
   non-determinism.
2. **The redeal RNG** is `math/rand/v2` PCG, seeded per deal, which is
   allowed (`TestNoLegacyMathRand`).

| entry | randomness | budgets | engine input | notes |
|---|---|---|---|---|
| `bot`, `lethal-pressure`, `cast-profile` | `seat.Bot`'s PCG(seed, seed^φ) (`seat/bot.go:76-77`) | none | Board or View | pinned today by `TestHostedPoliciesReplayDeterministically` (`host/host_test.go:93`) and `TestHeads` |
| `search` | the wrapped bot's PCG. Sampler seed `SampleSeed` plus a per-decision derivation; teacher seed `teacherSeed(base, turn)` (`internal/searchseat/searchseat.go:432-434`) | Worlds 8, Attempts 64, MaxSubmits 5000, Limit 6 | honest root (turn, actor board) and feed | `Choose` is pure (`internal/searchseat/searchbot.go:32-35`) |
| `az-redeal` | `DecisionSeed(seatSeed, d.Seq)` (`internal/azmcts/search.go:154`) and `RedealSeed` (`internal/azmcts/redeal.go:121`) | Sims 100, MaxSteps 1000, Limit 6 | honest root and feed | `Search` "is a pure function of (root position, bot answer, net, opts)" (`internal/azmcts/search.go:83-85`) |
| `sb-tactical`, `sb-heuristic`, `sb-uniform`, `sb-first` | SplitMix64 seeded per seat (`internal/spellbench/builtins/builtins.go:415-418`) | none. The planner's exact search is bounded by `exactBudget` = 2000 clones (`internal/spellbench/builtins/priority.go:447`) | View, plus the honest root as planner | the refusal ladder is deterministic (§5.5) |
| `sb-search-lite-atk` | `azmcts.DecisionSeed` for worlds, chance and rollout seats (SB §12.6 "Determinism") | Worlds 4, Horizon 2 turns, MaxSteps 4000 | honest root and feed | `TestSearchIsDeterministic` (`internal/spellbench/sbsearch/sbsearch_test.go:133`) |

Enforced by `TestEveryHostedPolicyIsDeterministic` (new, `host/hosted_policies_test.go`,
created in BP-11 and extended by BP-12 to BP-15). For each registered entry,
it plays two in-memory 2-seat tables with the same seed and a fixed
`MaxIntents` cap (search entries 150 intents, cheap entries a whole game),
then requires identical event logs and intents and a clean replay of each.
The `bot` entry keeps the existing golden check.

---

## 9. Wire, endpoint and flags

### 9.1 Structs (`protocol/protocol.go`, generated by `cmd/gentypes`)

```go
// BotMeasurement is one measured claim of a hosted bot policy.
type BotMeasurement struct {
	Claim   string `json:"claim"`
	Versus  string `json:"versus"`
	Setting string `json:"setting"`
	Source  string `json:"source"`
}

// BotPolicyInfo is one selectable hosted bot policy (GET /api/bot-policies).
type BotPolicyInfo struct {
	Name        string           `json:"name"`
	Label       string           `json:"label"`
	Description string           `json:"description"`
	Tier        string           `json:"tier"` // "production" | "experimental"
	Strength    []BotMeasurement `json:"strength"`
	MeanMS      float64          `json:"mean_ms"` // 0 = not measured
	P95MS       float64          `json:"p95_ms"`
	CostScope   string           `json:"cost_scope"`
	CostNote    string           `json:"cost_note"`
	Search      bool             `json:"search"`
	Formats     []string         `json:"formats"`
}

// BotPolicyList is GET /api/bot-policies: the offered set, in display order,
// and the server's default.
type BotPolicyList struct {
	Default  string          `json:"default"`
	Policies []BotPolicyInfo `json:"policies"`
}
```

`cmd/gentypes/main.go:25-38` gains the root `reflect.TypeOf(protocol.BotPolicyList{})`
and a `"BotTier": {"production", "experimental"}` union.
`web/src/protocol.ts` is regenerated, and `TestCommittedProtocolTSIsFresh`
(`cmd/gentypes/main_test.go:13`) pins it. `protocol` never imports `bots`.
gorged converts `bots.Info` to `BotPolicyInfo` with a small function in
`cmd/gorged/botpolicies.go`.

### 9.2 Endpoint

- `GET /api/bot-policies` is served from `httpapi.Options.BotPolicies
  *protocol.BotPolicyList`, an immutable list built by gorged at startup, as
  `Decks` is (`host/httpapi/handler.go:50-53`).
- A nil list serves `{"default":"","policies":[]}` with status 200.
- The route goes into the mux next to `/api/decks` (`host/httpapi/handler.go:84-94`)
  and into the 405 list (`:98-102`).
- Order: production first, then experimental, then by name
  (`bots.Entries()` filtered to the offered set).
- The response contains no live state.

### 9.3 Create flow

- **httpapi.** `games` (`host/httpapi/rest.go:392-437`) stops normalizing the
  empty name to `bot`. It validates a non-empty `bot_policy` with
  `bots.Normalize` (a 400 on an unregistered name) and passes `""` through
  unchanged. `CreateGameOptions.BotPolicy`'s doc says `""` means "the
  server's default".
- **gorged.** `createGame` (`cmd/gorged/game.go:33-110`) maps `""` to
  `c.botPolicy`. It refuses a name that is not offered ("bot policy %q is not
  offered on this server"), a name whose entry's `Formats` lack the requested
  format, and a name whose `MaxSeats` is below 2. The response's `bot_policy`
  is the effective name, as today.
- **Tests.** `TestCreateGameBotPolicyDecodeAndRejectsDiagnostic` changes by
  decision: `{}` now reaches the builder as `""`.
- **Host.** `TableConfig.validated` (`host/table.go:200-205`) calls
  `bots.Normalize`, so every persisted table still has an explicit registered
  name. Host does not know about the offered set.

### 9.4 Flags (`cmd/gorged`)

| flag | default | meaning | validated at startup |
|---|---|---|---|
| `-bot-policy` | `bot` | the policy a vs-bot request gets when it omits `bot_policy` | registered, offered, supports *both* formats and 2 seats. An error otherwise: a default must never refuse a format. |
| `-bot-policies` | `""` (every registered entry) | comma list of the **offered** set (the listing and new vs-bot tables). **Recommended: accept.** It lets the operator hide aliases or weak bots, or turn search off on a starved box, without a rebuild, and it cannot affect a running game's intents. | each name registered, no duplicates, contains `-bot-policy` |
| `-bot-search-slots` | `max(1, GOMAXPROCS/2)` | concurrent searched decisions (§7) | ≥ 1 |
| `-bot-search-parallelism` | `1` | goroutines within one searched decision (`search` only; latency only) | ≥ 1 |
| `-max-search-tables` | `8` | live on-demand tables whose policy is a search entry | ≥ 0 (0 = unbounded) |

Startup tables (`cmd/gorged/main.go:544-546`) keep policy `bot`. `-bot-policy`
does not apply to them (Q4).

---

## 10. The UI (ticket BP-19; codex or by hand, not the pipeline seats)

- **`web/src/lib/api.ts`** gets `botPoliciesURL()` and `fetchBotPolicies()`,
  typed by the generated `BotPolicyList`. `startPlayVsBot` (`web/src/lib/playvsbot.ts`)
  gains a `botPolicy` argument, and `CreateGameRequest.bot_policy` is already
  optional (`web/src/lib/api.ts:130-138`).
- **`web/src/components/PlayVsBot.svelte`** gets a policy `<select>`,
  populated from the listing and filtered by the chosen format. It shows:
  - the label and a tier badge (experimental entries are visibly marked);
  - the first `Strength.Claim` with its `Setting` in a disclosure;
  - the think time (`mean_ms` and `cost_note`).

  It defaults to `default`, remembers the last pick in `localStorage` (inside
  try/catch), sends `bot_policy` only when the pick differs from the default,
  and shows the server's 400 text on a refusal (capacity, not offered).
- **Rematch** already sends the table's policy (`web/src/lib/playvsbot.ts:53-87`),
  and needs no change.
- **Tests.** Vitest covers the fetch, the filter and the payload. The
  operator rebuilds the bundle (agents must not run `make web`).

---

## 11. Tickets (ordered)

Every ticket has `Depends-On: cli-20260928T193741Z-5822d0cd` plus the tickets
listed. "Pipeline" means the agentctl engine seats. Commands follow the box
rules: one at a time, `-p 1`, `GOMAXPROCS=3 GOMEMLIMIT=1GiB`. Each ticket's
report records anything INFERRED that it measured.

### BP-01 — `bots` registry and the three existing policies
- **Goal:** create `bots` (§4.1, without `env.go` / `refusal.go`), plus
  `bots/bot`, `bots/lethalpressure` and `bots/castprofile` (factories copied
  from `host/bot_policy.go:67-98`), and `bots/all`.
  - `host.NormalizeBotPolicy`, `NewBotPolicySeat` and
    `NewBotPolicySeatWithAutoPayMana` delegate to `bots`. The exported
    constants stay as aliases.
  - host non-test code imports `bots` and `bots/bot` only. Add
    `host/bots_link_test.go`, which blank-imports `bots/lethalpressure` and
    `bots/castprofile`.
  - `cmd/gorged` and `cmd/botbench` import `bots/all`. botbench's
    `hostedPolicy` (`cmd/botbench/main.go:404-412`) calls `bots.New`.
  - archtest rows: `protocol ↛ bots`, and the `bots/...` loop ↛ `host`,
    `internal/testutil`.
  - Add `bots/` to `docs/agents/repo-map.md`.
  - Fill `Info` for the three entries from §3.1.
- **Files:** `bots/registry.go`, `bots/registry_test.go`, `bots/bot/bot.go`,
  `bots/lethalpressure/lethalpressure.go`, `bots/castprofile/castprofile.go`,
  `bots/all/all.go`, `host/bot_policy.go`, `host/bots_link_test.go`,
  `cmd/gorged/main.go`, `cmd/botbench/main.go`, `internal/archtest/arch_test.go`,
  `docs/agents/repo-map.md`.
- **Out of scope:** search entries, the env seam, flags, endpoint.
  `TestHostedVocabularyRefusesSearchPolicies` stays green unchanged, because
  `search` is not yet registered.
- **Depends-On:** —. **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestRegister|TestNormalize|TestEntries|TestValidate' ./bots/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestNormalizeBotPolicy|TestNewBotPolicySeatIsDeterministic|TestHostedAutoPayBotPlaysCombatSim|TestHostedVocabularyRefusesSearchPolicies|TestHostedPoliciesReplayDeterministically|TestHumanCaretakerUsesConfiguredPolicy|TestTableBotPolicyDefaultsAndRejectsUnknown|TestDefaultSeatsConstructTheConfiguredPolicy' ./host/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestCreateGame' ./host/httpapi/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestCreateGamePersistsRequestedBotPolicy' ./cmd/gorged/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestSpellbenchRegistryRoundTrip' ./cmd/botbench/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestHeads$' ./rules/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestDependencyOrderHolds|TestTimeIsImportedOnlyByTheHost|TestNoExportLeaksAnEngineGame|TestEngineCompilesFor32Bit' ./internal/archtest/
  ```

### BP-02 — split the clairvoyant world out of azmcts
- **Goal:** §6.1 and §6.2 exactly.
- **Files:** `internal/azmcts/world.go`, `internal/azmcts/seat.go`,
  `internal/azmcts/redeal.go` (constants' doc), the new
  `internal/azmcts/clairvoyant/clairvoyant.go` and `clairvoyant_test.go`,
  `internal/azmcts/clairvoyant_helper_test.go`,
  `internal/azmcts/{bench,search,seat,world}_test.go`, `cmd/botbench/azcost.go`,
  `cmd/botbench/main.go` (the `az` entry and comment),
  `cmd/botbench/spellbench_registry.go` (`az`), `cmd/botbench/azcorpus_test.go`,
  `internal/archtest/arch_test.go`.
- **Out of scope:** any behaviour change. botbench `-a az -az-world
  clairvoyant` games must be event-identical before and after
  (`TestAZCorpusRecordsWithoutChangingTheGame`).
- **Depends-On:** BP-01. **Seat:** pipeline. The steps are mechanical: move
  the file, add the `Source` field, fix imports, add the test helper.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestSeat|TestNewSeat|TestSearchOnRedealtWorlds|TestRedealWorlds|TestSubmitRecoversAnEnginePanic|TestHypotheticalSubmitClassifiesRejections' ./internal/azmcts/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestClairvoyant' ./internal/azmcts/clairvoyant/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestAZCorpusRecordsWithoutChangingTheGame|TestAZSeatDoesNotReEquipForever|TestSpellbenchRegistryRoundTrip' ./cmd/botbench/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestDependencyOrderHolds|TestTimeIsImportedOnlyByTheHost' ./internal/archtest/
  ```

### BP-03 — `replay.Walk` and `searchseat.RebuildFeed`
- **Goal:**
  - Add `replay.Walk(l *events.Log, cfg rules.Config, n int, visit func(e *rules.Engine, i int) error) (*rules.Engine, error)`.
    It is `run` with a visit before each intent and once at n, sharing
    `run`'s genesis and compare logic. `run` becomes `Walk` with a nil visit.
  - Add `searchseat.RebuildFeed(cfg rules.Config, l *events.Log, n int, actor state.PlayerID) (*Feed, error)`:
    `Observe` at each visit, and `RecordAnswer(e.Pending(), l.Intents[i])`
    after the actor's own intents.
- **Files:** `replay/replay.go`, `replay/walk_test.go`,
  `internal/searchseat/feed.go`, `internal/searchseat/rebuild_test.go`.
- **Tests:**
  - `TestWalkVisitsEveryBoundaryAndMatchesReplayTo`.
  - `TestRebuildFeedMatchesTheLiveFeed`: a bench-driven game with a search
    seat compared with `RebuildFeed` over its log at three prefixes (frames,
    answers and collector state all equal).
- **Out of scope:** host wiring.
- **Depends-On:** —, and it can run in parallel with BP-01 and BP-02.
  **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestWalk|TestReplay' ./replay/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestRebuildFeed|TestFeed|TestBenchDriverFeedsTheTeacherLoop' ./internal/searchseat/
  ```

### BP-04 — the feed-owned known-card tracker and `searchseat.HonestRoot`
- **Goal:**
  - Move `KnownTracker` (`internal/azmcts/redeal.go:162-200`) to
    `internal/searchseat`. `azmcts.KnownTracker` becomes an alias.
  - Add `Feed.Known() (searchprobe.KnownCards, error)`.
  - `azmcts.Seat` (`internal/azmcts/seat.go:96,250-263`) and `sbsearch.Seat`
    (`internal/spellbench/sbsearch/sbsearch.go:157,670-679`) read
    `env.Feed.Known()`.
  - `RebuildFeed` inherits the tracker.
  - Add `searchseat.HonestRoot(setup, f *Feed, e *rules.Engine, seed [2]uint64) (*rules.Engine, string)` (§5.1).
- **Tests:**
  - `TestFeedKnownMatchesTheSeatTracker` (bench game, every decision).
  - `TestHonestRootKeepsTheObservation`: the root's capture with a
    collector clone equals the last frame.
  - `TestHonestRootIgnoresTheRealHiddenCards`: the
    `TestRedealWorldsIgnoreTheRealHiddenCards` fixture, name-identical roots.
  - `TestRedealFromAnHonestRootPrepares`: `NewRedeal` on a root does not
    refuse. This is the INFERRED claim from §5.3; if it fails, stop and
    report.
  - Existing sbsearch and azmcts suites unchanged.
- **Files:** `internal/searchseat/known.go`, `internal/searchseat/feed.go`,
  `internal/searchseat/honestroot.go` (+ tests), `internal/azmcts/redeal.go`,
  `internal/azmcts/seat.go`, `internal/spellbench/sbsearch/sbsearch.go`.
- **Out of scope:** host and bench wiring.
- **Depends-On:** BP-02 (seat.go), BP-03 (feed.go). **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestFeedKnown|TestHonestRoot|TestRedealFromAnHonestRoot|TestRebuildFeed' ./internal/searchseat/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestKnownTrackerMatchesProjection|TestRedeal|TestSeatOnRedealPlaysAndReplays|TestSeatSearchesAndReplaysExactly' ./internal/azmcts/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestZeroWorldsIsTactical|TestForcedErrorFallsBackToTactical|TestSearchIsDeterministic|TestCombatAndTargetSearch' ./internal/spellbench/sbsearch/
  ```

### BP-05 — the bench honest-root mode and the parity smoke
- **Goal:**
  - `bench.Hooks.HonestRoot bool`. When set, `PlayGame`
    (`internal/bench/bench.go:233-237`) passes
    `Env{Engine: searchseat.HonestRoot(...)}` in place of `e` (nil on a
    refusal, which takes the `DecideBoard` path), using the same
    `bots.RootSeed`. `bots.RootSeed` lives in `bots/env.go`; BP-05 adds that
    one function if BP-06 has not landed.
  - botbench `-hosted-root` sets it.
  - Parity smoke: 20 games per entry, 2 workers, using the brief's own seeds
    and the mono pair `uw-tempo:mono-red-prowess`. It reports the fraction of
    searched decisions whose intent matches between normal and `-hosted-root`
    runs for `search`, `az-redeal` (`-az-sims 25` for the smoke) and
    `sb-search-lite-atk`, plus refusal counts.
  - **Stop condition:** `search` agreement is not 100% (INFERRED exact, §5.3).
  - The full-size measurement is BP-20.
- **Files:** `internal/bench/bench.go`, `cmd/botbench/main.go`,
  `cmd/botbench/hostedroot_test.go`, `bots/env.go` (RootSeed only).
- **Out of scope:** host, and any strength claim.
- **Depends-On:** BP-04, BP-02. **Seat:** pipeline, smoke-sized, one run at
  a time.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestHostedRoot' ./cmd/botbench/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestBenchDriverFeedsTheTeacherLoop|TestSearchSeatWithoutCoveredKindsIsTheBotExactly' ./internal/searchseat/
  ```
  plus the parity table in the ticket report.

### BP-06 — the env and refusal types, and the host refusal ladder
- **Goal:**
  - `bots/env.go` (`EnvSeat`, `Env`, `RootSeed`) and `bots/refusal.go`
    (`RefusalAnswerer`, `Fallbacks`), per §4.2.
  - The host ladder of §5.5 in the Submit section of `play`
    (`host/match.go:626-646`). The re-projection happens under the lock,
    `AnswerRefused` runs outside it, and each Submit is its own `m.locked`.
  - Crash-report counters.
- **Tests:**
  - `TestRefusalLadderAcceptsTheSeatRetry` and
    `TestRefusalLadderFallsBackToMinimalThenBot` use a test seat that returns
    a refusable intent (a lone blocker on a menace attacker, the case named
    at `cmd/botbench/spellbench.go:35-37`).
  - `TestNonRefusalSeatStillCrashesOnRejection`.
  - `TestFallbacksMatchTheSpellbenchRunner` (bots): `Fallbacks` equals the
    runner's inline rungs on a table of decisions.
- **Files:** `bots/env.go`, `bots/refusal.go`, `bots/refusal_test.go`,
  `host/match.go`, `host/refusal_test.go`.
- **Out of scope:** feeds and Env dispatch.
- **Depends-On:** BP-01, and BP-05 (`bots/env.go`). **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestFallbacks' ./bots/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestRefusalLadder|TestNonRefusalSeatStillCrashesOnRejection|TestHostedPoliciesReplayDeterministically|TestStall' ./host/
  ```

### BP-07 — the host feed lifecycle
- **Goal:** §5.2 Create, Observe and Record for every non-human
  `bots.EnvSeat`. There is no Env dispatch yet: seats still take the
  Board/View path, so a match with no EnvSeat is byte-identical.
  - New unexported `host/botenv.go` (`type matchFeeds`) holds them, owned by
    the match goroutine.
- **Tests:**
  - `TestHostFeedEqualsRebuildFeed`: a spy EnvSeat table's live feed after
    N intents equals `RebuildFeed` over the log.
  - `TestNoEnvSeatNoFeed`: allocation-free fast path, and `TestHeads`
    unchanged.
  - Record per-decision capture cost and per-match feed memory (the INFERRED
    items in §7) in the report.
- **Files:** `host/match.go`, `host/botenv.go`, `host/botenv_test.go`.
- **Out of scope:** the honest root, undo, slots.
- **Depends-On:** BP-06, BP-04. **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestHostFeed|TestNoEnvSeatNoFeed|TestHostedPoliciesReplayDeterministically|TestNoRegistryDataResultIsAnEngineGame' ./host/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestHeads$' ./rules/
  ```

### BP-08 — host Env dispatch with the honest root
- **Goal:** §5.1 in `projectNext` / `parkSeat`.
  - A new `parkedData.env` field. The order is `HumanSeat`, then
    `EnvSeat && WantsEnv`, then `BoardSeat`, then View; the HumanSeat-first
    ordering is argued at `host/match.go:394-401`.
  - The honest root is built under the lock with `bots.RootSeed(m.seed^uint64(slot+1), d.Seq)`.
  - `DecideEnv` runs outside the lock.
  - Diagnostics count root refusals per match.
- **Tests:**
  - `TestHostedEnvSeatsNeverSeeTheLiveEngine` and
    `TestHostedEnvSeatsIgnoreTheRealHiddenCards`, both with a spy EnvSeat
    (§5.4).
  - `TestEnvRootRefusalPlaysFallback`.
- **Files:** `host/match.go`, `host/botenv.go`, `host/botenv_test.go`.
- **Out of scope:** real adapters, undo, slots.
- **Depends-On:** BP-07. **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestHostedEnvSeats|TestEnvRoot|TestHostFeed|TestHostedPoliciesReplayDeterministically|TestHumanCaretakerUsesConfiguredPolicy' ./host/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestNoExportLeaksAnEngineGame|TestDependencyOrderHolds' ./internal/archtest/
  ```

### BP-09 — undo rebuilds the feeds
- **Goal:** §5.2 Undo. `rewindToLastIntent` (`host/undo.go:241-285`) calls
  `RebuildFeed` for every feed after swapping the engine and before
  `projectNext`.
- **Tests:**
  - `TestUndoRebuildsEnvSeatFeeds`: after an undo, the feed equals
    `RebuildFeed` at the new head, and play continues. Undo is one human
    table with a spy EnvSeat bot.
  - Existing `TestUndo*` unchanged.
  - Report the rebuild wall time for a 400-intent prefix.
- **Files:** `host/undo.go`, `host/botenv.go`, `host/botenv_test.go`.
- **Depends-On:** BP-08, BP-03. **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestUndo' ./host/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestUndo' ./host/httpapi/
  ```

### BP-10 — search slots and admission in the host
- **Goal:** §7. Add `host.Options.SearchSlots`, `MaxSearchTables` and
  `BotSearchParallelism`, which is passed into `bots.Options`.
  - The FIFO slot around `DecideEnv` for `Search` entries.
  - Cancellation becomes an abort.
  - `AddTable` admission.
- **Tests:**
  - `TestSearchSlotsQueueFIFOAndNeverChangeIntents`: two spy search tables
    on one slot give identical logs to an unbounded run.
  - `TestSearchSlotWaitAbortsOnClose`.
  - `TestMaxSearchTablesRefusesAtCreate`.
- **Files:** `host/registry.go`, `host/botenv.go`, `host/match.go`,
  `host/slots_test.go`.
- **Depends-On:** BP-08. **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestSearchSlot|TestMaxSearchTables|TestOnDemand|TestHostedPoliciesReplayDeterministically' ./host/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestNoExportLeaksAnEngineGame' ./internal/archtest/
  ```

### BP-11 — `bots/search` (L10)
- **Goal:**
  - An adapter over `searchseat.SearchBot` implementing `EnvSeat` and
    `BoardSeat` (§5.1), with `Hosted()` = `searchseat.Defaults()` plus
    `Parallelism` from `Options`.
  - `Info` from §3.1, `Caretaker: bot`, `Env` and `Search` set.
  - Add it to `bots/all`.
  - Create `host/hosted_policies_test.go` with
    `TestEveryHostedPolicyIsDeterministic` (§8) and the `search` row of
    `TestHostedEnvSeatsIgnoreTheRealHiddenCards`.
  - `TestHostedSearchConfigIsHonest`.
  - Rewrite `TestHostedVocabularyRefusesSearchPolicies`
    (`host/bot_policy_az_test.go`) to refuse `az`, `policynet`, `legacy` and
    `explore`, and to accept `search` only when linked.
- **Files:** `bots/search/search.go` (+ test), `bots/all/all.go`,
  `host/hosted_policies_test.go`, `host/bot_policy_az_test.go`.
- **Depends-On:** BP-09, BP-10. **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestHostedSearch' ./bots/search/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestEveryHostedPolicyIsDeterministic|TestHostedEnvSeatsIgnoreTheRealHiddenCards|TestHostedVocabularyRefusesSearchPolicies' ./host/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestDependencyOrderHolds|TestTimeIsImportedOnlyByTheHost' ./internal/archtest/
  ```

### BP-12 — `bots/azredeal`
- **Goal:**
  - An adapter over `azmcts.Seat` with
    `Hosted()` = `DefaultSeatConfig()` with `World: WorldRedeal`, `Worlds: 0`,
    a nil net and `Source` nil. It must never set `Source`.
  - `Info` from §3.1.
  - Rows in the two host tests, `TestHostedAZIsRedeal`, and the `bots/all`
    line.
- **Files:** `bots/azredeal/azredeal.go` (+ test), `bots/all/all.go`,
  `host/hosted_policies_test.go`.
- **Depends-On:** BP-11, BP-02. **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestHostedAZ' ./bots/azredeal/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestEveryHostedPolicyIsDeterministic|TestHostedEnvSeatsIgnoreTheRealHiddenCards' ./host/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestDependencyOrderHolds' ./internal/archtest/
  ```

### BP-13 — `bots/sbtactical` and the cards dependency
- **Goal:**
  - `host.Options.BotDeps bots.Deps`, threaded into every `bots.New` call
    in the host (`host/bot_policy.go`, `host/match.go:291,500`). gorged sets
    `BotDeps{Cards: reg}` (`cmd/gorged/main.go:560-578`).
  - The adapter over `builtins.NewTactical(AutoPay, ...)` implements
    `EnvSeat` (planner = honest root, `SetPlanner(nil)` on refusal) and
    `RefusalAnswerer` (delegating to `builtins.Seat.Refused`). With
    `Deps.Cards == nil`, the factory returns an error.
  - Verify the priority-only `WantsEnv` claim (§5.1 INFERRED) by counting
    planner calls by decision kind on a smoke game.
  - Host rows.
- **Files:** `bots/sbtactical/sbtactical.go` (+ test), `bots/all/all.go`,
  `host/registry.go`, `host/bot_policy.go`, `host/match.go`,
  `cmd/gorged/main.go`, `host/hosted_policies_test.go`.
- **Depends-On:** BP-12. **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestSBTactical' ./bots/sbtactical/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestEveryHostedPolicyIsDeterministic|TestHostedEnvSeatsIgnoreTheRealHiddenCards|TestRefusalLadder' ./host/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestHostThreadsCorpusTokens|TestCreateGame' ./cmd/gorged/
  ```

### BP-14 — `bots/sbsearch` (`sb-search-lite-atk`)
- **Goal:**
  - An adapter over `sbsearch.New(<sb-tactical>, seed, LiteAtk())`.
    `LiteAtk()` is the `sbSearchVariants` row exported from this package,
    which botbench references in BP-16.
  - The fallback is the inner `sb-tactical` on `env.View` with a nil
    planner, never `sbsearch.DecideBoard` (§5.1).
  - `RefusalAnswerer` goes through `UnwrapSeat`.
  - Host rows.
- **Files:** `bots/sbsearch/sbsearch.go` (+ test), `bots/all/all.go`,
  `host/hosted_policies_test.go`.
- **Depends-On:** BP-13. **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestSBSearch' ./bots/sbsearch/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestEveryHostedPolicyIsDeterministic|TestHostedEnvSeatsIgnoreTheRealHiddenCards' ./host/
  ```

### BP-15 — `bots/sbheuristic`, `bots/sbuniform`, `bots/sbfirst`
- **Goal:** three small adapters over `builtins.New(<p>, AutoPay, ...)`
  (`sb-uniform` XORs `builtins.UniformSeed`, as at
  `internal/spellbench/registry/sb.go:17-19`). Each implements `EnvSeat`
  (planner) and `RefusalAnswerer`. Each exports
  `NewWithMode(seed uint64, m builtins.ManaMode) *builtins.Seat`, which
  `internal/spellbench/registry/sb.go` then calls for its plain names, so one
  constructor serves both registries. Host rows and `bots/all` lines.
- **Files:** `bots/sbheuristic/*`, `bots/sbuniform/*`, `bots/sbfirst/*`,
  `bots/all/all.go`, `internal/spellbench/registry/sb.go`,
  `host/hosted_policies_test.go`.
- **Depends-On:** BP-14. **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestSB' ./bots/sbheuristic/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestRegistry' ./internal/spellbench/registry/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestEveryHostedPolicyIsDeterministic' ./host/
  ```

### BP-16 — botbench consumes the bots registry
- **Goal:**
  1. The `policies` map (`cmd/botbench/main.go:144-364`): each name in
     `bots.Names()` resolves through `bots.New`, except that `search`,
     `az-redeal`, `sb-tactical` and `sb-search-lite-atk` are built with
     `bots/<pkg>.New(opts, overlay(bots/<pkg>.Hosted()))`, so the `-search-*`,
     `-az-*` and tactical-weights flags keep working.
  2. `cmd/botbench/spellbench_registry.go` registers `bot`, `az-redeal`,
     `sb-tactical` and `sb-search-lite-atk` through the same constructors;
     `sbSearchVariants`' lite-atk row references `bots/sbsearch.LiteAtk()`.
  3. `sbSubmitWithFallback` (`cmd/botbench/spellbench.go:243-285`) calls
     `bots.Fallbacks`.
  4. Botbench's own `SetPlanner(e)` for builtin seats is unchanged (the bench
     keeps the real engine; `-hosted-root` from BP-05 is the hosted variant).
- **Tests:**
  - `TestBotbenchDefaultsAreTheHostedConfigs`: with default flags, every
    overlay equals `Hosted()` (reflect.DeepEqual).
  - `TestEveryHostedNameIsABenchPolicy`.
- **Files:** `cmd/botbench/main.go`, `cmd/botbench/spellbench_registry.go`,
  `cmd/botbench/spellbench.go`, `cmd/botbench/searchcost.go`,
  `cmd/botbench/azcost.go`, `cmd/botbench/bots_registry_test.go`.
- **Depends-On:** BP-15, BP-05. **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestBotbenchDefaultsAreTheHostedConfigs|TestEveryHostedNameIsABenchPolicy|TestSpellbenchRegistryRoundTrip|TestSpellbenchUnknownNameListsRegistered|TestAZCorpusRecordsWithoutChangingTheGame|TestSearchCostReportNumbers' ./cmd/botbench/
  ```
  plus the sb-gauntlet digest golden (`cmd/botbench/spellbench_digest_test.go`),
  run with `-run 'Digest'`.

### BP-17 — the wire structs, `GET /api/bot-policies`, and gentypes
- **Goal:** §9.1 and §9.2. Add `httpapi.Options.BotPolicies` and the
  handler, the gentypes root and union, and regenerate `web/src/protocol.ts`
  with `go run ./cmd/gentypes`, which writes only that file.
- **Tests:**
  - `TestBotPoliciesEndpointServesTheList` and
    `TestBotPoliciesEndpointEmptyWhenUnset` (httpapi).
  - `TestCommittedProtocolTSIsFresh`.
- **Files:** `protocol/protocol.go`, `cmd/gentypes/main.go`,
  `web/src/protocol.ts`, `host/httpapi/handler.go`, `host/httpapi/rest.go`,
  `host/httpapi/botpolicies_test.go`.
- **Depends-On:** BP-01 (it runs in parallel with BP-02 to BP-16).
  **Seat:** pipeline. The TS change is a generated file, not UI work.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestBotPoliciesEndpoint|TestCreateGame' ./host/httpapi/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestCommittedProtocolTSIsFresh|TestGentypes' ./cmd/gentypes/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestDependencyOrderHolds' ./internal/archtest/
  ```

### BP-18 — gorged flags, the default policy, and the offered set
- **Goal:**
  - The §9.4 flags, validated at startup.
  - Build `protocol.BotPolicyList` in `cmd/gorged/botpolicies.go` and pass it
    to httpapi.
  - `createGame` applies the default and the offered, format and seat
    checks (§9.3).
  - httpapi `games` passes `""` through.
  - The host options get slots, admission and parallelism.
- **Tests:**
  - `TestBotPolicyFlagsValidateAtStartup`: unknown, not offered, default not
    in offered, and a default that lacks a format.
  - `TestCreateGameAppliesTheDefaultPolicy`.
  - `TestCreateGameRefusesAnUnofferedPolicy`.
  - `TestCreateGameRefusesAFormatThePolicyLacks`.
  - The updated `TestCreateGameBotPolicyDecodeAndRejectsDiagnostic`.
- **Files:** `cmd/gorged/main.go`, `cmd/gorged/game.go`,
  `cmd/gorged/botpolicies.go`, `cmd/gorged/game_test.go`,
  `cmd/gorged/main_test.go`, `host/httpapi/rest.go`, `host/httpapi/game_test.go`.
- **Depends-On:** BP-17, BP-10, BP-15. **Seat:** pipeline.
- **Done means:**
  ```
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestBotPolicyFlags|TestCreateGame' ./cmd/gorged/
  GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestCreateGame|TestBotPoliciesEndpoint' ./host/httpapi/
  ```
  plus a manual smoke on a port in 8090-8099, with `-dir /tmp/gorge-bp18`,
  `-vsbot` and `-bot-policies bot,search`: `curl /api/bot-policies`, create
  one `search` game, stop the server by its listening pid.

### BP-19 — landing-page policy picker (UI)
- **Goal:** §10.
- **Files:** `web/src/lib/api.ts`, `web/src/lib/playvsbot.ts`,
  `web/src/components/PlayVsBot.svelte`, `web/src/lib/api.test.ts`,
  `web/src/lib/playvsbot.test.ts`.
- **Depends-On:** BP-17, BP-18. **Seat:** **codex or by hand. Not glm or
  deepseek** (memory: UI work dies on the local seats). The operator rebuilds
  the bundle.
- **Done means:** the vitest files named above pass, run by whoever holds the
  web toolchain. A manual check against a BP-18 dev server: the picker lists
  the offered set, filters by format, sends `bot_policy`, and shows a
  capacity refusal.

### BP-20 — hosted re-measurement and label refresh
- **Goal:** for each search entry, a `botbench -hosted-root` run at the
  measurement sizes the original reports used:
  - `search`: the mono-suite held-out gate, 400 games per pair.
  - `az-redeal`: the SB §12.5 matrix.
  - `sb-search-lite-atk`: 512 games vs `sb-tactical`, plus a new vs-`bot`
    arm.

  Also a first `-decision-cost` measurement for `bot`. Update the §3.1
  `Info` literals and this document's table. Tier promotions are the
  operator's call.
- **Files:** `bots/*/…` `Info` literals, this spec.
- **Depends-On:** BP-16, BP-18. **Seat:** operator or by hand. These are
  heavy sweeps, one at a time, under `systemd-run --scope -p MemoryMax=4G`
  with data on `/mnt/sata`.
- **Done means:** a report under `docs/superpowers/reports/` holding each
  arm's command, games, CI and cost, and `Info` literals that cite it.

### 11.1 Dependency graph

```
spellbench-prep (cli-20260928T193741Z-5822d0cd)
 ├─ BP-01 ─┬─ BP-02 ─┐
 │         └─ BP-17 ─┼──────────────────────────────┐
 ├─ BP-03 ───────────┴─ BP-04 ─ BP-05 ─ BP-06 ─ BP-07 ─ BP-08 ─┬─ BP-09 ─┐
 │                                                             └─ BP-10 ─┴─ BP-11 ─ BP-12 ─ BP-13 ─ BP-14 ─ BP-15 ─┬─ BP-16 ─┐
 │                                                                                                                 └─ BP-18 ─┼─ BP-19
 └──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┴─ BP-20
```

Parallel lanes: BP-03 runs alongside BP-01 and BP-02, and BP-17 alongside
everything from BP-02 to BP-16.

### 11.2 Collision map (tickets that share a file run sequentially)

| file | tickets |
|---|---|
| `internal/archtest/arch_test.go` | BP-01 → BP-02 |
| `cmd/botbench/main.go` | BP-01 → BP-02 → BP-05 → BP-16 |
| `cmd/botbench/spellbench_registry.go`, `cmd/botbench/azcost.go` | BP-02 → BP-16 |
| `internal/azmcts/seat.go` | BP-02 → BP-04 |
| `internal/searchseat/feed.go` | BP-03 → BP-04 |
| `internal/bench/bench.go` | BP-05 |
| `bots/env.go` | BP-05 → BP-06 |
| `host/match.go` | BP-06 → BP-07 → BP-08 → BP-10 → BP-13 |
| `host/botenv.go`, `host/botenv_test.go` | BP-07 → BP-08 → BP-09 → BP-10 |
| `host/undo.go` | BP-09 |
| `host/registry.go` | BP-10 → BP-13 |
| `host/bot_policy.go` | BP-01 → BP-13 |
| `bots/all/all.go`, `host/hosted_policies_test.go` | BP-01 → BP-11 → BP-12 → BP-13 → BP-14 → BP-15 |
| `internal/spellbench/registry/sb.go` | BP-15 |
| `cmd/gorged/main.go` | BP-01 → BP-13 → BP-18 |
| `host/httpapi/rest.go` | BP-17 → BP-18 |
| `protocol/protocol.go`, `cmd/gentypes/main.go`, `web/src/protocol.ts` | BP-17 |
| `web/src/lib/*.ts`, `web/src/components/PlayVsBot.svelte` | BP-19 |

---

## 12. Open questions for the operator (each with a recommendation)

- **Q1. Tiers.** Should anything beyond `bot` start as production?
  **Recommend no.** `search` has the only held-out gate in the gorge setting
  (+3.3pp) but costs ~0.7 s per searched decision. `az-redeal`'s +20.5pp is
  from a one-deck-vs-five matrix. Revisit after BP-20.
- **Q2. The honest root's secret log.** Do we require redacting past secret
  events' object ids in the root's log before hosting (§5.3)?
  **Recommend: not before hosting.** No lineup bot reads it, the leak test
  covers use, and the bench exposure is larger. File a follow-up to add
  `Redealer` log redaction, then measure rules correctness in rollouts.
- **Q3. Commander for search entries.** `search` cannot sample Commander
  worlds (`PublicGame` has no format or commanders); the redeal paths are
  untested there. **Recommend constructed-only for every new entry** until a
  Commander smoke (BP-20 extension) shows refusals below 1% and no crashes.
- **Q4. Startup tables.** Should `-bot-policy` also drive the perpetual demo
  tables? **Recommend no.** They are 4-seat, the new entries are 2-seat only,
  and changing the demo's bot is a separate call.
- **Q5. Aliases.** `lethal-pressure` and `cast-profile` are, on auto-pay
  tables, *weaker* than `bot`. **Recommend keeping them registered**, so old
  tables load and replay-adjacent flows keep their names, but leaving them
  out of the default gorged `-bot-policies` in the deploy script.
- **Q6. Open decklists.** A search bot reads its opponent's list
  (`Setup.Decks`). That is honest while every deck is a public repo file
  whose id is on the wire. **Recommend** recording "open decklist" as a
  contract in `docs/superpowers/specs/2026-09-22-engine-contracts.md`, and
  refusing search entries for any future user-imported deck until a list
  posterior exists (SB §5.1).
- **Q7. The difficulty ladder.** Should `sb-uniform` and `sb-first` be
  offered at all? They are honest and cheap, and useful as an "easy" tier.
  **Recommend registering them and offering them**, labelled by their Elo
  gap to `bot`.
- **Q8. More budgets.** Should we also host `sb-search-fast-atk` (65.6%, 0.5 s)
  or `az-redeal` at 25 sims (+14.7pp, 45 ms)? **Recommend adding `az-redeal`
  at 25 sims as `az-redeal-fast`** once BP-20 is done. It is 5x cheaper for
  most of the gain. Hold `fast-atk`.
- **Q9. External bots.** `bots.EnvSeat` names `internal/searchseat` types,
  so a bot in another module can implement only plain `Seat` or `BoardSeat`.
  **Recommend accepting this** for now. Promoting the feed and the root types
  to a public package is a larger move with no current consumer.
- **Q10. Slot defaults.** Are `GOMAXPROCS/2` slots and 8 search tables
  right for the demo box? **Recommend** keeping these defaults and setting
  explicit values in `make deploy-demo` after BP-07 and BP-10 report the
  measured feed and search memory.

### 12.1 Operator answers (2026-09-28)

The operator accepted every recommendation above, Q1 through Q10, as written.
Tickets BP-01 to BP-18 are queued in agentctl. BP-19 (the UI) is done by hand
or by codex. BP-20 (heavy re-measurement) runs at night.
