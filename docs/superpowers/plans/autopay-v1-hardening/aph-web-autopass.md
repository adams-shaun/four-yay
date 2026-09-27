# Web auto-pay: plan-only casts are not an empty window; own-spell auto-pass follows the seat preference

Suggested issue ID: `aph-web-autopass`
Priority: 2
Kind: `payment-plan`
Size: S
Lane: web (Claude/codex seat; not the DeepSeek pipeline)
Depends-On: none
Changes offers: no (client only)

## Goal

Spec §8 (amended to the shipped mode-switch UX) keeps the shipped UI and fixes
one behaviour. With the seat's auto-pay preference ON:

1. **A plan-only cast is a real play.** `emptyPriorityWindow`
   (`web/src/lib/autopilot.ts:248`) and `decide()`'s actionable tests (the
   'smart' step rule and the own-turn main-phase floor, both through
   `actionables()`/`actionable()`, `autopilot.ts:183`) decide "nothing to do"
   from `decision.options` plus the server's `potential_actions`. A priority
   decision whose only play is a payment action (no legacy cast option,
   because the cast needs mana floated first) is then auto-passed whenever
   `potential_actions` does not also list the card (e.g. the Urborg-granted
   intrinsic case the engine misses today). Make a decision carrying a payment
   action with at least one plan count as actionable when -- and only when --
   the preference is on. Thread the preference in explicitly (a new argument
   or settings field on `emptyPriorityWindow`/`actionables`/`decide`), so the
   floor and `decide()` keep sharing one shape test.
   History: `7022042e6` (which squashed `4757be4e9` "keep auto-pay out of
   autopass policy") removed a blanket `derivePass` guard that stopped Auto
   from passing ANY window holding a plan; do not restore that blanket guard.
   The fix is the actionable test only.
2. **Own-spell auto-resolution follows the preference, not the table.**
   `decide()`'s own-object branch passes on "payment-plan tables"
   (`autopilot.ts:409` `if (autoManaAvailable) return { act: 'pass', … }`),
   and `SeatPanelState` feeds it `autoManaAvailable: this.autoManaAvailable`
   (`web/src/lib/seatpanel.svelte.ts:1451`, `:1663`), i.e. the table's
   `auto_mana` capability. Verified: on any capability table (gorged default
   `-auto-mana=true`) a player who never turns auto-pay on loses the
   historical own-stack stops. Spec §8: with the preference off, behave
   exactly as a capability-less table. Key it on `autoPayMana`.
3. **Remove the dead `Pay manually` branch.** `SeatPanel.svelte:719-723`
   renders `data-payment-manual` only when `!logic.autoPayMana` inside a block
   that only renders when `autoPayMana` is true. The amended spec has no
   `Pay manually` action (switch the preference off instead). Keep the
   "Tap mana manually, then cast." text only if it is reachable; otherwise
   remove it with the branch.
4. **Pin the untested PP-18/19 clauses**: toggling after a submission does not
   alter the in-flight post (exactly one post, carrying the plan chosen at
   click time); a stale HTTP response for seq N arriving after adopting seq
   N+1 attaches no plan and posts nothing for N.

Everything else in the shipped UX stays (mode chip, hidden manual mana taps
while on, `CAST` hand shortcut, legacy-click-submits-plan).

## Evidence

- Gaps audit web probe, `git show 124ed89fb:web/src/lib/payment.audit.test.ts`:
  the third probe (`emptyPriorityWindow(plannedOnly, …)` expected null, got 1)
  fails on main; the first two probes test spec text that the amendment
  withdraws (one-cast action while off; `Pay manually` while on) -- do not port
  them, port the third with the preference argument.
- Code on main `6c711fece`: `autopilot.ts:248-255` (`emptyPriorityWindow`),
  `:362-364` and `:400-410` (`autoManaAvailable` arg and own-object pass),
  `seatpanel.svelte.ts:1409-1455` (`derivePass`), `:1663`
  (`decide` call with `autoManaAvailable`), `:564-575` (`autoPayMana`,
  `setAutoManaAvailable`, `setAutoPayMana`), `SeatPanel.svelte:192`,
  `:719-723`; `autopilot.test.ts:369`, `:395` pin today's capability-keyed
  behaviour and must be rewritten to the preference.
- Suites run under node 24 (gaps audit): `seatpanel.payment.test.ts`,
  `SeatPanel.svelte.test.ts`, `autopilot.test.ts`, `HandFan.svelte.test.ts`,
  `HotButtonStrip.svelte.test.ts` = 158 tests pass on main; full web suite
  113 files / 1,363 tests.

## Files (symbols)

- `web/src/lib/autopilot.ts`: `emptyPriorityWindow`, `actionables`/`actionable`,
  `decide` (rename the `autoManaAvailable` arg to the preference it now means).
- `web/src/lib/seatpanel.svelte.ts`: `derivePass`, the `decide(...)` call near
  `:1663`.
- `web/src/components/SeatPanel.svelte`: remove the dead branch.
- Tests: `web/src/lib/autopilot.test.ts`, `web/src/lib/seatpanel.payment.test.ts`,
  `web/src/components/SeatPanel.svelte.test.ts` (drop the assertion that
  `data-payment-manual` is absent only if the markup no longer exists).

## Out of scope

- Server changes; last-resort disclosure/confirmation (`aph-last-resort-web`).
- Reintroducing a one-cast "Cast with suggested mana" action while off, or a
  `Pay manually` button (both withdrawn by the amendment).
- `npm install` / `npm ci` (never; `web/node_modules` is shared). Follow the
  repo's UI rules for rebuilding the bundle if the change ships.

## Done means

- `cd web && PATH=~/.nvm/versions/node/v24.15.0/bin:$PATH VITE_CACHE_DIR=<scratch> npx vitest run src/lib/autopilot.test.ts src/lib/seatpanel.payment.test.ts src/components/SeatPanel.svelte.test.ts src/components/HandFan.svelte.test.ts src/components/HotButtonStrip.svelte.test.ts`
  passes, including new tests:
  - preference on, decision = {activate Island for mana, pass, concede} +
    one payment action with a plan, no `potential_actions` for the card:
    `emptyPriorityWindow(...)` is null and `decide()` with step rule 'smart'
    stops;
  - the same decision with the preference off: `emptyPriorityWindow` returns
    the pass index (unchanged behaviour);
  - own spell on the stack, capability on, preference off: `decide()` does NOT
    take the payment-table own-object pass (falls through to step rules as on
    a capability-less table); preference on: it does;
  - toggle-after-submit: one post, carrying the plan chosen at click time;
  - stale response: after adopting seq N+1, a delayed response for N posts
    nothing and attaches no plan.
- Full web suite passes; `npx svelte-check --tsconfig ./tsconfig.json` 0 errors.

## Standing rules

- Authority: `docs/superpowers/specs/2026-09-24-cast-payment-plans.md` as
  amended 2026-09-26 (read the Amendment section and §8 first). Where this
  brief and the spec disagree, stop and report; do not pick one silently.
- Work in your own worktree (`scripts/agent-worktree.sh <ticket-id> main --web`),
  stage explicit paths only, never `git add -A`, and add no `Co-Authored-By`
  or other attribution trailer (the hook rejects them).
- Never run `npm install`, `npm ci` or `make web` (`web/node_modules` is
  shared). Run tests with node 24 and a scratch Vite cache:
  `PATH=~/.nvm/versions/node/v24.15.0/bin:$PATH VITE_CACHE_DIR=<scratch> npx vitest run …`.
  Rebuild the served bundle only as the repo's UI rules say.
- Never commit `.cards` content or Forge script text; do not add or grow any
  AGENTS.md "Known approximations" row.
- This is a client-only ticket. If you find you need a server change, stop and
  report. If any Go golden (`TestHeads`, repo-deck replays, `make sim`, the
  payment goldens) or `go run ./cmd/gentypes -check` changes, STOP and report.
- The proof tests cited in Evidence live on throwaway branches. Read them with
  `git show <sha>:<path>` and port what you need into the existing test files
  named in this brief; never merge or cherry-pick a proof branch. Nothing
  under `/mnt/sata` is reachable from your worktree.
- Line numbers in this brief are at main `6c711fece`; navigate by symbol.
- Gates before you hand back: the brief's vitest files, the full web suite,
  and `npx svelte-check --tsconfig ./tsconfig.json` with 0 errors.

