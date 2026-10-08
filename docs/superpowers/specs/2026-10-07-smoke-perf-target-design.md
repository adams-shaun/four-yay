# Smoke perf target design

## Goal

A single opt-in make target that plays a fixed, reproducible set of botbench
games on one pinned core **for a base revision and a candidate revision** and
prints a compact per-scenario performance comparison, so an operator can
eyeball whether a change helped, hurt or left engine/search-policy speed alone
on a shared, loaded box without a bespoke benchmark script each time. It is a
smoke test of *throughput health*, not a statistical benchmark: it must finish
in under five minutes, be deterministic run to run, and fail loudly only when a
game does not run to completion.

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

## Comparing revisions

The target compares two revisions, mirroring `scripts/enginebench-pair.sh`:

- Take `BASE` and `CAND` positionally (`scripts/smoke-perf.sh [BASE] [CAND]`),
  defaulting from the environment: **`BASE=main`, `CAND=.`** (the working tree).
- The base binary is built from a git revision with `git archive <rev>` into a
  scratch directory (never a checkout, so the shared checkout's HEAD is not
  moved — the same discipline as `scripts/enginebench-build.sh`). The candidate
  `.` is built from the working tree. Both build `./cmd/botbench` with the same
  extra build flags.
- Every scenario runs **both** revisions, base first then candidate, on the same
  pinned core, so a load drift falls equally on both.
- A revision may be a `.` working tree or any revision `git rev-parse` resolves.
  If a build fails, the target stops before running any scenario.

## Pinning and isolation

- `taskset -c 9` and `GOMAXPROCS=1` for every scenario (operator directive: the
  smoke runs on core 9, single-threaded, so the numbers are comparable across
  runs and do not contend with the worker pools the box is usually running).
- `GOMEMLIMIT=2GiB` bounds each run.
- A fixed `-seed` (a constant in the script, overridable by env) makes a run a
  pure function of its seeds.

## Reporting

Every scenario prints its own output live, for both revisions: the script runs
each invocation (base then candidate) with its stdout and stderr teed to the
terminal (and captured to a temp file for parsing), so each iteration — every
scenario's full botbench run on each revision — is visible as it happens rather
than silent until the end. A running header names the scenario and revision
before its output.

After all scenarios, a summary table is printed, one row per scenario, with
the two revisions side by side and their ratio:

```
scenario            base wall/user  cand wall/user  base ms/asked  cand ms/asked  ratio  result
random play                ...            ...             ...             ...     1.02  PASS
random decks               ...            ...             ...             ...     0.99  PASS
heuristics vs search       ...            ...             ...             ...     1.00  PASS
search vs random           ...            ...             ...             ...     1.01  PASS
total elapsed: Ns
```

- `wall`/`user` in seconds from `/usr/bin/time`.
- `ms/asked` parsed from each run's `ms/asked decision total:` summary line.
- `ratio` = candidate `ms/asked` / base `ms/asked` (lower is faster); `n/a`
  when either side's line is absent.
- `result` is PASS unless the failure policy trips.

The per-scenario output is the live stream above; the table is the digest of
it, never a substitute.

## Failure policy

Exit non-zero if, for any scenario on **either** revision:

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

- `scripts/smoke-perf.sh` — the two-revision build, run loop, pinning, table,
  failure checks.
- `Makefile` — a `$(BIN_DIR)/botbench` build rule and a `.PHONY: smoke-perf`
  target that runs the script with `BASE ?= main` and `CAND ?= .` (both
  overridable on the command line, like the existing enginebench targets). The
  target is opt-in and documented as not part of `make test`, matching the
  existing `smoke` target.

## Testing

The script is a thin orchestrator over `botbench`; its own risk is parsing and
failure detection, not the engine. It is validated by:

- running the target end to end once (base `main` vs the working tree) and
  confirming the two-revision table and a zero exit;
- a cheap self-check mode (`SMOKE_PERF_GAMES=N`, env) that overrides every
  scenario's `-games` to the same N (so `SMOKE_PERF_GAMES=1` shrinks all four to
  one game each), exercising the harness — both builds, pinning, per-revision
  parsing, table, failure checks — quickly without the full counts.

No automated shell test is added: it would run the same games again for little
signal, and the existing shell smoke lane is the browser suite.
