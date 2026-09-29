# Prompt System (Lane C) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build sub-project 4 of the table UI rework, the prompt system:

- **One renderer per decision kind**, replacing the generic branch chain in `SeatPanel.svelte` and the decision helpers in `lib/seatpanel.svelte.ts`. Both files shrink.
- **The prompt anatomy**: art banner (rail) or spine (floating), source line, serif question title, one plain-language line, numbered options `1`-`9`, footer with live state and primary/secondary buttons.
- **A prompt dock** docked at the top of the stack/log rail by default, floating (dragged by its grip) as an option.
- **Pick 1-9 by rendered order** everywhere options are numbered, including library search, name pick and the payment window.
- **The deferred keymap actions**: attack with all, no blocks, auto-pay.
- **A server-side contract test** rejecting raw engine cost/filter syntax in `decision.prompt`.

**Architecture:**

- Pure decision logic moves out of `lib/seatpanel.svelte.ts` into `lib/prompts/`:
  - `decision.ts` — selection and shape helpers (`pickOption`, `toneOf`, `mulliganPhase`, `genericListOptions`, …), re-exported from `seatpanel.svelte.ts` so importers keep working.
  - `autonote.ts` — the Auto note vocabulary and `autoNoteText`.
  - `anatomy.ts` — `promptAnatomy(decision, view, viewer)`: which renderer, source line, title, plain line.
  - `order.ts` — `renderedOrder(decision, ctx)`: the option indexes in on-screen order. The renderers number rows from it and `pickHotkey` resolves digit N through it, so the two cannot disagree.
  - `combat.ts` — attackers/blockers summaries (assignments, "you'd take N (17 → 12)"), attack-with-all and no-blocks answers.
  - `dock.ts` — the dock placement/position local default (`gorge.promptdock.v1`).
  - `hover.svelte.ts` — the hovered prompt option (the arrow/board-highlight hook).
- Renderers live in `components/prompts/`, one per kind; `PromptBody.svelte` dispatches by kind. Each renderer owns its footer through a shared `PromptFooter.svelte`, so it is self-contained wherever it mounts.
- `PromptDock.svelte` is the anatomy shell around `PromptBody` with `placement: 'rail' | 'floating'` and `position` props. `Table.svelte` mounts it at the top of the rail with one small edit; lane B will wire placement/position to layout profiles.
- `SeatPanel.svelte` keeps the lifecycle (polling, autopilot effect, Escape), the readout and the Auto bar, and renders `PromptBody` for its decision. When a dock is mounted, the strip's panel defers non-priority decisions to it, and the ACTIONS tab no longer auto-opens for them.
- The Go contract test lives beside the repo-deck acceptance tests and samples every decision those games pose.

**Tech Stack:** Svelte 5 (runes), TypeScript, Vitest (node + Playwright-backed browser fixtures), Go for the contract test.

**Spec:** `docs/superpowers/specs/2026-09-28-client-table-ui-rework-design.md`, section "4. Prompt system" and the keymap rows of section 1.

## Global Constraints

- Work only in `.worktrees/ui-prompts`. Stage explicit paths. No attribution trailers.
- Never `npm ci`/`npm install`/`make web`. Use `npx`. No new dependencies.
- `SeatPanel.svelte` and `lib/seatpanel.svelte.ts` must end SMALLER than at the base commit (1431 and 2678 lines).
- Every commit keeps today's behaviour: posting paths (`click`, `toggle`, `submit`, `submitArrange`, payment) and the wire intent are unchanged. Existing tests keep passing; a test changes only where it asserted markup this lane deliberately replaced.
- Layout settings (placement, position) are client-side only (localStorage), never sent to the server.
- Lane B owns `Table.svelte` layout: edits there are a mount point and props only. Lane A owns motion: `lib/stream.ts` is not touched.
- Colour tokens this lane needs and `app.css` does not yet define (`--gilt`, `--verdigris`, `--ember`, `--font-serif`) are read with literal fallbacks from the spec, so `app.css` is untouched.
- Go: one heavy job at a time, `GOMEMLIMIT=1GiB`; golden heads (`go test ./rules/ -run TestHeads`) must not move.

## Rulings

