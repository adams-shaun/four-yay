# Rules package split: compile-time slicing plan (2026-10-06)

Status: **plan + first slice landed** (`rules/scriptfacts`, commit on
`wt/cli-20261006T231747Z-a54b8662`). This plan sequences the remaining slices
as independent tickets; it does not queue them.

Related design (the wider "lasagna" refactor this serves):
`docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md` §3 (layers)
and §9 (W5 package extraction). That spec names the same L2–L7 layering; this
plan is the *compile-time* execution of its W5, measured against the tree of
2026-10-06.

## 1. Why (measured 2026-10-06)

- `rules/` is one Go package: **358 non-test files, 118,025 lines**, **1,851
  methods on `*Engine`** (one receiver). Measured at this commit with
  `ls rules/*.go | grep -v _test | wc -l` / `cat $(…) | wc -l` /
  `grep -rhE '^func \(e \*Engine\)' rules/*.go | wc -l`.
- **286 files import `rules`** outside the package
  (`grep -rl '"github.com/adams-shaun/gorge/rules"' --include='*.go' . | grep -v '^./rules/'`),
  so a `rules` rebuild rebuilds almost the whole tree.
- A cold `go build ./rules` (private empty `GOCACHE`) is **22.16 s wall / 36.75 s
  user / 689 MB peak** on this 2-vCPU box (measured §6). The shared GOCACHE
  cannot help across worktrees: each worktree's `rules` source differs.
- After the first slice, the same cold build is **18.70 s / 31.58 s / 672 MB**
  — the whole delta comes from `rules` being ~400 lines smaller, because
  `rules` still imports the leaf (§5).

File-prefix census of the root package (non-test):

```
trigger 23  replacement 21  engine 18  cast 18  legal 17  statics 9
oracle 9    resolution 8    mana 8    resolve 7  layers 7   host 7
target 5    static 5        potential 5  payment 5  unless 4  combat 4 ...
```

Already-extracted subpackages (non-test lines): `cost` 3,549 · `pay` 11,672 ·
`trigmatch` 5,479 · `chars` 1,833 · `combat` 1,776 · `resolve` 693 ·
`scriptfacts` 393 (this slice).

## 2. The dependency graph between clusters

`rules` imports only downward today (`cards → state → decision → events →
effects → rules`; `internal/archtest` `TestDependencyOrderHolds`). Inside the
package the coupling is *one receiver*: every `func (e *Engine)` can call every
other and read every embedded field. So the graph below is measured by **which
Engine surface a cluster reads**, not by Go imports.

Method: for each cluster, `grep -nE '^func'` for its entry points and the
distinct `e.<Name>` selectors it reads. A cluster is a viable leaf when its
distinct Engine surface is small enough to become an interface (the
`chars.Board` / `combat.Board` / `trigmatch.Board` pattern) or empty.

| cluster (files) | lines | distinct Engine surface | existing seam | verdict |
|---|---|---|---|---|
| printed/script facts (`printed_heads`, `foretell_cost`, `trigger_optional_permission`, `granted_keyword_trigger`, …) | ~4,000 across 54 zero-Engine files | **none** (`grep -cE '\*Engine\|\be\.'` = 0) | none needed | **leaf; slice 1 landed** |
| `layers_derived`/`layers_types`/`layers_static`/`layer5colors`/`layer6keywords` (7 files) | 3,929 | 24–107 each; reads `active()`, `matchesSpec`, CDAs | `chars.Board` (L4) | E4 second half; recursion `Derived → matchesSpec → Derived` is the blocker |
| `layers_effect` (lifecycle) + `layers.go` (`active`/`staticEffects`) | 1,888 | 227 / 85 | none | stays L7 (effect registration is engine-owned) |
| `statics*` (15 files) | ~3,300 | 9–149 each | `activeStatics` walk | viable after a statics-view vocabulary leaf |
| `mana*` + `payment*` + `cast_payment*` + `unless_payment` (ring) | ~5,800 | 145 Engine methods (spec §9.2, re-measure) | `pay.Engine` (16), `pay.Eval` (10), `chars.Reader` (11) | E7 continuation; blocked on flow + cost statics |
| `legal*` (17 files) + `potential*` (5) | ~4,200 | heavy; `legalWalk` scratch | none | **hot** (legal-walk S1–S3 live); do not open yet |
| `replacement*` (21 files) | 14,158 | `applyReplacements`/`replaceEvent` roots; `replMatch` | `replacementMatches` root | biggest cluster; needs its own `Board` (repl match vocabulary) |
| `trigger*` (23 files, the walk not the matchers) | ~9,000 | 110+ surface; `checkTriggers`, `putTriggersOnStack` | `trigmatch.Board` (matchers already out) | E3 remainder; blocked on the walk's Engine field reads |
| `sba*` (3 files) + `sbaquiet` | ~2,250 | 100+ | `sbaBoardFacts` | viable after a facts-struct split, not first |
| `oracle*` (9 files, the audit harness) | ~2,800 | 121 refs, mostly `e.emit`/`e.legalActions`/`e.Submit` | none | exported, coherent; needs a small `Board` |
| `decision_arena.go` | ~? | Engine-owned | none | **hot** (ticket 6ab47258 live); do not touch |

