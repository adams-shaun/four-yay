# ManaBrew protocol support: scoping spec

Status: DRAFT scoping spec. It is uncommitted and awaits operator rulings on §9.
Base SHA: `842c8d8ae94320534aa73b19fb5c2c628cc204ba` (`origin/main`, 2026-09-28).
Every gorge `path:line` below was read at that SHA in the worktree
`.worktrees/manabrew-scope`.
Protocol source: <https://docs.manabrew.app/protocol/> and all 22 of its subpages (game view, 19 prompts, shared types, deck),
fetched on 2026-09-28. All were reachable. Six site pages were also read
(`/self-hosting/`, `/hosting-relay/`, `/hosting-full-stack/`, `/formats/`, `/releases/`, `/getting-started/`).
Anything that was not read from gorge code or from those pages is marked **INFERRED**.

The ManaBrew protocol specification is licensed CC-BY-4.0 (protocol index page, "License").
The reference implementation is AGPL-3.0-or-later.
§8.5 covers what that means for gorge, which is Apache-2.0.

---

## 1. The protocol in one page

URL: <https://docs.manabrew.app/protocol/>

ManaBrew splits a card-game **engine** (it enforces the rules) from a **client**
(the UI or an agent). The two exchange **JSON messages whose transport is left open**.
The spec says "a client written against this spec can talk to any conforming engine".
Rules enforcement is entirely the engine's job.

**Engine → client: four message kinds**

- `state`: a `StateUpdate` that carries the full `gameView` (`GameViewDto`). It is the
  only carrier of authoritative state, and the client re-renders from each one.
  - An engine *may* send `stateDelta` instead.
  - A `stateDelta` carries a patch plus `base` and `fingerprint`.
  - Its reserved keys are `$v`, `$d`, `$k` and `$o`.
  - Patches are optional for engines. Conforming clients should accept them.
- `display`: a `DisplayEvent`. It is marked "⚠️ Work in Progress" and is not
  authoritative.
- `prompt`: an `AgentPrompt` with fields `{promptId: number, decidingPlayerId: string,
  sourceCard?: CardDto, input: PromptInput}`. It means the engine has paused and needs a
  decision from one player.
  - Prompts carry no `gameView`.
- `error`: a `ProtocolError` with fields `{code, message, promptId?}`.
  - `code` is one of `stalePrompt`, `wrongPlayer`, `wrongPromptType`,
    `unknownActionId`, `invalidShape`.

**Client → engine: two message kinds**

- `{kind:"response", promptId, action: {type:<promptType>, output:{type:<outputType>, …}}}`.
  - Responses pair strictly with prompts: exactly one response per prompt.
  - The `promptId` "is always the same as the last `promptId` emitted by the engine".
- `{kind:"directive", directive:{type:"concede"}}`. This is out-of-band and
  fire-and-forget. It is "legal at any time".

**Prompt catalogue (19 prompt types, one page each).**

- Choices & information: `chooseNumber`, `chooseCards`, `chooseColor`, `chooseBoolean`,
  `chooseFromSelection`, `revealCards`, `scry`, `reorder`, `diceRolled`.
- Priority & costs: `chooseAction`, `payManaCost`.
- Mulligan: `mulligan`, `mulliganPutBack`.
- Combat & targeting: `chooseAttackers`, `chooseBlockers`,
  `chooseDamageAssignmentOrder`, `chooseCombatDamageAssignment`, `chooseBoardTargets`.
- Terminal: `gameOver` (it has no response).

The fields of each prompt are listed in Appendix A.

**What the spec does NOT define.** These gaps drive §3, §8 and §9.

| Topic | Status in the published spec |
|---|---|
| Transport | "transport-agnostic". No framing, no endpoint, no WS subprotocol. |
| Handshake / auth / hello | Not specified. |
| Versioning | No protocol version field. `Deck.version` is optional and is not a protocol version. |
| Match lifecycle (create, join, seat, deck submission, start) | Not specified. The Deck *data format* is specified (`/protocol/deck/`), but no message carries it. |
| Timeouts | Not specified. |
| Reconnect / resume | Not specified. It is implied only by "full state every time". |
| The outer envelope of `state` and `prompt` | Not specified. The patch example shows `{"kind":"stateDelta",…}`. The prompt examples show a bare `AgentPrompt` with no `kind`. **INFERRED:** `{"kind":"state","gameView":…}` and `{"kind":"prompt",…AgentPrompt}`. |
| Referenced but undefined types | `SelectionOption`, `ReorderItem`, `PaymentAction`, `PlayCardMode`, `Mana`, `ZoneKind`, `DayTime`, `PlayerStatus`, `PlayerCounterKind`, `ManaColor`, `DungeonStateDto`, `ClassLevelDto`, `SagaChapterDto`, `CardChoiceDto`, `DisplayEvent`, `DirectiveInput` (only `{type:"concede"}` is shown). The shapes of `SelectionOption` (`label`, `weight`, `canRepeat`), `ReorderItem` (`id`, `card`, `oracle?`) and `Mana` (`color`, `amount`) are **INFERRED from the examples**. |
| Fingerprint algorithm for `stateDelta` | Not specified. |
| Land play in `chooseAction` | `AvailableAction` has only `cast`, `activateAbility` and `undoMana`. `PlayCardMode` is undefined. The only mode in any example is `"cast"`. |
| `restoreSnapshot.checkpointId` | It appears in `ChooseActionOutput`, but nothing tells a client which checkpoint ids exist. |

**Doc inconsistencies found** (they matter for strict decoding):

- `AvailableAction` cast has a `label` field, but the `chooseAction` example sends
  `modeLabel`.
- The `chooseBoardTargets` example includes a `label` field that the interface does not
  have. It also omits `presentation` and `cancellable`, which the interface requires.
- The `payManaCost` example omits the required `presentation`.

Conclusion: **gorge must decode leniently and encode canonically.** This is the same rule
SpellBench v2 arrived at. `internal/spellbench/v2agent` reads leniently and logs unknown
fields once (`internal/spellbench/v2agent/unknown.go:10-23`), and it writes canonically.

**The ManaBrew deployment topology** (from `/hosting-relay/`, `/self-hosting/` and
`/formats/`) decides the roles:

- The **relay** (`manabrew-server`) is a WebSocket lobby, matchmaker and message relay on
  `:9443`. It "never runs games itself".
  - It authenticates every client and node with one shared key (`MANABREW_SERVER_KEY`).
- The web and desktop **clients run the ManaBrew engine locally**, as WASM or native.
- A **self-hosted node** is a headless room host.
  - It connects to the relay with the server key, opens lobby rooms and runs the games.
  - Its backends are `manabrew` (a Rust port of Forge) or `forge` (the Java Forge
    engine, per game or GraalVM in-process).
  - It is configured by env vars (`SELF_HOSTED_NODE_*`: room name, password, format,
    max players 4, bot seat, auto start).
- **The relay/lobby/node wire protocol is not part of the published `/protocol/`
  spec.** It exists only in the AGPL reference implementation
  (`github.com/witchesofthehill/manabrew`). That repository was not read for this
  document.
