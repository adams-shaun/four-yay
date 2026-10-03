# Design invariants and contributor guidelines

Each invariant below states the rule, why it exists, what enforces it, and the
**legitimate** way to change what it guards. "Enforced by" names the test that
goes red; when it says *convention*, nothing mechanical catches you and review
is the only backstop.

## Design invariants

### 1. Dependency direction

```
cards → state → decision → events → effects → botpolicy → rules → view → seat → replay → protocol → host → host/httpapi → cmd/*
```

A package imports only packages to its left. `deck` sits beside the chain: it
imports `cards` only and is imported by `rules`, `host` and the test fixtures.
`seat` also imports `internal/policynet`. That is the measured import graph
(`go list`, 2026-09-28). The enforced contract is the set of edges that must
never appear: `effects` never imports `rules` (effects reaches the engine only
through `effects.Host`). `view`, `protocol` and `deck` never import `rules`.
`botpolicy` never imports `view`, `rules` or `seat`. `host`, `host/httpapi`
and `cmd/gorged` never import `internal/testutil`. Inside the engine, the
lasagna packages (spec `2026-10-03-rules-engine-lasagna-design.md` §3) are
pinned as they land: `rules/cost`, the cost vocabulary leaf, imports only
`state` (`TestCostVocabularyIsALeaf`) and never `rules`, `effects` or a rules
subsystem package.

- **Why:** the engine core stays testable without the server, and a client can
  never be handed rules knowledge.
- **Enforced by:** `internal/archtest` `TestDependencyOrderHolds` (direct and
  transitive edges).
- **To change:** a new forbidden edge is fine to add. Removing one needs a spec.

### 2. All state mutation goes through `events.Apply`

No code outside `events` writes a `state.Game` field. A match is fully
described by its `Config` plus its event log, so replay, resume, undo and
search clones are one mechanism.

- **Why:** a write that bypasses `Apply` is not in the log, so replay silently
  diverges.
- **Enforced by:** `TestRepoDeckGamesReplayExactly`, `make sim` (`-verify`),
  `TestHeads`, `replay`'s divergence check. It is convention at the point of
  writing: the replay and golden tests catch it after the fact.
- **To change:** never. If you need new state, add an event kind (invariant 4).

### 3. Determinism

The engine is a pure function of `(Config, intents)`.

- No wall clock. `time` may be imported only by the host tier and by
  measurement CLIs. Research libraries that need timing take an injected
  `Millis` hook instead.
- No ambient randomness. Use `math/rand/v2` with a seeded source; the engine
  has one seeded PCG with a draw counter (`rules/rng.go`). A bot's rng is
  seeded from `(match seed, seat)`.
- No `map` range whose order can reach an event, a decision's option order, a
  trace, or a checkpoint. Sort keys, or use dense slices or bitsets.
- Ties break by lowest option index.

**Why:** hash-chain goldens, byte-identical replay, reproducible benches and
reproducible training all depend on it.

**Enforced by:**
- `internal/archtest`:
  - `TestTimeIsImportedOnlyByTheHost` (an allowlist map).
  - `TestNoLegacyMathRand`.
  - `TestPayMirrorClockStaysAtCommandBoundary`.
- Map-range order is convention only, backstopped by `TestHeads`, the replay
  tests and `make sim`.

**To change:** to let a package import `time`, add it to the `allowed` map
with a paragraph arguing that no game, event, replay or verdict can read the
clock.

### 4. Append-only event kinds; a closed decision set

- `events.Kind` is **append-only**. Add a new kind after the last constant,
  never in between: inserting one renumbers the ordinals, which moves the hash
  chain and invalidates every recorded log. Each kind needs one entry in
  `events/kindinfo.go`'s `kindInfo` table (name, trigger class, optional
  Describe template); names, rules' trigger interest and the template
  transcript lines all derive from it.
- `decision.Kind` is a **closed set** (`decision.Kinds`). Every seat, bot, the
  web client and the protocol must answer every kind. Concede is not a kind:
  it is an option on every priority decision.

