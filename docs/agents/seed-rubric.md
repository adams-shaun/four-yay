# The seed agent's rubric

`scripts/seed-agent.sh` runs this rubric every 20 minutes from a user systemd
timer. It is the loop's own caretaker: it keeps the box safe, keeps the pipeline
moving, and keeps work flowing toward the reward axes when nobody is steering.

This file is the rubric's source of truth. Changing the seed's judgement means
changing this file in the same commit as the script, so a decision the seed made
last week can be read against the rules it had at the time.

Design: [`docs/superpowers/specs/2026-09-29-reward-loop-and-seed-agent-design.md`](../superpowers/specs/2026-09-29-reward-loop-and-seed-agent-design.md).

## What it is not

It is not a model. Every step below is deterministic shell and Python, and every
brief it files is written from a template that already carries the measurement,
the out-of-scope line and a "Done means" naming a real command — so tickets are
filed as `briefed`, not `--triage`. Nothing in the cycle waits on a model
answer, which is why the free-seat outage live during the first cycle (every
`bm-llms-glm` launch 503ing, 84 failures in 30 minutes) could not stop the seed
from measuring, pausing, reporting and filing.

## Priority order

The order is fixed, and a step never runs before the one above it has acted.

### 1. Stability — the 100x veto

Reward weight: a stability event makes the whole window's score negative.

- `broker.sh enforce` first, always: escalate overdue pauses, shed a heavy
  lease if available memory is under the kill floor.
- Measure the free axes (`reward-probe.sh free`). If `reward.py` reports
  `blocked_by_stability`, pause every heavy lease immediately — before any
  ticket is filed, because the pause is what stops the next OOM.
- File one P1 ticket per distinct stability event per head. Duplicate suppression
  is by marker file, not by judgement.

An OOM or a starved gate costs more than any axis can buy, so nothing else in
the cycle runs until this step has acted.

### 2. Stalls — the pipeline cannot move itself

- `dispatched` past `max_minutes` with no live seat: a seat died at step 1 and
  reported success. Record it; the daemon relaunches once the launch marker ages
  out, so the seed reports rather than intervenes unless it recurs.
- `human_needed`: the ladder ran out. The seed summarises the ticket's History
  into the journal and, when the count crosses the hold threshold, files one
  escalation ticket naming every parked id so a Claude session has a single
  entry point.
- A `provider_failure` / `endpoint_down` storm in the last 30 minutes: the seat
  tier is down, not the tickets. Record it, name the provider, and do not queue
  new work that cycle — queueing into a dead tier only grows the backlog.
- `waiting` over the configured hold threshold: the queue is already draining;
  the seed adds nothing that cycle.

Whether a parked ticket holds new work is the REPO's decision, read from
`policy.hold_new_while_parked` in `.agentctl/config.toml` -- gorge sets it false
after measuring 686 of 1440 minutes on hold in one day. The seed shipped with a
stricter rule of its own and held every reward ticket behind three parked ones;
an orchestrator that overrides a measured operator decision is guessing, not
steering. A live provider storm still withholds, because that is a fact about
right now rather than a policy.

### 3. Reward regression

For each axis with a measured delta, a move in the wrong direction past its own
noise scale gets one ticket, priority by axis weight: `correct` and `flow`
(1000x) first, `steward` (100x), `stability` already handled, `eff` (10x), then
`win`, `obs`, `audit`.

### 4. Opportunity — at most 3 tickets per cycle

Candidates are generated deterministically and ranked by
`Δreward ÷ expected cost` (`reward.py rank`), which is what keeps the loop from
brute-forcing win rate: the same strength bought more cheaply outranks more
strength bought expensively.

Generators, in the order they are consulted:

- **flow (1000x).** Every file two or more live task branches are editing
  (`reward_collect.py hotspots`). The ticket is to remove the contention —
  split the file along its seams, or sequence the tickets that share it — not
  to resolve one conflict. This is the axis with the worst live number in this
  repo: `merge_fix_rate` was 0.77 when the loop was built.
- **correct (1000x).** A card in the validated set — a repo-deck card neither
  ratchet table lists, so the build claims full support — with no oracle
  verdict. The ticket is to drive it through the oracle harness and record the
  verdict; a disagreement becomes a defect row in `.ds4/reward/defects.jsonl`,
  which is what the axis pays for.
- **steward (100x).** The largest source file, the gate suite's slowest gate,
  the agent context's growth. The ticket names the measured number and the
  number it must reach.
- **eff (10x).** A profile hotspot from the last `eff` probe, with a benchstat
  threshold in "Done means".
- **obs, audit, win (1x).** Checklist gaps, unaudited families, champion
  promotion when a candidate's CI clears the incumbent.

Every ticket carries its evidence: the measurement, the command that produced
it, and a "Done means" that re-runs that command.

### 5. Journal

Append one record per cycle to `.ds4/reward/seed-journal.jsonl` and rewrite
`.ds4/reward/SEED.md`: what it saw, what it judged, what it did, and what it
decided *not* to do and why. Full autonomy is only safe while every call is
legible afterwards.

## Hard limits

The seed may queue tickets, reorder priorities and pause heavy work. It may
not:

- merge anything (gates and the reviewer own that),
- raise seat caps or clear the paid-off marker (operator actions),
- `git checkout`, `git reset` or commit in the main checkout or any seat
  worktree,
- queue more than 3 opportunity tickets in one cycle, or queue any while a
  provider storm or a `human_needed` backlog is live.
