# Loop shortcut protocol, execution and replay: the deferred half of the loops design

**Status:** extracted 2026-09-29 from
[Loops and shortcuts](2026-09-28-loops-and-shortcuts-design.md) §5–§6, the
consent/execution half of that spec's four problems (§2). The client-driven
v1 ([implementation spec](2026-09-28-loop-shortcuts-implementation.md),
landing with the loops workstream) defers exactly this half until ×1,000,000
counts, opponent-consented skips or tournament adjudication are needed.
Section numbers follow the parent design doc so its ticket table (§9) and
in-flight branch notes keep resolving.

## 5. Protocol and decisions

**V1 adds no `decision.Kind`, no `events.Kind`, and no fields/reordering in
`events.Event`.** Implement negotiation in the host as versioned protocol
messages, separate from `decision.Decision`. Nonparticipating/legacy clients
retain manual play; they cannot implicitly accept on timeout. Generated TS
bindings, seats and external bots must advertise the shortcut capability.

Proposed wire messages:

- `shortcut_propose`: table/seat-bound auth, match epoch, pending sequence,
  catalog ID+revision or recorded template digest, role bindings, finite count,
  endpoint, request ID. Reject stale boundary, unowned proposer or unbounded N.
- `shortcut_offer`: redacted, expanded human-readable body; public predictable
  outputs; ordered interruption windows; current endpoint; negotiation revision.
- `shortcut_reply`: accept or shorten to a legal coordinate. Reject extending
  the endpoint and stale revisions; request next seat in turn order.
- `shortcut_progress`: completed iterations, partial cursor, intent/event range,
  public resource delta; no hidden event payloads or private state hash.
- `shortcut_pause`: transport scheduling/cancel request, not permission to
  retroactively undo accepted choices. Applied at the next unexecuted decision
  boundary, then renegotiate the remainder if necessary.
- `shortcut_end`: count/end cursor/reason (`completed`, `shortened`,
  `unexpected_decision`, `budget_pause`, `game_over`, `failed_precondition`).

Persist a versioned, canonical negotiation journal alongside match metadata:
proposal, ordered responses, accepted script digest, expanded intent range,
cursor and break obligation. Use a **separate chained sidecar**, rooted in the
match identity and event boundary; do not insert decorative Notes into the
rules chain. Crash recovery/undo atomically truncates/restores this journal
with intents. Store full operational boundary hashes only server-side; public
clients get opaque revision tokens. A shortening constraint is host control
state, not a write to `state.Game`. Its validator must be shared by submit,
clamp and bot action selection so a bot cannot resubmit a forbidden pass.

If a future embedder requires negotiation inside the engine decision API,
that is a separate operator-approved closed-set extension, **not** disguised
as a free-text `choose`. It needs explicit schemas and every seat implementation.

## 6. Execution, safety and replay

Lower accepted scripts to ordinary `Intent`s, submit on the single match
goroutine, validating before each intent. All rules state changes continue
through `events.Apply`. Server batching amortizes networking, **not** engine
work. Run every replacement, trigger, SBA, loss check and priority window.
Unexpected legal choice, hidden reveal requiring a choice, changed target,
chance-dependent branch or failed resource invariant stops before automation
of that decision. A mandatory in-resolution choice is answered only if already
specified and consented. Never take a rule snapshot mid-`Submit`.

Suggested admission/execution limits:

| Budget | Initial proposal |
|---|---|
| nesting depth | 1 (flat body); reject recursive/nested templates in v1 |
| template size | 256 instructions / 64 KiB request |
| numerical count | finite uint64 parsed with overflow checks; operational maximum 1,000,000 before cost admission |
| one batch | min(100 iterations, 2,000 expanded intents, 50,000 events) |
| host scheduling | yield after 20 ms at next decision boundary; 2 s request deadline pauses automation |
| memory | 16 MiB proof scratch, 64 MiB live batch/log-growth admission allowance; at most 10,000 new objects per batch |
| match work | existing 400,000 expanded-intent ceiling remains; cumulative object/log-byte caps apply across batches |

The wall clock only chooses scheduling pauses in `host`; it must not decide
whether a player won, a loop is mandatory, or a different intent is submitted.
A running `Submit` cannot safely be killed by a timer/goroutine. Keep deterministic
in-resolution event/object guards armed, and investigate any submit that
outlives the soft deadline. Repeated requests cannot reset match work budgets.
All budgets are negotiated deployment limits, not substitutes for Magic rules.

For an accepted, certified shortcut, track automated work separately from
unexplained per-turn decision repetition. `MaxDecisionsPerTurn` may exempt only
intents inside that exact accepted certificate, bounded by a reserved expanded
work allowance. Keep `MaxIntents`, object and memory caps on actual work, not
on the number of macro requests. Budget exhaustion **pauses** the shortcut
instead of declaring a draw. Large legal counts may be unsupported by v1;
say so before accepting them, rather than accepting a million and crashing.

Do **not** use today's `Disabled` guard as shortcut support. Later the repeating
signature watcher can consume a certificate proving progress at a known cycle
boundary; only that period's expected transitions are exempt. Unexpected
in-resolution repetition and runaway/object limits still trap. Initial v1 may
refuse a certificate whose ordinary execution trips the watcher. Reclassify a
known mandatory cycle only with a rules proof and intervention procedure, never
by catching any `LivelockError` and calling it a draw.

**Replay contract:** same initial Config and same expanded intents produce the
same event bytes and chain head as manual play. Sidecar grouping changes neither
sequence numbers nor events. Old logs continue to replay unchanged. Pin exact
expanded intents so a catalog/compiler upgrade cannot reinterpret history.
`replay.Replay` checks game semantics; a journal validator additionally checks
consent and break obligations. Feedback/resume must preserve both artifacts.

Compact summaries are view/protocol projections of verified event ranges, not
replacement events. A million-token shortcut is still expensive in v1. A later
aggregate event would be append-only, with unchanged Event layout, versioned
Apply semantics and an expansion witness; its head would be **different** from
manual execution. It cannot be called byte-identical. Defer it until a proof
handles object IDs, triggers, replacements and interruption prefixes; do not
implement `pool += N*delta` or bypass Apply now.