Evidence commands used (paste-ready):

```sh
# zero-Engine root files (the leaf pool): 54 files, 3,938 lines at this commit
for f in rules/*.go; do case "$f" in *_test.go) continue;; esac; \
  [ "$(grep -cE '\*Engine|\be\.[a-zA-Z]' "$f")" -eq 0 ] && echo "$f"; done | wc -l
# a cluster's Engine surface
grep -oE '\be\.[A-Za-z_]+' rules/statics_cost.go | sort -u
grep -nE '^func' rules/layers_effect.go
# the already-extracted seams, to copy
go doc ./rules/chars Board; go doc ./rules/combat Board
```

## 3. Interfaces needed to break cycles with `*Engine`

The rule (lasagna spec §3): **a lower layer reaches game state through a
read-only interface plus an emit function; it never holds `*rules.Engine`.**
`rules` implements the interface on a *pointer conversion* of `*Engine`, so
handing it out allocates nothing (the `chars.Board` / `combat.Board` /
`trigmatch.Board` pattern, all in `rules/*_board.go` bridge files).

Per future slice the interface is:

- **`scriptfacts`** — none (landed).
- **`oracle`** — `oracle.Board` with the ~8 reads `oracle_run.go` makes
  (`G`, `Derived`, `Keywords`, `Power`, `Toughness`, `Colors`, `Pending`,
  `Release`) plus `Emit`, `LegalActions`, `Submit`, `Advance` and the setup
  `Emit` — the exact set is `grep -oE '\be\.[A-Za-z_]+' rules/oracle_*.go |
  sort -u` (measured: 28 names, minus test-file hits).
- **statics/`replacement`** — a match-vocabulary leaf (`replMatch`, the
  `staticView` fields) plus a `Board` for the scan; the spec §9.2 names the
  static-view lowering as the precondition.
- **`layers` E4 second half** — `chars.Reader` (already declared,
  `rules/chars/reader.go`, 11 methods) plus a `chars.Eval` seam for CDA /
  `Amount$` evaluation, exactly as `pay.Eval` worked for E7.

Two mechanical rules from the landed slices:

1. **One bridge file in `rules`** aliases the moved exported names back to the
   historical unexported ones and forwards the functions (`rules/cost_vocab.go`
   for `rules/cost`; `rules/scriptfacts_bridge.go` for this slice). Call sites
   in `rules` never churn, so a slice does not edit the hot files.
2. **The param census scans subpackages into the `rules` namespace**
   (`rules/paramcensus_test.go` `sourceFilesUnder` is recursive). A bridge
   forwarder's call into a moved package must be recognised as a local edge, or
   the census' attribution roots lose the reads. The scanner already has cases
   for `trigmatch.`/`pay.`/`combat.`/`chars.` (`scanCall`,
   `rules/paramcensus_test.go`); this slice added `scriptfacts.` there.

## 4. Ordered slice list (each is one landable ticket)

Order is by (fewest Engine refs first) × (avoid the two live hot-file sets:
`rules/legal*` S1–S3 and `rules/decision_arena.go` 6ab47258).

