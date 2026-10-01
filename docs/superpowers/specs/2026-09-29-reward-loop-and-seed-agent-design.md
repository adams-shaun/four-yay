# The reward loop and the seed agent — design (2026-09-29)

Status: operator-approved design (2026-09-29). Implementation lands in the same
branch as this document.

## 1. Problem this closes

gorge's pipeline is good at *executing* tickets and bad at *choosing* them. The
daemon dispatches whatever is queued; what gets queued depends on whoever is
sitting in an orchestrator session. When no one is, the queue drains and the
box idles — or worse, a heavy job (training, a gauntlet sweep, cardfuzz) takes
the machine and a gate dies of OOM, which halts landings for everyone.

Seven outcomes matter, and none of them is what the pipeline currently
optimises. The operator set the weights (2026-09-29); they are deliberately
far apart, so the ranking is decided by which axis a candidate is on before any
estimate of its size matters:

1. **An engine defect on a card the build claims to support fully — 1000x.**
   Constrained to the *validated set*: repo-deck cards that neither ratchet
   table lists. A defect there is silently wrong rules in the set nobody is
   checking any more. Finding one pays, and so does closing it.
2. **A merge-flow hot spot — 1000x.** A file several live branches all edit is
   where the fleet loses whole paid rounds to `merge_fix` instead of to work.
3. **Box stability — 100x, as a veto.** The box must not crash, OOM, or starve
   a gate. A merge that cannot run its gates produces no progress at any price,
   so heavy work — training, gauntlets, sweeps — must be pausable in favour of
   a gate rather than merely polite about it.
4. **Code stewardship — 100x.** New functionality and tests must not leave a
   recurring tax on every future iteration: a slower gate suite, a fatter
   always-loaded agent context, a file too large to change one line of.
5. **Win rate per unit of compute — 10x.** Simplifying observations, engine
   efficiency, profiled hotspots. Brute force is explicitly not the goal.
6. **Win rate in the SpellBench type-gauntlet — 1x.**
7. **The bots' state space and corpus audit coverage — 1x each.** Whether a bot
   sees what a human would see (its own library as a *list*, archetype priors
   for its own deck and the opponent's, curve and zone counts), whether
   decisions should route to a more targeted model; and audit coverage of the
   33.7k-card corpus against an external oracle (Forge primarily, ManaBrew
   secondarily) rather than against gorge's own reading of forgescript.

This document specifies three things that together make the pipeline chase
those outcomes on its own: a **reward vector** with one scorer, a **resource
broker** that makes heavy work yield to gates, and a **seed agent** that runs
on a timer, reads both, and keeps the loop moving without an operator present.

## 2. The reward vector

One append-only ledger, `.ds4/reward/scoreboard.jsonl`, one row per
measurement:

```json
{"ts":"2026-09-29T21:40:00Z","git_head":"2486f6a33","axis":"eff",
 "metric":"ms_per_searched_decision_p50","value":18.4,"cost_s":214,
 "cmd":"scripts/reward-probe.sh eff","note":""}
```

Rows are never rewritten. A row's `cost_s` is the wall time the measurement
itself spent, so the loop can see when measuring costs more than the thing it
measures.

Seven axes. Every metric is produced by a command that already exists or is a
thin wrapper over one; nothing here re-derives a number gorge already knows,
and every axis except `eff` costs under a second to measure.

| axis | metrics | source | weight |
|---|---|---|---|
| `correct` | `validated_defects_found_cum`, `validated_defects_closed_cum`, `validated_cards` | `.ds4/reward/defects.jsonl`, the two ratchet tables, `internal/testutil/decks/*.json` | **1000x (summed)** |
| `flow` | `conflict_hotspots`, `merge_fix_rate`, `merge_fix_rounds_max` | `git worktree list` + `git diff main...<branch>`, `.ds4/orchestrator/journal.jsonl` | **1000x (summed)** |
| `stability` | `oom_kills`, `gate_timeouts`, `gate_starved_minutes`, `broker_kills`, `swap_in_pages`, `provider_failures_24h` | `/proc/vmstat` deltas, the journal, `broker.sh`'s own interventions | **veto (100x)** |
| `steward` | `gate_wall_s`, `agent_context_bytes`, `oversized_files` | gate log mtimes under `.ds4/orchestrator/gates/`, `AGENTS.md` + the dispatch context file, a line count over tracked `*.go` | **100x (summed)** |
| `eff` | `elo_per_ms`, `ms_per_searched_decision_p50/p90`, `ms_per_game`, `sim_games_per_s` | `botbench -grind` (throughput), `sbsearchcost.go`, `decisioncost.go`, pprof | **10x** |
| `win` | `champion_elo`, `champion_ci_lo` | `scripts/sb-gauntlet.sh` → `gauntlet/results.jsonl` | 1x |
| `obs` | `observable_facts_exposed / observable_facts_total` | the observability checklist (§6) and its ratchet test | 1x |
| `audit` | `cards_with_oracle_verdict` | `rules/testdata/oracle/`, forgedelta/mbdelta | 1x |

`correct`, `flow` and `steward` SUM their metrics rather than falling back to
one: finding a defect and closing it are two separate wins, and a faster gate
suite and a thinner agent context are two separate taxes.

### 2.1 The scorer

`scripts/reward.py` reads the scoreboard and emits, per axis, the latest value
and the delta against the previous distinct `git_head`, plus one scalar:

```
score = 1000.0*Δcorrect_norm + 1000.0*Δflow_norm + 100.0*Δsteward_norm
        + 10.0*Δeff_norm + 1.0*Δwin_norm + 1.0*Δobs_norm + 1.0*Δaudit_norm
        − 100.0*stability_penalty
```

Each axis delta is normalised by that axis's own historical scale (median
absolute delta over the ledger, floored so a fresh ledger cannot divide by
zero), so axes measured in Elo, milliseconds and card counts are comparable
without hand-tuned units.

