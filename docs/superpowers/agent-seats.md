# Agent seats available for gorge work

*Verified 2026-09-19. Re-verify with `pi --list-models` and the `enabledModels`
key in `~/.pi/agent/settings.json` before relying on it — this file records what
was dispatchable, not what exists.*

**All `anthropic/*` seats currently fail at the first request (2026-09-19):**
Anthropic third-party apps draw from the account's EXTRA USAGE, not plan limits,
and the account carries none — `400 "Add more at claude.ai/settings/usage"`.
The seats below are enabled and authenticated but undepatchable until credits
are added; prove any anthropic dispatch with a one-turn `pi -p` smoke test
first.

Implementation work is offloaded to agent seats; the expensive model is the
reviewer and the gate, not the implementer. This file records **which seats can
actually be dispatched**, because `pi --list-models` prints a large catalogue
(huggingface, openai, anthropic, deepseek…) of which only a small subset is
enabled. Dispatching a model outside that set fails at launch.

## Dispatchable through `pi-agent`

`enabledModels` in `~/.pi/agent/settings.json` is the authority:

| Seat | `--provider` / `--model` | Context / max out | Images | Cost | Use for |
|---|---|---|---|---|---|
| **Local (default)** | `bm-llms` / `DeepSeek-V4-Flash` | 262K / 32K | no | free | all ordinary task work |
| **Local vision** | `bm-llms-vision` / `DeepSeek-V4-Flash-Vision-Exp` | 1M / 32K | **yes** | free | visual/UI work, screenshot review |
| Codex | `openai-codex` / `gpt-6.1-sol` | 272K / 128K | yes | ChatGPT plan | escalation (gorge `sol` tier since 2026-10-05) |
| Codex | `openai-codex` / `gpt-6-luna` | 272K / 128K | yes | ChatGPT plan | review / triage / implement (gorge `luna` tier) |
| Codex | `openai-codex` / `gpt-6-astra` | 272K / 128K | yes | ChatGPT plan | not for reviews (user rule); top-end rescue only |
| Codex | `openai-codex` / `gpt-6-sol` | 272K / 128K | yes | ChatGPT plan | superseded by gpt-6.1-sol |
| Codex | `openai-codex` / `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.6-luna`, `gpt-5.5`, `gpt-5.3-codex-spark` | 272K / 128K (spark 128K) | yes | ChatGPT plan | legacy; still enabled, not used by tiers |
| DeepSeek | `deepinfra` / `deepseek-ai/DeepSeek-V4.1-Flash` | 1M / 384K | no | pay-per-token $0.14/$0.42 per M | escalation — cheapest paid seat, measured 145–190 tok/s |
| Claude | `anthropic` / `claude-opus-4-8` | 1M / 128K | yes | Anthropic extra usage | rescue (currently blocked: no extra-usage credits) |

There are TWO local providers, on different hosts:

- `bm-llms` → `http://ds4-active.llm.local/v1`, text only.
- `bm-llms-vision` → `http://ds4-r8-vision.llm.local/v1`, text **and images**,
  1M context. Both must appear in `enabledModels` to be dispatchable;
  `bm-llms-vision/*` was added 2026-09-07.

Check a local seat's HOST before blaming a model: the two are served
separately and one can be down while the other is up. `curl -o /dev/null -w
'%{http_code}' <baseUrl>/models` is the whole diagnosis, and a `000` means DNS
or routing, not the model.

The codex seats were **authenticated** at the time of writing
(`pi auth check --provider openai-codex`).

**The 5.6 variants are unbenchmarked on this repo.** luna / terra / sol have not
been compared here, and neither has astra against them. Do not assert one is
better; if the choice matters, run the same brief on two worktrees and measure,
the way the pi-vs-ds4 A/B was run.

## Claude subagents

The `Agent` tool takes a `model` of `opus`, `sonnet`, `haiku` or `fable`. On a
Claude subscription these draw on the user's plan rather than being billed per
token; they are still a limited seat, not the default implementer.

**The old "visual and design work always goes to Claude first, no ladder" rule
is REMOVED (user, 2026-09-07).** It existed because the only local model could
not see images. `bm-llms-vision` can, so a UI task with a screenshot to review
is ordinary local work and takes the ordinary ladder. Do not route a task to
Claude merely because it is visual.

Claude subagents remain the right seat for **rescue**, after a task has failed
two local fix rounds, and for work that genuinely needs their design judgement
rather than merely needing to see a picture.

