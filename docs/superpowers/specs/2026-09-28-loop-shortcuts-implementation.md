# Loop shortcuts v1: client-driven repeat and sticky choices

Status: operator-approved design (2026-09-28 session), implementation spec.
Branch: all work lands on **`feat/loop-shortcuts`**, never directly on `main`.
The branch merges to `main` as one slice after W4 passes.

Companion docs: `2026-09-28-loops-and-shortcuts-design.md` (the research
design; its §5/§6 are deferred by this spec, see §6 below) and
`2026-09-28-scam-exe-combo-lines.md`.

## 1. Decisions

| # | Decision | Ruling |
|---|---|---|
| D1 | Who v1 serves | A human pilot at a vs-bot table. Repeat also works at human tables because nothing is skipped. |
| D2 | Who drives the loop | **The client.** Every action is an ordinary `POST .../intent`, and every opponent gets every priority window. No consent protocol, no sidecar journal, no new wire message, no host change. Replay is identical to manual play by construction. |
| D3 | Model | **MTGO-style sticky choices plus "Repeat ×N"** on an activation. There are no recorded scripts. MTGO reference: <https://www.mtgo.com/news/combo-play-improvements>. |
| D4 | Pacing | vs-bot tables are already unpaced: `cmd/gorged/game.go` builds `TableConfig` without `Pace`. Repeat has its own client beat (`REPEAT_BEAT_MS = 30`) and ignores `settings.pacing`. On a paced startup table, repeat runs at table pace. A runtime pace override is out of scope. |
| D5 | Sticky key and scope | Kind + source card **name** + ability/prompt text, **per game**. The answer is a label pattern, never a wire index. If several options match, **take the first in wire order**: duplicates are ignored, not an error. |
| D6 | Sticky kinds | `target`, `choose` (single-select only), `modes`, `trigger_optional`, `trigger_order`. A `choose` of `mana` options with `min = max = 1` is stickable ("always add B from sources named X"). Mana PAYMENT windows (`mana_payment`, `autofill`) and multi-mana allocations (`min > 1`) are non-sticky; an unsettled one halts repeat as `payment`. Repeat arms on both `ability` and mana `activate` options. |

## 2. Sticky choices (`web/src/lib/sticky.ts`, new)

Modelled on `web/src/lib/yields.ts`: an in-memory map plus a sessionStorage
mirror, keyed by `${table}:${match}`. It uses the swallowed-throw storage
pattern and exports `clear`.

```ts
export type StickyKind = 'target' | 'choose' | 'modes' | 'trigger_optional' | 'trigger_order';

export interface StickyRule {
  key: string;            // stickyKey(...)
  kind: StickyKind;
  label: string;          // display: "<source name>: <prompt tail>"
  // target/choose/modes/trigger_optional: the chosen option's label; for
  // target also the chosen object's controller seat (null for a player target).
  answer?: { label: string; controller: number | null };
  // trigger_order: the answered order as `${name}\u0000${text}` entries.
  order?: string[];
}

export function stickyKey(d: Decision, view: View): string | null;   // null = not stickable
export function stickyAnswer(d: Decision, view: View, rules: ReadonlyMap<string, StickyRule>): number[] | null;
export function ruleFromAnswer(d: Decision, view: View, chosen: number[]): StickyRule | null;
```

- **Key.** `kind \0 sourceName \0 prompt`. `sourceName` resolves
  `Decision.source` (an object id, `web/src/protocol.ts`) through the view's
  objects. If the object can't be found, the name is `''` and the prompt
  alone keys the rule; the engine's prompts already embed the source name
  (`rules/trigger_queue.go` `triggerLabel`). For `trigger_order`, the key is
  `kind \0` + the sorted `name \0 text` entries of the listed triggers.
- **Not stickable** (`stickyKey` returns `null`): any other kind, `choose`
  with `max > 1`, `choose` offering an X/amount value, mana allocations other
  than a one-pick colour choice, and any `mana_payment`/`autofill` window.
