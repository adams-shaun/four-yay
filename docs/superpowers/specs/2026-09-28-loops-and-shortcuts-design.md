# Loops and shortcuts: a bounded, replayable execution protocol

**Status:** proposal, not an implementation. Research baseline `86a41a2f7`.
Companion: [Scam.EXE measurements](2026-09-28-scam-exe-combo-lines.md).
No production rule, protocol, event ordinal, bot, or client changes accompany this spec.

> **v1 as built (2026-09-28):** the operator chose a client-driven v1 instead:
> MTGO-style sticky choices plus "Repeat ×N" on an activation, with every
> action an ordinary intent and every opponent priority window kept. See
> [the implementation spec](2026-09-28-loop-shortcuts-implementation.md).
> §5 (negotiation/sidecar) and §6 (certificate/batch executor) below are
> deferred until ×1,000,000 counts, opponent-consented skips or tournament
> adjudication are needed.

## 1. Rules authority, checked rather than remembered

Downloaded from the [official rules page](https://magic.wizards.com/en/rules)
on 2026-09-28: [CR effective September 25, 2026](https://media.wizards.com/2026/downloads/MagicCompRules%2020260925.txt).
SHA256 of the downloaded text:
`8d860e451f20f38865b725b42d82feb714c725373dd8f3b32b8652b3eeb070ca`.
The current chapter is **732**, not the older 727/729 numbering.
Also checked **MTR 4.4**, pages 24–25 of the
[February 27, 2026 MTR PDF](https://media.wizards.com/ContentResources/WPN/MTG_MTR_2026_Feb27_EN.pdf),
the English MTR linked by [WPN](https://wpn.wizards.com/en/rules-documents)
at retrieval. The judge-site [4.4 transcription](https://blogs.magicjudges.org/rules/mtr4-4/)
agrees on the provisions below. Downloads stay in ignored research scratch.

| Authority | Obligation | gorge mapping |
|---|---|---|
| CR 732.1a–c | Mutually understood shortcuts; tournament rules take precedence in tournaments. | Version the table's rules/policy profile. Start with single-turn CR shortcuts; do not silently implement tournament-only draw rules everywhere. |
| CR 732.2a | The player with priority describes legal, predictable choices for all players, a finite count, and an endpoint where a player has priority. Nested/multiple loops and cross-turn shortcuts are permitted by the rules. Conditional action trees are not. | Proposal binds a pending `priority` decision and describes **all** passes, targets, trigger orders, costs and modal choices, not just activations. First implementation rejects nesting/cross-turn scripts explicitly. |
| CR 732.2b | Other players, in turn order after proposer, accept or shorten at a named different-choice point; they need not disclose the replacement choice yet. | A negotiation envelope, not an ordinary priority pass. Endpoint coordinates are iteration + instruction + decision occurrence + seat. Later responses may only shorten the current endpoint. |
| CR 732.2c | Execute to the final endpoint. A shortening player must make a different choice there. | Stop **before** the replaced intent; expose the normal decision with a replay-persisted forbidden-original-choice constraint. It is not permission to act in the middle of resolution. |
| CR 732.3 | In fragmented loops, active player, or first involved player in turn order, must choose differently to stop repetition. | Certified equivalence plus an involved-seat set; offer the relevant ordinary decision with the progress constraint. Do not penalize the last responder automatically. |
| CR 732.4; 104.4b/f | Mandatory-action loops draw (limited range of influence has special partial-draw rules). | Certified mandatory loop yields existing `GameOver` (`Amount=1`, `Text="mandatory loop CR 732.4"`; Player irrelevant), via Apply. Limited-range formats are out of v1 scope. An event-count watchdog is not a certificate. |
| CR 732.5–6 | Nobody must use an unrelated object to break a loop; the optional B in “A unless B” need not be performed. | Never force a removal spell or expenditure from hand to avoid a draw. Ask whether players intervene, without revealing hidden options. |
| MTR 4.4 | A single maintaining player chooses a count; others may shorten. Multiple maintainers choose counts. Partial final iterations are allowed. | Explicit maintainer metadata, but game legality remains engine-owned. Competitive profile adds the corresponding negotiation discipline. |
| MTR 4.4 | No opting out of shortcuts or disguising repetition with irrelevant changes. A shortcut loop cannot restart until a relevant change. Nondeterministic loops cannot be shortcut and must stop on relevant-state repetition. | Store a relevant-state/cycle witness and the break obligation. Randomly shuffle-until-success is not a macro. Mere fresh object IDs are not progress. |

The last two rows are **tournament policy**, not invented CR subrules. There is
no “infinity” integer in Magic. A player chooses a finite number. “Until dead”
is UI sugar only when a predictable calculation gives a finite count and legal
endpoint; otherwise offer ordinary manual play, not a conditional CR shortcut.
Emptying a library is not itself a loss (CR 104.3c: failed draw).

## 2. Separate four problems

1. **Recognition:** advisory pattern candidates, possibly wrong/incomplete.
2. **Certification:** a bounded engine proof that this script is legal from
   this boundary, predictable, and has enumerated interruption windows.
3. **Consent/execution:** seats agree on how far normal decisions are automated.
4. **Adjudication:** mandatory/fragmented-loop rules. Never infer this from a
   recognition score or watchdog panic.

Today `rules/livelock.go` panics with `*LivelockError`; it does **not** emit a
rules draw. Defaults are 400 repeating-signature events, period at most 128,
and 50,000 events without a decision/step/turn change. Signatures omit Amount
and Text. Fresh token/stack-copy mints are distinguished. `DecisionAsk` resets
the quiet counter, **not** the repeating-signature detector. Host defaults are
400,000 total intents and a recommended `MaxDecisionsPerTurn=25000`; zero for
the latter disables both that host guard and the engine watcher. Guard trips
halt/crash the hosted table. These are operational safety nets, not CR 732.

## 3. Known-combos catalog

Proposed home: `combos/data/v1/*.json`, embedded by a pure-Go `combos` package
beside `deck`, importing `cards` only. Runtime recognition consumes facts, not
engine pointers. The research seed is `rules/testdata/loop-combos.json` and the
companion's line catalog; it includes off-list primer lines deliberately.

A record has a stable ID and revision; schema version; provenance URL and
review date; piece slots (printed identity/face or compiled fact predicates),
roles (`outlet`, `engine`, `payoff`, `fuel`); zone/controller/distinctness
constraints; starting resources; ordered action templates; a cycle boundary;
symbolic net resources; stopping conditions; proof status and regression test.
Counts use checked signed 64-bit arithmetic, never floating point. Schema
migration does not change previously persisted script expansions.

Example of the intended **typed** action representation (the prototype manifest
uses human-readable actions until the compiler ticket):

```json
{
  "id": "rakdos-miner-altar", "revision": 1,
  "pieces": [
    {"slot":"outlet","name":"Phyrexian Altar","zone":"battlefield"},
    {"slot":"engine","name":"Forsaken Miner","zone":"battlefield"},
    {"slot":"crime","name":"Rakdos, the Muscle","zone":"battlefield"}
  ],
  "resources": {"mana":{},"minimum_life":1},
  "body": [
    {"op":"activate","source":"outlet","sacrifice":"engine","mana":"B"},
    {"op":"target","source":"crime","player":"opponent"},
    {"op":"pass_all","until":"miner_payment"},
    {"op":"pay","source":"engine","mana":"B"},
    {"op":"pass_all","until":"empty_stack_pilot_priority"}
  ],
  "delta": {"deaths":1,"enters":1,"storm":0,"mana":0,
             "opponent_library":"-min(1,remaining)"},
  "stop": ["requested_count","lost_piece","new_unbound_decision","game_over"]
}
```

The compiler expands `pass_all` into exact windows; it is not an unbounded
“keep passing” interpreter. Bind *roles* across zone changes using deterministic
provenance/generation checks. Token copies receive new bindings. Never persist
raw option indices as a reusable template: options change as objects enter.
Resolved intents still contain the engine's current indices and sequence IDs.

**Derived versus curated.** IR can cheaply index free sacrifice costs, return
zones, death/entry/crime triggers, printed mana values, resource producers,
cast origins, once-per-turn limits and restrictions. It cannot prove, merely
from registered API names, legal timing, Last Known Information, target
availability, energy conservation, permission duration, life sustainability,
or interactions with arbitrary replacement effects. Derivation produces
candidate slot matches; curated records supply ordering and invariants. The
Thug reproducer demonstrates why `Unsupported=[]` is not a proof.

Pin catalog digest, engine build, compiler fingerprint, corpus pin and script
compiler version in match metadata. Git-review facts and tests together. Never
commit Forge scripts, translated script dumps, or token script text. Card
names and independently written rules/combo facts are suitable catalog data;
an imported third-party combo database needs a separate license review.

## 4. Recognition, cost, and information boundaries

Compile names/predicates to dense card/fact IDs at load time. Maintain per-seat,
per-zone bitsets and dense resource summaries at decision boundaries. Index
combos by required facts, invalidate only candidates touched by an event's
zone/card/resource-interest mask. No maps, string matching, or whole-corpus
scan on the per-event hot path.

- **Live now:** all slots match distinct eligible objects; resources, priority,
  turn/phase, graveyard access and hate-piece constraints hold. Verify one
  candidate cycle on a recycled clone, including every choice window, then
  verify the declared invariant for count N. Unknown interaction => not
  certified, not “probably executable.” Dry-running once alone is insufficient.
- **One piece away:** bitset deficits of one required slot, then a resource and
  legality qualifier (“missing Priest, also need two black mana”). Suggestions
  use only that seat's view: own hand/known cards and public zones. Do not reveal
  that an opponent secretly holds a missing card or interaction. Tutoring advice
  can use the player's declared deck, never hidden library order.
- **Repetition:** bounded ring of boundary fingerprints plus collision-checked
  canonical equality. Exclude log sequence/cosmetic identities but include
  priority, pending choices, stack, zone ordering, relevant history (storm,
  crimes, sacrifices, activations, resolved-this-turn), delayed effects, RNG
  position where relevant and continuous restrictions. Private fingerprints
  stay server-side. Hash-chain heads cannot detect state repetition.
- **Symbolic cycles:** a later opt-in analyzer abstracts declared dimensions
  (mana/life/tokens/library size) and proves transitions `s -> s + delta`, with
  guards and branch thresholds. It must split at Sephiroth's fourth resolution,
  lethal damage, empty library, expired permissions, etc. Growing quantities
  alone do not establish a legal repeatable loop. No general complete solver
  is promised.

Proposed budgets, **targets not measured guarantees**: advisory matching p95
<100 microseconds and zero steady-state allocation per changed decision at
1,000 catalog entries; at most 8 candidate validations/decision; clone proof
budget 2,000 primitive intents/50,000 events, 16 MiB scratch; history ring 256
boundaries. Benchmark across 2/4/6/8 seats and 1k/10k entries. Defer proof work
out of the event emitter. Fail closed on work exhaustion without leaking which
hidden condition failed. Registry matches are hints, never legal-action lists.

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

## 7. Bot and search

Recognition consumes a redacted Board/fact view, never an engine, log, seed or
opponent hand. Keep the dependency direction: catalog facts below `rules`;
legality/certification in rules-facing helpers; seats/protocol in their own tier.
The policy chooses a finite objective count (lethal, enough mana for a planned
spell, or bounded value), then proposes the same script humans use. At an
interruption or unexpected decision, invalidate the macro and re-evaluate.

Every evaluator that follows spell/ETB/recursion dependencies needs both a
**path-local visiting bitset** keyed by card/ability/context and an absolute
budget (initial candidates: depth 32, 4,096 nodes). A revisit returns “cyclic,
unknown marginal value,” not another recursive call or infinite score. Cache
completed evaluations separately from visiting nodes; clear visits on unwind;
include target/zone/context in the key. Prefer an explicit work stack. Tests
must include self-reanimation, two-card mutual recursion, long acyclic chains
and legal repeated visits on separate branches. A Go stack overflow is not a
recoverable policy error; panic recovery is not this guard.

Search uses `(template, bindings, count, endpoint)` as a macro-action, counts
its expanded work against the rollout budget, and exposes opponent interruption
choices. Candidate counts include 1, a resource threshold, lethal threshold
and a capped exploratory value, not every integer. Terminal win/draw is evaluated
only after engine rules, not from the combo label. Hidden-world search uses the
existing hypothetical/redeal path and pooled `CloneInto`/`Release`. Adoption
still follows README's held-out bot promotion gate; discovering a combo does
not authorize silently changing the default hosted policy.

## 8. Playback and tutorial

Existing pieces: `cmd/repro -list`, `-at N`, `-emit-test`, feedback snapshots,
event-by-event divergence detection; web `Table.svelte` finished-match mode
(`/t/:table/m/:match`) loads JSON rather than a live stream and has log scrubbing.
There is no loop-specific grouping, consent display or pilot tutorial.

Add sidecar-based folded rows: “Miner loop ×20, opponent library −20,” expandable
to an iteration, then intent/target/payment/pass. Tutorial steps bind the same
instruction to the actual option and event range, explain stack ordering and
net resources, and identify the legal interruption before the chosen choice.
Show start resources, ending resources, and why continuing would stop/win.
Never show a hidden-zone card merely because the raw replay can read it.

The prototype's optional JSON logs and prose walkthroughs are the first slice.
They are **event-seeded test fixtures**, not host feedback captures: replay is
from the cloned setup boundary using the actual recorded intents. `cmd/repro`
cannot consume these raw logs alone. A follow-up needs fixture setup metadata
or, preferably, real host snapshots reached by legal pregame/play sequences.

## 9. Ordered implementation tickets

All commands below use prefix `GOMAXPROCS=4 GOMEMLIMIT=2GiB`; new test names are
**proposed acceptance targets**, not claims that those tests exist today.

| Order / ticket | Done means | Targeted test |
|---|---|---|
| L0: behavior evidence | Fix targeted top-library placement with the Thug reproducer; inventory false coverage positives, without changing combo machinery. | `GORGE_LOOP_STRICT=1 go test -p 1 ./rules -run TestLoopPrototypeThug -v` |
| L1: evaluator cycle guard | Every recursive evaluator has path and node bounds; default policy returns a legal deterministic answer on mutual recursion. | `go test -p 1 ./botpolicy -run TestEvaluationCycleBudget` |
| L2: catalog compiler | Strict versioned schema, all seed families, dense fact indices, license boundary test and deterministic compilation. | `go test -p 1 ./combos -run 'TestCatalog|TestCompileDeterministic'` |
| L3: advisory recognition | Live/one-away on redacted facts; no hidden-hand leak; bounded allocation and measured budget. | `go test -p 1 ./combos -run TestRecognize`; `go test -p 1 ./combos -run '^$' -bench BenchmarkRecognize -benchmem` |
| L4: script certificate | Flat action binder validates every choice and boundary; rejects unknown/chance-dependent cases; resource/branch thresholds checked. | `go test -p 1 ./rules -run TestShortcutCertificate` |
| L5: consent journal | Auth, stale requests, turn-order shortening, no auto-accept timeout, durable break obligation, resume/undo all tested. | `go test -p 1 ./host -run TestShortcutNegotiation` |
| L6: bounded executor | Expanded intents, deterministic pauses, idempotent requests; actual-work caps; guards never blanket-disabled. | `go test -p 1 ./host -run TestShortcutExecution` |
| L7: replay and protocol | Manual versus macro event bytes/head equal; sidecar tamper rejection; capability fallback; regenerated wire types. | `go test -p 1 ./replay -run TestShortcutReplay`; `go run -p 1 ./cmd/gentypes -check` |
| L8: human pilot UI | Offer/count/window/shorten/pause controls plus folded replay tutorial; private information redacted. | `cd web && npm test -- --run shortcut` (only in provisioned worktree) |
| L9: bot/search macros | Legal macro plans, interruption fallback, bounded evaluation, honest-world tests; bench-only before promotion. | `go test -p 1 ./seat -run TestShortcutBot`; `go test -p 1 ./internal/searchseat -run TestShortcutMacro` |
| L10: adjudication | Certified mandatory draw and fragmented choice enforcement; CR versus tournament profile explicit; no watchdog-as-draw. | `go test -p 1 ./rules -run TestCR732`; `go test -p 1 ./host -run TestTournamentLoopPolicy` |
| L11: optional acceleration study | Quantify event/object bottlenecks; propose versioned proof-carrying fast path or retain exact executor. No new event until approved. | `go test -p 1 ./rules -run '^$' -bench BenchmarkShortcutExpansion -benchmem` |

Operator decisions: tournament-policy scope; capability/decision-set strategy;
resource-cap UX; whether formal mandatory-loop certification or manual judge
adjudication is required first. General symbolic completeness and million-token
compression are explicitly not prerequisites to shipping bounded exact scripts.
