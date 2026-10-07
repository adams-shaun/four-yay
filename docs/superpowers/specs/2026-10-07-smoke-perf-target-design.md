# Smoke perf target design

## Goal

A single opt-in make target that plays a fixed, reproducible set of botbench
games on one pinned core and prints a compact per-scenario performance report,
so an operator can eyeball engine/search-policy speed on a shared, loaded box
without a bespoke benchmark script each time. It is a smoke test of
*throughput health*, not a statistical benchmark: it must finish in under five
minutes, be deterministic run to run, and fail loudly only when a game does not
run to completion.

## Non-goals

- No performance threshold or trend assertion; wall-clock on the shared box is
  too noisy to gate on. It reports numbers; it does not ratchet them.
- Not part of `make test` or the merge gate.
- Not a replacement for `enginebench` (component micro-benchmarks) or
  `searchbench` (sealed-manifest policy evaluation). It uses `botbench`, whose
  whole-game path is what this target watches.

## Scenarios

Four cases, all through `cmd/botbench`, all with shared flags
`-dir .cards -format constructed -workers 1 -seed <fixed>`:

| # | name | invocation |
|---|------|-----------|
| 1 | random play | `-a sb-uniform -b sb-uniform -games 8` |
| 2 | random decks | `-a bot -b bot -pairs coverage -games 2` |
| 3 | heuristics vs search | `-a bot -b search -hosted-root -games 8` |
| 4 | search vs random | `-a search -b sb-uniform -hosted-root -games 8` |

The uniform search arm (`sb-uniform`) is the "random" side: it is a search
policy whose rollouts are the random-arm uniform play, which is the closest
existing thing to random play without adding a policy. Cases 3 and 4 run
`-hosted-root` so the search seat answers from a redeal of the live position,
the honest-root path the perf work targets.

## Pinning and isolation

- `taskset -c 9` and `GOMAXPROCS=1` for every scenario (operator directive: the
  smoke runs on core 9, single-threaded, so the numbers are comparable across
  runs and do not contend with the worker pools the box is usually running).
- `GOMEMLIMIT=2GiB` bounds each run.
- A fixed `-seed` (a constant in the script, overridable by env) makes a run a
  pure function of its seeds.

## Reporting

One row per scenario, printed as a table:

```
scenario              wall_s  user_s  ms/asked  games  result
random play              ...     ...      ...      8   PASS
...
total elapsed: Ns
```

- `wall_s` / `user_s` from `/usr/bin/time`.
- `ms/asked` parsed from the run's `ms/asked decision total:` summary line.
- `games` the requested game count.

## Failure policy

Exit non-zero if, for any scenario:

- the run's process exits non-zero (botbench already fails on a livelock or
  engine panic), or
- the run's output contains the `@@ STALLED` notice (a game hit the
  `-max-turns` or `-max-intents` watchdog).

A hosted-root redeal refusal is *reported* (its notice line is surfaced in the
scenario row) but is not fatal: a refused redeal plays the wrapped bot and is a
documented degradation, not a hang.

No perf threshold is enforced.

## Budget

The script measures total elapsed time and prints it. `SMOKE_PERF_BUDGET`
(default `300`, seconds) is a soft warning bound: when elapsed exceeds it the
script prints a warning naming the overrun but does not fail. The scenario game
counts above keep a healthy run well under five minutes; if a future change
makes one scenario stall, the watchdog caps bound that scenario, not the whole
target.

## Components

- `scripts/smoke-perf.sh` — the run loop, pinning, table, failure checks.
- `Makefile` — a `$(BIN_DIR)/botbench` build rule and a `.PHONY: smoke-perf`
  target that runs the script. The target is opt-in and documented as not part
  of `make test`, matching the existing `smoke` target.

## Testing

The script is a thin orchestrator over `botbench`; its own risk is parsing and
failure detection, not the engine. It is validated by:

- running the target end to end once and confirming the table and a zero exit;
- a cheap self-check mode (`SMOKE_PERF_GAMES=N`, env) that overrides every
  scenario's `-games` to the same N (so `SMOKE_PERF_GAMES=1` shrinks all four to
  one game each), exercising the harness — pinning, parsing, table, failure
  checks — quickly without the full per-scenario counts.

No automated shell test is added: it would run the same games again for little
signal, and the existing shell smoke lane is the browser suite.