| # | ticket title | scope (files → package) | Engine surface | notes |
|---|---|---|---|---|
| 1 | `refactor(rules): move the pure card-fact queries to rules/scriptfacts` | `printed_heads.go`, `foretell_cost.go`, `trigger_optional_permission.go`, `granted_keyword_trigger.go` → `rules/scriptfacts` + bridge | none | **LANDED this ticket** |
| 2 | `refactor(rules): move the remaining zero-Engine script/printed helpers` | the other ~50 zero-Engine root files (3,938 lines total; `card_mentions.go`, `specderived.go` after its `provGateSlot` dep moves, `present_spec.go`, `trigger_condition_hellbent.go`, `pool_probes.go`, `resume_modes.go`, `resolution_targets.go`, `intent_clone.go`, `acting_view.go`, …) → `rules/scriptfacts` (or split: `rules/query` for pure state reads) | none | one ticket per coherent group; `ability_selfskip.go` needs a `paramcensus` root key rename (its exported name) |
| 3 | `refactor(rules): extract the oracle audit harness to rules/oracle` | `oracle_{run,run_faces,snapshot,snapshot_offers,setup_state,setup_triggers,trigchain,export,can_attack}.go` (~2,800 lines) → `rules/oracle` behind `oracle.Board` | ~12 methods | exported API (`RunOracleScenarioJSON`) keeps its name via a bridge; touches `cmd/oraclediff`/`compliance` only through that name |
| 4 | `refactor(rules): lower the static-view vocabulary to a leaf` | the `staticView`/`costStaticViews` type homes → `rules/statics` (vocabulary only, no walk) | none | precondition for slices 5–6 (spec §9.2) |
| 5 | `refactor(rules): extract the cost-static scan to rules/statics` | `statics.go`, `statics_cost*.go`, `static_condition.go`, `static_gatememo.go`, `static_scan_reuse.go`, `statics_assignment.go` behind `statics.Board` | ~20 methods | reads `activeStatics`; sequence after slice 4 |
| 6 | `refactor(rules): extract the E4 layer read to chars` | `layers_derived.go`, `layers_types.go`, `layers_static.go`, `layer5colors.go`, `layer6keywords.go` behind `chars.Reader` + `chars.Eval` | existing 11 + ~8 | the recursion blocker (`Derived → matchesSpec → Derived`) is the ticket's main risk; measure it first |
| 7 | `refactor(rules): extract the sba prefilter and quiet audit to rules/sba` | `sba_prefilter.go`, `sbaquiet.go` behind an `sbaBoardFacts` accessor | ~30 methods | the SBA *sweep* (`sba.go`) stays in `rules` (it emits events) |
| 8 | `refactor(rules): extract the replacement match engine to rules/repl` | `replacement_match.go` + `replacement_replace.go` + the per-kind `replacement_*.go` behind `repl.Board` | ~40 methods | largest cluster; split into 2–3 tickets at extraction time |
| 9 | `refactor(rules): extract the legal walk to rules/legal` | `legal*.go`, `potential*.go` | heavy | **blocked until legal-walk S1–S3 and the `decision_arena.go` ticket land; sequenced, never parallel** |
| 10 | `refactor(rules): extract the trigger walk to rules/trigger` | the `trigger_*.go` walk (matchers already in `rules/trigmatch`) | heavy | after `chars` (slice 6) and the statics leaf |

**Do not reorder 9 or 10 ahead of 6/7/8**: both read `chars`, the statics view
and the replacement engine, and both are the live hot files.

### What each ticket must prove (same contract as slice 1)

- `go build ./... && go vet ./rules/...` clean.
- `TestHeads`, `TestRepoDeckGamesReplayExactly` unchanged, no golden edits.
- `TestEveryRepoDeckIsFullySupported`, `TestEveryRepoDeckParamsAreRead`,
  `TestKnownUnmodelledCountHeads` unchanged (ratchets).
- `./internal/codeshape` and `./internal/archtest` green, plus the new
  package's own tests and an `archtest` import pin
  (`Test<Leaf>ImportsStayBelowRules`) and layering-forbidden rows, landed with
  the package.
- Cold `go build ./rules` before/after with a private `GOCACHE`; report both.

## 5. The first slice (landed)

`rules/scriptfacts` (393 non-test lines + `doc.go`) holds the pure facts a
card's printed text and compiled script imply, with **no `*Engine` and no game
state**:

- `printed_heads.go` — the printed keyword-head summary the priority offer
  walk reads (`Heads`, `Of`, `Verify`; the bridge keeps rules' unexported
  `printedHeads`/`has`/`ph*` names and the `ph*` constants).
