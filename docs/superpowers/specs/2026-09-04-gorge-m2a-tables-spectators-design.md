# gorge M2a — tables and spectators

Date: 2026-09-04. Status: approved in brainstorm; ready for a plan.
Parent: `2026-09-03-mtgcore-go-engine-design.md` (engine) and
`2026-09-04-gorge-post-m1-roadmap.md` (sequence). Design evidence: the UI
survey `ui-inspiration.md` (recommendations 23–33 shape the client), the
mtgplay/mtgserve interface comparison, and the ui-fix history — all three are
evidence of needs, not contracts.

## Problem

The engine plays complete 4-player games headless and replays them
byte-identically, but nobody can watch one. The next thing the project needs
is a match host that keeps tables running and a browser client that lets a
person watch several tables at once, focus on one, and rewind it — without
the engine learning anything about clocks, sockets or browsers. M2a builds
exactly that, on bot-only tables. A human at a seat is M2b.

## Goals

- `gorged` runs N tables of 4 bots on the repo decks, paced for humans, and
  restarts each table with a fresh match when one ends.
- A spectator sees an overview of every table, focuses one, and gets the
  whole board (omniscient on bot tables), the stack, the pending-trigger
  tray, a rules transcript, and card images.
- A spectator pauses their own view, steps event by event in either
  direction, scrubs by turn, and returns to live — while the match keeps
  running.
- Late joiners and reconnects are backfilled; finished matches are replayable
  after a `gorged` restart.
- The `host` package is importable by mtgserve without `gorged`.
- The Go module stays stdlib-only. The browser client holds no rules
  knowledge.

## Non-goals

- Human seats, intents, mulligans, priority UI (M2b).
- Accounts, authentication, persistence beyond files on disk (M2c). The host
  exposes an authorizer hook; `gorged` allows everyone.
- Commander (M2d). Pausing the match itself (a pace control is a later
  message type if ever wanted).
- Resume of an in-progress match across a restart (M5).
- WebSocket, protobuf, any non-stdlib server dependency.

## Decisions

Numbering continues the engine spec's D1–D8.

### D9. Host is a library plus a thin binary

`host` is an importable package: table registry, match loop, sessions,
subscriptions, persistence. `cmd/gorged` wraps it with `net/http`, flags and
the embedded web build. mtgserve imports `host` in-process at M2c; a separate
service was rejected because one binary is the project goal and the engine's
one-goroutine-per-match rule already isolates matches from each other.

### D10. Transport is SSE downstream and POST upstream (amends engine D5)

| Option | Pros | Cons |
|---|---|---|
| **SSE + POST (chosen)** | `net/http` only; `EventSource` reconnects and resends `Last-Event-ID` by itself; survives every proxy; trivially debuggable with `curl` | Two channels to correlate — solved by carrying the decision `seq` in every POST |
| WebSocket | One channel; lower latency for rapid priority round-trips | Needs a handshake and framing implementation or a dependency; reconnect and backfill are hand-rolled |

Spectating is one-directional and even the M2b player seat exchanges one
intent per decision, so the round-trip argument for WebSocket has no measured
weight yet. The envelope is transport-agnostic; a WebSocket adapter can be
added beside the SSE one without changing a frame.

### D11. Spectator pause is a client-side DVR cursor with server-side view-at-seq

The match never pauses. Each client holds a cursor into the match's event
log. The server keeps turn-start snapshots per live match and answers
"view at seq N" by replaying at most one turn of events from the nearest
snapshot (the `replay` package already does this). Rejected: client-side
reconstruction (duplicates the engine in TypeScript) and a snapshot per event
(unbounded memory on perpetual tables).

### D12. Visibility is a per-table policy

`spectator: public | omniscient`. Bot-only tables default to omniscient; any
table with a human seat defaults to public and the host may opt in. Library
order is hidden in both modes (count only): it spoils draws and teaches
nothing. `public` is exactly today's spectator redaction — a viewer with no
seat.

