# Web auto-pay: disclose last-resort consequences; confirm life payments from every entry point

Suggested issue ID: `aph-last-resort-web`
Priority: 2
Kind: `payment-plan`
Size: M
Lane: web (Claude/codex seat; not the DeepSeek pipeline)
Depends-On: aph-last-resort-plans, aph-web-autopass
Changes offers: no (client only)

## Goal

Operator decision 2 / spec §8 (amended). `aph-last-resort-plans` adds
`consequence` to plan steps (`web/src/protocol.ts`, generated:
`{ sacrifice?, life?, damage?, no_untap?, return_to_hand? }`). The client must:

1. **Disclose.** `paymentPlanSummary` (`web/src/lib/seatpanel.svelte.ts:165`)
   today prints only "Tap N sources for …". Append each consequence, naming
   the source card where the view has it (the seat's own battlefield card
   names): "sacrifices Treasure", "you take 2 (Ancient Tomb)",
   "Mana Vault doesn't untap", "returns Undiscovered Paradise to hand",
   "pay 1 life (Mana Confluence)". Used by the seat panel's payment-action
   list, button tooltips, the hand-fan `CAST` shortcut's title/aria-label
   (`web/src/components/HandFan.svelte:286-287`) and the hot-button strip
   (`web/src/components/HotButtonStrip.svelte` `visiblePaymentActions`).
   Mark a last-resort plan visibly (e.g. a warning tone on the button).
2. **Confirm life, centrally.** Every entry point funnels through
   `SeatPanelState.submitPayment` (`seatpanel.svelte.ts:2116`): the list
   button (`SeatPanel.svelte` `castSuggested`), the legacy cast click with a
   plan (`SeatPanelState.click`, `:1956`), the hand shortcut (`Table.svelte:460`
   `onCastPayment`), and the hot strip. Gate there: if the plan has any step
   with `life > 0` and it is not the confirmed plan, set a
   `paymentConfirm = { seq, actionId, planId, life }` state and post nothing;
   render a confirmation naming the total life ("Pay 1 life to cast X?") with
   Confirm / Cancel; Confirm re-resolves the action and plan on the CURRENT
   pending decision by identity (the existing stale-seq protections) and posts;
   Cancel, a new Seq, or toggling the preference off clears it without
   posting. The confirmation is not suppressible by the preference or by
   Auto. Plans without a life step keep one-click submission.
3. **Nothing else changes**: normal plans look exactly as today apart from the
   (empty) consequence text; auto-pass rules are `aph-web-autopass`'s.

## Evidence

- Arena: "Autotap won't automatically pay life for mana ability costs; if it
  detects a life cost it will prompt you to confirm" (developer reply,
  2022-08-31); community complaints about Treasures sacrificed without notice
  (disclosure is the answer the operator chose for non-life consequences).
- Shipped UI entry points on main `6c711fece`: `SeatPanel.svelte:192`,
  `:698-723` (payment-action block), `seatpanel.svelte.ts:1956-1964`
  (legacy click submits the plan), `:2116-2127` (`submitPayment`),
  `Table.svelte:460` (hand fan), `HotButtonStrip.svelte:51-69`, `:257-267`.
- The concede option already uses a two-step `confirming` state in
  `SeatPanelState` (`click`, R-E4-1): follow that pattern.

## Files (symbols)

- `web/src/lib/seatpanel.svelte.ts`: `paymentPlanSummary`, `submitPayment`,
  new confirm state + `confirmPayment()` / `cancelPayment()`.
- `web/src/components/SeatPanel.svelte`, `HandFan.svelte`,
  `HotButtonStrip.svelte`, `web/src/routes/Table.svelte` (render the confirm
  and the disclosures).
- Tests: `web/src/lib/seatpanel.payment.test.ts`,
  `web/src/components/SeatPanel.svelte.test.ts`,
  `web/src/components/HandFan.svelte.test.ts`,
  `web/src/components/HotButtonStrip.svelte.test.ts`.

## Out of scope

- Server changes; the caretaker rule (engine side, `aph-last-resort-plans`).
- Showing no-plan reasons (not on the wire, by design).
- `npm install` / `npm ci` (never). Follow the repo's UI rules for rebuilding
  the bundle if the change ships.

## Done means

- `cd web && PATH=~/.nvm/versions/node/v24.15.0/bin:$PATH VITE_CACHE_DIR=<scratch> npx vitest run src/lib/seatpanel.payment.test.ts src/components/SeatPanel.svelte.test.ts src/components/HandFan.svelte.test.ts src/components/HotButtonStrip.svelte.test.ts src/lib/autopilot.test.ts`
  passes, including:
  - a `sacrifice` plan: the summary contains "sacrifices Treasure"; one click
    (list button, legacy cast click, hand shortcut, hot strip) posts it once;
  - a `damage` plan: summary names the damage; one click posts;
  - a `life:1` plan: each of the four entry points posts nothing and shows the
    confirmation naming 1 life; Confirm posts exactly once with that plan;
    Cancel posts nothing; a new Seq arriving while confirming clears it and a
    late Confirm posts nothing; toggling auto-pay off clears it;
  - a normal plan is unchanged (one click, no confirmation, summary as before).
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

