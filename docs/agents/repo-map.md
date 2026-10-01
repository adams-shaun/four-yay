# Repo map — what lives where

A lookup table for agents and new contributors. One line per package or
directory, plus where to go when you need to change a particular kind of
thing. Verified against `main` on 2026-09-28; where this file and the code
disagree, the code wins — fix this file in the same commit.

See also: [invariants.md](invariants.md) (the rules every change must keep)
and [do-not.md](do-not.md) (the mistakes that have actually happened here).

## The engine chain

The import direction is one-way. `internal/archtest`'s
`TestDependencyOrderHolds` pins the arrows that must never appear (see
[invariants.md](invariants.md#1-dependency-direction)).

| Package | What it owns |
|---|---|
| `cards` | Compiles Forge card scripts into the card IR; keyword expanders (`cards/kw_*.go`); `cards.Registry`, coverage. `cards.OpenCorpusFor` (`cards/subset.go`) opens only a known card pool's cards from the per-card segment file, falling back to the whole corpus when the pool reads the name universe; botbench's deck and `-spellbench` modes use it. `cards.CompilerFingerprint` hashes every non-test `.go` file here, so **any edit in `cards/` invalidates every IR cache** and forces a recompile. |
| `cards/oracletext` | Printed-card (Oracle) view for the Oracle audit. A subpackage on purpose so it does not move the fingerprint. |
| `state` | Objects, zones, players, `state.Game`, the `ContinuousEffect` vocabulary. Plain data; mutated only by `events.Apply`. |
| `decision` | `Decision` / `Option` / `Intent`, and the closed set of `decision.Kind`s (`decision.Kinds`). The whole engine↔seat contract. |
| `deck` | `{name, count}` deck-list JSON resolved against a `cards.Registry`. |
| `botpolicy` | The one heuristic bot policy (`botpolicy.Decide`), in one copy, shared by `seat` and rules' own fuzz driver. Reads a `Board`, never a view or an engine. Cast profiles in `botpolicy/profiles/`. |
| `bots` | Registry of hosted bot policies (`bots/registry.go`); built-ins register from `bots/bot`, `bots/lethalpressure` and `bots/castprofile`, linked together by `bots/all`. |
| `events` | The event union, `events.Apply` (the only state mutator), the log, the sha256 hash chain. `events.Kind` is append-only. |
| `effects` | `api:` primitive implementations, filter/count/value evaluators. Talks to the engine only through the small `effects.Host` interface; never imports `rules`. |
| `rules` | The engine: turn structure, priority, stack, combat, SBAs, layers, casting and payment, and the `kw:`/`trig:`/`stat:`/`repl:` primitives. `rules.New`, `Pending`, `Submit`, `Advance`, `Clone`. `rules.NewStaged` (`staging.go`) builds a hypothetical engine at a described position. Also holds the coverage ratchets and golden heads (`rules/*_test.go`). |
| `view` | Projects one seat's redacted `view.View` (`Project`, `ProjectFor`, `RedactEvents`). The only way a client reads state. `view/view.go` is the orchestration (`Project`/`project`, `View`, the shared text helpers); the card/battlefield projection family lives in `view/card.go` (`CardView`, `cardViews`, `cardView`) and the stack family in `view/stack.go` (`StackView`, `stackViews`). A projection change goes in the family file for its seam, not in `view/view.go` — the two families were interleaved in one file and two projection tickets collided on it (split out in `97655ff0b`). |
| `seat` | Who answers decisions: `seat.Seat` (+ `BoardSeat`, `PaymentPlanConsumer`), `NewBot`, `NewAttackSimBot`, `NewPolicyNetBot`, explore bot. Handed a view, never an engine. |
| `replay` | Re-executes `(Config, Log)` and names the first divergent event (`Replay`, `ReplayTo`). |
| `protocol` | Versioned wire types for host↔client. Types only; never imports `rules`. |
| `host` | Tables (one goroutine each), sessions, snapshots, persistence, undo, the hosted bot-policy vocabulary (`host/bot_policy.go`). Its only clock is an injected sleep. |
| `host/httpapi` | `net/http` JSON + SSE + the embedded web client. Every seat route goes through `claimForTable`. |

## `internal/`

| Package | What it owns |
|---|---|
| `archtest` | Import-graph and structural tests (dependency order, `time` allowlist, no `math/rand`, 32-bit build, resume-state census, no engine leaks from host). |
| `testutil` | Corpus registry (`CorpusRegistry` — **skips when `.cards/` is absent**), repo decks (`testutil/decks/*.json`), shared invariants, and the AGENTS.md ratchet (`agentsdoc_test.go`). |
| `testutil/feedback` | Loads player feedback snapshots; `feedback.EngineAt`. |
| `tsgen` | Go→TypeScript for `web/src/protocol.ts`. |
| `bench` | Policy-agnostic game runner: `PlayGame`, `RunPairs`, watchdogs, livelock recovery. Shared by `botbench` and `policytune`. |
| `traceboard` | The redacted, map-free decision-trace board schema (`botbench -decision-trace`). |
| `policynet` | Pure-Go learned policy: hashed features, MLP, value head, `.gpol` checkpoints, label-corpus loaders. |
| `searchbench` | Native search-benchmark replication: the sealed item manifest and scoring, `Materialize` (StateSpec → staged engine), `Reach`, canonical options (`BuildCanon`, `Canon.Project`) and 17lands label matching (`LabelItem`). Research harness. |
| `searchbench/statespec` | Strict Go mirror of Draft Zero's StateSpec v1 JSON (validation, aliases, typed labels). |
| `searchprobe` | Hidden-information world sampler (`Collector`, `Sample`, `Redealer`, known-card tracking) and PIMC teacher scoring. Research harness, not production. |
| `searchseat` | The PIMC search decision function (`Choose`) shared by the teacher corpus generator and `SearchBot`. |
| `hindsight` | Deterministic hindsight-branch mechanics (no clock, no files). |
| `paymirror` | A/B check of planned vs manual cast payment. |

## `cmd/`

| Command | Purpose |
|---|---|
| `forgec` | Fetch and compile the Forge corpus; `report`, `coverage`. |
| `gorged` | The server: perpetual bot tables, vs-bot games, web client. |
| `mtgsim` | Headless self-play over the repo decks with replay verification (`make sim`). |
| `repro` | Replays a player feedback snapshot; `-emit-test` writes a failing test. |
| `cardfuzz` | Random mono-colour decks over the supported corpus; hunts panics, livelocks, divergences. |
| `botbench` | Head-to-head policy evaluation: deck-pair matrices, CIs, decision traces, search/az front doors. |
| `searchteacher` / `searchprobe` | PIMC label-corpus generator / search calibration (also the engine perf oracle). |
| `policytrain` / `policytune` / `exitloop` / `hindsight` | Train `policynet` / SPSA-fit cast weights / expert-iteration loop / hindsight labels. |
| `traindash` | Read-only training dashboard over `/mnt/sata/gorge-training` (`make traindash`). |
| `testtime` / `gcgate` / `allocgate` | Test wall-time, GC-share and allocation/RSS budgets (`TEST_HISTORY.md`, `ALLOC_HISTORY.md`). |
| `gentypes` | Regenerates `web/src/protocol.ts` (`make gentypes`; `-check` in lint). |
| `enginebench` | The draft-zero docs/015 engine-speed rows (random/bot play, Clone, one step, search rates); builds at a4af596 too. Results: `docs/superpowers/reports/2026-09-30-gorge-engine-speed.md`. |
| `deckimport` | Plain-text decklist → repo deck JSON. |
| `ledger` | Rebuilds the derived issue ledger (`.ds4/ledger.json`). |
| `keywordbench` / `oraclepacket` / `autopayaudit` / `paymirror` | Keyword presence stats / Oracle text for audit authors (write to a gitignored path) / auto-pay audit (`-tags autopayaudit`) / payment A/B CLI. |

## Everything else

| Path | What it is |
|---|---|
| `web/` | Svelte + Vite client. `web/src/protocol.ts` is **generated** — never hand-edit. |
| `docs/superpowers/specs/` | Design specs, `YYYY-MM-DD-<slug>[-design].md`. The founding design is `2026-09-03-mtgcore-go-engine-design.md`; deliberate contracts (not debt) are in `2026-09-22-engine-contracts.md`. |
| `docs/superpowers/plans/` | Implementation plans (single files or dirs with a `README.md`). |
| `docs/superpowers/reports/` | Measured results. Numbers you quote should come from here, with the file named. |
| `docs/agents/` | This guide. |
| `orchestrator/hooks.py` | agentctl pipeline hooks. Stdlib-only Python; the old daemon is gone. |
| `.agentctl/config.toml` | agentctl pipeline config: tiers, gates, landing. |
| `.githooks/` | pre-commit, commit-msg, pre-push (`core.hooksPath=.githooks`). |
| `scripts/` | `agent-worktree.sh` (the only sanctioned worktree creator), `fleet.sh` (port allocation), `smoke.sh`, `gate-ws.sh`, `cleanup.sh`, `deploy-demo.sh` (operator only). |
| `.github/workflows/coverage.yml` | On push to main: regenerates the README coverage block and `docs/coverage.md`. |
| `TEST_HISTORY.md`, `ALLOC_HISTORY.md` (per package) | Test-time and allocation budgets. |

## Untracked but load-bearing

| Path | Notes |
|---|---|
| `.cards/` | Forge corpus (`cardsfolder/`, `tokenscripts/`) and the IR cache `ir-<fingerprint>.gob.gz` with its per-card segment file `ir-<fingerprint>.seg` (written by `Registry.Save`, read by the subset loader). GPL-3.0 — never tracked. Without it, corpus tests **skip and read green**. |
| `.ds4/` | Pipeline scratch (issues, briefs, `ledger.json`). `TestNothingUnderDS4IsTracked` keeps it out of git. |
| `.superpowers/` | Orchestration records and seat prompt context. |
| `.worktrees/` | Sibling task checkouts. Exclude from searches or you get duplicate hits. |
| `gorged-data/` | Default server persistence. Never share one directory between servers. |
| `feedback/` | Player bug reports (agentctl intake). Excluded via `.git/info/exclude`. |
| `cmd/repro/testdata/.tokens/` | GPL token text stripped from committed feedback fixtures. |
| `/mnt/sata/gorge-training` | Training corpora, checkpoints, `GOTMPDIR`. Never `/tmp` (RAM-backed). |

## Where do I change…

| I want to… | Go to |
|---|---|
| Implement an `api:` effect | `effects/`, `effects.Register("Name", fn)` in an `init()`. If it needs more than `effects.Host` offers, it is rules work. |
| Implement a keyword/trigger/static/replacement | `rules/`, `effects.RegisterNonAPI("kw:X", …)`. A keyword that expands to script lines is a `cards/kw_*.go` expander instead (moves the fingerprint). |
| Add a `count:` value head | The evaluator arm in `effects/` **and** `effects.modelledValueHeads` (`TestValueHeadRegistryMatchesEvaluator`). |
| Add an event kind | Append to `events/event.go` after the last constant, add a `kindNames` entry and the rules trigger-interest mapping. |
| Add a decision kind | Don't, without an operator decision — the set is closed; every seat, bot, client and the protocol must answer it. |
| Add a bot policy | Bench-only: `cmd/botbench`. Hosted: `host/bot_policy.go` (closed vocabulary). See the README's *Bot player training and adoption guidelines*. |
| Add a repo deck | `internal/testutil/decks/*.json` via `cmd/deckimport`; it must be fully supported (the acceptance ratchet). |
| Turn a player report into a test | `go run ./cmd/repro -emit-test <pkg> <feedback-dir>`. |
| Change the wire | `protocol/`, then `make gentypes`. |
| Change a seat's projected view | The family file for the seam: a card/battlefield projection in `view/card.go`, a stack projection in `view/stack.go`. Only an assembler change (`Project`/`project`) belongs in `view/view.go`, which two projection tickets once collided on. |
