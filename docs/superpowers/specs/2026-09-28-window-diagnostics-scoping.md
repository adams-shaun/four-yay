# Window diagnostics scoping — operator decision and B1 implementation brief

Scoping deliverable for ticket `agent-20260928T045455Z` (split out of
`fb-20260928T040525Z-6c48e3f5`'s Option B). The player request was:

> add an "advanced logging" option that can detail: when autopass skips a
> phase, what conditionals were evaluated and what gamestate was sent from
> server

The client half of that (Option A: pass verdict, evaluated conditionals and a
pass-time snapshot on the autolog row) is already landed
(`89d5052a9`, `cf818c47f`, `5c26f7feb`). What Option A cannot answer is
**"why can't I cast this?"** — the client only sees the option list the server
sent; it cannot know why the engine withheld an option. That is this ticket.

## Operator decision (recorded 2026-09-28)

**Fund B1 (per-option-kind reasons). Decline B2 (full engine evaluation
trace). No staging of B2.**

Why B1:

- B1 answers the actual player question. "Why wasn't this card offered?" is
  answered by *the first gate that refused it*, stated as one token from a
  closed vocabulary — the exact shape `decision/payment_plan.go`'s
  `PaymentPlanOutcome{Reason, Detail}` already established.
- B1's redaction surface is small and provable: every reason concerns the
  **asked seat's own** cards and public state (timing, land drop, mana,
  target census, activation limits). Nothing in it reads another seat's
  hidden zones.
- B1 is bounded work: ~a dozen ordered gates in `rules/legal.go`, one
  collector, one additive decision field. It fits one round.

Why B2 is declined:

- B2 is a per-decision execution trace (every gate, in order, with the
  game-state inputs each read). Its redaction surface is fundamentally
  wider: a condition gate may read an **opponent's** hidden zone, and a trace
  that names what a gate read is a seat-privacy violation by construction
  (`view/`'s hidden-zone rules, CR 400.2). Building a seat-safe trace means
  auditing every read of every walk — months, not a round.
- The player-visible value of B2 is marginal over B1: a player who wants to
  know why a card was not offered does not want a gate-by-gate dump.
- The engine/tool-side need B2 serves already has an accepted, landed home:
  the botbench decision-trace design
  (`docs/superpowers/specs/2026-09-18-botbench-decision-trace-design.md`)
  produces exactly that artifact for **tools**, where no redaction boundary
  applies. If a deeper trace is ever needed for debugging, it belongs there,
  not on the seat wire.

## Answers to the four scoping questions (as recorded at scoping)

### 1. Where the reason is computed

Engine-side, single home. A reason collector is threaded through the offer
walks in `rules/legal.go` (`mayPlayLandIds`, `mayPlaySpellIds`, the
activation walk and its gate helpers, `targetSAAvailable` /
`targetChoiceFeasible`), invoked from the one offer seam,
`rules/turn.go` `(*Engine).askPriority` → `e.legalActions(p)`. The collector
is a fresh value per decision build; the walks record the **first** gate that
refused each candidate — gate order is the player-facing truth ("the gate
that fires FIRST is the reason a player should see").

Never client-side: the client never runs the gates, so it cannot know a
reason. Never in `events`: the collector writes no event, draws no RNG and
changes no offer, so a diagnostics-enabled game produces the **same event
stream, chain head and replay** as one without (the regression test below
pins this — same contract the botbench decision trace and
`PaymentPlanStats` already hold).

### 2. Representation

One additive, `omitempty` sidecar on `decision.Decision`
(`decision/decision.go`), following the struct's own documented contract
("byte-identical when absent") and the `PaymentFallback` /
`ManaPayment` precedents:

```go
// WindowReasons carries, when the table enabled window diagnostics, the
// FIRST withholding gate that refused each of this seat's own candidates
// the priority offer walked. Tokens only; never an event, never replay
// input; absent (nil) on every table that did not opt in, so every
// existing decision serialises byte-identically.
WindowReasons []WindowReason `json:"window_reasons,omitempty"`
```

with

```go
type WindowReason struct {
    Obj    state.ObjID `json:"obj"`            // the seat's own candidate (0 = not object-bound)
    Kind   string      `json:"kind"`           // "card" | "ability" | "land" | "activation"
    Reason string      `json:"reason"`         // closed-vocabulary token below
}
```

**Wire-path correction to the scoping brief (verified in this worktree):**
the brief proposed adding a `DecisionBody` field built at
`host/fanout.go:214` / `host/undo.go:414`. That premise is wrong and the
design is *simpler* than the brief assumed. The `TDecision` frame those two
sites build is only a `{Player, Kind, Prompt}` pointer; the offered decision
itself reaches the seat through `view.View` — `view/view.go` `project()` sets
`v.Decision = copyDecision(d)` **only when `d.Player == viewer`**, so
spectators never receive it and the seat-privacy boundary already exists. A
field on `decision.Decision` therefore flows to the asked seat through the
existing projection with zero host changes. `cmd/gentypes` regenerates
`web/src/protocol.ts` transitively (the `Decision` interface is already
generated from the View/Snapshot root), so the implementer runs
`make gentypes` and `go run ./cmd/gentypes -check` gates it. **No
`DecisionBody` change. No `host/fanout.go` or `host/undo.go` change.**

`view.copyDecision` carries the field unchanged; because the field holds
only vocabulary tokens and the seat's own object ids, the projection needs no
new redaction logic — but a view test pins that the projected decision a
non-asked viewer receives has no field (the existing `d.Player == viewer`
gate already guarantees it).

### 3. Redaction and bounding

Closed vocabulary, tokens only, never free text. Initial set (mirroring
`rules/payment_plan.go`'s fixed-token discipline):

- timing: `timing:not_main`, `timing:stack_not_empty`, `timing:not_active`
- lands: `land:drop_exhausted`, `land:play_grant_missing`
- cost: `cost:insufficient_mana`, `cost:unpayable`, plus the whitelisted
  payment-plan cost tokens (`cost:tap`, `cost:sacrifice`, `cost:life`,
  `cost:x`, `cost:phyrexian`) where the walk knows them
- targets: `target:no_legal_target`, `target:cross_constraint`
- activation: `activation:limit_reached`, `activation:condition_failed`,
  `activation:gate_failed` (sVar/monstrosity/adapt/boast),
  `activation:zone_wrong`, `activation:activator_refused`
- fail-closed unknown shapes: `ability:unsupported`

Redaction rules the implementer must enforce:

- `Obj` is set **only** for an object the asked seat can already see (its own
  hand, command zone, battlefield, graveyard, exile, or a visible stack
  object). An opponent-owned candidate never produces an entry.
- `Reason` is always a token from the table above; a walk that cannot
  classify its refusal emits `ability:unsupported` rather than inventing a
  string. No card names, no opponent state, ever — this is the seat-facing
  narrowing of `aph-plan-diagnostics.md`'s "reasons never on the wire" rule:
  payment-plan *Details* can name the acting seat's hand, so the Detail
  string is NOT carried; only the classified token is.
- Bounds: at most 24 entries per decision; token ≤ 48 bytes; entries sorted
  by `(Obj, Kind, Reason)` — never map range order.
- The `payment_plan` `Detail` vocabulary (`shape:*`) is **not** carried; a
  cast refused for a plan-shape reason emits `cost:unpayable` only.

### 4. Default off

Off by default, opt-in per table:

- additive `WindowDiagnostics bool` on `rules.Config` (the `Mulligans`
  precedent: genesis configuration, zero value unchanged, passed to replay
  identically — though here it changes no event at all),
- additive persisted `WindowDiagnostics bool json:"window_diagnostics"` on
  `host.TableConfig` (the `AutoMana` precedent, `host/table.go:124`), set at
  table creation, preserved across restart,
- host passes it into `rules.New`'s config at match construction; the engine
  records reasons only when it is set, so a non-opted engine's
  `legalActions` walk is byte-for-byte today's path with zero allocation.

A per-seat opt-in was considered and rejected: the diagnostic is computed
while building the asked seat's own decision, which is already per-seat, and
a table-level switch keeps the config surface (and the Svelte settings UI)
in the shape `AutoMana` established.

## Implementation brief (for the implementer ticket)

Scope, in order:

1. `decision/decision.go`: the two additive types above (doc comments carry
   the byte-identical-when-absent and redaction contracts).
2. `rules/window_diagnostics.go`: the collector value and its recording
   helpers; the closed-vocabulary constants live here.
3. `rules/legal.go`: thread the collector through the walks named in the
   workspace facts (`sorcerySpeed`, `mayPlayLandIds`, `mayPlaySpellIds`,
   `activationConditionOK`, `activatorAllows`, `abilityPresentHolds`,
   `sVarGateOK`, `activationLimitBlocked`, `targetSAAvailable`,
   `targetChoiceFeasible`). With a nil collector every function keeps
   today's exact behaviour; the collector-nil path must not allocate.
4. `rules/turn.go` `askPriority`: when the engine's config enables
   diagnostics, build the decision with `WindowReasons` attached; otherwise
   leave the field nil.
5. `rules/engine.go` `Config`: the additive bool; `host` (newMatch) copies it
   from `TableConfig`; `host/table.go` the additive persisted config field
   with validation normalization like `AutoMana`'s.
6. `make gentypes`; commit the regenerated `web/src/protocol.ts`.
7. Web surface (may be a follow-up slice if the round is tight): a toggle in
   the seat panel's play settings and a rendering of `window_reasons` on the
   option-less state — the Option A autolog row's expandable pattern
   (`web/src/lib/autolog.ts`) is the template. The server-side feature is
   complete and testable without the UI.

Tests (each in a NEW file; every one must be able to fail):

- `rules/window_diagnostics_test.go`:
  - a diagnostics-enabled game and a diagnostics-disabled game from the same
    seed/deck replay **byte-identically** (same event stream, same head) —
    the collector is an observer;
  - a board where a card is withheld for each vocabulary family asserts the
    exact token and the exact `Obj` (e.g. a sorcery at an opponent's turn →
    `timing:not_main`; a land with the drop spent → `land:drop_exhausted`;
    an unpayable cost → `cost:insufficient_mana`; a spell whose only legal
    target shape is absent → `target:no_legal_target`; a used-up
    activation → `activation:limit_reached`);
  - preconditions asserted before each token assertion (the card IS in hand,
    the permanent IS on the battlefield, the activation count IS spent).
- `view/window_diagnostics_view_test.go`: a non-asked viewer (opponent seat
  and spectator) receives a View whose `Decision` is nil while the asked
  seat's copy carries the field.
- Byte-identical-when-absent: `go test -run
  TestConstructedDefaultIsByteIdentical ./cmd/botbench/` (the pinned 20-game
  split must not move — a default-off observer changes no bot game).
- Wire drift: `go run ./cmd/gentypes -check` after regeneration.
- Structural: `go test ./internal/archtest/` (no allowlist edits).

Targeted commands for the implementer:

```sh
go test -run 'TestWindowDiagnostics' ./rules/ 2>&1 | tail -30
go test -run 'TestWindowDiagnostics' ./view/ 2>&1 | tail -20
go test -run TestConstructedDefaultIsByteIdentical ./cmd/botbench/ 2>&1 | tail -5
go run ./cmd/gentypes -check
go test ./internal/archtest/ 2>&1 | tail -15
```

Must not change: `events.Kind` (append-only, untouched), chain heads, the
`knownUnsupported`/param ratchets, the Known-approximations table (no row
added, none closed — this ticket adds a diagnostic, it does not close an
approximation), and every existing decision's wire bytes when the table did
not opt in.

Risks named for the implementer:

- Gate ORDER is the contract. A candidate refused by two gates reports the
  first in walk order; add a test pinning one candidate that fails both a
  timing and a cost gate.
- The activation walk offers per-ability; `Kind: "activation"` entries must
  name the ability's source object, not a synthetic id.
- `legalTargetCandidates`' limited census
  (`candidatesForLimit`) and the full census must classify identically; the
  existing `walkCacheVerify` panic guard is the model for asserting the
  collector never disagrees between the two paths.