Amended by `own_library_list` (reward-loop spec §6): a real seat's OWN view
(Seat visibility) also carries its own library as an UNORDERED list of cards,
gated on "is this the viewer's own seat" exactly like Hand (CR 400.2). The
list is canonicalised by card name then object id, so the ORDER stays hidden —
only the contents are exposed, to that seat alone. Both spectator modes
(public and omniscient) still list no library at all.

### D13. Client is a Svelte + Vite TypeScript SPA under `web/`

Built assets are `go:embed`ed into `gorged`. Envelope types are generated
from the Go structs; CI fails if the generated file is stale. SVG overlay for
arrows. Node touches the web build only; the Go module does not change.

### D14. Tables are perpetual with derived seeds

When a match ends the table cools down and starts the next one. Match `k` on
a table uses seed `hash(tableSeed, k)`, so a table's entire history is a pure
function of its configuration. Table IDs are stable; match IDs increment.

### D15. A crashed table halts

A panic or a rejected `Apply` inside the match loop marks the match `crashed`,
writes a crash report, and halts the table. No automatic restart: the
totality rule says a reachable panic is a bug, and a self-healing table hides
it. The overview shows the halted table in red.

### D16. Pacing is the host's clock and nothing else's

The only `time` call in the whole system is the host's inter-decision sleep,
injected as a function so tests run at zero pace. No timestamp enters an
event, an option order, a view or a file the hash chain covers.

## Architecture

### Packages

| Package | Responsibility | Depends on |
|---|---|---|
| `view` (extended) | Omniscient mode; printing identity and identity token on cards; typed stack objects with labelled targets and bound parameters; deterministic one-line event descriptions | `state`, `events`, `decision`, `cards` |
| `protocol` | Versioned envelope, frame types, JSON encoding, golden fixtures | `view`, `events`, `decision` — types only, never `rules` |
| `host` | Table registry, match loop, pacing, sessions, subscriptions, snapshots, view-at-seq, persistence, crash handling, authorizer hook | `rules`, `seat`, `view`, `replay`, `events`, `protocol` |
| `cmd/gentypes` | Generates `web/src/protocol.ts` from the `protocol` structs by reflection | `protocol` |
| `cmd/gorged` | `net/http` server: SSE stream, POST endpoints, JSON GETs, embedded `web/dist`, flags | `host`, `protocol` |
| `web/` | Svelte SPA: overview, focused table, DVR, images | nothing in Go |

`effects` still never imports `rules`; `host` is the first package allowed to
import `time`.

### `host` surface (illustrative, the plan fixes names)

```go
type Visibility int // Public, Omniscient

type TableConfig struct {
    Name      string
    Seats     int
    Decks     []string       // deck file paths; rotated per match when more than Seats
    Seed      uint64
    Pace      time.Duration  // sleep after each decision; 0 = as fast as possible
    Spectator Visibility
    Perpetual bool
}

type Options struct {
    Dir       string                       // persistence root
    Sleep     func(time.Duration)          // injected; tests pass a no-op
    Authorize func(*http.Request) (Principal, error)
    Ring      int                          // frames kept per session for Last-Event-ID resume
}

type Registry struct{ /* ... */ }

func New(o Options) *Registry
func (r *Registry) AddTable(c TableConfig) (TableID, error)
func (r *Registry) Tables() []TableInfo
func (r *Registry) OpenSession(p Principal) *Session          // one per SSE connection
func (r *Registry) Subscribe(s *Session, sub Subscription) error
func (r *Registry) Unsubscribe(s *Session, t TableID) error
func (r *Registry) ViewAt(t TableID, m MatchID, seq int) (view.View, error)
func (r *Registry) Events(t TableID, m MatchID, since int) ([]events.Event, error)
func (r *Registry) Close() error                              // aborts in-progress matches
```

A `Session` owns one bounded outbound channel of `protocol.Frame` and a ring
of the last `Ring` frames it sent. Subscriptions are `{Table, Mode}` with
`Mode ∈ {overview, focus}`.

### Match loop

One goroutine per table, forever:

```
match := start(table, k)                   // seed = hash(table.Seed, k); write sidecar
for !engine.Over() {
    d := engine.Pending()
    intent := seats[d.Player].Decide(view.Project(..., d))   // bots only in M2a
    if err := engine.Submit(intent); err != nil { crash(err) }   // rejected intent = fatal
    append(match.file, newEvents)          // one JSON line per event, fsync per decision
    if turnStarted { snapshots.keep(seq, engine.Game().Clone()) }
    fanout(newEvents)                      // never blocks: full channel => drop that session
    sleep(table.Pace)
}
finish(match)                              // sidecar: result, chain head, ended
sleep(cooldown); k++                       // Perpetual; else the table goes idle
```

`crash` recovers a panic too: sidecar `crashed`, `<Dir>/crash/<table>-<k>.txt`
with chain head, seq, panic value and stack; table state `halted`; loop exits.

### Persistence

```
<Dir>/
  tables.json                       # registry: table configs and current match index
  <table>/
    <k>.events                      # append-only, one events.Event JSON per line
    <k>.json                        # sidecar: decks, seats, seed, started, ended,
                                    #   result, chain head, state ∈ {live, finished, aborted, crashed}
  crash/<table>-<k>.txt
```

On start the registry reads `tables.json`, marks any `live` sidecar `aborted`,
and starts match `k+1` on each perpetual table. Finished matches are served
read-only through the same `ViewAt` / `Events` calls, reconstructed by
`replay.ReplayTo`.

## Protocol

### Envelope

```json
{ "v": 1, "t": "event", "id": 4182, "table": "t1", "match": 7, "seq": 9130, "body": { } }
```

- `v` — protocol version; bumps only on a breaking change.
- `t` — frame type.
- `id` — session-wide monotonic frame counter; this is the SSE `id:` line, so
  `Last-Event-ID` addresses the session ring, not a match.
- `table`, `match`, `seq` — where the body sits in a match's chain; `seq` is
  the engine's event sequence (the same number the hash chain covers).
- `body` — per-type payload, generated types below.

### Frames, server → client

| `t` | When | Body |
|---|---|---|
| `hello` | first frame of a stream | `{session, tables: [TableInfo]}` |
| `widget` | every decision on a table subscribed in `overview`, coalesced to at most one per pace tick (one per 250 ms when pace is 0) | `{turn, step, active, priority, life: [], poison: [], stack_depth, last: "one-line description", state}` |
| `match_start` | a subscribed table begins match `k` | `{seats: [{name, deck, colour}], seed, spectator}` |
| `snapshot` | on `focus` subscribe, on resume when the ring cannot backfill, on `match_start` for focused tables | `{view: view.View, turn_starts: [seq]}` |
| `event` | each engine event on a focused table | `{event: redacted events.Event, line: "description"}` |
| `decision` | a seat is asked something on a focused table | `{player, kind, prompt}` — no options in M2a |
| `match_end` | a subscribed match ends | `{result, head}` |
| `table_halted` | D15 | `{reason}` |
| `overflow` | the session's channel filled; stream closes after this frame | `{dropped}` |
| `error` | a POST was accepted but failed later | `{code, message}` |

### Requests, client → server

| Method | Path | Body / query | Reply |
|---|---|---|---|
| GET | `/api/stream` | `Last-Event-ID` header honoured | SSE; `hello` first |
| GET | `/api/tables` | — | `[TableInfo]` |
| GET | `/api/tables/{t}/matches` | — | `[MatchInfo]` (sidecars) |
| GET | `/api/tables/{t}/matches/{k}/view?seq=N` | — | `view.View` at N in the table's visibility |
| GET | `/api/tables/{t}/matches/{k}/events?since=N` | — | `[{seq, event, line}]` |
| POST | `/api/subscribe` | `{session, table, mode}`; `table: "*"` with `mode: overview` subscribes to every table | 204 |
| POST | `/api/unsubscribe` | `{session, table}` | 204 |

