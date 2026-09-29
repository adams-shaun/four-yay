# ManaBrew Client Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** the web client can play a seated game over the ManaBrew protocol
(`host/manabrewhttp`, mounted by `gorged -manabrew`) instead of gorge's native
stream, chosen by a client setting "Protocol: Native / ManaBrew" (default
Native). No component changes what it reads: the board, prompts, motion and
log all consume the normalized client model (`ClientView` = `View`,
`PendingDecision` = `Decision`, `Transition[]`).

**Architecture:**

- `web/src/lib/manabrew/` is the ONE home of ManaBrew wire knowledge. A
  protocol change touches only it.
  - `wire.ts`: hand-written TS types from the spec's Appendix A and
    `protocol/manabrew` (no codegen: tsgen does not cover these types).
  - `ids.ts`: the id mint's inverse (`player-N`, `oN`, `sN`, `h-…`, `opt-N`, `pay-…`).
  - `project.ts`: `GameViewDto` → `View` (the inverse of `internal/manabrew/state.go`).
  - `prompt.ts`: `AgentPrompt` → `Decision` plus a binding that maps the
    UI's `Intent` (option indices) back to a ManaBrew `response`
    (the inverse of `internal/manabrew/prompt_*.go`). Concede → `directive`.
  - `patch.ts`: `stateDelta` application (`$v`/`$d`/`$k`/`$o`).
  - `transport.ts`: route URLs, the SSE client, `send`, the `state` probe.
  - `source.svelte.ts`: `ManaBrewMatch`, the adapter object the table route
    uses in place of `MatchState`. It implements `ClientModelSource`, keeps
    a snapshot ring (DVR), derives transitions with `diffViews`, and builds
    the log from `transitionLine`.
  - `pref.ts`: the persisted protocol setting.
- The seat panel is untouched. `SeatCtx` gains an optional `transport`
  (post intent / read pending / undo); `lib/api.ts`'s three seat calls
  delegate to it when present. Under ManaBrew the route hands the panel a
  ctx whose transport is the `ManaBrewMatch`.
- `routes/Table.svelte` picks the source once per mount. A failed ManaBrew
  probe falls back to `MatchState` with a visible notice.

**Tech Stack:** Svelte 5 (runes), TypeScript, Vitest; Playwright for the smoke.

**Spec:** `docs/superpowers/specs/2026-09-28-client-table-ui-rework-design.md`
sub-project 0; `docs/superpowers/specs/2026-09-28-manabrew-protocol-scope.md`
§5.2, §5.4, §6, §10.1, Appendix A.

## Global Constraints

- Work only in `.worktrees/ui-manabrew`; port 8096; persistence under
  `/tmp/gorge-ui-manabrew/`.
- No new npm deps; no `npm ci`/`install`; `/usr/bin/grep`.
- Big files do not grow: `SeatPanel.svelte`, `lib/seatpanel.svelte.ts`,
  `PlaySettingsPanel.svelte` get at most a mount line.
- The setting is client-side only, persisted in `localStorage`
  (`gorge.protocol.v1`) through `lib/storage.ts`.
- Fixtures come from a real server: `lib/manabrew/testdata/capture.json` is
  recorded from `gorged -manabrew` by a capture script (native view+decision
  and ManaBrew state+prompt at the same instant). Every `text` field is
  blanked before commit (no Forge-derived text in fixtures).
- Go changes only where the server blocks the client, minimal, tested.

## Rulings

- Ruling: transport is same-origin only (`/api/manabrew/v0/tables/{t}/matches/{k}/…`
  with `?token=`) — EventSource cannot set headers, and a separate
  `-manabrew-addr` listener would be cross-origin; that mode is for external
  agents, not this client.
- Ruling: the adapter replaces only the game channel. Lobby calls
  (`/api/games`, `/api/tables`, `/api/tables/{t}/matches`) stay native: the
  protocol has no lobby (scope spec G-11). The live match number comes from
  `GET /api/tables/{t}/matches`.