A Claude subagent is **invisible to the monitor at :8765**, which globs
`.worktrees/*/.ds4/{transcripts,pi-sessions}/*.jsonl` — Claude seats write none
of those. `scripts/fleet.sh status` labels which seats the dash cannot show and
when each last wrote. Do not synthesise fake pi transcripts to force them onto
the dash: an invented heartbeat is worse than an honest gap.

## Choosing a seat

1. **Default to the local seat.** It is free and good enough when the brief is
   sharp. A vague brief is the usual cause of a bad local diff, not the model.
2. **A visual task goes to the local VISION seat** (`bm-llms-vision`), not to
   Claude. It can read a screenshot, so it takes the ordinary ladder like any
   other task.
3. **Escalate on evidence, not on the task feeling important.** Two failed local
   fix rounds, or fabricated evidence (a report claiming a test passed that your
   own run fails), or the same failure signature twice.
4. **Paid seat is the cheaper escalation** — offer it and name a model, then let the
   user pick. `deepinfra/deepseek-ai/DeepSeek-V4.1-Flash` is the default offer
   (per-token, cheapest); codex models when the task wants a plan model. Either is an
   alternative to a Claude implementer, not a replacement for the two local rounds.
   Note: pi's built-in `deepseek` provider (api.deepseek.com) is NOT this seat and
   must not be dispatched — the `DEEPSEEK_API_KEY` env var holds the DeepInfra key,
   so the built-in looks authed but every call fails.
5. **Claude implementer last.**

## Concurrency — two pools, not one cap

User ruling, 2026-09-07. The seat limit is **two independent pools**, because
the two ceilings exist for unrelated reasons:

| pool | cap | claimed with | what the cap protects |
|---|---|---|---|
| **local** | 3 | `fleet.sh claim <id> --local` (the default) | THIS BOX **and the GPU**. Past the cap it saturates and load-sensitive tests fail for reasons unrelated to any diff. |
| **paid** | 1 | `fleet.sh claim <id> --paid` | The MONEY. Codex seats share one ChatGPT plan's rate limit and throttle each other and the user's own sessions; the deepinfra seat has no rate limit but is pure per-token burn ($0.14/$0.42 per M). Either way one paid seat runs at a time. |

So **4 agents can run at once** — 3 local plus 1 paid (user ruling
2026-09-07, lowered from 4+2; see "The local cap is about the GPU too" below). A full local pool does
NOT block a codex dispatch, and a full paid pool does not block a local one;
`fleet.sh claim` says which pool is full and points at the other. Counting them
against one number was wrong: it made a free seat and a paid seat
interchangeable when they are limited by different resources entirely.

Pass the pool as a flag. A dispatch knows which seat it is about to spend, and
recovering it afterwards from a transcript is how a paid run gets miscounted as
free. A seat directory with no `kind` file predates pools and counts as local.

Claude subagents load neither pool, so a full fleet routes visual/rescue work
there rather than into a queue.

## The local cap is about the GPU too, not just the box

Lowered to 3 local / 1 paid by user ruling 2026-09-07. On that date a CUDA OOM
in the vLLM engine serving `ds4-r8-vision` killed **three concurrent local
seats within 2.1 seconds of each other** — one event, not three failures. The
engine runs at `gpu_memory_utilization: 0.975` (weights 83.06 GiB, KV cache
7.97 GiB, of 94.97 GiB), so a transient runtime allocation has nowhere to go:
the crash was a 34 MiB allocation failing with 73 MiB free, inside the
attention forward pass while NCCL was lazily initialising a communicator.

Two things follow that are easy to get wrong:

- **It was not KV-cache exhaustion.** Usage was 15-19% and `Running: 2-3 reqs`
  against `max_num_seqs: 4`. So "N agents saturates the box" is not the
  mechanism, and host RAM is not either (`/proc/vmstat` `oom_kill 0` — the
  kernel OOM killer has never fired). Concurrent streams are an amplifier of
  peak activation memory, not the cause.
- **The harness reports a dead engine as a CLEAN EXIT.** All three seats wrote
  `"exit_code": 0` with `"status": "UNKNOWN"`, because the agent was cut off
  before emitting its `STATUS=` trailer. A seat that vanishes with exit 0 and
  no status has probably NOT finished — check for uncommitted work in its
  worktree before you reset it. Two of those three trees held a near-complete
  implementation; a fresh agent dropped on top would have destroyed it.