- `foretell_cost.go` — `ForetellCost` (imports `rules/cost`).
- `trigger_optional_permission.go` — `TriggerOptionalSpec`.
- `granted_keyword_trigger.go` — `GrantedTriggerHeads`, `GrantedKeywordTrigger`.

`rules/scriptfacts_bridge.go` aliases/forwards every historical rules name, so
the call sites in the hot files (`legal_walk_hand.go`, `legal_walk_alt.go`,
`legal_walk_battlefield.go`, `cast_begin.go`, `trigger_queue.go`, `stack.go`,
`trigger_granted.go`, `altcast_modes.go`, `walk_face_facts.go`) are untouched.

Import pin: `rules/scriptfacts` imports only `cards`, `state`, `rules/cost`
(`internal/archtest` `TestScriptFactsImportsStayBelowRules` + ten forbidden
rows in `TestDependencyOrderHolds`).

Byte-identity: the moved logic was copied verbatim (renamed only); the
`headsOfCodes` key list and the 22 switch arms were diffed against
`git show HEAD:rules/printed_heads.go` and are identical. `TestHeads` and
`TestRepoDeckGamesReplayExactly` pass, and all of
`rules/foretell_adjacent_test.go`, `rules/trigger_optional_permission_test.go`,
`rules/granted_keyword_trigger_test.go`, `rules/ability_selfskip_test.go`
(unchanged, still in `rules`) pass through the bridge.

**Honest ceiling of this slice (measured §6).** A leaf that `rules` imports
does not let an edit to *that leaf's behaviour* avoid recompiling `rules`:
`rules` is a dependent, so a semantic edit in `scriptfacts` rebuilds `rules`
in ~5 s — the same as editing a root `rules` file. What the slice buys today
is (a) a smaller `rules` (cold build 22.16 s → 18.70 s) and (b) a package other
subsystems can import *without* importing `rules`. Comment/declaration-only
edits in the leaf do stay cheap (0.18 s; Go keys dependents on export data).
The compile-time payoff compounds only as slices 3–8 give the leaf real
independent importers (`pay`, `chars`, `trigmatch`, the oracle harness).

## 6. Measurements (this box: 2 vCPU, 14 GB free)

Method: cold build with a private empty cache —
`/usr/bin/time -v env GOCACHE=$PWD/.ds4/scratch/gocache-<x> GOMAXPROCS=2
go build -o /dev/null ./rules`. (`/mnt/sata/gorge-training` named in the brief
is **read-only inside the task jail**; `mktemp -d -p` there fails with
`Read-only file system`, so the cache lives inside the worktree.)

| metric | before slice | after slice |
|---|---|---|
| cold `go build ./rules` wall | 22.16 s | **18.70 s** |
| cold `go build ./rules` user | 36.75 s | 31.58 s |
| cold `go build ./rules` peak RSS | 689 MB | 672 MB |
| rebuild after semantic edit in the new package | — | ~5.1 s (rebuilds `rules`) |
| rebuild after semantic edit in a root rules file | ~4.7 s | ~4.7 s |
| rebuild after a comment-only edit in the new package | — | **0.18 s** (export data unchanged) |

The ~15% cold delta is the direct effect of removing ~400 lines from `rules`;
the incremental numbers show why the remaining slices must target clusters
that *other packages* import, not just `rules` itself.

## 7. Risks

- **Hot files.** The plan's slices 9/10 must not open until the live legal-walk
  and `decision_arena.go` tickets land. Slices 1–8 were chosen to touch neither;
  slice 1 edited no hot file (bridge only).
- **Census attribution.** Every slice must teach
  `rules/paramcensus_test.go`'s `scanCall` its new package prefix (the
  `scriptfacts.` case here), or the trig/stat/API-attribution rot guard fails.
- **Ratchet tables.** `heads_test.go`, `acceptance_test.go`,
  `paramcensus_test.go`, `count_head_ratchet_test.go` must be untouched by a
  pure move; the only allowed edits are the attribution-root keys a moved
  function forces (`abilitySelfSkipTurns` → its exported name when it moves).
- **Duplicate definitions in the bridge.** A bridge that re-declares constants
  by conversion (`ph* = printedHeads(scriptfacts.*)`) keeps one source of truth;
  never re-list the values.
- **`effects` cannot move down.** No slice may make `effects` import anything
  that reaches `rules` (`TestDependencyOrderHolds`); use the existing
  `effects.Host` role interfaces.