- **Matching.** `target`: options whose `label` equals `answer.label`, and
  whose controller equals `answer.controller` when that is non-null.
  `choose`/`modes`/`trigger_optional`: equal `label`. Take the first match
  in wire order. No match → `null`, meaning the rule does not fire.
  `trigger_order`: map each stored entry to the first unused option with
  that name/text. If the multiset doesn't match exactly → `null`.
- **Firing.** `decide()` in `web/src/lib/autopilot.ts` gains a sticky step
  that runs before the auto-pass rules. A hit returns
  `{ act: 'answer', choices }`, which `SeatPanelState` posts through the
  same `post()` a click uses (`seatpanel.svelte.ts`, around the `post` /
  `postIntent` call). Sticky answers fire whenever the decision appears,
  inside or outside a repeat; that is MTGO behaviour and makes trigger-only
  loops run by themselves.
- **Coexistence.** `web/src/lib/remembered.ts` (global `trigger_optional`
  memory) is untouched. If both stores hold a rule, sticky wins, because it
  is the per-game and more recent intent.

## 3. Sticky UI

- A **"Sticky" checkbox** on every stickable decision, next to the submit
  button and styled like the remembered-answer checkbox. Answering with it
  ticked stores `ruleFromAnswer(...)`.
- A **marker** on a decision a sticky rule answered, and on the source card
  while any rule names it. It is the counterpart of MTGO's yellow triangle:
  small, with an accessible label.
- **GAME OPTIONS** (`web/src/components/PlaySettingsPanel.svelte`, next to
  "Clear yields"): lists active sticky rules by `label`, with a forget
  button each and "Clear sticky choices".
- **Hotkey `5`** → `clear-sticky` in `web/src/lib/hotkeys.ts`. `5` is
  unbound today. It follows the same focus guard as the other non-panic keys.

## 4. Repeat driver (`web/src/lib/repeat.ts`, new; wired in `seatpanel.svelte.ts`)

Repeat is a **fourth one-shot run**:
`oneShot: 'none' | 'end-turn' | 'hard-skip' | 'resolve-all' | 'repeat'`.
It reuses `armRun`/`cancelRun`, and `Escape` → `cancel-run` stops it with
no new binding.

```ts
export interface RepeatPlan {
  sourceName: string;      // the activated card's name
  abilityText: string;     // the priority option's label/ability text
  target: number;          // N, 1..REPEAT_MAX
  done: number;            // activations issued
  ownStackIds: Set<number>;// stack ids created since arming (ours)
}
export const REPEAT_MAX = 999;
export const REPEAT_BEAT_MS = 30;

export type RepeatStep =
  | { act: 'activate'; index: number }
  | { act: 'pass'; index: number }
  | { act: 'halt'; reason: RepeatHalt };
export type RepeatHalt = 'done' | 'not_offered' | 'opponent_stack' | 'unanswered_decision'
  | 'game_over' | 'cancelled' | 'undo' | 'post_error' | 'payment';

export function repeatStep(plan: RepeatPlan, d: Decision, view: View, seat: number): RepeatStep; // pure
```

- **Arming.** After the pilot manually activates an ability (a `priority`
  option of kind `ability` or mana `activate`), the source card offers
  **"Repeat ×N"** beside its OptionPicker, with a number input: default 10,
  clamped 1..`REPEAT_MAX`. A non-mana `ability` also offers it on the resulting
  stack tile. Mana abilities never use the stack: arm from the source card
  only, never from a resulting trigger tile. The manual activation is
  iteration 1, so the arm sets `done = 1`.