- Ruling: ManaBrew prompts are reconstructed as native-shaped `Decision`s
  with DENSE option indices (0..n-1) and a per-prompt binding table from
  index → wire answer. The server's own `opt-N` ids are carried in the
  binding, never assumed equal to the synthetic index. `promptId` becomes
  `Decision.seq`, `decidingPlayerId` becomes `player`.
- Ruling: `chooseAction` gets synthetic `pass` and `concede` options (the
  server omits both from `actions`); `pass` answers `{type:"pass",
  exhaustStack:false}` (client auto-pass stays client-side, so no server
  `until` policy is ever installed); `concede` answers the `concede`
  directive.
- Ruling: `pay-<id>` cast actions become `payment_actions` WITHOUT plans.
  The protocol carries no payment plans, so the UI's one-click planned cast
  is unavailable; casting under ManaBrew is tap-then-cast (the float path
  the manual-mana UI already has). The mana taps stay visible because no
  plan pays those casts (`manualManaHidden`).
- Ruling: each `pay-<id>` cast is also projected as a `cast` entry in the
  seat's `potential_actions` — it is exactly "a play the engine would offer
  once mana is paid". Without it the autopilot's smart stops saw no play in a
  mana-only window and auto-passed the seat's main phases (found in smoke).
- Ruling: a card pick's option kind (`discard`/`search`/`sacrifice`/…) and
  the starting-player ask are inferred from the prompt's title / option
  labels (the protocol drops both); presentation only — the server maps the
  answer by card id or index.
- Ruling: `step`→ gorge step names by the inverse of the server's table;
  `combatFirstStrikeDamage` reads as `combat-damage`. `phase` is derived
  from the step; `round` is `ceil(turn / seats)` (not carried).
- Ruling: a hidden zone entry (`visibility:"hidden"`) becomes a face-down
  `CardView` with a stable NEGATIVE synthetic id per (zone, owner, index),
  so it renders as a card back and never collides with a real object id.