- Ruling: mulligan keeps its board-centred placement (the opening hand needs board width) but renders through the new `MulliganPrompt` renderer with the anatomy title — the rail dock is ~22rem wide and seven cards at readable size do not fit.
- Ruling: priority never uses the dock; its option list stays in the ACTIONS strip (`PriorityOptions.svelte`) as the spec says "priority is the action button".
- Ruling: the dock is the answer surface for every non-priority decision when mounted; the strip's panel then shows a one-line pointer instead of a second copy, so one decision never has two live answer surfaces. Without a dock (fixtures, finished replays) the strip renders the renderer itself, as today.
- Ruling: the plain-language line is derived client-side from wire facts only (kind, min/max, `target_effect`, the source's stack text, option counts); no new server field. When nothing better is known it restates the answer shape.
- Ruling: the title is the question in the UI's words by kind ("Choose a target", "Choose two", "Declare blockers", "Order your triggers"); the server `prompt` is kept as the plain line when it carries information beyond the kind (e.g. `choose` and `replacement`, whose prompt IS the question).
- Ruling: `attack-with-all` selects one option per attacking creature (the first offered defender, required attackers always included, capped by `max`) and does not submit; the Confirm button (or the confirm key) commits. Selecting first is recoverable; a blind declaration is not.
- Ruling: `no-blocks` submits the empty declaration immediately, and refuses when an option is required (a forced block) or `min > 0`, because the server would reject it.
- Ruling: `auto-pay` presses the select-mana window's Auto-fill option; outside that window it is refused (the key is not consumed). The Auto-pay mana *setting* stays a switch, not a hotkey.
- Ruling: default bindings — attack-with-all `Shift+A`, no-blocks `Shift+N`, auto-pay `Shift+P` (all rebindable). Shifted rather than bare because `hotkeys.test.ts` pins "ordinary typing is never a hotkey", and Ctrl+Shift+A/N/P are browser chords (tab search, incognito, private window).
- Ruling: pick 1-9 numbers the first nine rows in rendered order; a filtered list re-numbers as the filter changes, and the digit is refused while the filter input has focus (typing a digit filters). Rows past nine carry no number.
- Ruling: the floating position is stored as the dock's top-left in viewport pixels, clamped into the viewport on load and on resize.
- Ruling: the board highlight for a hovered chip is a `data-prompt-hover` attribute toggled on the existing `[data-obj]`/`.identity[data-seat]` anchor (the same anchors `Arrows.svelte` resolves), styled globally by the dock; the arrow is a solid verdigris `target-hover` arrow drawn by `Arrows.svelte` from the hover store.

## File Structure

| File | Role |
|---|---|
| `web/src/lib/prompts/decision.ts` | pure decision helpers moved from `seatpanel.svelte.ts` |
| `web/src/lib/prompts/autonote.ts` | Auto note text moved from `seatpanel.svelte.ts` |
| `web/src/lib/prompts/anatomy.ts` (+test) | renderer choice, source line, title, plain line |
| `web/src/lib/prompts/order.ts` (+test) | rendered order and digit numbering |
| `web/src/lib/prompts/combat.ts` (+test) | attackers/blockers summaries and quick answers |
| `web/src/lib/prompts/dock.ts` (+test) | placement/position local default |
| `web/src/lib/prompts/hover.svelte.ts` | hovered-option hook |
| `web/src/components/prompts/*.svelte` | `PromptBody`, `PromptDock`, `PromptFooter`, `OptionRows`, `TargetChips`, `CombatPrompt`, `MulliganPrompt`, `ArrangePrompt`, `DiscardPrompt`, `SearchPrompt`, `NamePickPrompt`, `PriorityOptions` |
| `web/src/components/SeatPanel.svelte` | shrinks to lifecycle + readout + `PromptBody` |
| `web/src/lib/seatpanel.svelte.ts` | shrinks; `pickHotkey` uses `renderedOrder`; `attackWithAll`/`noBlocks`/`autoPay` |
| `web/src/lib/keymap.ts` | three new actions |
| `web/src/components/HotButtonStrip.svelte` | dispatch of the three actions; no auto-open while docked |
| `web/src/components/Arrows.svelte` | the hover arrow |
| `web/src/routes/Table.svelte` | one dock mount in the rail |
| `rules/prompttext_test.go` (Go) | the prompt-text contract test |

### Task 1: Move decision helpers out of seatpanel.svelte.ts

**Files:** create `lib/prompts/decision.ts`, `lib/prompts/autonote.ts`; modify `lib/seatpanel.svelte.ts` (re-export).

- [ ] Move `actedOption`…`pickableInOrder` and `AUTO_PASS_CAP`…`runStopNote` verbatim; `seatpanel.svelte.ts` re-exports them.
- [ ] `npx vitest run src/lib` passes unchanged. Commit.

### Task 2: Split SeatPanel's kind branches into renderers

**Files:** create `components/prompts/{PromptBody,PromptFooter,OptionRows,MulliganPrompt,ArrangePrompt,DiscardPrompt,SearchPrompt,NamePickPrompt,PriorityOptions}.svelte`; modify `SeatPanel.svelte`.

- [ ] Each branch of SeatPanel's `{#if}` chain becomes a renderer taking `{ decision, view, logic, placement }`; its hover inspector and modal move with it.
- [ ] `SeatPanel.svelte` renders `<PromptBody>`; its style block keeps only what it still renders.
- [ ] Component and browser-fixture suites pass. Commit.

### Task 3: Anatomy and rendered order (pure, TDD)

**Files:** `lib/prompts/anatomy.ts`, `lib/prompts/order.ts` with tests.

- [ ] Tests: every kind maps to its renderer; titles per kind ("Choose two" for min=max=2 modes); source line "Mira's Rhystic Study · triggered ability"; plain line from `target_effect` damage ("Deals 3 damage to the target you choose").
- [ ] Tests: `renderedOrder` equals each renderer's row order — search sorted A→Z and filtered, name pick filtered and capped, payment window (sources, then Auto-fill, Pay, Undo, Cancel), generic lists without the primary/concede; priority refused.
- [ ] Implement; commit.

### Task 4: Per-kind renderers with numbered options

**Files:** `OptionRows`, `TargetChips`, `CombatPrompt` + `lib/prompts/combat.ts` (TDD), trigger order/optional, commander zone, starting player, replacement, choose, modes (count), mulligan.

- [ ] Rows carry a `kbd` digit from `renderedOrder`; modes show "N of M chosen"; single-select kinds post on click.
- [ ] Target chips show the art thumbnail (`images.url`), P/T or life, set the hover store on enter/leave.
- [ ] Attackers/blockers: numbered candidate rows plus the summary, footer with "Attack with all"/"No attack", "No blocks"/"Confirm blocks".
- [ ] Commit.

### Task 5: Prompt dock

**Files:** `PromptDock.svelte`, `lib/prompts/dock.ts` (+test), `Table.svelte` (mount), `SeatPanel.svelte`/`HotButtonStrip.svelte` (defer while docked), `SeatPanelState.dockCount`.

- [ ] Rail: banner art, grip, float/dock toggle. Floating: fixed position, spine art, drag by grip (pointer capture), position saved.
- [ ] Commit.

### Task 6: Numbered picks, keymap actions, hover hook

**Files:** `seatpanel.svelte.ts` (`pickHotkey`, `attackWithAll`, `noBlocks`, `autoPay`), `keymap.ts`, `HotButtonStrip.svelte`, `Arrows.svelte`, `lib/arrows.ts`, tests.

- [ ] `pickHotkey(n)` resolves through `renderedOrder`; the search/name/payment refusals are removed and their tests flipped to assert the rendered-order answer.
- [ ] New actions in the Decisions group with default bindings; cheat sheet and editor list them automatically.
- [ ] Commit.

### Task 7: Prompt-text contract test (Go)

**Files:** `rules/prompttext_test.go` (package chosen where repo-deck games are played).

- [ ] Pure checker with a table of forbidden patterns (`Sac<`, `Word<…>` cost parts, `$`, dotted Forge filters like `Creature.Other`, `ValidTgts`, `SVar`) and positive/negative unit cases.
- [ ] Sample repo-deck games and assert every posed decision's prompt passes; fix prompt generation minimally if it finds real violations; `TestHeads` unchanged.
- [ ] Commit.

### Task 8: Gate and smoke

- [ ] `npx vitest run`, `npx svelte-check`, `npx eslint .`, `npx vite build` all clean; restore `cmd/gorged/webdist/.keep`.
- [ ] Build gorged, play a vs-bot game on port 8094 with Playwright, screenshot the dock for target, modes/choose, attackers, blockers, trigger optional, floating placement; compare with the mockups; fix; stop the server by pid.