- **Step function** (for each fresh decision for this seat while `oneShot === 'repeat'`):
  1. Not `priority` → the sticky step answers it, or `halt('unanswered_decision')`.
     A one-pick (`min = max = 1`) `choose` of `mana` options gets sticky
     first refusal like any single-select choose; without a matching sticky
     it halts as `unanswered_decision`. Multi-mana allocations (`min > 1`),
     `mana_payment` windows and `autofill` remain non-sticky and halt as
     `payment` if unsettled.
  2. The stack holds an object not in `ownStackIds` and controlled by
     another seat → `halt('opponent_stack')`. Objects we control that a
     sticky/auto answer created are added to `ownStackIds` as they appear.
  3. Stack non-empty → `pass`. Our own items resolve, and bots pass by default.
  4. Stack empty: `done === target` → `halt('done')`. Otherwise find the
     first `priority` option with `kind === 'ability' || kind === 'activate'` whose source name
     and ability text match the plan: found → `activate`, `done++`;
     missing → `halt('not_offered')`.
- **Mana iteration.** Activate → sticky sacrifice and colour choices →
  triggers on the stack → passes (sticky target/pay prompts) until the stack
  is empty → re-activate. Mana left in the pool does not prevent repetition.
- **Other halts:** game over, `cancel-run` (Escape or the Stop button), an
  undo/rewind (`rewind()` sets `machinePaused`), and any POST error.
- **Caps.** Repeat does not count toward the shared auto-pass cap
  (`AUTO_PASS_CAP = 40`). Its own bound is `target` activations plus a
  per-activation pass allowance (`REPEAT_PASSES_PER_ITERATION = 64`). Going
  over it → `halt('unanswered_decision')`, so a runaway can't spin.
- **Beat.** Posts are spaced `REPEAT_BEAT_MS` apart; `settings.pacing` is
  not used.
- **Visibility.** A progress chip reads "Repeating <source> 7/20 · Stop".
  A halt shows a toast naming the reason. `web/src/lib/autolog.ts` gets one
  folded note per repeat ("Repeat <source> ×20: done 20, halted: done"),
  not one per pass. The transcript itself is unchanged: those rows are real
  events.

## 5. Tickets

All web tickets run in `scripts/agent-worktree.sh <id> feat/loop-shortcuts --web`
worktrees. npm is allowed only in such a worktree. Every ticket's base
branch, rebase target and merge target is **`feat/loop-shortcuts`** (agentctl
`--target-branch feat/loop-shortcuts`). No ticket merges into or rebases
onto `main`.

| # | Ticket | Files | Done means | Test |
|---|---|---|---|---|
| W1 | Sticky store + `decide()` firing | new `web/src/lib/sticky.ts`, `sticky.test.ts`; `web/src/lib/autopilot.ts` (+ its test) | §2 as written. Tests cover all five kinds (hit, miss, duplicate takes first, controller mismatch, stale source name falling back to prompt), `choose` max>1 and X not stickable, per-game isolation (a match N rule is absent in N+1), clear, corrupt storage → empty. `decide()`: a sticky hit returns the matching indices before auto-pass, a miss falls through unchanged. Every existing autopilot test still passes. | `cd web && npx vitest run src/lib/sticky src/lib/autopilot && npm run check` |
| W2 | Sticky UI | seat panel decision components, `web/src/components/PlaySettingsPanel.svelte`, `web/src/lib/hotkeys.ts`, `web/src/lib/seatpanel.svelte.ts` (store wiring only) | §3 as written: checkbox stores a rule, marker, list with forget/clear, `5` clears. Component tests for the checkbox → rule round trip and for clear. | `cd web && npx vitest run src/lib/hotkeys src/lib/seatpanel && npm run check` |
| W3 | Repeat driver | new `web/src/lib/repeat.ts`, `repeat.test.ts`; `web/src/lib/seatpanel.svelte.ts`; `web/src/lib/autolog.ts`; stack/card tile component for the arm affordance | §4 as written. Pure `repeatStep` tests for every `RepeatHalt`, for `done` counting from the manual first activation, for own versus opponent stack objects, for the pass-allowance cap, and for exemption from `AUTO_PASS_CAP`. A seatpanel test covers arm → N activations → `halt('done')` against a scripted view sequence, and Escape cancels mid-run. | `cd web && npx vitest run src/lib/repeat src/lib/seatpanel src/lib/autolog && npm run check` |
| W4 | Live smoke (verification, no product code) | report only; a test deck is not needed: `internal/testutil/decks/rakdos-muscle-scam-exe.json` holds Forsaken Miner, Phyrexian Altar, Rakdos the Muscle and Golgari Thug | Build `gorged` and the web bundle in the worktree. Run on one port from 8090–8099 (`scripts/fleet.sh port`) with persistence under `/tmp/gorge-loop-smoke-<ts>`. Start a vs-bot constructed game with `human_deck` = that deck, reach the Miner board (via play or a feedback-fixture setup), make stickies for the target, sacrifice, one-pick B colour and pay prompts, and arm Repeat ×20 from Phyrexian Altar's source card (not a stack tile). Record wall time, final opponent library, and the halt reason. Capture feedback and run `go run ./cmd/repro <dir>`: it must exit 0 (verified replay). Stop the server by pid found via `ss -lptn`. | the `cmd/repro` exit code plus the recorded numbers |