- Ruling: a stack spell's `card` is the last-known `CardView` for that id
  (the id is the card's own ObjID, so the hand→stack flight is detected);
  when never seen, a minimal card from `identity.name`.
- Ruling: DVR and log are client-side. Each accepted snapshot is stored in a
  bounded ring (500) at a synthetic seq; each transition line is one
  synthetic `EventBody` whose seq precedes its snapshot's. Scrubbing shows
  the newest snapshot at or before the cursor. A turn change adds a
  "Turn N" line and a `turnStarts` entry.
- Ruling: a `prompt` whose `promptId` is lower than the last one seen for
  the same game is a rewind (undo): the ring's tail is kept (history), the
  model emits `reset`, and the table route calls the panel's `rewind()`.
- Ruling: undo maps to `restoreSnapshot{checkpointId: <open promptId>}`
  (scope spec §6.4); it is only legal while a `chooseAction` is open.
- Ruling: `error` messages surface in a visible banner on the table
  (`stalePrompt` etc.); a rejected `/send` also rejects the panel's post, so
  the panel shows its own error and re-reads pending (from the adapter).
- Ruling: `stateDelta` is accepted and applied; the fingerprint algorithm is
  unspecified upstream, so it is not verified. A patch that does not apply
  (unknown `base`, malformed) triggers a `GET …/state` resync. gorge's
  server does not send patches today (scope spec Q7).
- Ruling: selecting ManaBrew on a server without `-manabrew` probes
  `GET …/state` first; a 404/403/401 or network failure shows "ManaBrew is
  not available on this server (…) — playing over Native" and mounts the
  native `MatchState`. It never hangs: the probe is bounded (10s).
- Ruling: the protocol setting takes effect on the next table load; the
  toggle offers a "Reconnect now" button that reloads the page.
- Ruling: the spectator path stays native (ManaBrew is seat-scoped only).

## Degradations (native has, ManaBrew lacks)

Loud-but-graceful: each is either a visible notice or an inert control, never
a hang.

- Planned one-click casts (payment plans) → tap-then-cast (see ruling).
- Server log lines, event-seq DVR, redacted transcript backfill → client
  transition log and snapshot ring (resolution: one snapshot per server
  refresh; phases with no visible change collapse).
- `potential_actions`, `ability_costs`, `produces`, `pool_restrictions`,
  `spell_api`, `activated_this_turn`, `pending` (unordered triggers),
  `window_reasons`, dungeon, planar deck: not carried → absent.
- Priority prompt text ("turn 3, main1 — X has priority"): synthesized.
- Mulligan: `mulligan` option offered even when the allowance is spent
  (`mulliganCount` is always 0); the server refuses with `invalidShape`.
- Target `group`/budget constraints, attack costs: not carried; server
  `Validate` is the fence (scope spec G-4).
- Concede before a priority prompt is queued by the server (G-2).
- Undo only while a `chooseAction` prompt is open.
- `trigger_order`/`arrange`/mana-window prompts: the client maps
  `reorder`/`scry`/`payManaCost`, but this base's server does not pose them
  (their MB-6 builders are not wired into `dispatch`); the server sends an
  `error` instead, which the banner shows.

## Server bugs found

- Choice responses unmapped: `translateResponse` had no case for
  `chooseFromSelection`/`chooseBoolean`/`chooseCards`/`chooseNumber`/`chooseColor`,
  so the starting-player ask that opens every vs-bot game could not be
  answered. Fixed minimally in `internal/manabrew/parse_choose.go` (+ test).
- `CardIdentity.isToken` is true for every card (`c.Token != ""`, but
  `Token` is the `#<id>` disambiguator). Not blocking; the client ignores it.
- MB-6's `promptPayment`/`parseTriggerOrder`/`parseArrange*` are not
  reachable from `dispatch`/`translateResponse` (also on main).

## Tasks

### Task 1: server — choice responses (Go)
- Files: `internal/manabrew/parse_choose.go`, `parse_choose_test.go`, `errors.go` (one case).
- Test: `TestChoiceResponsesTranslate`, `TestChoiceResponsesRejectMisfits`.

### Task 2: wire types, ids, projection
- Files: `lib/manabrew/wire.ts`, `ids.ts`, `project.ts`, `project.test.ts`,
  `testdata/capture.json`, `scripts` note in `testdata/README.md`.
- Test: every captured record projects to a `View` equal to the native view
  on every carried field; diffs of successive projected views equal diffs of
  the native views.

### Task 3: prompts and answers
- Files: `lib/manabrew/prompt.ts`, `prompt.test.ts`.
- Test: every captured prompt reconstructs the native decision's kind and
  per-option identity (kind/obj/player/attacker); answering option i yields
  the response the server's translator expects.

### Task 4: patches
- Files: `lib/manabrew/patch.ts`, `patch.test.ts`.

### Task 5: transport and source
- Files: `lib/manabrew/transport.ts`, `source.svelte.ts`, `source.test.ts`.
- Test: a fake EventSource feeding captured messages drives view/decision,
  emits `step` transitions, fills the log and ring, maps errors, detects
  rewind, and a failed probe reports fallback.

### Task 6: seat transport seam + route wiring + setting
- Files: `lib/seat.ts` (optional `transport`), `lib/api.ts` (delegate),
  `lib/manabrew/pref.ts`, `components/ProtocolSetting.svelte`,
  `routes/Table.svelte` (source choice, banner), mount in
  `PlaySettingsPanel.svelte` and `PlayVsBot.svelte`.
- Test: `api` delegation test; pref load/save test.

### Task 7: gates and smoke
- `npx vitest run`, `svelte-check`, `eslint`, `vite build`; Go
  `./host/manabrewhttp/ ./internal/manabrew/`; Playwright vs-bot game over
  ManaBrew (mulligan, land, targeted spell, attack, block) and Native on the
  same server.