- Card identity:
  - ManaBrew's card database *is* Forge's card scripts ("parses Forge's full library of
    32,000+ card scripts").
  - `TokenScript` is "taken directly from the Forge spec", meaning Forge's
    `tokenscripts` stems such as `w_2_2_knight_vigilance`.
  - gorge compiles the same corpus, so names and token keys are a shared vocabulary.

---

## 2. What gorge has today (the surfaces an adapter sits on)

| Surface | Where | What it gives an adapter |
|---|---|---|
| The decision contract | `decision/decision.go:14-165` (13 kinds, `decision.Kinds` at `:160-164`); `Option` at `:175-476`; `Decision` at `:571-867`; `Intent` at `:1164-1190`; `Validate` at `:1194` | The engine lists every legal option, and a rules-ignorant client answers with option indices. **This is the same philosophy as ManaBrew prompts**, where `actionId`s must be advertised ones. |
| Seat-scoped host API | `host/action.go:47` `Registry.Pending`; `host/action.go:68` `Registry.SubmitIntent` (it validates exactly as `Decision.Validate` does and never drives the engine); `host/viewat.go:66` `Registry.ViewAtSeat`; `host/viewat.go:139` `Registry.EventsSeat`; `host/undo.go:159` `Registry.Undo`; `host/session.go:42,230` `OpenSession`/`Subscribe` | All of the adapter's inputs and outputs. None of them exposes a `*state.Game` (archtest `TestNoExportLeaksAnEngineGame`). |
| Seat claims | `host/httpapi/handler.go:19-22` `SeatClaim{Table,Seat}`; `host/httpapi/rest.go:80-95` `claimForTable`; `cmd/gorged/seats.go:56-72` `seatGate.resolve` (a Bearer header or a `?token=` value) | A credential names a table **and** a seat. An adapter that takes its seat from a claim can never read another seat. |
| Native wire | `protocol/protocol.go:17` `Version = 1`; frame types at `:22-34`; `host/httpapi/handler.go:84-94` routes; `host/httpapi/sse.go:15-17` `GET /api/stream` | SSE for push and POST for intents is the house transport. The catch-all `/api/` answers a JSON 404 (`handler.go:104-106`). |
| Server wiring | `cmd/gorged/main.go:168-197` flags; `:266` `httpapi.Options`; `:313-329` `topMux` mounts `/art/`, `/cards/named` and `/api/feedback` *ahead of* httpapi, then `topMux.Handle("/", httpapi.NewHandler(r, opts))` | Existing opt-in pattern: a nil or empty option mounts nothing, so an unknown path falls through to httpapi's 404 (`main.go:316-328` for feedback, `:278-292` for vsbot). |
| Redacted view | `view/view.go:86` `View`; `:184` `PlayerView`; `:340` `CardView`; `:428` `StackView`; `:449` `TargetView`; the opponent `Hand` is JSON `null` (`:200-210`); face-down redaction at `:913-925`; the decision is attached only for the viewer (`:678-683`) | Everything a `GameViewDto` needs, with the gaps listed in §6.2. |
| Event redaction | `view/redact.go:117-150` `RedactEvent`: a hidden→hidden move zeroes `Obj`. Public `Note`s (reveals) pass through (`:152-160`). | Reveals and dice exist only in the event stream (`effects/flipcoin.go:33-36`, `effects/dice.go:46,84`, `effects/cardflow.go:2543-2568`). |
| External-seat precedent | `README.md:221-226`: "External processes can play a seat in two ways: over HTTP, through `host/httpapi` … as a SpellBench v2 agent, in NDJSON over stdio" | ManaBrew becomes a third way. |
| SpellBench v2 engine role | `internal/spellbench/v2engine`: `translate.go:177-281` kind dispatch; `game.go:127-140` per-viewer HMAC object ids with a zone-change count (`game.go:148-169`); `game.go:218-246` refusal→repair→halt; coverage counters `posed:`/`enumerated:`/`dropped_option:` (`game.go:330-331`, `translate.go:1479-1480`); `parity_test.go:83-131` | Prior art for gorge-as-engine behind a foreign protocol. It drives `rules` **directly**, not through `host` (it imports rules, view and botpolicy). |
| SpellBench v2 agent role | `internal/spellbench/v2shadow`: it stages a throwaway gorge engine per decision (`setup.go:1-32`), resolves names with "A // B"→front-face fallback (`setup.go:63-99`), keeps a stable-id `Tracker` (`stage.go:40-49`) and runs a refusal loop of up to 8 tries (`policy.go:502-532`) | Prior art for gorge-as-agent on a foreign engine. |

**How the dependency order constrains this.** The order is
`… view → seat → replay → protocol → host → host/httpapi → cmd/*`
(`docs/agents/invariants.md:13`). Archtest pins these facts:

- `protocol` must not import `rules` (`internal/archtest/arch_test.go:146`).
- `host`, `host/httpapi` and `cmd/gorged` must not import `internal/testutil` or
  `internal/azmcts` (`:147-158`).
- `time` is allowlisted per package (`:109-133`).
- `go.mod` has **no** `require` lines (invariant 7), so there is no third-party
  WebSocket library.

---

## 3. Roles and the role decision

The protocol supports three roles for gorge.

| Role | What gorge is | Does the published spec cover it? | What else it needs |
|---|---|---|---|
| **E: engine** | A conforming ManaBrew *engine*. gorge sends `state`/`prompt`/`error` and accepts `response`/`directive` for one seat. | **Yes, fully.** This is the spec's whole subject. | A transport. The spec leaves it open, so gorge picks one. |
| **N: relay node** | A self-hosted *room node* on a ManaBrew relay, so that the real ManaBrew web and desktop clients can join rooms whose games gorge runs. | **No.** Role E's messages are what flow inside a game, but lobby, room, seat, deck-submission, auth and framing belong to the relay protocol, which is undocumented. | A clean-room description of the relay protocol, a stdlib RFC 6455 WebSocket client, a lobby/room state machine, and ManaBrew-deck→gorge-deck import. |
| **A: agent** | A gorge-brained *player* on a ManaBrew engine (for example a Forge room). gorge receives `GameViewDto` and prompts, and answers. | The engine↔client half is covered. Reaching an engine (WASM in someone's browser, or a node behind the relay) needs the same undocumented relay protocol. | The relay client (as for N), plus a **staging brain**: rebuild a gorge engine from each `GameViewDto` (v2shadow's approach, `internal/spellbench/v2shadow/setup.go:1-32`), then map prompts to gorge decisions and back. |

**Recommendation: build E first.** Treat N as phase 2, behind a research spike (MB-12).
Treat A as phase 3, and only if the operator wants gorge's bots to play on Forge or
ManaBrew engines.

Why E first:

1. **It is the only role the public spec fully specifies.** Both N and A depend on the
   undocumented relay protocol, which today can only be learned from AGPL source.
2. **The design philosophies match.** gorge's `Decision` already means "the engine
   enumerates every legal option; the rules-ignorant client picks"
   (`decision/decision.go:1-3`). ManaBrew's prompts mean the same thing ("`actionId`
   MUST be an `id` advertised in this prompt's `actions`"). Most of E is shape
   translation, not rules work.
3. **E can be built entirely on the existing seat-scoped host API** (`Pending`,
   `SubmitIntent`, `ViewAtSeat`, `EventsSeat`, `Undo`). That gives four guarantees:
   - The engine is never touched.
   - Every invariant in §4 holds by construction.
   - The adapter inherits `claimForTable`'s privacy fence.
   - HumanSeat's think-timeout and caretaker behaviour carries over.
4. **E is the substrate for N and A.** The translator (view/decision ↔ ManaBrew) is the
   same code in every role. N replaces only the transport. A runs the translator in
   reverse.

**Honest limit of E alone:** no *shipped* ManaBrew client can reach a gorge engine
without N.

- The reference client runs its engine locally or talks to a relay.
- **INFERRED:** it has no "connect to engine at URL" option. The docs describe none.

Phase 1's consumers are therefore:
- third-party clients and agents written against the spec (LLM agents are an obvious
  one);
- gorge's own conformance harness;
- phase 2.

If the operator's goal is "ManaBrew players can play against gorge", then **N is the
deliverable and E is its prerequisite** (open question Q1).

---

## 4. Invariants and how the design keeps each one

| Invariant (`docs/agents/invariants.md`) | How the design holds it | Enforced by |
|---|---|---|
| 1. Dependency direction | The new packages sit at the host tier or above. No engine-tier package imports them. | New archtest rows (§5.3, MB-2) |
| 2. All mutation through `events.Apply` | The adapter's only write path is `Registry.SubmitIntent` / `Registry.Undo`. It holds no `*state.Game`, and host exports none. | `TestNoExportLeaksAnEngineGame`; the replay-equivalence test (MB-8) |
| 3. Determinism | 1. The translator package imports no `time` and ranges over no map into output order. `encoding/json` sorts map keys, which covers `manaPool`, `counters` and `commanderDamage`. 2. `promptId` is derived from `Decision.Seq`, not from a counter (§6.4). 3. Auto-pass (`pass.until`, `exhaustStack`) is carried out by the adapter as ordinary `pass` intents, so they are logged and replay unchanged. 4. Only the transport package imports `time`. | Time allowlist row (MB-2); the replay-equivalence test (MB-8); `TestHeads` untouched |
| 4. Closed `decision.Kind` set | Every ManaBrew prompt is *derived from* an existing kind. No kind is added. Each shape gap is recorded in §7 as a finding. | A closed-set coverage test: every `decision.Kinds` entry has a translator case (MB-4) |
| 5. Information boundary | 1. A connection is bound to one `SeatClaim`, and the seat comes from the claim, never from the message. 2. Views come only from `ViewAtSeat(…, claim.Seat)`. 3. The host `Session` subscription is used only as a *wake-up*; its (spectator-visibility) frames are never forwarded. 4. Hidden cards become `{"visibility":"hidden","id":…}` entries or bare counts. | Transport tests (MB-10): seat B's token cannot read seat A's prompt or state; a ManaBrew stream never contains another seat's hand |
| 6. Licensing boundary | No Forge script text goes on the wire or into fixtures. `CardDto.text` is left empty in v1 (§6.2). Golden transcripts contain card *names* only. ManaBrew-derived Go types carry a CC-BY-4.0 attribution. No AGPL code is copied (§8.5). | `cards/boundary_test.go`; review |
| 7. No third-party deps | No WebSocket library. Phase 1 uses SSE and POST. A phase-2 WebSocket is a stdlib RFC 6455 implementation (MB-13). | `go.mod` review |
| 9. Golden heads move only with a cause | No engine, events or rules edits at all. | `TestHeads` in every ticket's Done-means where relevant |
| Engine never imports the application | The adapter imports `host`. Nothing in `rules`/`effects`/`state`/`events`/`decision`/`view`/`seat`/`host`/`host/httpapi` imports the adapter. Only `cmd/gorged` does. | Archtest reverse rows (MB-2) |

---

## 5. Architecture

### 5.1 Packages

| Package | Tier | Imports (non-test) | Owns |
|---|---|---|---|
| `protocol/manabrew` | Types only | **stdlib only** (`encoding/json`, `fmt`) | Every ManaBrew message, prompt input/output and DTO as Go structs with camelCase JSON tags. Discriminated-union codecs (`kind`, `input.type`, `output.type`, `visibility`). A **lenient** `Decode` that records unknown fields, and a **canonical** `Encode`. The CC-BY-4.0 attribution goes in `doc.go`. |
| `internal/manabrew` | Pure translator | `protocol/manabrew`, `view`, `decision`, `state` (types) | `Translator`: `(view.View, *decision.Decision, []protocol.EventBody) → []manabrew.EngineMessage`, and `(manabrew.ClientMessage, pending) → (decision.Intent \| manabrew.ProtocolError \| AutoPass)`. The id mint, the step map, the per-kind prompt builders and the response parsers. No `time`, no `host`, no `rules`. |
| `internal/manabrew/mbtest` | Test support | `internal/manabrew`, `protocol/manabrew`, `seat`, `internal/bench` | A deterministic **mock ManaBrew client** that answers every prompt type (seeded first-legal or random), strictly type-checking each message. A `seat.Seat` wrapper `TranslatingSeat` that runs decision → prompt → mock response → intent in-process (used by MB-8). |
| `host/manabrewhttp` | Host transport | `host`, `internal/manabrew`, `protocol/manabrew`, `net/http`, `time` | The ManaBrew routes (§5.2). It binds a connection to a `SeatClaim` through an injected resolver, as httpapi does. It uses the host `Session` as a wake-up, coalesces views, and queues `concede`. |
| `cmd/mbbridge` (optional, MB-11) | CLI | `protocol/manabrew`, `net/http` | Relays stdio NDJSON to and from a gorged ManaBrew endpoint, so an agent process can speak ManaBrew over stdin/stdout, as `cmd/sbagent` does for SpellBench. |

Naming note: `internal/manabrew` could equally live at `host/manabrew`. It is put under
`internal/` because it is a pure library with no host dependency. That matches how
`internal/spellbench/v2engine` is placed.

### 5.2 Wire transport (phase 1): SSE + POST

The spec leaves transport open, and this is the house pattern (`host/httpapi/sse.go`).
It is stdlib-only and proxy-friendly, and gorged already serves it.

| Route (mounted only when enabled) | Auth | Behaviour |
|---|---|---|
| `GET /api/manabrew/v0/tables/{t}/matches/{k}/stream` | Seat token (Bearer or `?token=`) through the same resolver as `httpapi.Options.Seat` (`cmd/gorged/seats.go:56-72`); the claim table must equal `{t}` (the `claimForTable` semantics, `host/httpapi/rest.go:80-95`) | SSE. Each `data:` line is one engine→client message (`state`, `prompt`, `error`, and the terminal `prompt` `gameOver`). On connect it sends the current full `state`, then the open prompt if one exists. It never replays history. |
| `POST /api/manabrew/v0/tables/{t}/matches/{k}/send` | Same | Body is one `ClientToServerMessage`. `204` means accepted. A rejected response is **also** pushed on the stream as an `error` message. The HTTP reply body carries the same `ProtocolError`, so a client that does not stream sees it too. |
| `GET /api/manabrew/v0/tables/{t}/matches/{k}/state` | Same | One-shot `state` plus the open `prompt` for poll-only agents. It lets a client re-synchronise without SSE. |

- The `v0` path segment is gorge's own version of *its mapping*, because the spec has
  no version. It moves to `v1` when the spec publishes a version field (open question Q6).
- **Why `/api/manabrew/…`:** when the toggle is off, nothing is mounted and httpapi's
  catch-all answers exactly as it does today (`host/httpapi/handler.go:104-106`). §5.4
  depends on this.
- **Wake-up:** the transport opens a host `Session` and `Subscribe`s the table in focus
  mode (`host/session.go:230`).
  - Any frame for the match is treated only as "something changed", and its content is
    discarded.
  - The transport then re-reads `ViewAtSeat(t,k,head,claim.Seat)`, and `Pending` when
    this seat is asked.
  - Bursts are coalesced by draining the channel before each re-read. `ViewAtSeat`
    clones the log and replays up to a turn (`host/viewat.go:66-110`), exactly as the
    native `?seat=` view route does.
  - Performance is at parity with the native client. A later host export can make it
    cheaper; that is out of scope.
- **Timeouts and reconnect:**
  - The spec has neither. gorge keeps HumanSeat's `ThinkTimeout` plus caretaker (`host/humanseat.go:15-23`).
  - When the caretaker answers, the prompt goes stale. The client then receives a new
    `state` and the next `prompt`. A late `response` gets a `stalePrompt` error.
  - Reconnect is simply a new stream. It gets full state plus the open prompt, and it is
    idempotent because `promptId` is derived from `Seq`.
- **Match rollover** (perpetual tables): after `gameOver` the stream closes. The client
  reads the new match number from `GET /api/tables/{t}/matches` (existing).

### 5.3 Archtest rows (all land in MB-2, before any package exists)

`TestDependencyOrderHolds` skips a `from` package that is not built yet
(`internal/archtest/arch_test.go:162-165`). The time allowlist is a plain map. So every
row can land first and bind as each package appears. After that, no later ticket
touches `arch_test.go`.

- Forbidden edges:
  - `internal/manabrew` must not import `rules`, `effects`, `events`, `host`,
    `host/httpapi`, `internal/azmcts` or `internal/testutil`.
  - `protocol/manabrew` must not import any module package. This is a new small test:
    `TestManaBrewWireIsStdlibOnly`.
  - `host/manabrewhttp` must not import `rules`, `effects`, `internal/testutil` or
    `internal/azmcts`.
  - **Reverse rows:** none of `cards`, `state`, `decision`, `events`, `effects`,
    `botpolicy`, `rules`, `view`, `seat`, `replay`, `protocol`, `host` or `host/httpapi`
    may depend on `protocol/manabrew`, `internal/manabrew` or `host/manabrewhttp`.
- The `time` allowlist gets `host/manabrewhttp`. Its paragraph: SSE keep-alive and
  write deadlines only. No game, event, view or replay reads the clock, and intents
  reach the engine only through `SubmitIntent`.
- `internal/manabrew` does NOT get `time`.

### 5.4 The runtime toggle

Support is off by default. When it is off, the build is byte-identical to today: no
route, no goroutine, no behaviour change.

| Flag | Default | Meaning |
|---|---|---|
| `-manabrew` | `false` | Mounts §5.2's routes on the main listener's `topMux`, ahead of `httpapi.NewHandler`, in the same way as `/api/feedback` (`cmd/gorged/main.go:316-328`). It requires a seat gate (`-humans` or `-vsbot`). Without one, startup fails with a clear error rather than mounting routes that can only answer 403. |
| `-manabrew-addr` | `""` | Optional *separate* listener for the ManaBrew routes, for example `:8091` in the engine-task range. Setting it implies `-manabrew`. Ports 8080/8081 are refused at parse time with a message. |
| `-manabrew-think` | `0` (inherit) | Optional override of the think timeout for seats driven over ManaBrew. **INFERRED useful**; drop it if the operator prefers fewer knobs (Q5). |

- **Per-table option: recommended NOT to add.**
  - A ManaBrew connection is only a different *view* of an existing human seat.
    `HumanSeat` does not care which wire answered it.
  - So the same seat can be driven from the native web client or from a ManaBrew client,
    and neither `TableConfig` (`host/table.go:109`) nor its persisted sidecar changes.
  - If the operator wants to forbid ManaBrew on some tables, the right shape is an
    allowlist on the transport, `-manabrew-tables t1,t3`. It is still not a
    `TableConfig` field (Q4).

**How "off is byte-identical" is tested** (MB-9):

1. `TestManaBrewOffMountsNothing` (in `cmd/gorged`) builds the server config without
   the flag and checks that
   `GET /api/manabrew/v0/tables/t1/matches/1/stream` returns exactly the same status,
   headers and body as `GET /api/definitely-not-a-route`, which is the JSON 404 from
   `handler.go:104-106`.
2. `TestManaBrewOnRequiresSeatGate`: startup errors without `-humans`/`-vsbot`.
3. Archtest reverse rows (MB-2): no engine or host package can import the adapter, so
   "off" cannot change engine behaviour even in principle.
4. `TestHeads` and `TestRepoDeckGamesReplayExactly` are unchanged. No ticket touches an
   engine package, and every ticket that edits a package on the engine side of
   `cmd/gorged` is a review MAJOR.

---

## 6. Mapping (role E)

### 6.1 Identifiers

| ManaBrew | gorge | Rule |
|---|---|---|
| player id | `state.PlayerID` | `"player-<seat>"` (the docs' own examples use `player-0`). |
| card / permanent id (`CardDto.id`, `TargetRef{kind:"card"}`) | `state.ObjID` | `"o<ObjID>"`. This is **parity with the native wire**, which already exposes `ObjID`. `ObjID` is stable across zone changes; CR 400.7 is tracked by `Object.Incarnation` (`state/object.go:660-664`). See G-9 for the stronger epoch-salted option that SpellBench v2 uses (`internal/spellbench/v2engine/game.go:127-169`). |
| stack object id | `StackView.ID` (`view/view.go:428`) | `"s<ObjID>"`. `TargetRef{kind:"spell"}` points here. |
| hidden entry id (face-down exile) | none | `"h-<zone>-<owner>-<i>"`, positional. It carries no `ObjID`. |
| `promptId` | `Decision.Seq` | `promptId = Seq`. It is stable across reconnect and caretaker, and a stale answer is simply `Seq` ≠ current. A gorge Seq is < 2^53 (a JS safe integer). **INFERRED** from game lengths. MB-4 adds an assertion. |
| `actionId` (`chooseAction`, `payManaCost`) | `Option.Index`, or `PaymentAction.ID` | `"opt-<index>"`, or `"pay-<PaymentAction.ID>"` for an announce-able cast. |
| `gameId` | table id + match k | `"<table>/<k>"`. |
| `CardIdentity.name` | `CardView.Printing.Name` (`view/view.go:326-334,979`) | The face name. Forge names are shared vocabulary (§1). |
| `CardIdentity.setCode` / `cardNumber` | `Printing.Set` / `Number`, which are **always empty today** ("until a printing table exists", `view/view.go:326-329`) | Empty strings (G-7). |
| `CardIdentity.tokenScript` | Not in `CardView` (`Token` is the `"#<id>"` disambiguator, `view/view.go:360-363`). The registry keys tokens by the same Forge stem (`cards/tokens.go:12`). | Omitted in v1 (G-7). |
| Deck card identity (`DeckCardIdentity.name`) | `deck.Entry` name → `cards.Registry.Lookup` (`cards/registry.go:88`) | Match by name. Borrow the "A // B" → front-face fallback from `internal/spellbench/v2engine/server.go:436-440` and the diacritic fold from `internal/spellbench/v2shadow/setup.go:63-69`. |

### 6.2 State: `view.View` → `GameViewDto`

| GameViewDto / PlayerDto / CardDto field | gorge source | Status |
|---|---|---|
| `turn`, `activePlayerId`, `priorityPlayerId` | `View.Turn/Active/Priority` | direct |
| `step` | `View.Step` (state step names `state/ids.go:96-98`) | map (see the step table below) |
| `players[].life`, `name`, `status` | `PlayerView.Life/Name/Lost` | direct. `status` enum values are INFERRED. |
| `players[].manaPool` | `PlayerView.Pool` (`view/view.go:227`) | direct. `ManaColor` keys are INFERRED as `W/U/B/R/G/C`. |
| `players[].commanderDamage`, `commanderCasts` | `CmdDamage`, `CommanderCasts` | adapter work (re-key to string ids) |
| `players[].counters`, `maxHandSize`, `landsPlayedThisTurn`, `cardsDrawnThisTurn`, `ringLevel`, `speed`, … | not in `PlayerView` | **gap G-6**: defaults are sent (7, 0, …) |
| `zones[]` hand | own `Hand`; for opponents `Hand` is null and `HandSize` is used | direct: an opponent zone gets `cards:[]`, `count:HandSize` |
| `zones[]` library | `LibrarySize`, `LibraryTop` | direct: count, plus the top card when visible |
| `zones[]` graveyard/exile/command | `Graveyard/Exile/Command` | direct. Face-down exile becomes a hidden entry. |
| `zones[]` battlefield | `Battlefield`, **bucketed by controller** as the spec requires | adapter work |
| `stack[]` | `StackView` (`id`, `Name`, `Text`, `Controller`, `Source`, `Targets`, `Card`) | direct. `faceIndex` = 0. |
| `combatAssignments` | `CardView.BlockedBy` | adapter work |
| `gameOver`, `winnerId` | `View.Over/Winner/Draw` | direct |
| `initiativeHolderId` | `PlayerView.HasInitiative` | direct |
| `monarchId`, `dayTime`, `activePlaneNames` | not in `View` (INFERRED from the field list) | **gap G-6**: `null` / INFERRED default |
| `CardDto.identity.name`, `manaCost`, `power`, `toughness`, `damage`, `tapped`, `counters`, `keywords`, `controllerId`, `ownerId`, `summoningSick`, `isAttacking`, `attackingPlayerId`, `attachedTo`, `isFaceDown` | `CardView` fields (`view/view.go:340-424`) | direct (P/T become strings) |
| `CardDto.types/subtypes/supertypes` | `CardView.Types` is one string | adapter work: split it (the format is INFERRED; verify in MB-3) |
| `CardDto.text` | not in `CardView` (only `AbilityCosts`) | **gap G-7**: sent as `""` (licensing: never Forge script text) |
| `CardDto.isToken`, `isCopy`, `isDoubleFaced`, `isTransformed`, `phasedOut`, `exerted`, … | mostly not in `CardView` | **gap G-7**: `false` |
| face-down permanent for a non-looker | `CardView{ID, FaceDown, Token, …}` (`view/view.go:913-925`) | direct: blank `identity`, keep public state, as the spec prescribes |

Step map (gorge → `StepKind`):

| gorge step | StepKind |
|---|---|
| `untap` | `untap` |
| `upkeep` | `upkeep` |
| `draw` | `draw` |
| `main1` | `main1` |
| `begin-combat` | `combatBegin` |
| `declare-attackers` | `combatDeclareAttackers` |
| `declare-blockers` | `combatDeclareBlockers` |
| `combat-damage` | `combatDamage` |
| `end-combat` | `combatEnd` |
| `main2` | `main2` |
| `end` | `endOfTurn` |
| `cleanup` | `cleanup` |

`combatFirstStrikeDamage` is never emitted. gorge has no separate step constant for it
(`state/ids.go:81-93`); **INFERRED** that first-strike damage is resolved inside
`combat-damage`.

`stateDelta` is **not sent** in v1. Patches are optional for engines. It is a later
bandwidth optimisation (Q7).

### 6.3 Prompts: `decision.Kind` × `Option.Kind` → ManaBrew

Status key:
- **direct**: field-for-field.
- **adapter**: needs translation logic but loses nothing.
- **lossy**: a constraint the prompt cannot express. It is enforced by `Validate`; a
  violating answer gets an `invalidShape` error and the prompt stays open.
- **gap**: something the protocol or gorge cannot express at all.

| gorge ask (where built) | ManaBrew prompt | Status | Notes / consequence |
|---|---|---|---|
| `KPriority` `"pass"` (`rules/legal.go:4221`) | `chooseAction` → `{type:"pass"}` | direct | `until` and `exhaustStack` are carried out in the adapter as repeated `pass` intents, and stop when a new stack object appears (§6.5). |
| `KPriority` `"cast"` (`rules/legal.go:2508`) | `AvailableAction{type:"cast", mode, label}` | adapter | Alternative costs (`Option.AltCostIndex`) become distinct actions with `label`s. `mode` values are undefined (G-1). |
| `KPriority` `"play_land"` (`rules/legal.go:2247`) | `cast` with `mode:"play"` | **gap G-1** | `PlayCardMode` is undefined in the spec. A spec client may not recognise a land play. |
| `KPriority` `"activate"` (mana, `rules/legal.go:3496`) | `activateAbility{isManaAbility:true, producedMana}` | adapter | `producedMana` comes from `CardView.Produces`. |
| `KPriority` `"ability"`/`"station"`/`"unlock"`/`"turn_face_up"`/`"specialize"` | `activateAbility{isManaAbility:false, abilityIndex:Option.Ability, description:Label}` | adapter | Special actions have no distinct kind in ManaBrew. The label carries them. |
| `KPriority` `"concede"` (`rules/legal.go:4227`) | *directive* `concede` | adapter / **gap G-2** | ManaBrew allows concede "at any time". gorge offers it only on a priority decision. The adapter queues it and answers the seat's next priority decision with the concede option, so the concession is delayed through non-priority asks. |
| `KPriority` + `PaymentActions` / `Intent.Announce` (`decision/decision.go:587`; `decision/announce.go:17`) | `cast` action with id `pay-<ID>` → `Intent.Announce` | adapter | This opens the announce window below. It applies only when the table runs `AutoMana`, as `host/viewat.go`'s attach logic does. |
| `KChoose` + `ManaPayment` (announce window, `rules/announce_pay.go:173-240`) | `payManaCost` | adapter | `"mana"` → `act` with an `activateAbility` action. `autofill` → `pay{auto:true}`. `"done"` → `pay`. `cancel_cast` → `cancel`. `undo_tap` → an `undoMana` action (`decision/announce.go:37-41`). The best fit in the whole matrix. |
| `KChoose` legacy mana window `"activate"`/`"done"` (`rules/cast.go:10058-10076`) | `payManaCost` | lossy **G-3** | This window has no cancel, so `{type:"cancel"}` → `invalidShape`. |
| `KChoose` `"mana"` any-colour pick (`rules/mana_activation.go:2842`) | `chooseColor{amount:1, validColors from ManaSymbol}` | adapter | |
| `KChoose` `"color"` (`effects/choose.go:191`) | `chooseColor` | direct | |
| `KChoose` `"x"` (`rules/cast.go:4978-4984`) / `"number"` (`effects/number_choices.go:31-36`) | `chooseNumber{min,max}` when `Option.Amount` values are contiguous, otherwise `chooseFromSelection` | adapter | `chosenNumber:null` → `invalidShape` (no cancel). |
| `KChoose` `"yes"`/`"no"` (e.g. `effects/cardflow.go:245`) | `chooseBoolean` | direct | |
| `KChoose` single `"yes"` "Continue" look-ack (`effects/look.go:51-101`) | `chooseFromSelection` with one option | adapter | ManaBrew's `revealCards` would fit better, but the looked-at cards arrive only *after* the ack (as a Secret Note), so they cannot be put in the prompt (G-5). |
| `KChoose` card picks `"exile"`/`"sacrifice"`/`"discard"`/`"search"`/`"dig"`/`"keep"` (`decision/decision.go:71-94`) | `chooseCards{cards: CardDto[], min, max}` | lossy **G-4** | `MaxSum`/`MinSum`/`Budgeted`/`Group`/`GroupLimit(s)` (`decision/decision.go:608-684`) cannot be expressed. A rules-ignorant client may loop on `invalidShape`. The order of `"search"` answers comes from the response array order. |
| `KChoose` `"name"`/`"type"`/`"dungeon"`/`"room"`/`"roll"` | `chooseFromSelection` (weights 1) | adapter | The name universe is about 24k labels. The reference client switches to type-to-filter above six options. |
| `KChoose` `"division"` (`rules/combat.go:2424-2478`, capped at 128 splits at `:2271`) | `chooseFromSelection` over split labels | lossy **G-8** | `chooseCombatDamageAssignment` fits the meaning better, but `Option.Amount` carries only the first blocker's share (the full split is server-side), so the adapter cannot match an arbitrary assignment to an option. |
| `KChoose` `"asunblocked"` (`rules/combat.go:2317-2319`) | `chooseBoolean` | adapter | |
| `KChoose` damage_split (`Repeatable`, `effects/damage.go:97-166`) | `chooseFromSelection{canRepeat:true, weight:1, minTotal=maxTotal=total}` | direct | The shapes match exactly. |
| `KTarget` (`rules/cast.go:8883-8900`; candidate kinds at `rules/stack.go:1249-1257,1629,1833,1920`) | `chooseBoardTargets{candidates, minTargets, maxTargets, intent, hostile, cancellable:false}` | adapter / lossy | `"player"` → `player`, `"permanent"`/`"graveyard"` → `card`, `"spell"`/`"trigger"`/`"ability"` → `spell`. `intent` comes from `TargetEffect.API` (`decision/decision.go:502-515`). `Group`/`SetPropMode`/`TargetsWithSameController` are lossy (G-4). gorge has no target cancel. |
| `KAttackers` (Option `Attacker` + defender `Player`/`Battle`/`Obj`, `decision/decision.go:188-203`) | `chooseAttackers{attackers[].validTargetIds, mustAttack, attackTargets}` | adapter / lossy | Each response assignment maps to the matching (attacker, target) option index. `PayerLife`, `ChargeTapPool`, `GroupLimits` and per-attack costs are lossy (G-4). |
| `KBlockers` (`MinBlockers`/`MaxBlockers`/`BlockMust`, `decision/decision.go:212-240`) | `chooseBlockers{attackers[].validBlockerIds, minBlockers, maxBlockers, mustBeBlocked}` | adapter | Response order is **kept**: gorge's damage order among blockers is submission order (`rules/combat.go:2026-2030`). |
| `KMulligan` keep/mulligan (`rules/mulligan.go:206-220`) | `mulligan{handCardIds, mulliganCount}` | direct | |
| `KMulligan` bottom (`rules/mulligan.go:225-237`) | `mulliganPutBack{count}` | direct | |
| `KModes` mode pick | `chooseFromSelection{weight:1, canRepeat:Repeatable}` | direct | |
| `KModes` `unless_pay`/`unless_decline` (`decision/decision.go:166-171`) | `chooseBoolean{confirmLabel, denyLabel}` | adapter | If only the decline option is offered (`effects/unless.go:548`), the prompt is a one-option `chooseFromSelection`. |
| `KTriggerOrder` (`decision/decision.go:38-52`) | `reorder{items:[{id, card, oracle:Label}]}` | adapter, **direction flip** | ManaBrew says the first id resolves first. gorge's `Choices[0]` is put on the stack first and resolves **last**, so the adapter reverses. This is silent if wrong, so it gets a golden test. |
| `KTriggerOptional` | `chooseBoolean` | direct | |
| `KCommanderZone` | `chooseBoolean{confirm:"Command zone", deny:"Leave"}` | direct | |
| `KReplacement` competition | `chooseFromSelection` | adapter | |
| `KReplacement` `"mana"` (5 colours) | `chooseColor` | adapter | |
| `KReplacement` `apply`/`decline` | `chooseBoolean` | adapter | |
| `KArrange` split kinds (`"bottom"`/`"graveyard"`/`"exile"`/`"hand"`, `decision/decision.go:107-142`) | `scry{zones:["libraryTop", <pile-B dest>]}` | adapter / lossy | `zoneCardIds[0]` → `Choices` (first = top, same direction). `zoneCardIds[1]` → `Rest` when `Restable`, otherwise its order is dropped. Pile-A `Min`/`Max` is lossy (G-4). |
| `KArrange` full order or `"hideaway_bottom"`/`"dig_bottom"` | `reorder` | adapter | |
| `KStartingPlayer` (`decision/decision.go:143-152`) | `chooseFromSelection` over players | adapter | |
| end of match (`View.Over`) | `gameOver` prompt, no response | direct | Sent after the final `state`. |

ManaBrew prompts gorge never emits:

- **`chooseDamageAssignmentOrder`**: gorge takes the order from the order blocks were
  declared in (above).
- **`chooseCombatDamageAssignment`**: trample is automatic (`rules/combat.go:2257`), and
  non-trample splits go through `"division"`.
- **`diceRolled`** and **`revealCards`**: dice and reveals are Notes, not asks
  (`effects/dice.go:46,84`, `effects/cardflow.go:2543-2568`).

None of these is a protocol gap, because the engine decides which prompts it sends.
Synthetic acknowledge-only `revealCards` and `diceRolled` prompts are an optional
fidelity ticket (MB-14, G-5).

### 6.4 Responses → intents, errors

| ManaBrew check | gorge equivalent | Code |
|---|---|---|
| `promptId` ≠ the current prompt, or nothing pending | `Intent.Seq` mismatch (`decision/decision.go:1195-1197`) or `Registry.Pending` error | `stalePrompt` |
| The connection's seat is not `decidingPlayerId` | `Intent.Player` mismatch. It cannot occur on a claim-bound connection, but it is kept for defence. | `wrongPlayer` |
| `action.type` ≠ the prompt's `input.type` | adapter check | `wrongPromptType` |
| `actionId` not advertised | adapter check | `unknownActionId` |
| Anything `Decision.Validate` rejects (count, duplicates, groups, budgets) or a malformed body | `SubmitIntent` error text in `message` | `invalidShape` |

- After an error the prompt stays open, and the client may answer again. The spec does
  not say whether the engine re-sends the prompt. gorge does **not** re-send it
  (**INFERRED** reading; Q6).
- `restoreSnapshot{checkpointId}` → `Registry.Undo`. That call exists only on a table
  with a single human seat (`host/undo.go:168-175`), and it rewinds to that seat's last
  answered decision.
  - `checkpointId` must equal the current `promptId`, meaning "undo my last answer".
    Anything else is `invalidShape`.
  - Undo is not advertised in the spec, so this is **G-10**. Note also that undo lets a
    player learn upcoming draws (`host/undo.go:36-42`).

### 6.5 Auto-pass (`pass.until`, `exhaustStack`)

gorge has no server-side auto-pass. Auto-pass exists only in the web client:
`web/src/lib/autopilot.ts:6-40` and `web/src/lib/playsettings.ts`.

The adapter keeps a per-connection `passPolicy`:
- `until{playerId, phase}`: keep answering `pass` on this seat's priority decisions until
  the view shows that player active in that step.
- `exhaustStack`: keep answering `pass` until the stack is empty. Stop early when a stack
  object id that was not there when the pass was sent appears.

These rules follow the spec text. Every auto-pass is an ordinary logged intent, so
replay is unaffected. The policy is cleared by the first non-priority prompt, and after
a rewind.

---

## 7. Gap list (ranked by consequence)

| # | Gap | Side | Consequence | Disposition |
|---|---|---|---|---|
| G-1 | No land-play action and `PlayCardMode` is undefined | spec | A spec-only client may not render or play lands. | Send `mode:"play"` with a clear `label`. Ask upstream (Q8). |
| G-2 | `concede` is "any time", but gorge offers it only at priority | gorge | The concession is delayed until the seat's next priority decision. | Adapter queue. A kind change is not warranted (invariant 4). |
| G-3 | The legacy manual mana window has no cancel | gorge | `payManaCost.cancel` is refused on non-`AutoMana` tables. | Recommend ManaBrew connections require `-auto-mana` (Q3). |
| G-4 | Budget, group and charge constraints (`MaxSum`, `MinSum`, `Group*`, `SetProps`, `PayerLife`, `ChargeTapPool`) are not expressible in `chooseCards`/`chooseBoardTargets`/`chooseAttackers`/`scry` | spec | A rules-ignorant client can submit an illegal combination and loop on `invalidShape`. | `Validate` is the fence. Put a human-readable constraint in `presentation.description`. Count rejections in the census (MB-8). |
| G-5 | Reveals, looks and dice have no prompt or state carrier; `display` is WIP | both | The ManaBrew client does not see revealed cards or roll results. | Optional synthetic ack prompts (MB-14). Their promptIds live in a separate `Seq<<8 \| n` namespace (INFERRED safe). |
| G-6 | `PlayerDto` scalars gorge does not project (counters such as poison, hand/land limits, monarch, day/night, ring, speed) | gorge | These show as defaults on the client. | Additive `view` fields are a separate engine-side ticket, deliberately outside this program (golden-safe, but they widen scope). |
| G-7 | `CardDto.text`, `setCode`/`cardNumber`, `isToken`/`tokenScript`, DFC/transform flags | gorge | Clients render names, and images resolve by name only (INFERRED). A token's identity is its name only. | `Printing` is already reserved for this (`view/view.go:326-329`). `text` stays empty for licensing reasons unless it is sourced from Scryfall (Q9). |
| G-8 | `"division"` options do not carry the full split | gorge | The adapter cannot offer `chooseCombatDamageAssignment`, so damage division shows as label buttons. | Accept it, or add an additive `Option` field later (engine ticket, out of scope). |
| G-9 | Object ids are raw `ObjID`s, stable across zone changes including hidden hops | gorge (native too) | A client can recognise a card it saw earlier when it reappears, for example a morph cast face-down (`view/view.go:913-925` keeps `ID`). This is a **possible pre-existing native-wire leak**: it was read in code but not tested end to end. | v1 keeps parity. Follow-up: an epoch-salted id per viewer, as in `internal/spellbench/v2engine/game.go:127-169`. Report the native case to the operator separately (Q10). |
| G-10 | No `checkpointId` discovery, and undo is single-human only | spec + gorge | `restoreSnapshot` works only as "undo my last answer". | Documented behaviour. |
| G-11 | No protocol version, handshake or lobby in the spec | spec | gorge cannot negotiate. A client must be configured with a gorged URL and token. | `v0` path segment. Revisit when upstream versions the spec. |
| G-12 | The relay/node protocol is not published | spec | Role N is blocked on a clean-room study of AGPL source. | Spike MB-12 plus Q2. |

---

## 8. Conformance: how we prove it

1. **Wire round-trip and doc examples (MB-1).** Every example on the 19 prompt pages,
   plus the game-view and patch examples, is decoded, re-encoded and re-decoded to a
   fixed point.
   - The examples are stored under `protocol/manabrew/testdata/docs/` with a CC-BY-4.0
     attribution header file.
   - The known doc inconsistencies (`modeLabel`, the extra `label`, missing
     `presentation`) must decode leniently, and the unknown-field census must name them.
2. **Fuzz decoding (MB-1, MB-7).**
   - `FuzzDecodeClientMessage`: arbitrary bytes never panic, and every failure maps to a
     `ProtocolError` code.
   - `FuzzTranslateResponse`: a random well-formed response against recorded prompts
     yields an intent that `Validate` accepts, or a `ProtocolError`. It never panics and
     never yields an invalid intent.
3. **Closed-set coverage (MB-4).** `TestEveryDecisionKindTranslates` iterates
   `decision.Kinds` and fails on a kind with no case. It is the same pattern as
   `TestKindsListsEveryKindOnce`, and it binds automatically if an operator ever adds a
   kind.
4. **Mock ManaBrew peer and census (MB-8).**
   - `internal/manabrew/mbtest.TranslatingSeat` plays repo-deck games through
     `internal/bench`. It is seeded, and its size is capped at the box rules (20 games).
   - Every gorge decision goes decision → prompt JSON → mock client → response JSON →
     intent.
   - Census counters: `posed:<prompt>`, `enumerated:<Kind>:<optkinds>`,
     `rejected:<code>`, `unmapped:*`. This is SpellBench's approach
     (`internal/spellbench/v2engine/game.go:330-331`).
   - The test fails on any `unmapped` count, and on any `rejected` count for the
     first-legal client (a first-legal client must never be refused).
5. **Replay equivalence (MB-8).** For each game played through the adapter:
   - `replay.Replay(cfg, log)` reproduces the head.
   - The same seed played natively with the same underlying policy
     (`TranslatingSeat` wrapping the policy it translates *for*) yields the **identical
     head**. The adapter is a pure bijection on the answers it forwards.
6. **Golden transcripts (MB-11).** A fixed-seed 2-seat game through the real HTTP
   transport (`httptest`, no port binding) with the mock client. The complete
   engine→client NDJSON stream is compared byte for byte with
   `internal/manabrew/testdata/golden/*.ndjson`.
   - It is regenerated by `MANABREW_REGEN_GOLDEN=1`.
   - It contains card names only, never script text.
7. **Privacy (MB-10).**
   - Seat 1's token on seat 0's stream gets 403.
   - A stream for seat 0 never contains a `visibility:"visible"` entry for seat 1's hand
     or library. This is asserted over every message of a full game.
8. **Off byte-identical (MB-9).** See §5.4.

### 8.5 Licensing

- **Spec (CC-BY-4.0).** Implementing the wire format is explicitly invited. Go structs
  that mirror the spec's types, and doc examples copied into testdata, need attribution:
  the spec title, its URL, "CC-BY-4.0", and a note that we changed it. That attribution
  goes in `protocol/manabrew/doc.go` and `testdata/docs/ATTRIBUTION`.
- **Reference implementation (AGPL-3.0-or-later).** No code is copied or ported. MB-12
  (the relay study) produces a *prose* description of the wire behaviour, written
  without copying code. Whether the operator is comfortable basing an Apache-2.0
  implementation on a description of AGPL behaviour is Q2.
- **Forge (GPL-3.0).** This is unchanged. ManaBrew's use of Forge names and token stems
  is a shared vocabulary of card *names*, which gorge already handles.

---

## 9. Tickets (ordered)

Box rules for every ticket:
- Run only targeted commands, in the form
  `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run '<pattern>' ./<pkg>/`.
- Never run `go test ./...`.
- Never bind 8080/8081. httptest listeners only.
- Work in a worktree created by `scripts/agent-worktree.sh`, which has `.cards`.

Seat guidance: glm and deepseek handle single-module work. **sol** is named where the
review risk is concurrency or silent direction errors.

### Phase 1: role E

**MB-1: `protocol/manabrew` wire types and codecs**
- **Goal:** Go structs for every message, prompt input, prompt output and DTO in
  Appendix A.
  - Discriminated-union `MarshalJSON`/`UnmarshalJSON` for `kind`, `input.type`,
    `output.type` and `visibility`.
  - A lenient `Decode` that returns the unknown-field paths, and a canonical `Encode`.
  - CC-BY attribution in `doc.go`.
- **Files:** `protocol/manabrew/{doc.go,messages.go,prompts.go,dto.go,codec.go}`, tests,
  and `testdata/docs/*.json` plus `ATTRIBUTION`.
- **Out of scope:** any gorge import, `stateDelta` *encoding* (decode-only is fine),
  and translation.
- **Depends-On:** none. **Seat:** deepseek.
- **Done means:**
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestDocExamplesRoundTrip|TestLenientDecodeReportsUnknown|TestEveryPromptTypeHasCodec' ./protocol/manabrew/`
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -tags fuzz -run '^$' -fuzz FuzzDecodeClientMessage -fuzztime 30s ./protocol/manabrew/`
  - `go list -deps ./protocol/manabrew/ | /usr/bin/grep adams-shaun` prints only the
    package itself.

**MB-2: archtest rows (lands before any adapter package)**
- **Goal:** add every row in §5.3.
  - New `TestManaBrewWireIsStdlibOnly`.
  - `host/manabrewhttp` in the `time` allowlist, with its paragraph.
  - All rows bind lazily for packages that do not exist yet.
- **Files:** `internal/archtest/arch_test.go` only.
- **Out of scope:** any other file.
- **Depends-On:** none. It may land in parallel with MB-1. **Seat:** glm.
- **Done means:**
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestDependencyOrderHolds|TestTimeIsImportedOnlyByTheHost|TestManaBrewWireIsStdlibOnly' ./internal/archtest/`

**MB-3: translator skeleton and state projection**
- **Goal:** create `internal/manabrew` with:
  - `Translator`, the id mint (§6.1) and the step map (§6.2);
  - `view.View → GameViewDto`, including hidden entries, battlefield bucketing by
    controller and face-down redaction;
  - `dispatch.go`, which holds the **full** `switch` over `decision.Kinds`. Each case
    calls a per-kind function defined in its own stub file (`prompt_priority.go`,
    `prompt_target.go`, `prompt_combat.go`, `prompt_mulligan.go`, `prompt_choose.go`,
    `prompt_modes.go`, `prompt_order.go`, `prompt_arrange.go`, `prompt_payment.go`,
    `prompt_misc.go`). Each stub returns `ErrUnmapped`.
  Later tickets edit only their own stub files, never `dispatch.go`.
- **Files:** `internal/manabrew/{doc.go,translator.go,ids.go,state.go,dispatch.go,prompt_*.go}`,
  plus golden views under `testdata/state/`.
- **Out of scope:** prompt bodies, the transport, and any `view` change.
- **Depends-On:** MB-1, MB-2. **Seat:** deepseek.
- **Done means:**
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestStateProjectionGolden|TestHiddenZonesNeverVisible|TestStepMapTotal|TestIDsDeterministic' ./internal/manabrew/`

**MB-4: priority, targets, combat, mulligan prompts, and errors**
- **Goal:** fill in these stubs:
  - `prompt_priority.go`: `chooseAction`, including cast/play/activate/ability actions,
    the concede queue, the `passPolicy` (§6.5), `restoreSnapshot` → an undo request
    marker, and `pay-<ID>` → `Intent.Announce`;
  - `prompt_target.go` (`chooseBoardTargets`, and the `TargetEffect.API` →
    `TargetingIntent` table);
  - `prompt_combat.go` (attackers and blockers);
  - `prompt_mulligan.go` (both phases).
  Also the §6.4 error mapping, and `TestEveryDecisionKindTranslates`. That test lists
  the kinds still stubbed in an explicit `pendingKinds` set, which MB-5 and MB-6 empty.
- **Files:** only the four `prompt_*.go` files named above, plus `errors.go` and tests.
- **Out of scope:** the other stubs and the transport.
- **Depends-On:** MB-3. **Seat:** deepseek. Escalate to sol on a second review round:
  auto-pass stop conditions are subtle.
- **Done means:**
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestChooseAction|TestPassPolicy|TestBoardTargets|TestAttackersBlockers|TestMulligan|TestErrorCodes|TestEveryDecisionKindTranslates' ./internal/manabrew/`

**MB-5: choice prompts**
- **Goal:** fill in:
  - `prompt_choose.go`: every non-payment `KChoose` option kind in §6.3, including
    look-ack, damage_split, division, name/type/number/x/color/yes-no and the
    card-pick kinds, with the constraint text in `presentation.description`;
  - `prompt_modes.go` (`KModes`, unless-pay);
  - `prompt_misc.go` (`KTriggerOptional`, `KCommanderZone`, `KReplacement`,
    `KStartingPlayer`).
  Remove these kinds from `pendingKinds`.
- **Files:** the three stubs and tests.
- **Out of scope:** payment and ordering.
- **Depends-On:** MB-4 (for `errors.go` and the coverage test). **Seat:** deepseek.
- **Done means:**
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestChooseKinds|TestModes|TestUnlessPay|TestReplacement|TestStartingPlayer|TestCommanderZone|TestEveryDecisionKindTranslates' ./internal/manabrew/`

**MB-6: ordering, arrange, and payment prompts**
- **Goal:** fill in:
  - `prompt_order.go`: `KTriggerOrder` → `reorder`, **with the direction flip**, and a
    test named `TestTriggerOrderFirstIdResolvesFirst` that runs a two-trigger fixture
    through the engine to prove it;
  - `prompt_arrange.go`: `KArrange` → `scry`/`reorder`, `Rest` handling;
  - `prompt_payment.go`: the announce window and the legacy window → `payManaCost`.
  After this ticket `pendingKinds` is empty.
- **Files:** the three stubs and tests.
- **Out of scope:** the transport.
- **Depends-On:** MB-5. **Seat:** sol. Two silent-direction contracts (trigger order and
  arrange piles) plus the payment state machine.
- **Done means:**
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestTriggerOrderFirstIdResolvesFirst|TestArrangeScry|TestArrangeReorder|TestPayManaCostAnnounce|TestPayManaCostLegacy|TestEveryDecisionKindTranslates' ./internal/manabrew/`

**MB-7: response fuzzing**
- **Goal:** `FuzzTranslateResponse`, seeded with recorded prompts from MB-4 to MB-6's
  fixtures.
- **Files:** `internal/manabrew/fuzz_test.go` and `testdata/fuzz/`.
- **Depends-On:** MB-6. **Seat:** glm.
- **Done means:**
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -tags fuzz -run '^$' -fuzz FuzzTranslateResponse -fuzztime 60s ./internal/manabrew/`

**MB-8: mock peer, census, and replay equivalence**
- **Goal:** `internal/manabrew/mbtest`, containing:
  - the mock client (first-legal and seeded-random modes);
  - `TranslatingSeat`;
  - a census test over at most 20 seeded repo-deck games, 2 and 4 seats, one at a time;
  - replay-equivalence, as in §8 items 4 and 5.
- **Files:** `internal/manabrew/mbtest/*`.
- **Out of scope:** HTTP.
- **Depends-On:** MB-6. **Seat:** deepseek.
- **Done means:**
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestManaBrewCensusNoUnmapped|TestManaBrewReplayEquivalent|TestFirstLegalNeverRefused' ./internal/manabrew/mbtest/`
    must run (not skip). Check with `-v` that the census log line shows games > 0: the
    corpus must be present.

**MB-9: gorged toggle**
- **Goal:** the `-manabrew`, `-manabrew-addr` and (if Q5 says yes) `-manabrew-think`
  flags. Mount `host/manabrewhttp` on `topMux` ahead of httpapi. Startup validation.
  The tests in §5.4 items 1 and 2. A `README.md` line under "External processes can play
  a seat" (a third way).
- **Files:** `cmd/gorged/main.go`, `cmd/gorged/manabrew.go` (new), tests, `README.md`.
- **Out of scope:** the transport internals.
- **Depends-On:** MB-10. **Seat:** glm.
- **Done means:**
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestManaBrewOffMountsNothing|TestManaBrewOnRequiresSeatGate|TestManaBrewAddrRefusesDemoPorts' ./cmd/gorged/`
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestHeads$' ./rules/`
    (unchanged heads).

**MB-10: `host/manabrewhttp` transport**
- **Goal:** the §5.2 routes.
  - Claim binding through an injected `func(*http.Request) (httpapi.SeatClaim, bool)`.
    **INFERRED** acceptable import of `host/httpapi` for the type. Alternatively, define
    a local twin to avoid the edge; decide during review.
  - Session wake-up and coalescing.
  - SSE writer with keep-alive.
  - `send` handling: response → `SubmitIntent`, directive → concede queue,
    `restoreSnapshot` → `Undo`.
  - Caretaker and stale behaviour.
  - Privacy tests (§8 item 7).
- **Files:** `host/manabrewhttp/*`.
- **Out of scope:** gorged flags.
- **Depends-On:** MB-6. It may proceed in parallel with MB-7 and MB-8. **Seat:** sol
  (goroutine lifetimes, lock ordering against the host `fanMu` — see
  `host/session.go:249-262`).
- **Done means:**
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestStreamSendsStateThenPrompt|TestOtherSeatTokenForbidden|TestNoForeignHandEverVisible|TestStaleAfterCaretaker|TestConcedeQueuedToPriority|TestReconnectIdempotent' ./host/manabrewhttp/`
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -race -run 'TestStreamSendsStateThenPrompt|TestReconnectIdempotent' ./host/manabrewhttp/`

**MB-11: golden transcripts and `cmd/mbbridge`**
- **Goal:**
  - An end-to-end golden NDJSON transcript over `httptest` (§8 item 6).
  - `cmd/mbbridge`, which relays stdio NDJSON to and from the HTTP routes. It gets an
    archtest `time` row only if it needs one; prefer none.
- **Files:** `host/manabrewhttp/golden_test.go`, `testdata/golden/`, `cmd/mbbridge/*`.
- **Depends-On:** MB-9. **Seat:** deepseek.
- **Done means:**
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestGoldenTranscript' ./host/manabrewhttp/`
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestBridge' ./cmd/mbbridge/`

**MB-14 (optional, after MB-11): synthetic `revealCards`/`diceRolled`**
- **Goal:** turn public reveal Notes and dice Notes in `EventsSeat` into ack-only
  prompts, answered inside the adapter. They never reach the engine, and they use the
  separate promptId namespace (G-5).
- **Files:** `internal/manabrew/synthetic.go` and tests.
- **Depends-On:** MB-8. **Seat:** deepseek.
- **Done means:**
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestSyntheticReveal|TestSyntheticDice|TestManaBrewReplayEquivalent' ./internal/manabrew/...`

### Phase 2: role N (gated on Q1 and Q2)

**MB-12: relay/node protocol study (hand or Opus; no code)**
- **Goal:** a clean-room *prose* description of the relay's WebSocket messages:
  - auth with the shared key;
  - lobby, room and seat lifecycle;
  - deck submission;
  - how node-hosted games carry `/protocol/` messages;
  - health.
  It is written from the public repository without copying code, and its output is a
  spec in `docs/superpowers/specs/`.
- **Depends-On:** operator ruling on Q2. **Seat:** hand/Opus.
- **Done means:** a spec with every message field cited to a repo file and line at a
  pinned commit, and a go/no-go.

**MB-13: stdlib RFC 6455 WebSocket client, then the node (sizes decided by MB-12)**
- **Goal:** `internal/wsclient`. This is framing, masking, ping/pong and close, with no
  third-party deps. Autobahn-style unit vectors come from RFC 6455's examples. A node
  ticket list follows MB-12.
- **Seat:** sol.

**MB-15: ManaBrew deck import**
- **Goal:** ManaBrew `Deck` JSON → `deck.File` by name, with the §6.1 fallbacks.
  `setCode`, `cardNumber`, `tokens` and `attractions` etc. are ignored and counted.
  Unsupported cards are reported with `reg.Unsupported`.
- `httpapi.CreateGameOptions` takes a catalogue deck *name* today
  (`host/httpapi/rest.go:366-374`). Custom-deck play therefore needs its own design
  (Q11).
- **Files:** `internal/manabrew/deckimport.go` and tests.
- **Depends-On:** MB-1. **Seat:** glm.
- **Done means:**
  - `GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestDeckImport' ./internal/manabrew/`

### Phase 3: role A. Not ticketed.

This reuses `internal/spellbench/v2shadow`'s staging brain and MB-13's client. Scope it
only if the operator wants gorge bots on Forge or ManaBrew engines (Q1).

### Web/UI

Phase 1 needs **no** web change. If the operator wants a landing-page "connect with a
ManaBrew client" affordance (show the stream URL and token), that is a **codex/hand**
ticket, per the house rule that no UI work goes to ds4 or glm.

### Dependency graph and collision map

```
MB-1 ─┬─> MB-3 ─> MB-4 ─> MB-5 ─> MB-6 ─┬─> MB-7
MB-2 ─┘                                  ├─> MB-8 ─> MB-14
                                         └─> MB-10 ─> MB-9 ─> MB-11
MB-1 ─> MB-15 (independent)
Q1/Q2 ─> MB-12 ─> MB-13 ─> (node tickets)
```

| File or area | Tickets | Rule |
|---|---|---|
| `internal/archtest/arch_test.go` | MB-2 only | All rows land up front. Any later need is a review finding. |
| `internal/manabrew/dispatch.go` | MB-3 only | Later tickets edit only their own `prompt_*.go`. |
| `internal/manabrew/errors.go`, the coverage test's `pendingKinds` | MB-4 creates; MB-5 and MB-6 shrink `pendingKinds` | Serialised by Depends-On. |
| `cmd/gorged/main.go` | MB-9 (and a future node ticket) | Only one runs at a time. |
| `README.md` | MB-9 | |
| `protocol/manabrew/*` | MB-1. Later tickets may only *add* optional fields. | |
| Engine packages (`rules`, `effects`, `events`, `state`, `decision`, `view`, `seat`, `host`, `host/httpapi`) | **none** | Any edit there is a MAJOR finding for this program. G-6, G-7 and G-8 fixes are separate engine tickets. |

---

## 10. Open questions for the operator (each with a recommendation)

- **Q1. What outcome do you want?**
  - (a) A standard protocol that agents and third-party clients can drive gorge with.
  - (b) Real ManaBrew players playing on gorge.
  - (c) gorge bots playing on ManaBrew/Forge engines.
  - *Recommendation:* approve Phase 1 (E) now. It is required for (b) and reusable for
    (c). Decide (b) versus (c) after MB-12's study, because both are blocked on the same
    undocumented relay protocol.
- **Q2. Clean-room reading of AGPL source.** Are we comfortable deriving an Apache-2.0
  implementation from a prose description of the AGPL relay's wire behaviour?
  - *Recommendation:* yes, with the study done by a seat that writes prose only. Or,
    better, ask the ManaBrew maintainers (Discord or GitHub) to publish the relay
    protocol under CC-BY like `/protocol/`. That request is the operator's call; this
    work did not contact them.
- **Q3. Require `-auto-mana` for ManaBrew seats?**
  - *Recommendation:* yes. `payManaCost` matches the announce window almost one to one
    (cancel, undo, auto). The legacy window cannot cancel (G-3).
- **Q4. Per-table enablement?**
  - *Recommendation:* no `TableConfig` field. A server-level toggle, plus an optional
    `-manabrew-tables` allowlist only if it is actually needed.
- **Q5. Do we want `-manabrew-think`?**
  - *Recommendation:* drop it for v1 and inherit the table's think timeout. Fewer knobs.
- **Q6. Envelope and error behaviour the spec leaves open**: the `state`/`prompt`
  envelope shape, and whether a prompt is re-sent after an `error`.
  - *Recommendation:* use `{"kind":"state",…}` and `{"kind":"prompt",…flattened
    AgentPrompt}`, and do not re-send (the prompt stays open).
  - Keep the path at `v0` so either can change without breaking the flag.
- **Q7. `stateDelta` patches?**
  - *Recommendation:* not in phase 1. Full states only; the spec allows it. Revisit
    only if SSE bandwidth is measured to matter.
- **Q8. Upstream spec feedback** (land play/`PlayCardMode`, undefined types, doc example
  inconsistencies, versioning, `checkpointId` discovery).
  - *Recommendation:* the operator files one upstream issue listing G-1, G-10, G-11 and
    the §1 inconsistencies. It is cheap and it de-risks role N.
- **Q9. `CardDto.text`.**
  - *Recommendation:* keep it empty. Forge script text must never be served from
    committed code paths, and Oracle text is better sourced from the existing
    Scryfall-backed `/cards/named` cache (`cmd/gorged/main.go:303-314`) in a later
    ticket if clients need it.
- **Q10. The G-9 native-wire observation.** A face-down permanent keeps its real `ObjID`
  in the redacted view (`view/view.go:913-925`), and `ObjID`s are stable across hidden
  hops. So a card seen earlier may be recognisable when it is cast face-down.
  - *Recommendation:* file a separate engine-side ticket to measure it (a test that
    bounces a known creature to hand and then casts it face-down), independent of
    ManaBrew. The adapter ships at parity.
- **Q11. Custom decks from ManaBrew clients.**
  - *Recommendation:* defer. Phase 1 plays catalogue decks through the existing seat
    tokens. Custom-deck upload is a lobby concern and belongs with role N.

---

## Appendix A: ManaBrew message reference (condensed)

Source pages are given per section. Wire keys are camelCase. `?` marks an optional
field. **UNDEFINED** means the type is referenced but no page defines it.

**A.1 Envelopes** (<https://docs.manabrew.app/protocol/>)

- `AgentPrompt {promptId:number, decidingPlayerId:string, sourceCard?:CardDto, input:PromptInput}`
- `ClientToServerMessage = {kind:"response", promptId:number, action:PromptOutput} | {kind:"directive", directive:DirectiveInput}`
  - `PromptOutput = {type:<promptType>, output:{type:<outputType>, …}}`, shape per example.
  - `DirectiveInput = {type:"concede"}`; nothing else is shown.
- `ProtocolError {code: "stalePrompt"|"wrongPlayer"|"wrongPromptType"|"unknownActionId"|"invalidShape", message:string, promptId?:number}`
- Engine→client kinds: `state` (`StateUpdate{gameView}`), `stateDelta`
  (`{kind, base, fingerprint, patch}`), `display` (`DisplayEvent`, UNDEFINED, WIP),
  `prompt`, `error`.

**A.2 Game view** (<https://docs.manabrew.app/protocol/game-view/>)

- `GameViewDto {gameId, turn, step:StepKind, combatAssignments:CombatAssignmentDto[], activePlayerId, priorityPlayerId, players:PlayerDto[], zones:ZoneDto[], stack:StackObjectDto[], gameOver:boolean, winnerId:string|null, monarchId:string|null, initiativeHolderId:string|null, dayTime:DayTime(UNDEFINED), activePlaneNames?:string[]}`
- `PlayerDto {id, name, status:PlayerStatus(UNDEFINED), isHuman, life, maxHandSize, unlimitedHandSize, landsPlayedThisTurn, maxLandPlaysPerTurn, unlimitedLandPlays, cardsDrawnThisTurn, damagePrevention, isExtraTurn, extraTurnCount, controlledBy?, playerKeywords:string[], commanderCasts:Record<string,number>, dungeonState?:DungeonStateDto(UNDEFINED), activeSchemeNames?, teamNumber?, counters:{[PlayerCounterKind]?:number}, manaPool:{[ManaColor]?:number}, commanderDamage:Record<string,number>, hasCityBlessing, hasEnduringStory, ringLevel, speed}`
- `ZoneDto {zone:ZoneKind(UNDEFINED), ownerId, cards:CardView[], count}`
  - One `ZoneDto` per (zone, owner). Battlefield is bucketed by controller.
  - Cards are ordered top-first where the order is public.
- `CardView = ({visibility:"visible"} & CardDto) | {visibility:"hidden", id}`
- Visibility rules:
  - Hands: visible to the owner only; others get the count.
  - Library: count, plus a visible top card when permitted.
  - Face-down exile: hidden entries.
  - Face-down permanents: a redacted `CardDto` with a blank `identity` that keeps public
    state.
- `StepKind = untap|upkeep|draw|main1|combatBegin|combatDeclareAttackers|combatDeclareBlockers|combatFirstStrikeDamage|combatDamage|combatEnd|main2|endOfTurn|cleanup`
- `CardDto {id, identity:CardIdentity, color, manaCost, cmc, types[], subtypes[], supertypes[], power:string|null, toughness:string|null, basePower?, baseToughness?, finalChapter?, classLevel?, classLevels:ClassLevelDto[](UNDEFINED), sagaChapters:SagaChapterDto[](UNDEFINED), text, choices:CardChoiceDto[](UNDEFINED), controllerId, ownerId, tapped, isCrewed, isAttacking, attackingPlayerId?, attackTargetId?, keywords[], counters:Record<string,number>, damage, summoningSick, isCopy, isDoubleFaced, isTransformed, isFaceDown, isBestowed, phasedOut, exerted, isRingBearer, attachedTo?, attachmentIds[], mergedCardIds[], flashbackCost?, kickerCost?, effectiveManaCost?, commanderTax?, madnessCost?, isMadnessExiled, isPlotted, isWarpExiled, foil, wouldDieInCombat}`
  - Counter keys follow the engine's canonical form: "P1P1", "Loyalty", and uppercase
    for one-offs.
- `CardIdentity {name, setCode, cardNumber, isToken, tokenScript?:TokenScript}`
- `CombatAssignmentDto {blockerId, attackerId}`
- `StackObjectDto {id, sourceId, controllerId, ownerId, identity, text, sourceAbilityText?, isPermanentSpell, isCasting, isDoubleFaced, faceIndex, targets:TargetRef[]}`
- `TargetingIntent = damage|destroy|sacrifice|exile|bounce|mill|discard|counter|tap|untap|copy|buff|debuff|heal|loseLife|reveal|draw|fetch|gainControl|fight|attach|attack|block|hostile|friendly`
- Patches: `$v` replace, `$d` remove, `$k` keyed array edits (element key = `id`; zones
  use `"<zone>/<ownerId>"`), `$o` full key order. `null` is a value.

**A.3 Shared types** (<https://docs.manabrew.app/protocol/shared-types/>)

- `PromptPresentation {title, description?, text?, targets:TargetRef[]}`
- `TargetRef {kind:"player"|"card"|"spell", id, intent?:TargetingIntent, oracle?}`
- `ScryDestination = libraryTop|libraryBottom|graveyard|exile|hand`
- `DiceRollEntry {label?, playerId?, round, naturalResults[], finalResults[], ignoredRolls[], highlighted}`
- `AvailableAction = {id} & ({type:"cast", cardId, mode:PlayCardMode(UNDEFINED), label} | ({type:"activateAbility"} & ActivatableAbilityInfo) | {type:"undoMana", cardId})`
- `ActivatableAbilityInfo {cardId, abilityIndex, description, isManaAbility, isClassLevelUp?, cost?, producedMana?:Mana[](UNDEFINED; example {color, amount})}`
- `AttackerOptionDto {attackerId, validTargetIds[], mustAttack}`
- `AttackTargetDto {id, label, kind:"player"|"planeswalker"|"battle"}`
- `BlockableAttackerDto {attackerId, validBlockerIds[], minBlockers, maxBlockers?, mustBeBlocked}`
- `TokenScript = string` (Forge `tokenscripts` stem)

**A.4 Prompts.** Each row gives the input, then the output. The page is
`https://docs.manabrew.app/protocol/<kebab-name>/`.

| Prompt | Input fields (after `type`) | Output |
|---|---|---|
| `chooseNumber` | `presentation, min, max` | `{type:"numberDecision", chosenNumber:number\|null}` (null only where cancel is legal) |
| `chooseCards` | `presentation, cards:CardDto[], min, max` | `{type:"chooseCardsDecision", chosenCardIds:string[]}` |
| `chooseColor` | `presentation, validColors:string[], amount, repeatAllowed` | `{type:"colorDecision", chosenColors:{[color]:number}}` (sums to `amount`) |
| `chooseBoolean` | `presentation, confirmLabel, denyLabel` | `{type:"decision", value:boolean}` |
| `chooseFromSelection` | `presentation, options:SelectionOption[] (UNDEFINED; example {label, weight, canRepeat}), minTotal, maxTotal` | `{type:"selectionDecision", chosenIndices:number[]}` (weights must sum within bounds; repeats only if `canRepeat`) |
| `revealCards` | `presentation, cards:CardDto[], zone:ZoneKind, ownerPlayerId` | `{type:"revealCardsAcknowledged"}` |
| `scry` | `presentation, cards:CardDto[], zones:ScryDestination[]` | `{type:"scryDecision", zoneCardIds:string[][]}` (parallel to `zones`, final order, every card exactly once) |
| `reorder` | `presentation, items:ReorderItem[] (UNDEFINED; example {id, card, oracle?})` | `{type:"reorderDecision", orderedIds:string[]}` (first id on top; for triggers, first resolves first) |
| `diceRolled` | `presentation, sides, rolls:DiceRollEntry[]` | `{type:"diceRolledAcknowledged"}` |
| `chooseAction` | `actions:AvailableAction[]` | `{type:"pass", until?:{playerId, phase:StepKind}, exhaustStack:boolean} \| {type:"restoreSnapshot", checkpointId:number} \| {type:"act", actionId}` |
| `payManaCost` | `presentation, cardId, cardName, manaCost, canConfirmFromPool, actions:PaymentAction[] (UNDEFINED; example is activateAbility-shaped)` | `{type:"act", actionId} \| {type:"pay", auto:boolean} \| {type:"cancel"}` (the engine re-sends an updated prompt after each `act`) |
| `mulligan` | `handCardIds[], mulliganCount` | `{type:"mulliganDecision", keep:boolean}` |
| `mulliganPutBack` | `handCardIds[], cards:CardDto[], count` | `{type:"mulliganPutBackDecision", cardIds:string[]}` (exactly `count`) |
| `chooseAttackers` | `attackers:AttackerOptionDto[], attackTargets:AttackTargetDto[]` | `{type:"declareAttackers", assignments:{attackerId, targetId}[]}` |
| `chooseBlockers` | `attackers:BlockableAttackerDto[], availableBlockerIds[], error?` | `{type:"declareBlockers", assignments:{blockerId, attackerId}[]}` |
| `chooseDamageAssignmentOrder` | `attackerId, blockerIds[], blockerCards:CardDto[]` | `{type:"damageAssignmentOrderDecision", orderedBlockerIds:string[]}` |
| `chooseCombatDamageAssignment` | `attackerId, blockerIds[], defenderId?, totalDamage, attackerHasDeathtouch` | `{type:"combatDamageAssignmentDecision", assignments:{assigneeId, damage}[]}` (sums to `totalDamage`) |
| `chooseBoardTargets` | `presentation, candidates:TargetRef[], hostile, intent:TargetingIntent, minTargets, maxTargets, chosenTargets, cancellable` | `{type:"boardTargets", chosen:TargetRef[]}` |
| `gameOver` | none (`{type:"gameOver"}`) | none; terminal |

**A.5 Deck** (<https://docs.manabrew.app/protocol/deck/>)

- `Deck {name (required), version?, id?, description?, color?, format?, cards?:DeckCard[], sideboard?, attractions?, contraptions?, schemes?, planes?, commanders?, companion?, maybeboard?, draft?, labels?:DeckLabel[], customTags?, cardTags?, editor?, coverCardName?, coverCardFace?, playmatUrl?, playmatAssetId?, playmatSettings?, stackPositions?, tokens?:DeckCard[]}`
- `DeckCard {identity:DeckCardIdentity, uris:CardImageUris, imageLanguage?, allParts?, color, colorIdentity, manaCost, cmc, types, subtypes, supertypes, keywords, power, toughness, text, layout, isDoubleFaced, backFace?:CardBackFaceSummary}`
- `DeckCardIdentity {id (client instance id), name, setCode, cardNumber, oracleId?, tokenScript?, foil?}`
- `DeckLabel {name, color?}`

**A.6 Deployment facts** (<https://docs.manabrew.app/hosting-relay/>, `/self-hosting/`,
`/hosting-full-stack/`, `/formats/`)

- The relay is plain `ws://` on `:9443` with health on `:9444/health`. It uses a shared
  key (`MANABREW_SERVER_KEY`, default `forge`) and does no TLS itself.
- The node env is `SELF_HOSTED_NODE_{RELAY_URL, SERVER_KEY, ROOM_NAME, ROOM_PASSWORD,
  FORMAT(any), MAX_PLAYERS(4), MAX_GAMES(1), ENGINE_BACKEND(manabrew|forge),
  BOT_ENABLED, AUTO_START}`.
- `SIGUSR1` drains the node.
- Formats: Commander, Standard, Pioneer, Modern, Legacy, Vintage, Pauper, Brawl,
  Oathbreaker, Draft, Sealed.
- The ManaBrew engine is an in-progress Rust port of Forge and is "experimental".

## 10.1 Operator answers (2026-09-28)

- **Q1:** accepted. Phase 1 (role E) is approved and queued as MB-1 to MB-11.
  Roles N and A wait for later.
- **Q2:** the published `/protocol/` spec is CC-BY-4.0, which covers phase 1.
  For role N, the operator allows investigating by connecting to ManaBrew's live
  server and observing the wire, which avoids reading AGPL source.
- **Q3:** `-auto-mana` is already on by default on main (`cmd/gorged/main.go`:
  `fs.BoolVar(&c.autoMana, "auto-mana", true, ...)`). The adapter requires it and
  refuses ManaBrew seats when `-auto-mana=false`.
- **Q4:** accepted.
- **Q5–Q7, Q11:** the recommendations are accepted. Envelope details may be
  refined after observing engines that already speak the protocol on ManaBrew's
  server.
- **Q8:** held. No upstream contact until the operator decides.
- **Q9:** fill `CardDto.text` from Scryfall Oracle text, using gorged's existing
  Scryfall catalog (`cmd/gorged/art.go`: per-face `oracle_text` sidecar, startup
  prewarm of every deck name, 10 req/s limiter). Ticket MB-16. The rules that keep
  this out of a corner:
  1. **Presentation only.** The text never enters events, the log, replay, hashes,
     golden heads or any bot input. A different text changes no game.
  2. **A consumer-defined seam.** `internal/manabrew` declares
     `type CardText interface { Text(name string) (string, bool) }`. The translator
     takes it as a dependency; nil means empty. It never fetches, blocks or reads a
     clock, so the translator stays pure. gorged implements it over the art cache.
     Tests and golden transcripts use a fixed stub. Any later protocol or client
     can use the same seam; nothing about it is ManaBrew-specific.
  3. **Keyed only by what the redacted view already shows.** The lookup uses the
     name on the seat's `CardView` (the face name for DFC, split, adventure and
     MDFC cards). A face-down or hidden card has no name in the view, so it is
     never looked up. The text can leak nothing the name did not already.
  4. **A miss is an empty string, never a wait.** The startup prewarm covers deck
     cards. Tokens, emblems and anything else not prewarmed may miss, and a miss
     may queue a background fill through the existing limiter. A game never
     waits on Scryfall.
  5. **Oracle text, not printed text,** matching what the web client shows
     today. No Forge script text is ever used (GPL boundary).
- **Q10:** filed as a separate follow-up engine ticket.