Errors: unknown table or match 404; `seq` beyond the chain 409 with the
current head in the body; malformed JSON 400. Every reply is JSON.

### Generated types

`cmd/gentypes` walks the `protocol` package by reflection and writes
`web/src/protocol.ts` (interfaces for every frame body, `view.View` and its
parts, `events.Event`, `decision.Decision`). `make gentypes` regenerates;
`make lint` runs it and fails on a diff.

## Data flow

### Subscribe, snapshot, stream

1. The client opens `/api/stream`; the first frame is `hello` with the
   session id and the table list.
2. It POSTs `subscribe {table: "*", mode: overview}` once, and
   `subscribe {table, mode: focus}` for the one it is looking at.
3. An overview subscription yields `widget` frames. A focus subscription
   yields one `snapshot` (view at the current head in the table's visibility,
   plus the turn-start seqs so far) and then `event` and `decision` frames in
   chain order.
4. On disconnect, `EventSource` reconnects with `Last-Event-ID`. The host
   replays the session ring from that id. If the id is older than the ring or
   the session expired, the stream starts with a fresh `hello`; the client
   re-subscribes and receives fresh snapshots.

### DVR cursor

- The client keeps every `event` it has received for the focused match plus
  the `turn_starts` list. The rendered position is a cursor; live mode pins
  the cursor to the head.
- **Pause** stops the cursor; frames keep arriving and the badge shows how
  many events behind live the cursor is.
- **Step** moves the cursor by one event and fetches `view?seq=N` (cached per
  seq). The transcript highlights the event at the cursor; the board renders
  the fetched view.
- **Scrub** ticks are `turn_starts`; dragging fetches the view at the chosen
  turn start.
- **Return to live** jumps the cursor to the head and resumes rendering from
  the stream.
- Viewing a finished match is the same machinery with no live stream.

### Visibility

`view.Project` gains a mode. `Omniscient` renders every hand and every
face-down object; `Public` is today's redaction for a viewer with no seat;
the per-seat mode is unchanged. `RedactEvents` gets the same mode. Library
order is hidden in every mode.

## View additions

- `CardView.Printing {Name, Set, Number}` — `Set` and `Number` empty until a
  printing table exists (roadmap open question 1); `Token` — a short per-object
  identity token (for example `#12`) so two Goblin Guides are distinguishable
  in the stack, the log and an arrow.