`stability_penalty` is not a smooth term. Any OOM kill, daemon crash-restart or
gate timeout in the window sets it to at least 1, which makes the scalar
negative no matter how much Elo the window bought. That is the 100x: the loop
cannot buy strength with instability.

Work selection uses **Δscore ÷ expected seat cost**, not Δscore. This is what
makes the second goal mechanical rather than aspirational: an Elo gain bought
by spending more simulations per decision moves `eff` the wrong way and scores
near zero, while the same Elo at lower `ms_per_searched_decision` scores ten
times a plain win-rate gain.

### 2.2 Candidate backlog

`.ds4/reward/candidates.jsonl` holds work the loop has *identified* but not yet
queued: `{id, axis, title, evidence, est_delta, est_cost, brief_template}`.
Probes append candidates (a pprof hotspot, a checklist gap, an oracle
disagreement); the seed agent drains the top of the ranked list into real
tickets. A candidate carries its own evidence so the brief it becomes can name
a measurement command in "Done means" — vague briefs are the pipeline's main
source of wasted rounds.

## 3. The resource broker

Three classes, three cgroup slices, one arbiter. cgroups do the bounding;
cooperation only handles what cgroups cannot (pausing without killing).

| class | slice | rule |
|---|---|---|
| `gate` | `gorge-gate.slice` | gates, merges, landings. High `CPUWeight`, no hard memory cap below what the gate config asks. Never paused. |
| `probe` | `gorge-probe.slice` | reward measurement. `MemoryMax=8G`, preemptible, only runs in headroom the broker grants. |
| `heavy` | `gorge-heavy.slice` | training, gauntlets, sweeps, cardfuzz. `MemoryMax` hard so heavy *cannot* take the box, low `CPUWeight`, `AllowedCPUs` a subset. Preemptible. |

### 3.1 Leases and the pause contract

Every heavy or probe job launches through `scripts/heavy.sh <class> -- <cmd>`,
which:

1. writes a lease file `.ds4/reward/leases/<pid>.json` (`class`, `cmd`, `pid`,
   `started`, `pause_file`),
2. runs the command inside its class's slice via
   `systemd-run --user --scope --slice=<slice> -p MemoryMax=…`,
3. removes the lease on exit, including on signal.

Pausing is two-tier, because the two tiers fail differently:

- **Cooperative (preferred).** The broker touches the lease's `pause_file`. A
  job with a step loop — training epochs, a gauntlet's per-matchup loop —
  checks that file at its own checkpoint boundary, flushes, and sleeps until
  the file disappears. Nothing is lost and nothing is half-written.
- **SIGSTOP fallback.** If a paused lease is still consuming after a 60 s
  grace, the broker `SIGSTOP`s its process group and records that it had to.
  SIGSTOP holds the job's memory, so it is a CPU remedy, not a memory remedy;
  a lease that must be stopped for memory is killed instead and the kill is
  recorded on the `stability` axis as a broker action, not an OOM.

Checkpoint-boundary first, SIGSTOP after grace, kill only for memory. A job
that wants to never be stopped mid-epoch simply has to honour its pause file.

### 3.2 When the broker acts

`scripts/broker.sh` exposes `gate-begin`, `gate-end`, `status`, `pause-all`,
`resume-all`, `may-i <class>`:

- `gate-begin` pauses every heavy AND probe lease and holds them until
  `gate-end`. Gates are short and landings are the point. It is wired into the
  pipeline as the FIRST entry in the repo's `[[gates]]` list, and the release
  half is the existing `post_gates` hook (`orchestrator.hooks:testheads_policy`
  calls `broker.sh gate-end` before it does anything else). Both halves are
  best-effort: the gate exits 0 whether or not a lease exists, and a broker
  failure must never be why a passing gate run does not land.
- Because the release half can be skipped entirely — a crashed tick, a halt, a
  killed daemon — a gate flag older than `GATE_FLAG_TTL_S` (default 1800) is
  treated as abandoned: `gate_active` removes it and resumes both classes. A
  stranded flag would otherwise pause heavy work forever, which is its own kind
  of starvation.
- `may-i probe|heavy` answers yes only when free memory is above the class's
  floor, no gate is in flight, and the box's load leaves the class's core
  budget available. This is the adaptive cadence: probes and heavy work consume
  the headroom that exists rather than a fixed schedule that ignores it.
- `status` is what the seed agent reads, and it is also the honest answer to
  "is the box healthy right now".

Existing heavy callers (`scripts/sb-gauntlet.sh`, `scripts/az-stage0.sh`,
`scripts/m1b-distill.sh`) already wrap themselves in `flock`+`systemd-run`
scopes. They convert to `heavy.sh`, which keeps the flock and adds the lease
and the slice. The flock stays: it is what keeps two heavy jobs from starting
at once, and the broker does not replace it.

## 4. The seed agent

A user systemd timer, every 20 minutes, `Persistent=true` so a suspended box
catches up on resume. It runs read-only against the main checkout — it never
switches branches, never commits there — and writes only under `.ds4/reward/`
and through `agentctl`'s CLI. Its rubric is committed at
`docs/agents/seed-rubric.md` so its judgement is auditable and versioned.

**It uses no model at all.** The original design gave the cycle a free GLM seat
for the judgement in step 4 and, failing that, `--triage` so a seat would write
each brief. Both were removed once the work was done, for one measured reason:
during the first live cycle every `bm-llms-glm` launch was failing (84
provider failures in 30 minutes; the serving namespace was not deployed), and a
loop whose caretaker needs a seat is a loop that stops exactly when the fleet
is already stuck. Every step is deterministic shell and Python, and each brief
comes from a per-axis template that already carries the measurement, the
out-of-scope line and a "Done means" naming a real command — so tickets are
filed as `briefed` and go straight to an implementer. The model tier is for
doing the work, not for deciding that work should be done.

### 4.1 One cycle, in priority order