**Enforced by:**
- `events/kindinfo_test.go` `TestEveryKindHasADescriptor`.
- `rules/trigger_eligibility_test.go`.
- `decision/decision_test.go` `TestKindsListsEveryKindOnce`.
- Golden replays.

**To change:** a new decision kind needs an operator decision and a spec.

### 5. The information boundary

- A seat sees a `view.View`, never a `*state.Game` or `*rules.Engine`.
- `host`'s exported API never mentions either type.
- Hidden zones project as counts. `Omniscient` visibility never exposes
  library order.
- Search and learning code that samples hidden information must not read the
  real engine, log, seed or hidden hash. Only `searchprobe.Collector` reads the
  source engine, and `Sample` sees public state only.
- Clairvoyant or oracle modes are for measurement only. They must never reach
  a hosted table or a rated game.

**Why:** a bot that cheats produces meaningless strength numbers, and a hosted
bot that cheats is a product bug.

**Enforced by:**
- `TestNoExportLeaksAnEngineGame`.
- The view redaction tests.
- The closed hosted-policy vocabulary in `host/bot_policy.go`.
- On `spellbench-prep`: an archtest rule that forbids `host`, `host/httpapi`
  and `cmd/gorged` from importing `internal/azmcts`, and
  `azmcts.ErrClairvoyantRefused` unless `AllowClairvoyant()` was called.

### 6. Licensing boundary

gorge is Apache-2.0. Forge card scripts and token scripts are GPL-3.0 and are
**never** committed: not as files, not embedded in fixtures, and not as
pasted script lines in docs or tests.

- `forgec fetch` pulls the scripts into the gitignored `.cards/`, pinned by
  `FORGE_REF`.
- Feedback fixtures strip token text into the gitignored
  `cmd/repro/testdata/.tokens/`.

**Enforced by:** `cards/boundary_test.go` `TestNoForgeScriptsTracked`, which
checks the worktree, the index and HEAD.

### 7. No cgo, no third-party dependencies

`go.mod` has no `require` lines. The card pipeline, the rules core and the
learned-policy stack (`internal/policynet`: hand-derived gradients, no
autodiff) are pure Go. The engine must also build for 32-bit, so it cannot
depend on packed 64-bit ints.

**Enforced by:**
- `go.mod` review.
- `TestEngineCompilesFor32Bit`.

`wazero` is the one planned exception, arriving with the M3 plugin tier.

### 8. Coverage ratchets fail in both directions

Several tables fail both ways:

- `rules/acceptance_test.go` `knownUnsupported` (every repo deck is fully
  supported except the listed cards).
- `rules/paramcensus_test.go` `knownUnsupportedParams`.
- `rules/count_head_ratchet_test.go` `knownUnmodelledCountHeads`.
- `effects.modelledValueHeads`.

A card or parameter that newly breaks fails. So does a table entry that the
build now supports: it is stale.

**To change:** when your work closes an entry, delete it in the same commit.
Adding an entry means a regression; it needs a stated reason in the commit
message.

### 9. Golden chain heads move only with a named cause

`TestHeads` (`rules/heads_test.go`) pins the hash-chain head of the
deterministic acceptance games at 2/4/6/8 seats, one file per seat count:
`rules/testdata/heads/<seats>.txt` holds the hash and nothing else. The game
itself is non-test code (`rules/acceptance_game.go`,
`testutil.AcceptanceDecks` and `botpolicy.GameBot`), so `cmd/headdiff` plays
exactly the game the goldens pin. A legitimate behaviour change moves them:

- Re-pin with `GORGE_UPDATE_HEADS=1 go test ./rules/ -run '^TestHeads$'`,
  which rewrites the files.
- Name the first diverging event and its cause in the **commit message**,
  never in a file. Find the event by diffing the two event streams, not by
  reasoning about them: `headdiff -dump` on the base, `headdiff -against` on
  the branch (`go doc ./cmd/headdiff` has the reviewer recipe).