**Collision map.** W1 → W2 → W3 run strictly in sequence, since they share
`autopilot.ts`/`seatpanel.svelte.ts`. W4 runs after W3. Engine tickets E1–E5
touch no web files and may run in parallel with the W chain, but E1/E2
share `rules/loops_prototype_test.go`, so E1 goes before E2.

### Engine/host tickets (also into `feat/loop-shortcuts`)

| # | Ticket | Done means | Test |
|---|---|---|---|
| E1 | Thug prototype flip | Non-strict `TestLoopPrototypeThug` runs and asserts the real N=1/20 line; it no longer reports "blocked". Measured 2026-09-28 in strict mode: N=1 1.5 ms/iter, N=20 17.3 ms/iter, pool 40 at N=20. Update the fixture's status text. | `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -run '^TestLoopPrototypeThug' -v ./rules/` |
| E2 | Per-iteration growth profile | **Profile only, no fix.** Per-iteration cost grows with N: Miner 2.5→~50 ms from N=20 to N=100, Thug 1.5→17 ms from N=1 to N=20. Produce a CPU+alloc profile at two Ns, name the growing function(s) with evidence, and propose the fix as a follow-up ticket text in the report. | `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -run '^TestLoopPrototypeMiner' -cpuprofile ... ./rules/` (small slot by day) |
| E3 | Host loop-latency bench | A `host` benchmark drives the Miner line ×N through the table run loop (Submit + projection + fan-out per decision, `Pace` 0, one bot seat), reporting ns/decision at N=20 and N=100. It measures what the repeat driver pays per decision. No behaviour change. | `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -run '^$' -bench BenchmarkHostLoopRepeat -benchmem ./host/` |
| E4 | L1 evaluator cycle guard | Design §7: every recursive evaluator in `botpolicy` has a path-local visiting bitset and a depth-32 / 4096-node budget. Tests cover self-reanimation, two-card mutual recursion, a long acyclic chain, and legal repeated visits on separate branches. | `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -run TestEvaluationCycleBudget ./botpolicy/` |
| E5 | Coverage-semantics guard | The Scam.EXE deck enters the full-support ratchet only when its prototype lines pass, not on an empty census alone (Thug was the false positive). | `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -run 'TestEveryRepoDeckIsFullySupported' ./rules/` |

## 6. What this defers from the research design

Everything in `2026-09-28-loops-and-shortcuts-design.md` §5 and §6 is
deferred, not rejected: the negotiation messages, the sidecar journal,
certificates, the host batch executor, `shorten`, counts above
`REPEAT_MAX`, and tournament/mandatory-loop adjudication (L10). They become
necessary only for ×1,000,000 proposals, opponent-consented skips, or
tournament play. L2/L3 (catalog, recognition) remain future work. A later
"live combo" hint would pre-fill a Repeat plus its sticky rules, and needs
no new execution path.

## 7. Merge back to `main`

After W1–W4 and E1–E5 land on `feat/loop-shortcuts` and W4's report shows a
verified replay, the operator merges `feat/loop-shortcuts` into `main`. That
merge is a hand merge, so the full suite runs first (night slot), plus the
web suite in a `--web` worktree.