1. **Stability.** `broker.sh status`, OOM lines since the last cycle, daemon
   liveness, gate timeouts, `/mnt/sata` free space, swap-in. Any finding pauses
   heavy leases immediately (the action does not wait for the seat's opinion)
   and files a P1 ticket.
2. **Stalls.** `dispatched` past `max_minutes` with no live seat,
   `human_needed`, a `waiting` pile past the hold threshold, `merge_fix`. The
   operator prompt's triage table is the rubric; the seat proposes per-ticket
   actions and the cycle applies the allowed ones.
3. **Reward regression.** Any axis stale past its max age, or worse than the
   previous head beyond its noise floor, queues a probe or a regression
   ticket.
4. **Opportunity.** Drain the top of the ranked candidate list into tickets,
   at most **3 per cycle**, each brief built from its axis template with a real
   measurement command in "Done means".
5. **Journal.** Append to `.ds4/reward/seed-journal.jsonl` and rewrite
   `.ds4/reward/SEED.md`: what it saw, what it judged, what it did, and why —
   including work it decided *not* to do. Full autonomy is only safe if every
   call it makes is legible afterwards.

### 4.2 Authority

Operator decision, 2026-09-29: full autonomy, including spec authoring. The
seed may add tickets (capped), reorder priorities, requeue parked tickets with
a written reason, queue `kind=spec` tickets for structural work, and escalate a
ticket to a paid tier. It may not merge — gates and the reviewer own that — and
it may not raise seat caps or clear the paid-off marker, which stay operator
actions.

### 4.3 Degradation

The loop must survive every tier being unavailable, because paid seats regularly
are and the free local endpoint was down the day this was built:

- Every step runs with no model. Measuring, pausing heavy work, reporting a
  stall and filing a ticket all work with the whole seat fleet dark.
- A provider storm (10+ `provider_failure`/`endpoint_down` entries in 30
  minutes) suppresses *new* opportunity tickets for that cycle and files one
  P1 naming the provider, because queueing into a dead tier only grows the
  backlog. The same holds while any ticket sits in `human_needed`.
- The seed never blocks on anything it did not get. A missing answer is a
  journal line, not a hang.

### 4.4 What the first live cycle found

Run against `main` at `2486f6a33` on 2026-09-29, before any of this was
installed on a timer:

| axis | measurement | reading |
|---|---|---|
| `flow` | `merge_fix_rate` 0.248 (199 tickets / 802 merged, 7d); worst ticket 48 resolver rounds | one merge in four needs a resolver round. The loop's first reading said 0.769 because it counted journal TRANSITIONS, not distinct tickets; corrected the same day, and the alarm threshold (0.25) is now right at the true figure |
| `flow` | `conflict_hotspots` 6 | six files edited by 2+ live branches right now |
| `steward` | `gate_wall_s` 137, `agent_context_bytes` 46273, `oversized_files` 89 (largest `rules/cast.go`, 12515 lines) | every landing pays 137s; every seat turn pays 46KB |
| `eff` | 124.4 games/s, 8.04 ms/game (`botbench -grind mono-red-prowess`) | the throughput baseline the 10x axis moves against |
| `win` | `champion_elo` 1477.9 (`sb-tactical+curve`) | from the existing gauntlet ledger, no games replayed |
| `correct` | 1126 repo-deck cards, 3 ratchet gaps, 21 param gaps → 1102 validated; 190 oracle scenarios | most of what the build claims to support has never been checked against an oracle |
| `stability` | clean | no OOM, no gate timeout, but 137 provider failures in 24h |

The `flow` number is why that axis exists: it was not visible anywhere before
the loop measured it. It is also why a measured number gets checked before it
gets acted on -- the first version of this metric was 3x too high, and the
hand pass that verified it is what found the 48-round ticket worth naming.

## 5. Why this shape

The alternative designs, and why they lost:

- **Daemon intake generator** (agentctl grows a seed job). Most integrated, but
  it puts judgement inside the tick loop: a seed bug wedges dispatch for every
  ticket. The timer keeps the loop's failure domain outside the pipeline's.
- **Claude cron in a session.** Best judgement per cycle, but session-scoped —
  it dies with the window, which is exactly the failure the operator asked to
  eliminate — and it spends the expensive tier on work a free seat can do.
- **Report-only seed.** Safest, and useless: a findings document nobody reads
  is the stall it was built to fix.

## 6. The observability checklist

`obs` needs a denominator, so the checklist is data, not prose:
`internal/botobs/checklist.json` enumerates each fact a human player has access
to, with a predicate naming where a bot could read it. A ratchet test asserts
the exposed count equals the recorded count in both directions, exactly like
the coverage ratchet: a fact that becomes exposed and is not recorded fails,
and a recorded fact that regresses fails. First entries, from the questions
that motivated this work: own library as an unordered list, own deck archetype
prior, opponent archetype posterior from revealed cards, own curve, per-zone
counts per player, and the decision-routing arm count.

The checklist's first fill and the ratchet are the `obs` axis's own first
tickets; this document does not presume the answers.

## 7. Files

| path | role |
|---|---|
| `scripts/reward.py` | scorer and ranker, `--json`/`--md`/`rank`/`--selftest` |
| `scripts/reward_collect.py` | the free collectors, one per axis, pure (prints rows) |
| `scripts/reward-probe.sh` | appends the free rows; runs the costly `eff` probe under a lease |
| `scripts/broker.sh` | classes, leases, pause/resume, `may-i`, `enforce`, `status` |
| `scripts/heavy.sh` | lease + slice + pause-contract wrapper for heavy/probe jobs |
| `scripts/seed_candidates.py` | deterministic candidate generators and ticket templates |
| `scripts/seed-agent.sh` | one cycle: measure, act, file, journal |
| `scripts/seed-install.sh` | installs/removes the user units |
| `scripts/tests/broker_smoke.sh`, `scripts/tests/seed_smoke.sh` | the tests a reviewer runs to believe the loop |
| `deploy/gorge-seed.{service,timer}` | the 20-minute timer |
| `deploy/gorge-{heavy,probe}.slice` | the cgroup bounds (a transient `--slice=` alone gets none) |
| `docs/agents/seed-rubric.md` | the seed's committed rubric |
| `orchestrator/hooks.py` | `post_gates` now also releases the broker's gate bracket |
| `.agentctl/config.toml` | `broker gate-begin` as the first gate |
| `.ds4/reward/` | scoreboard, candidates, defects, leases, markers, journal (gitignored state) |

## 8. Testing

- `scripts/reward.py --selftest` (28 checks) covers the scoring math against
  fixture ledgers: normalisation with a one-row ledger, direction per metric,
  the 10x/100x/1000x weights relative to each other, the summed axes, the
  stability veto overriding a large positive window, ranking by Δscore ÷ cost,
  and a malformed ledger row.
- `scripts/reward_collect.py --selftest` (15 checks) covers each collector
  against a synthetic repo: the merge_fix rate arithmetic, OOM counters read as
  deltas rather than boot totals, the champion pick, the validated set excluding
  both ratchet tables, defects outside the validated set not counting, gate wall
  time from log mtimes, and a missing checklist explaining itself.
- `scripts/seed_candidates.py --selftest` (14 checks) covers every generator
  firing on its trigger, every ticket body carrying a "Done means", single-line
  TSV emission, and the ranking putting the 1000x axes first.
- `scripts/tests/broker_smoke.sh` starts a fake heavy job through `heavy.sh`,
  asserts the lease appears, asserts `gate-begin` pauses it cooperatively,
  asserts SIGSTOP after the grace when the job ignores its pause file, and
  asserts the lease is gone after exit.
- `scripts/tests/seed_smoke.sh` runs one cycle with the seat stubbed, asserting
  the deterministic steps act and journal without a model, and that the ticket
  cap holds.

These run outside the gate list (they are orchestration, not engine), and the
smoke tests are what a reviewer runs to believe the loop.