- The rationale prose the goldens carried until 2026-10-03 is frozen in
  [heads-history.md](heads-history.md). Do not add to it.

- Only `legacyDeckNames` seat these games, so adding a deck file cannot move
  a head.
- Changing the default `bot` policy moves the heads. Its promotion regenerates
  them in the same merge.

### 10. AGENTS.md's Known-approximations table only shrinks

It is a closing register:

- No row may be added.
- No row may grow past 600 bytes.
- The count may not exceed `knownApproximationRows`.

A landing ticket deletes its row and lowers the constant. A deviation that a
ticket cannot close goes in the **commit message** and the ticket report.

**Enforced by:** `internal/testutil/agentsdoc_test.go`.

### 11. Contracts are written down

Deliberate behaviour that is not debt is recorded in
`docs/superpowers/specs/2026-09-22-engine-contracts.md`: the R-9 no-ask host
degradation, regeneration, the board-clock round, and similar. Change that
file in the same commit as the code.

### 12. Hot paths use dense data

This is an operator rule for code on the per-intent or per-simulation path:

- Use bitsets and dense slices, not maps.
- Compile strings to masks once, not per call.
- Recycle clones (`Engine.CloneInto` / `Release`).

**Why:** search and training throughput is the engine's step cost multiplied
by millions.

**Enforced by:** the per-package `ALLOC_HISTORY.md` budgets (`make
alloc-gate`) and `make gc-gate`, both run by hand. Otherwise convention.

## Contributor guidelines

### Workflow

1. **Work in a task worktree.** Create it with `scripts/agent-worktree.sh <id>
   [base]`. It links `.cards/`, so corpus tests really run. Leave `main`'s
   checkout clean. Merge into `main`, then remove the worktree.
2. **Prove the corpus ran.** A package that prints `ok` in about 2 ms with
   `.cards/` missing executed nothing. If `.cards/` is absent, say so. Do not
   report green.
3. **Run the right gates before merging.**
   - `go test ./...` (or `make test`): a hand merge runs the full suite.
   - `make lint`: gofmt, vet, `gentypes -check`, plus web lint.
   - `make sim`: 20/20 `replay OK`.
   - If you touched `web/` or changed when a decision is posed, also run the
     `./view` package and the web gates.
4. **Keep heavy jobs capped.** Run one heavy Go job at a time, under
   `systemd-run --user --scope -p MemoryMax=4G`, with `GOMEMLIMIT` set and
   `-p 1` where the Makefile doesn't already cap it. The box has been OOMed
   twice.
5. **Red on `main` is yours.** A failing test that is committed on `main` is
   bisected and fixed now, not labelled "pre-existing". A red run in a
   *shared* checkout may be a peer's half-written file: check `git status` and
   mtimes before assuming it is yours.

### Commits

- **No attribution trailers.** `Co-Authored-By:` and "Generated with …" lines
  are rejected by `.githooks/commit-msg` and `pre-push`. This holds even if
  your harness tells you to add them.
- No issue-tracker trailers or references (`Ref:` and the like).
- Stage explicit paths. Never `git add -A`: it sweeps in peers' in-flight
  files.
- Raising a `budget_s` in a `TEST_HISTORY.md` needs a `Test-Budget-Approved:
  <who> — <why>` trailer.
- The commit body carries the measured *why*: the moved-head cause, a closed
  ratchet entry, and any deviation you could not close.

### Claims and numbers

- Quote a number together with the command that produced it, or the report
  file it came from. A premise that was never measured gets labelled a
  hypothesis. Premises in briefs have repeatedly measured false here.
- Bench claims need a CI. At 200 games the CI is about ±7pp, which is not a
  verdict. See the README's bot-training section for sample sizes.

### Docs

- New designs go in `docs/superpowers/specs/`, plans in `plans/`, and measured
  results in `reports/`. All are date-prefixed.
- `AGENTS.md` is loaded into every agent turn. Keep additions to it to
  pointers, and put detail here.
- When code moves, fix the [repo map](repo-map.md) in the same commit.