The permanent fix is on the deployment, not here: lower
`gpu_memory_utilization` to ~0.93, or pin the KV cache with
`--kv-cache-memory=8190094951` as the engine's own startup log suggests.

## Thinking level and model preference (user, 2026-09-07)

- **ds4 seats run at `--thinking medium`.** A previous round found medium
  outperformed high on this repo for the local model. Do not raise it to high
  without a measurement that says so.
- **Prefer the gpt seats for non-ds4 work** — `gpt-6.1-sol` (implement,
  escalation) and `gpt-6-luna` (review, triage); the gpt-5.6 series is legacy. When a task is going to a paid seat rather than the local one,
  a codex model is the default choice, not Claude.
- **`claude-opus-4-8` runs at `--thinking medium` (user, 2026-09-19)** —
  `pi-agent --provider anthropic --model claude-opus-4-8 --thinking medium`
  (the wrapper's own default). Do not raise it without a measurement.

## Stopping a pi-agent: kill the TREE, by explicit pid

`pi-agent` is a wrapper. Sending TERM to the pid you launched **orphans its
children rather than stopping them** — the `pi` process and its bwrap jail
survive, reparent to init, and keep writing into the worktree. Observed
2026-09-07: four seats were "stopped" and relaunched, and the result was two
agents per worktree editing the same files, with the tree reset underneath
both.

Walk the tree first and kill every pid in it, deepest first:

    ps -eo pid,ppid,lstart,args --no-headers | grep -- '[-]-name <id>'

then `kill` each number. Never a `-f` pattern. Verify the count is zero before
relaunching, and `git reset --hard` the worktree AFTER the last process is
gone, never before.

Launch with `setsid` so the run survives the dispatching shell.

## Always evaluate an agent's tool errors when it completes

Standing rule, user, 2026-09-07. `STATUS=DONE` does not mean the run was clean.
Every finished agent's tool-error count must be READ and JUDGED before the diff
is gated — the dash shows it per row (`tool errs N`) and the `--out` JSON
carries it as `tool_errors`.

The count on its own means nothing; only the errors themselves do. Pull them
out of the transcript (match each `toolResult` with `isError` back to the
`toolCall` that caused it) and sort each into one of three buckets:

- **Expected** — the error IS the deliverable. A mutation test disables a guard
  and runs the suite; the suite fails; that failure is recorded as a tool error
  even though it proves the guard works. Task mt1 finished with 4 tool errors of
  which 3 were exactly this. Never treat these as a defect.
- **Benign** — a `read` of a path that does not exist, a grep with no match. The
  agent recovered and moved on. Note and drop.
- **Real** — a gate the agent could not run, a command it retried and abandoned,
  a compile failure it worked around rather than fixed. These change the merge
  decision: whatever that command was going to verify is UNVERIFIED, and the
  agent's report may claim otherwise.

Record the bucket counts in the merge note. An agent that reports `DONE` while
its transcript shows a real error on the very gate its report claims to have
run is the "fabricated evidence" case, and it is grounds for escalation.

Related: `--out`'s `report_written` is computed against the WRAPPER's cwd, not
the worktree, so a report written to the brief's worktree-relative path reads as
`false` even when the file is there. Check the file yourself before believing
that field — it is the same shape of lie as `commits: []`.

## Every seat gets a worktree from the script

`scripts/agent-worktree.sh <id> [base] [--web]`. Never `git worktree add` by
hand: it carries only tracked files, and a worktree without the `.cards` symlink
makes every corpus-dependent test **skip** rather than fail — the package still
prints `ok` (in 2ms) and the agent, the pre-filter and the gate all read a green
run that executed nothing. Four worktrees across both threads have been poisoned
this way. The script links the corpus and then *proves* it is reachable before
declaring the worktree ready.

A seat parks an idle sibling worktree with
`APPLY=1 scripts/park-branch.sh --registry-only <branch>`. The seat's jail
mounts sibling worktrees read-only, so a plain park aborts on
`git worktree remove`'s final `rm -rf` (EROFS); the `--registry-only` form
deregisters the worktree, keeps the branch, and **leaves the directory on disk
for the host** — `make clean-worktrees` (APPLY=1) reclaims it as an orphaned
dir. This is the sanctioned seat-side park procedure, not a workaround; the
plain (no flag) park remains the host path and still removes the directory.