- `StackView.Kind` widens to `spell | ability | trigger`; `Targets` carry a
  label (what the target is, from the ability's `ValidTgts$`); `Params` carry
  bound values (X, chosen modes) when the object has them.
- `view.Describe(g, ev) string` — one deterministic line per event, the same
  string on every replay, used by `widget.last`, `event.line` and the
  transcript. The client never composes rules text.

## Client (`web/`)

- **Routes**: `/` overview, `/t/{table}` focused table, `/t/{table}/m/{k}`
  finished match.
- **Overview** (survey 29–33): a grid of state widgets, never shrunken boards
  — each cell shows a 2×2 life grid in seat colours, a centre turn marker,
  the table name and a stack-depth badge; a shared right rail carries the
  merged, table-tagged feed of `widget.last` lines. Clicking a cell focuses
  it and the seat colours carry across.
- **Focused table** (survey 23–28): board about 70 % of the frame, four
  quadrants in seat colours; a rail about 18 % showing every revealed hand as
  a text list with mana symbols, then the stack as type-banded tiles with
  labelled targets, then the pending-trigger tray (no platform surveyed shows
  pending triggers before the stack; this is ours); identity bars about 11 %
  anchored to each seat's outer corner with life centred; a "recently
  mattered" strip showing the last resolved object large; a rules transcript
  along the bottom.
- **DVR bar**: timeline scrubber with turn ticks, step back / forward,
  live / paused indicator with the behind-live count, return-to-live.
- **Arrows**: an SVG overlay drawing target and attack relationships,
  coloured by relationship.
- **Images**: resolved by exact card name through Scryfall's named-card
  endpoint, memoised in memory and `localStorage`; a text card is the
  fallback and the only rendering when offline. Requests are spaced to stay
  under Scryfall's published rate limit.
- **State**: one store per concern — session/stream, tables, focused match
  (events + cursor), image cache. The DVR cursor is a pure reducer.

## Error handling

| Failure | Behaviour |
|---|---|
| Slow or stuck client | Its session channel (bounded, default 256) fills; the host sends `overflow` and closes the stream; the engine loop never waited. The client reconnects and is backfilled or re-snapshotted |
| Engine panic or rejected intent in a match loop | D15: sidecar `crashed`, crash file, table halted, `table_halted` frame |
| Host restart | In-progress matches `aborted`; perpetual tables start their next match; finished matches remain browsable |
| Bad request | 400 / 404 / 409 as above; the stream is unaffected |
| Scryfall unavailable | Text cards; no error surfaced beyond a muted badge |

## Testing

- **View**: leak walk in `Public` mode over random 4-seat games — no hand or
  library identity of any seat in any view or redacted event; the same walk
  in `Omniscient` — every hand visible, library still count-only.
  `Describe` is deterministic across replays.
- **View-at-seq property**: for random N, the view from nearest snapshot plus
  replay equals the view from a full replay to N (compared by hash).
- **Protocol**: golden fixtures under `protocol/testdata/`; encode/decode
  round-trip; `make gentypes` output diff-checked in `make lint`.
- **Host** (injected no-op sleep): the same configuration run twice yields
  byte-identical `.events` files and sidecar heads; a subscriber that never
  reads is dropped with `overflow` inside a bounded time while the match
  advances; an injected panic produces `crashed`, a crash file, `halted`,
  and no restart; a restart marks `live` matches `aborted` and starts `k+1`
  with the derived seed.
- **HTTP** (`httptest`): `hello` first; `focus` yields `snapshot` then
  monotonic `seq`; `Last-Event-ID` resume returns exactly the missed frames;
  an id older than the ring yields a fresh `hello`; 404 / 409 paths.
- **Client**: Vitest for the cursor reducer (pause, step, scrub, return to
  live, behind-live count) and envelope parsing; Playwright against a real
  `gorged` with two bot tables at a small pace — overview shows two widgets,
  focus shows a board, pause plus step back moves the seq badge, return to
  live resumes. `make test-e2e-web`.
- **Soak**: `gorged` with 4 perpetual tables at zero pace for 10 minutes;
  RSS bounded, no goroutine growth, every match replays.
- **Lint**: `make lint` gains `svelte-check` and eslint; blocking.
- **Totality**: unchanged — one goroutine per match; any stall or panic in a
  test is a failure.

## Done when

`gorged -decks internal/testutil/decks -tables 4 -seats 4 -pace 1.5s` runs
four perpetual bot tables. In a browser: the overview shows four live
widgets; focusing one shows the omniscient board, hands, stack, pending tray,
transcript and card images; pause, step back through a combat, scrub to the
previous turn, return to live; reload mid-match and be backfilled; restart
`gorged` and replay a finished match from the list. All tests above green;
chain heads and `make sim` unchanged; `git ls-files | grep -c '\.txt$'` is 0.

## Risks

| Risk | Mitigation |
|---|---|
| Snapshot memory on long matches | Turn-start snapshots only, dropped when the match finishes (finished matches replay from file) |
| Session rings on many clients | Bounded ring per session; sessions expire after a disconnect timeout |
| The client learns rules to render well | `Describe`, labels and params come from the server; the client is a renderer, and a test greps `web/src` for legality words |
| Scryfall rate limits or downtime | Cache, spacing, text fallback |
| `time` creeps past the host | A new constraint test walks every package's imports and fails on `time` anywhere but `host` and `cmd/gorged` |

## Out of scope, booked

- Player seat: options in `decision` frames, `POST /api/intent {seq, choices}`,
  auto-pass and stops, mulligans, "why not" for illegal actions — M2b.
- Pace control per table, human-visible clocks — later message types.
- Resume of a live match across restart — M5.
