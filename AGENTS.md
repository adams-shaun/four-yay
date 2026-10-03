# AGENTS.md — gorge

Pure-Go Magic rules engine.

## What this is

The rules engine that will replace `mtgplay`'s XMage bridge. Card behaviour is
compiled from Forge card scripts into an IR; the engine implements the
primitives that IR references. See
`docs/superpowers/specs/2026-09-03-mtgcore-go-engine-design.md`.

## Agent guide

The detail behind this file lives in [`docs/agents/`](docs/agents/README.md),
so it stays out of every turn's context:

- [repo-map.md](docs/agents/repo-map.md): what lives where, and where each
  kind of change goes.
- [invariants.md](docs/agents/invariants.md): the design invariants, what
  enforces each one, the legitimate way to change it, and the contributor
  workflow.
- [do-not.md](docs/agents/do-not.md): mistakes that have actually happened
  here.

Bot and policy work starts at README's *Bot player training and adoption
guidelines*.

## Hard rules

- **Never commit Forge card scripts.** They are GPL-3.0; gorge is Apache-2.0.
  `forgec fetch` pulls the card corpus and token scripts into `.cards/`,
  pinned to a commit SHA (`FORGE_REF` in the Makefile), which is gitignored.
  `cards/boundary_test.go` fails the build if any are tracked.
- **No cgo, no third-party deps** in the card pipeline and rules core.
  `wazero` arrives with the plugin tier in M3.
- **All state mutation goes through `events.Apply`.** If you are writing to a
  `state.Game` field outside `events`, you are introducing a replay bug.
  `events.Kind` is append-only, and every kind M2r and M2d added was appended
  after all earlier kinds so ordinals, the hash chain and golden replays stay
  untouched: M2r appended `CastInfo`, `Choose`, `TokenCreate`, `StackCopy`,
  `Attach` and `AbilityPush`; M2d appended `ModeChosen` for mid-resolution
  modal answers.
- **No nondeterminism.** No wall clock, no ambient randomness, no `map` range
  where iteration order can reach an event.
- **This engine never imports anything from the mtgbld/mtgserve application.**

## Build / run / test

```sh
make fetch-cards          # one-time; ~25 MB, pinned commit from Card-Forge/forge
make compile-cards        # parse into the IR cache
make report               # card coverage against implemented primitives
make sim                  # build mtgsim and play 20 verified 4-seat games
make test lint
make conformance          # the focused CR conformance audit
```

`make conformance` is the focused CR 601/733 audit. I-2 (mandatory-target
feasibility), I-7 (targets before payment), and the CR 733.1 illegal-cast
reversal are fixed and asserted in the ordinary suite; the historical
`requireCR601Audit` guard and `GORGE_CR_CONFORMANCE=1` switch were removed. The
Makefile target remains explicit so reviewers can run the focused audit without
running the full suite.

## Reproduce a feedback report

A player-submitted bug report that named a table carries a replayable
snapshot (`match.json`, `log.json`, the seat's `view.json`, captured by the
server's feedback capture). Turning it into a failing test is one command:

```sh
go run ./cmd/repro <feedback-dir>              # replay, verify the head, print the board
                                             # at the report point (turn, step, life,
                                             # battlefield, stack, last ~20 log lines)
                                             # (-at 0 also works flags-after-dir:
                                             #  `repro <dir> -at 0` is parsed same as
                                             #  `repro -at 0 <dir>`)
go run ./cmd/repro -at N <feedback-dir>        # replay to intent N instead
                                             # (-list prints the intent timeline to find N)
go run ./cmd/repro -omniscient <feedback-dir>  # show every hand in the summary
go run ./cmd/repro -emit-test <pkg> <feedback-dir>
                                             # copy the snapshot into
                                             # <pkg>/testdata/feedback/<id>/ and write a
                                             # failing test skeleton (feedback.EngineAt) as
                                             # the target package's EXTERNAL test package
                                             # (package <name>_test: feedback imports the
                                             # engine tier, so an internal-package skeleton
                                             # would be an import cycle for an engine-side
                                             # target); replace the TODO with the assertion

A committed fixture's `match.json` does not embed token script text — Forge
scripts are GPL-3.0 and must never be committed — so `-emit-test` and the
fixture generator strip `match.json`'s `tokens`/`tokens_unread` and sync the
exact text into the gitignored `cmd/repro/testdata/.tokens/<id>/` keyed by
the fixture id. `feedback.Load` resolves token scripts from that sync
directory first (exact historical text) and falls back to the live corpus
(`.cards/` at the current `FORGE_REF`) when it is missing (a fresh clone), so
a fixture still replays without ever re-embedding the text into committed
files.

A capture also records the name-card universe MODE (`name_universe`) and the
exact sorted label list that match offered (`name_universe_names`), because
a universe-backed match poses `NameCard` asks the legacy path never poses and
a replay rebuilt without the mode refuses the recorded name intent. The
committed fixture keeps only the mode bit -- the list is ~24k entries -- and
`feedback.config` re-derives it from the live corpus, so a `FORGE_REF` move
can renumber a recorded name choice and shows up as the same `DIVERGED`.
```

Exit 0 is a verified replay: the rebuilt event stream matches the recording
event for event and the final head equals log.json's `head`. A non-zero exit
prints `DIVERGED` naming the first mismatching event — **a corpus change since
the report was filed is a real, expected cause** (the snapshot replays against
the corpus as it was at the report's pin), and so is an engine change; read the
first diverging event before assuming either. In a test, load the same
snapshot with `feedback.EngineAt(t, dir, n)` (`internal/testutil/feedback`;
`n < 0` = every recorded intent) and assert at the engine it returns.

The committed fixture `cmd/repro/testdata/feedback/20260914T120000Z-fb01/` is
a real capture produced by `host.SnapshotForFeedback`; regenerate it with
`REPRO_REGEN_FIXTURE=1 go test ./cmd/repro -run TestGenerateCommittedFixture`
after a corpus pin bump (`FORGE_REF`), or the fixture's own gate reports
DIVERGED exactly as a stale report would.

## Status

**The coverage ratchet.** `rules/acceptance_test.go`'s `knownUnsupported`
(Ruling P12/D2-a) names exactly the repo-deck cards the build does not fully
support. `TestEveryRepoDeckIsFullySupported` asserts that the measured gap
equals the table in both directions (Ruling R-20). A card the build newly
cannot support fails, named together with its missing primitives. A table
entry the build now supports is stale and fails too. **The test's log line is
the live count.**

Measured 2026-09-28 on `main` (`go test ./rules/ -run
'TestEveryRepoDeckIsFullySupported|TestEveryRepoDeckParamsAreRead|TestHeads$'
-v`, `make report`, `make sim`):

- **Ratchet: 3 of 1110 distinct cards unsupported.** The cards come from the
  29 deck files in `internal/testutil/decks/*.json`: 14 60-card constructed
  decks and 15 100-card Commander decks. The three are:
  - Incinerate (`stat:CantRegenerate`)
  - Vines of Vastwood (`stat:CantTarget`)
  - Ojer Axonil, Deepest Might (`count:NonCombatDamageThisTurn`)
- **Param census: 23 of 1110.** `TestEveryRepoDeckParamsAreRead`
  (`rules/paramcensus_test.go`) is the companion ratchet over the same decks'
  parameters. Its `knownUnsupportedParams` table holds 23 cards that carry an
  unread param or an unmodelled cost token: 20 distinct param labels and 1
  cost label.
- **Golden games.** The 12 pinned Legacy decks (`legacyDeckNames`, which
  deliberately excludes the Commander decks) round-robin across 2/4/6/8 seats
  (`TestRepoDecksPlayAtEverySeatCount`). They replay byte-identically
  (`TestRepoDeckGamesReplayExactly`), and `TestHeads` pins their chain heads
  in `rules/heads_test.go` `acceptanceHeads`. That map is the authority. At
  this date:

  | seats | 2 | 4 | 6 | 8 |
  |---|---|---|---|---|
  | chain head | `a991478b3edb213c` | `d4876057e0477830` | `b4a5d33f302e6bfc` | `b73492c5ccdaadca` |

- **`make sim`:** 20/20 `replay OK` over 20 verified 4-seat games.
- **Corpus coverage** at the pin `95f04e8a04c8925fa97cb226fc3341cabcc90a53`
  (`FORGE_REF`): `make report` prints `cards: 33667  playable: 31005 (92.1%)`
  and `tokens: 839`, matching the README's CI-generated coverage block. Since
  fuzz-cov3 (2026-09-24) the gate also counts the `count:<head>` value heads
  that a card's SVars read (`effects.modelledValueHeads`, held to the
  evaluator by `TestValueHeadRegistryMatchesEvaluator`). `repl:Untap` is out
  of scope and remains unsupported.

These figures go stale with every merge. Re-measure them before quoting;
don't copy them.

A seat's clients must be able to answer every `decision.Kind`, and the set is
closed: the M1 kinds (`priority`, `target`, `attackers`, `blockers`,
`trigger_order`, `trigger_optional`), M2r's `choose` (an {X} value, delve
exiles, cost sacrifices, "as it enters" name/type/number, miracle-style
yes/no), the two M2d closures -- `mulligan`, the London keep/mulligan
and bottoming round `Config.Mulligans` runs between the deal and turn 1, and
`modes`, the modal pick and unless-pay ask a mid-resolution answer serves
(the `ModeChosen` event carries the answer into the log) -- and the four
kinds the later card work added: `commander_zone` (a commander's OWNER's
CR 903.9 command-zone replacement choice), `replacement` (the CR 616.1
order choice over competing replacement effects) and `arrange` (the ordered
subset ask `Scry`/`Surveil`/`RearrangeTopOfLibrary` share, Ruling J0), and
`starting_player` (CR 103.1's second half: the toss winner's choice of who
takes the first turn, posed by `NewStartingPlayerChoice`/`AskStartingPlayer`
with the pregame rounds deferred until it resolves -- plain `New` is the
deterministic R-9 no-host fallback and never offers it).
Concede (M2d-3) is not a kind: it is a `concede` option on every priority
decision that emits the existing `PlayerLost` with Text "conceded". The
engine-side defaults that still stand in for a choice the engine cannot yet
ask are listed under **Known approximations** below.

Acceptance commands:

```sh
go test ./rules/ -run 'TestEveryRepoDeck|TestRepoDecks|TestRepoDeckGames' -v
make sim
```

## Known approximations

Each row is one place a registered primitive defaults instead of asking, or
behaves more narrowly than the card text, with the stand-in's location and
the milestone that removes it.

**This table is a CLOSING REGISTER, frozen at this commit. No row may ever be
added, and no row may be grown.** It started at 13 rows as an honest audit of
where "supported" was not the whole truth; it then became a place every
landed ticket wrote a paragraph, reached 264 KB on 2026-09-22 across ~100
rows, and was loaded into every agent's context on every turn. Every
remaining row now has a P1 ticket whose only job is to delete it.

So:

- **A landing ticket DELETES its row** and lowers the
  `knownApproximationRows` constant in `internal/testutil/agentsdoc_test.go`
  by the number of rows it deleted. `TestKnownApproximationsOnlyShrinks`
  fails the build on a row added, a row grown past 600 bytes, or a count
  above the constant.
- **A deviation a ticket cannot close goes in the COMMIT MESSAGE and the
  ticket report — never here.** If it needs tracking, the implementer says so
  plainly in the report and the operator files a ticket for it.
- **A reviewer treats any added or grown row as a MAJOR finding.**

Behaviour that is a deliberate contract rather than debt — the R-9 no-ask
host degradation contract, regeneration, the exact round number — is in
[`docs/superpowers/specs/2026-09-22-engine-contracts.md`](docs/superpowers/specs/2026-09-22-engine-contracts.md),
not here.

| Stand-in | Where | Removed by |
|---|---|---|
| `RestrictValid$` is carried into the mana pool; only dotted `Spell.<filter>`/`Activated.<filter>` alternatives supported by the matcher are enforced. Unsupported alternatives fail closed, including bare `Spell`/`Activated` (13 raw lines), `CostContainsX`, `CumulativeUpkeep`, `CantCast*`, `Static.*` and `nonSpell`; 41 raw `RestrictValid$` lines across 39 corpus files contain at least one such term, so mixed alternatives can lose only the unsupported branch. | `effects/misc.go` (`effMana`), `rules/stack.go` (`restrictValidMatches`) | M4 (the remaining restriction grammar) |
| `Effect` registers real continuous effects only for `CantTarget`, `CantRegenerate`, the cost-modifier modes, and `Triggers$ BecomeMonarch` (Palace Jailer). Every other `StaticAbilities$`/`Triggers$` mode stays a bare Note. An absent `Duration$` now correctly expires at cleanup for every source kind, and `UntilYourNextTurn` ends as that turn begins rather than a full turn too long; explicit exotic `Duration$` forms remain incomplete. | `effects/misc.go` (`effEffect`, `effectUntilEOT`, `IsNextTurnDuration`, `IsUntilYourNextTurn`), `rules/layers.go` (`restrictionApplies`, `AddContinuous`, `EndOfTurnCleanup`), `state/continuous.go` | M4 (remaining Triggers$ modes, full Duration$ grammar, the other Effect-delivered grant keywords) |
| An explicit multi-zone `ChangeZone Origin$` including `Hand` has no origin-aware chooser: the exact-hand and exact-library walkers cannot build one private option list across hand, graveyard, library and battlefield, so a source-default picker emits one replay-visible Note and moves nothing loudly. | `effects/zone.go` (`mixedOriginIncludesHand`, `effChangeZone`) | M4 (origin-aware mixed hidden-zone chooser) |
| (cloak1) Turn-face-up (CR 708.6) is not implemented: a manifested or cloaked card stays face down forever. Loud-unimplemented: `ManifestDread` (a different API), `RememberManifested$`, `Defined$`-object manifests, the `Choices$` asking forms; for Cloak, the from-hand chooser and `Defined$ ValidLibrary`. Ordinary filters read the printed face, missing a manifested Elf. | `effects/zone.go` (`effManifest`, `effCloak`), `rules/layers.go` (`typeCharacteristics`, `faceDownPrintedHides`), `effects/filter.go` (`faceDown`) | M4 (turn-face-up CR 708.6 for both -- the Morph/Megamorph/Disguise ticket owns the shared path; ManifestDread; RememberManifested$) |
| (copyperm-grants) `api:CopyPermanent` now grants named `AddTriggers$`/`AddSVars$`/`AddAbilities$` from the resolving source table, supports a named `AttachedTo$` destination and the exact Zndrsplt `Choices$`/`Chooser$` shape. `WithDifferentNames$` remains loud-unimplemented, while `ImprintTokens$ True` still records no copies for delayed exile/sacrifice (Kharasha Foothills, Shredder, Shadow Master); `TokenRemembered$` is real. | `effects/copypermanent.go` (`effCopyPermanent`, unread-parameter Note block) | M4 (`WithDifferentNames$`, `ImprintTokens$`) |
| (combatrestriction1) Only whitelisted static shapes are enforced; an unwhitelisted parameter means the static is SKIPPED, not enforced blanket — the deliberate permissive direction. `MustAttack$ ChosenPlayer`/`RememberedPlayer` name their defender for real (Territorial Hellkite's ChoosePlayer riders, via `attackRequirementSet`); every OTHER `MustAttack$` player reference stays unread and fails closed, and a NAMED duty whose defender no offered pair reaches releases the creature entirely (CR 508.1d: attacking anyone else obeys zero requirements too), so it is not marked Required. Unread: CantAttack's UnlessDefender$/Condition$/CheckSVar$ family, CantSacrifice's `ForCost$ True`, a `Target$` list's walker half, `stat:MustBlock`. | `rules/layers.go` (`SacrificeBlocked`, `attackBlocked`), `rules/combat.go` (`mustAttackRequired`, `attackRequirements`, `attackDutyDischargeable`), `effects/choose_control.go` (`effChoosePlayer`), `effects/misc.go` (`effEffect`) | M4 (the conditional parameter grammar; ForCost$ True cost provenance; stat:MustBlock; the walker Target$ half) |
| (ap1) AddPhase loud-degrades (one Note, grant still applied) on an unresolvable `ExtraPhase$`/`AfterPhase$`/`FollowedBy$`/`NumPhases$`/DelTrig value or a multi-step set. Deliberate: a grant whose splice point has passed is dropped silently at TurnChange; a splice mid-range of another active extra phase is untested; a multi-completion takes the LAST entry's resume point. `FirstAttack$` unread. | `effects/addphase.go` (`effAddPhase`, `parseExtraPhaseValue`), `rules/turn.go` (`extraPhaseBoundary`) | M4 (a real per-turn attack history for the unread `FirstAttack$` gate) |
| (kw:MayFlashSac) The keyword is implemented rules-side, not as a `cards/keywords.go` expansion: its meaning is a casting OPTION, which `expandKeywords`' own doc lists as the family rules reads directly, and the sibling `kw:MayFlashCost` landed the same way. The rider ("if you CAST it any time a sorcery couldn't have been cast") rides a pay-time `FlagMayFlashSac` CastInfo, and `state.CastProvenanceFlags` strips that bit from a stack copy, since a copy is put on the stack and never cast (CR 707.10). Deliberately NOT changed with it: the sibling entry-hook bits `FlagEvoked`/`FlagDashed`/`FlagWarped` are still inherited by a copy, because they are conditioned on an alternative COST having been paid — a cast-time choice the copy rules do carry for the comparable kicked case. | `rules/mayflashsac.go`, `rules/statics.go` (`spellTimingOK`), `rules/altcast.go` (`altCostEnter`), `state/object.go` (`CastProvenanceFlags`), `events/apply.go` (`StackCopy`) | M4 (a copy ruling for the evoke/dash/warp entry hooks) |


## Trigger-relative filter arguments (pg2)

`ControlledBy <ref>` and `OwnedBy <ref>` recognise exactly `TriggeredTarget`,
`TriggeredDefendingPlayer`, `TriggeredPlayer` and `TriggeredCard`, plus a
`Spawner> <known-inner-ref>` chain in that argument position (resolved
against the same riding TriggerContext); an absent binding fails closed
(also under `!`); the `Targeted*` family stays unknown, as does every
`Spawner>` chain outside that argument position. `effects.TriggerContext` carries event roles separately from the
resolving ability's source, targets and Remembered, survives suspension,
cloning and stack copying, and is rebuilt by replay without new events. Not
implemented: `TargetingPlayer$` (Magus of the Abyss asks the trigger
controller), LKI owner/control snapshots for a referent that changes before
resolution (live owner/controller is read), carrying these bindings into a
registered continuous effect, and the Self/Other target-source normalisation
(Flickerwisp's `Permanent.Other`).

## Cast-provenance filter arguments (castprov1/2/3 + wascastfrom)

The `Card.wasCast*` family is a rules-side split, not a filter predicate:
`castProvenanceAdmits` (`rules/cast_provenance.go`) strips the tokens and reads
the log (latest PutOnStack cast wins; a copy was never cast; a never-cast card
reads false). Hand family: `wasCastFromYourHandByYou`, `wasCastByYou`,
`wasCastFromYourHand`. Origin-zone family: `wasCastFromExile`,
`wasCastFromYourGraveyard` and `...ByYou` (identical here: a cast's origin zone
is always the caster's), `wasCastFromTheirHand`. Bare `wasCastFromGraveyard`
is the effects-side CastFlags predicate (flashback/harmonize/escape) instead.
Wired at the trigger match walks, `Count$ThisTurnCast_<spec>`,
`Count$wasCastFromExile`, the target walks (offer and CR 608.2b recheck) and
the CantBeCast walk (`castOriginAdmitsAtZone` reads the pending cast's
origin). Still open: the tokens are not evaluated by effects' own
ConditionPresent/ConditionDefined evaluator (such a gate runs its sub
unconditionally), there is no `Count$wasCastFromYourGraveyard` head, and the
may-play provenance predicates (`MayPlaySource`/`CastSa`) stay fail-closed
(see the ValidLKI row).

## Host behaviour notes (embedder observer hooks, D15)

`OnBurst` errors crash the match like a persist failure (D15): the table
halts and the chain does not continue. `OnMatchEnd` errors are discarded
because the outcome is already recorded and an error cannot un-record it, so
an embedder that persists through `OnMatchEnd` must handle its own
persistence failures inside the callback.

## Seat-privacy boundary

A seat credential names **both its table and seat**. Every current
seat-authorised HTTP route (`view`, `events`, `pending`, `intent`, `undo`)
goes through `host/httpapi`'s `claimForTable`; a mismatched or legacy unbound
claim is 403 and must never reach registry state. `cmd/gorged` mints startup
and vs-bot tokens with that pair, so after deployment old tokens stop working;
the web client returns a rejected stale join to the lobby, where it obtains a
fresh game claim. Stream session ids are random 128-bit values issued only in
`hello`; `host.Session.serial`, not the public id, preserves fan-out order.

## Running a gorged server while you work

Two orchestrator sessions share this box, and one of them serves a live demo
that the operator redeploys by hand (`make deploy-demo`; a redeploy aborts
every in-flight vs-bot game, so nothing does it automatically). So ports are
allocated, not first-come:

| range | who |
|---|---|
| 8080-8081 | the demo. **Never bind these**, and never run `make deploy-demo` |
| 8082-8089 | the bot-policy / botbench workstream |
| 8090-8099 | task agents on the engine side — pick one of these |

Put persistence under `/tmp/gorge-<something-unique>`, never the repo-root
default `gorged-data`: several servers sharing one persistence directory
silently corrupt each other, and a resumed directory written by an older binary
comes back with the fields that binary lacked set to their zero values
(`Format`'s zero is `constructed`, a real value, so `-format` is ignored with
nothing in the output to say so).

**Stopping a server: never `pkill -f` or a bare `pgrep -f`.** The pattern is
matched against every process's `/proc/<pid>/cmdline` including your own shell's,
and it has killed a session here. Find the server by its listening socket
(`ss -lptn`), confirm the pid, then signal that pid. Note that `/proc/<pid>/comm`
is the BINARY's name -- if you built `gorged-after`, its `comm` is
`gorged-after`, not `gorged` -- so match on the port rather than the name.

`scripts/fleet.sh ports` prints the current allocation, and `scripts/fleet.sh
port` prints a free one in your range.

## Working in a task worktree

**Every change is made in its own worktree, and `main`'s checkout is left
clean.** This is not only for dispatched seats -- it applies to an operator
session's own hand work too, including a one-line fix. Merge into `main`, then
remove the worktree immediately: a stale worktree is what makes the next
agent's `git grep` and `find` return duplicate hits from a sibling checkout.

The reason is that several sessions share the one `/home/sadams/projects/gorge`
checkout, and a working tree has exactly ONE HEAD, so anything done to it
happens to everyone:

- `git checkout -b` in the main checkout to commit your own work switches the
  shared tree. A peer switching back to `main` then reverts your edited files
  out from under you mid-task.
- `git reset` and branch switches are worse than dirty files: they move HEAD
  for the other session too, so a peer's next commit lands on a base it never
  chose.
- A peer mid-refactor leaves the shared tree not compiling. That has produced
  19 build failures in `go test ./...` belonging to nobody in the room. **In a
  shared checkout a red suite is not evidence about your own change**: check
  `git status` for untracked or modified files you did not write, and their
  mtimes -- a file seconds old is a peer mid-edit, not a defect to fix, and not
  yours to fix. (Red that is genuinely committed on `main` is still yours to
  bisect and fix in the same turn.)

So, in the main checkout: never `git checkout`, `git switch`, `git reset` or
bare `git stash`, and never `git checkout` inside another worktree either (it
discards that seat's uncommitted work). Stage explicit paths and never
`git add -A`, which sweeps up a peer's in-flight files.

Task worktrees are created with `scripts/agent-worktree.sh <id> [base] [--web]`,
never with a bare `git worktree add`. A worktree carries only tracked files, and
this repo needs one untracked thing to test honestly: the `.cards` corpus. Without
it `internal/testutil`'s `CorpusRegistry` calls `t.Skip`, so every
corpus-dependent test SKIPS instead of running, the package still prints `ok` --
in about 2ms -- and the run reads green to you, to the review pre-filter and to
the gate while having executed almost nothing. If you find yourself in a worktree
with no `.cards`, stop and say so rather than reporting a green suite.

Which agent seats exist, what they cost and when to escalate between them is
recorded in `docs/superpowers/agent-seats.md`.

## Hot files: a branch that touches one of these is sequenced, not parallel

Measured by `scripts/reward_collect.py hotspots --repo .`: one merge in four
(199 of 802 tickets, 7d) needed a resolver round, and one ticket burned 48 of
them. The cause is always the same shape — two or three live branches editing
the same file, each rebasing onto the other's landing. If your brief touches a
file below, keep the change as small as the brief allows; prefer adding a new
file next to it over growing it.

**This table is generated; do not hand-edit it.** Regenerate with:
`python3 scripts/reward_collect.py hotspots --repo . --format md` (add
`--max-rows N` for this embedded cut). The durable "why it collides" prose
lives in `scripts/hotfiles-notes.json` (tracked): a seat filing a new
contended pair adds one note entry there in its own commit and re-renders.
Rendered 2026-10-01 at merge of main@0b368ba80 into 078f1bb3d, with `--max-rows 5`:
| file | why it collides |
|---|---|
| `docs/agents/repo-map.md` | the per-package map table: every workstream that adds or re-describes a package appends a row here, so the file collides by construction while a docs-heavy workstream is live. The 2026-10-01 set (eng-board tip a27a125db, enginecmp tip 6db84d459, sbrep-fast tip 683b3cf69) is NOT a resolver round: an in-memory `git merge-tree --write-tree` of every pairwise tip AUTO-MERGES docs/agents/repo-map.md clean, and the file is one markdown table with no seam a split could cut — a main-side split would only fragment the agent-facing package map and move the very lines a live branch is rebasing onto. eng-board's whole change to this file (the `cards` and `.cards/` rows) is a STRICT SUBSET of sbrep-fast's (the same two rows plus the `rules` row and the `searchbench` / `searchbench/statespec` rows), i.e. duplicate work carried from the shared ancestor wt/perf-tip (e43cde989), whose blob at that merge-base is eng-board's blob BYTE-IDENTICALLY (599e565c6); enginecmp adds the unrelated one-row `enginebench` entry. The script's carrier pass keeps eng-board because it is a carrier against sbrep-fast but not against enginecmp, and dropping it would still leave the two real editors, so no script fix removes this row — the resolution is this note plus sequencing. SEQUENCE: land sbrep-fast first (it already contains eng-board's repo-map rows), then enginecmp (its one-row append rebases trivially), then eng-board (whose file change is then already present and drops out); the branches carry no ticket file, so no Depends-On chain is expressible and the ordering is a controller action. A durable package-map seam is filed as a follow-up so the next docs-heavy workstream does not re-collide. Re-briefed after cli-20261001T042757Z-fcbaaa35; dedupe against that id. |
| `view/card.go` | the card projection: every projection-perf ticket and every exposed-card-fact ticket edits cardViews/cardView. The 2026-10-01 pair is a GENUINE intra-function collision — cpu-redeal rewrites cardViews/cardView into a pooled projector method while libcomp inserts the printed CardName assignment inside the old cardView body (main line 250) — so the second branch to rebase re-applies its hunk in the new shape; no file split can separate edits inside one function. Land libcomp first (its other four files merge clean on rebase); cpu-redeal's rewrite folds the 5-line assignment in as it goes |
| `botpolicy/combat.go` | the Board refill: combat.go's BoardFromGameInto fills the fields policy.go's Board declares, so every own-state exposure ticket extends one function and one struct and the edits are adjacent by construction — no file split can separate them. The 2026-10-01 pair (cpu-redeal's shared-manifest read, OwnDeckShared, at combat.go lines 187-194 vs libcomp's fillOwnLibrary insert and its OwnLibrary field) is line-disjoint and merges clean on rebase; only the per-file hotspot granularity flags it. Keep each exposure to its own field + fill lines RECURRED: the cpu workstream and libcomp were briefed again as cli-20261001T024615Z-caad3047 after cli-20261001T020456Z-751ccaf0 (merged, commit e6ab2d08d) already recorded this exact pair; dedupe against that id. The cpu head's name rotates (cpu-redeal / cpu-emit / cpu-maps); the note is keyed by FILE and applies to whichever cpu head is the live representative, so a new cpu head is not a new collision. |
| `botpolicy/policy.go` | the Board refill: combat.go's BoardFromGameInto fills the fields policy.go's Board declares, so every own-state exposure ticket extends one function and one struct and the edits are adjacent by construction — no file split can separate them. The 2026-10-01 pair (cpu-redeal's shared-manifest read, OwnDeckShared, at combat.go lines 187-194 vs libcomp's fillOwnLibrary insert and its OwnLibrary field) is line-disjoint and merges clean on rebase; only the per-file hotspot granularity flags it. Keep each exposure to its own field + fill lines RECURRED: the cpu workstream and libcomp were briefed again as cli-20261001T024615Z-caad3047 after cli-20261001T020456Z-751ccaf0 (merged, commit e6ab2d08d) already recorded this exact pair; dedupe against that id. The cpu head's name rotates (cpu-redeal / cpu-emit / cpu-maps); the note is keyed by FILE and applies to whichever cpu head is the live representative, so a new cpu head is not a new collision. |
| `cmd/botbench/azcost.go` | the az policy flag block: every az workstream ticket registers a flag in registerAZFlags or rewords one in azFrontDoor — one function, so edits are adjacent by construction. The 2026-10-01 pair (cpu-redeal adds -az-node-cache; libcomp rewords -az-corpus-features' help and error) is line-disjoint and merges clean on rebase. Append new flags to the registration list; no split is honest inside one flag block RECURRED: re-briefed as cli-20261001T024615Z-caad3047 after cli-20261001T020456Z-751ccaf0 (merged, commit e6ab2d08d) recorded it; dedupe against that id. The cpu head's name rotates; the note is keyed by FILE and covers whichever cpu head is live. |
… and 11 more; run the command for the live list

Durable notes for files not shown above:
- `rules/cast_commit.go`, `rules/clone.go`, `rules/testdata/oracle/activated-ability/known-divergent.json`, `rules/testdata/oracle/attack-triggers/known-divergent.json`, `rules/trigger_queue.go` — re-measured 2026-10-03 against main@2d4e01009: wt/fdn-fix5 and wt/fdn-fix6 share five changed paths (exact intersection of their main-relative path sets). An in-memory `git merge-tree --write-tree wt/fdn-fix5 wt/fdn-fix6` CONFLICTS ONLY in the two oracle `known-divergent.json` maps; `rules/cast_commit.go`, `rules/clone.go` and `rules/trigger_queue.go` AUTO-MERGE clean and are flagged only by the per-file hotspot granularity. Each JSON conflict is both branches deleting rows for a DIFFERENT card from the same per-family aggregate; the durable per-card seam is filed as agent-20261003T005756Z-8a3e98ba and deferred until the live audit branches land. clone.go's two edits are independent additions inside the same `Engine.cloneWith` deep-copy operation, and cast_commit.go/trigger_queue.go are cohesive shared modules: no honest split while both branches are live. Neither branch has a `.ds4/issues` ticket, so no Depends-On chain is expressible: sequence manually, landing fdn-fix5 before rebasing and landing fdn-fix6. Dedupe against cli-20261003T000415Z-792b1e7f and cli-20261002T234430Z-55d9239e.
- `rules/cast.go` — 12515 lines and the entry point for every cast-side ticket
- `rules/layers.go`, `rules/statics.go` — every continuous-effect and keyword ticket lands here
- `view/view.go` — every projection change; two ManaBrew/UI tickets collided on it
- `rules/loops_prototype_test.go`, `rules/testdata/loop-combos.json`, `rules/testdata/scam-exe.json` — the loops/shortcuts workstream has three branches in them at once
- `AGENTS.md`, `.superpowers/ds4/gorge-context.md` — every ticket wants to add a paragraph; the Known-approximations table is append-only-by-deletion and a rebase resurrects deleted rows
- `rules/acceptance_test.go`, `rules/paramcensus_test.go`, `rules/heads_test.go`, `rules/count_head_ratchet_test.go` — the ratchet tables: a merge with main newly enforces them, so edit only the entries your own change moves
- `scripts/deploy-demo.sh` — the operator demo's one deploy procedure: every layout ticket (bot policy, ports, tables, pace, humans) and every deploy-mechanics fix (port sweep, art fill, health wait) lands here, and a layout change spans knobs and start/stop lines, so the knob/mechanics seam cannot separate two editors; 9 commits in the 3 days to 2026-09-30, and 2 live branches held it the same day (measured by `scripts/reward_collect.py hotspots`)
- `internal/botobs/checklist.json`, `web/src/protocol.ts` — the obs-axis fact-exposure workstream: every "expose a view fact" ticket flips one entry in the checklist and regenerates the wire twin. Neither can be split — `protocol.ts` is single generated output (`cmd/gentypes -o`, byte-identical or `make lint`/`cmd/gentypes` tests fail), and `checklist.json` is one authoritative `facts` list that `scripts/reward_collect.py:collect_obs` sums and the ratchet checks probe-for-fact, so per-fact files would fragment the obs denominator and rewire the reward axis. A fact ticket edits only its own entry's `where`/`exposed` and never reorders or reformats the array
- `cmd/botbench/main.go` — the bench harness's run entry points (`run`, `runMatrixTraced`, `main`): every measuring arm lands here — corpus opening, az/spellbench/GC/pprof flag setup, the deck matrix. The 2026-10-01 collision (cpu-derived vs sbrep-fix) was the SAME subset-corpus-loader patch (identical git patch-id) carried by both live branches with a divergent tail hunk (`applyGCFlags()` add vs `startPprof()` removal) — duplicate work, not a seam, so there was nothing to split
- `internal/searchprobe/action.go` — the Collector's action-to-key mapping: CPU-perf rewrites of the observation path and searchbench action-key work land in ADJACENT NON-OVERLAPPING functions (cpu-maps extends `IntentActions`, sbrep-fast extends `action`); the hunks stay line-disjoint and an in-memory `git merge-tree --write-tree` of the current tips (cpu-maps 248f9829b, sbrep-fast 683b3cf69, merge-base wt/perf-tip e43cde989) AUTO-MERGES this file clean, so the per-file hotspot granularity flags a resolver round that the merge does not cost. Keep each change to its own function so it stays that way. The cpu head's name rotates (cpu-redeal / cpu-emit / cpu-maps); the note is keyed by FILE and covers whichever cpu head is live. Re-briefed as cli-20261001T034731Z-88d00a48 at the 6-file set (rules/potential_kinds_test.go joined); dedupe against that id and against cli-20261001T030655Z-da1dccdc.
- `rules/genesis.go` — engine construction: the CPU-perf workstream pools engine internals through `Spare` (`Release`/adopt in `newWithRNG`) and the searchbench workstream carves staged-construction entry points out of the same `newWithRNG` — different concerns inside one function, so no file split is honest while either branch is live (a split moves the exact lines the other is editing). Keep construction changes small and land the branches in sequence
- `internal/azmcts/candidates.go` — candidate enumeration plumbing: the CPU-perf workstream threads a reusable board scratch (`boardScratch`/`enumerateWhyInto`/`worthOptionsInto`) and the searchbench workstream threads payment candidates (`enumerateWhyAutoPayment`/`enumerateCut`/`enumerateLimit`) through the SAME function — both rewrite `enumerateWhyInto`'s head in place with a different wrapper chain and both edit its body, an intra-function collision no file split can separate; it is one of only TWO files of the pair that conflict in the tips' in-memory merge. SEQUENCE, and land cpu-maps FIRST: it contains cpu-redeal and wt/perf-tip, so sbrep-fast's rebase replays only its own searchbench changes and resolves inside enumerateWhyInto once; do not restructure the enumeration head while either is live. Re-measured 2026-10-01 at the current live tips (cpu-maps 248f9829b, sbrep-fast 683b3cf69, merge-base wt/perf-tip e43cde989): an in-memory `git merge-tree --write-tree` of the two tips CONFLICTS ONLY in internal/azmcts/candidates.go and rules/potential_kinds_test.go; the other four files auto-merge clean. The cpu head's name rotates (cpu-redeal / cpu-emit / cpu-maps; cpu-maps CONTAINS cpu-redeal 65ec357ee, so this is the same workstream, not a new collision); the note is keyed by FILE and applies to whichever cpu head is the live representative. Re-briefed as cli-20261001T034731Z-88d00a48 at the 6-file set (rules/potential_kinds_test.go joined); dedupe against that id and against cli-20261001T030655Z-da1dccdc.
- `internal/searchprobe/observation.go` — the observation capture path: the CPU-perf workstream edits the import block, the `Collector` struct and `rollback`, while the searchbench workstream's footprint is `Clone` and `Capture` — adjacent, line-disjoint, and an in-memory `git merge-tree --write-tree` of the current tips (cpu-maps 248f9829b, sbrep-fast 683b3cf69) AUTO-MERGES it clean; only the per-file hotspot granularity flags it. Keep searchbench additions to self-contained accessors at stable anchors so it stays that way. The cpu head's name rotates (cpu-redeal / cpu-emit / cpu-maps); the note is keyed by FILE and covers whichever cpu head is live. Re-briefed as cli-20261001T034731Z-88d00a48 at the 6-file set (rules/potential_kinds_test.go joined); dedupe against that id and against cli-20261001T030655Z-da1dccdc.
- `deck/deck.go` — carries two genuinely unrelated concerns: File (parse/resolve/commander eligibility, the corpus side) and Manifest (ManifestRow/manifestRows, the own-deck side). The 2026-10-01 collision (cpu-redeal's File.CardNames vs libcomp's ManifestRow land flag) is fully line-disjoint and merges clean — only the per-file hotspot granularity flags it. A deck/manifest.go split is the real seam but was deliberately NOT made while either branch is live: a main-side split cannot change a live branch's measured footprint (git diff main...ref is merge-base..ref) and would move exactly the lines libcomp is about to rebase onto, manufacturing the resolver round it is meant to avoid. Split only after both branches land, if manifest work recurs RECURRED: re-briefed as cli-20261001T024615Z-caad3047 after cli-20261001T020456Z-751ccaf0 (merged, commit e6ab2d08d) recorded it and filed the deferred split as agent-20261001T020717Z-6650338b (human_needed, gated on the branches landing). Dedupe against those ids; the cpu head's name rotates, the note is keyed by FILE.
- `internal/azmcts/nodecache.go`, `internal/azmcts/nodecache_test.go`, `internal/searchprobe/redeal.go` — measured ONE-SIDED against the pair's true merge-base wt/perf-tip (e43cde989; tips 65ec357ee cpu-redeal / 7ce4e10a4 sbrep-fast, 2026-10-01): cpu-redeal carries perf-tip's nodecache.go/nodecache_test.go blob byte-identically (blob hashes equal; its only live editor is sbrep-fast's fixed-chance PIMC resume, commit 7ce4e10a4), and sbrep-fast carries perf-tip's redeal.go byte-identically (its only live editor is cpu-redeal's Spare-pooled Deal/NewRedealer rewrite). A branch that only carries an ancestor's blob cannot conflict with the file's editor, so both files merge clean in EITHER landing order. NO ACTION NEEDED: the carrier pass landed as 1f7135180 (merge ea5fc05d7) drops a pure carrier of the surviving editors' common ancestor, so reward_collect.py no longer lists these three files as contention on main; this note records only WHY they are one-sided. The cpu head's name rotates (cpu-redeal / cpu-emit / cpu-maps); the note is keyed by FILE and covers whichever cpu head is live. Land cpu-redeal first (perf-tip + three perf commits) so sbrep-fast's rebase replays only its own searchbench changes. Re-briefed as cli-20261001T034731Z-88d00a48 at the 6-file set (rules/potential_kinds_test.go joined); dedupe against that id and against cli-20261001T030655Z-da1dccdc.
- `internal/searchprobe/canon.go`, `internal/searchprobe/canon_test.go` — the canonical encoder: NEITHER FILE IS IN MAIN YET — both were added inside the workstream after main forked, so a main-side split (the durable canon_key.go seam for the action-key comparison half) is impossible until the workstream lands; defer it as agent-20261001T020717Z-6650338b defers the deck/manifest.go split. At the current tips (cpu-maps 248f9829b, sbrep-fast 683b3cf69) cpu adds the stdlib unsafe import, widens canonPlan/buildCanonPlan (p.size) and appends the in-place encodeAt reader block after canonEncoder.encode, while sbrep adds module imports and appends AppendActionsKey/CompareActionsJSON/ParseActionsKey/keyReader after the encoder plus its tests at canon_test.go's EOF; both files AUTO-MERGE CLEAN in an in-memory `git merge-tree --write-tree` of the tips, the only adjacency being the import block. Keep each side's additions to its own appended block; after both branches land, split canon_key.go if canon work recurs. Re-briefed as cli-20261001T034731Z-88d00a48 at the 6-file set (rules/potential_kinds_test.go joined); dedupe against that id and against cli-20261001T030655Z-da1dccdc.
- `rules/potential_kinds_test.go` — the legal-walk vocabulary ratchet: this test walks rules/legal*.go with go/ast to find the *Engine method that is legalActionsPriced's shared entry body, by NAME. The CPU-perf line renamed that method (legalActionsWalkAsk -> legalActionsWalkWithWindow) and this ratchet hardcodes whichever name it expects, so BOTH branches edit the SAME lookup block: it is the second (and only other) file that conflicts in an in-memory `git merge-tree --write-tree` of the current tips (cpu-maps 248f9829b renames the expected name, sbrep-fast accepts either name at 683b3cf69). This is NOT two independent concerns a file split can separate — it is one rename of one symbol that both workstreams track. The durable fix (filed as a follow-up) is to make the ratchet NAME-AGNOSTIC: discover the entry by following legalActionsPriced's delegation chain to the method whose body is the walk section source, rather than matching a hardcoded `legalActionsWalkAsk` / `legalActionsWalkWithWindow` string. Until then, SEQUENCE: land cpu-maps FIRST (it contains the rename), then sbrep-fast's rebase takes the renamed name and drops its either-name fallback. Re-briefed as cli-20261001T034731Z-88d00a48 at the 6-file set (rules/potential_kinds_test.go joined); dedupe against that id and against cli-20261001T030655Z-da1dccdc.
- `rules/replacement_life.go` — the CR 614.1 life-gain replacement machinery: the CPU-perf workstream's pointer pass (`for _, ce := range e.active()` -> index-and-pointer in `lifeReplacementCandidates`, commit `841e37d1c`) and a feature ticket's new registered-restriction walk (`lifeGainForbidden`'s second `range e.active()` loop reading Effect-delivered CantGainLife through `restrictionPlayerSpecMatches`, landed as `76a807b8a`) are ADJACENT functions in this 390-line module, both rewriting `active()` iteration — no file split separates edits inside one function, and the file is already the focused output of the 15-way `replacement.go` split. The 2026-10-01 pair is line-disjoint and merges clean on rebase; keep each change to its own loop and land the branches in sequence (the feature ticket landed first; the perf pointer pass folds the new loop in as it goes)
- `internal/azmcts/env.go`, `internal/azmcts/options.go`, `internal/azmcts/search.go`, `internal/azmcts/tree.go` — the azmcts search engine's environment/options/entry/simulate files are measured ONE-SIDED between the CPU-perf workstream and the searchbench branch against their true merge-base (wt/perf-tip base e43cde989, 2026-10-01): every perf-workstream edit to these four files predates e43cde989, so the perf head carries that base's blob byte-identically and the ONLY live editor since the base is the searchbench branch (env.go maps roots onto decision keys via matchRoot/priorNet/Plies/Turn in newEngineEnv/Play/advance; options.go adds DiscountUnit/ParseDiscountUnit/BenchCandidateLimit/Stats.MeanLeaf*; search.go rewrites Search for RootRow/Result.RootTable; tree.go adds the backup discount PathEnv/pathClock/commit to simulate). NO ACTION NEEDED: the carrier pass landed as 1f7135180 (merge ea5fc05d7) drops a pure carrier of the surviving editors' common ancestor, so reward_collect.py already omits these four files; this note records only WHY they are one-sided, superseding the earlier intra-struct measurement taken against e43cde989 mislabelled as the perf head. The one file where a real collision remains in the group is internal/azmcts/candidates.go, already recorded separately.
- `rules/engine_layercache.go`, `rules/layercache.go`, `rules/layers.go`, `rules/trigger_zoneskip.go` — measured 2026-10-01 across the live branches that reach these four files: the CPU-perf workstream (heads cpu-emit tip e8920a7b0 and cpu-maps tip 1e629118b, which the tool's own _integrates reads as MUTUALLY MERGING and therefore collapses to one representative) and the searchbench branch sbrep-fast (tip 68690fb83). Against the pair's true merge-base wt/perf-tip (e43cde989) sbrep-fast's blob for every one of engine_layercache.go / layercache.go / layers.go / trigger_zoneskip.go is perf-tip's fork blob BYTE-IDENTICALLY (0376f4765 / 5669d7e7b / 810aff613 / 938363d54), so sbrep-fast edits none of them and only carries perf-tip's content; cpu-maps likewise still carries the fork blob, and the perf workstream's sole editor of these four is cpu-emit (blobs 8ffee95e2 / a81f12faa / 4ae0fed26 / 2f6dd1e94, +8/+97/+5/+100 lines over the fork). Only one DISTINCT content therefore reaches these four files across the live set, which is why reward_collect.py hotspots no longer lists them: a carried blob cannot conflict with the editor, so either landing order is clean. The fifth file the brief named, rules/genesis.go, is the one the pair still shares as a listed hot spot and already has its own note here (the CPU-perf Spare recycling vs the searchbench newWithRNG/newEngineShell/genesisDeal carve-out, hunks line-disjoint: cpu-emit old lines 54-102, sbrep-fast old lines 136-235). The tool's ancestry pass cannot drop a carrier because perf-tip itself has no worktree. Sequencing: land the CPU-perf workstream first, then rebase sbrep-fast onto it replaying only its own searchbench changes; the branches carry no ticket id, so no Depends-On chain is expressible — sequencing is a controller action and this seat records it
- `AGENTS.md`, `scripts/hotfiles-notes.json` — the hot-file notes workstream's own two output files: every 'record a hot file' seat appends one entry to the same one authoritative JSON list (scripts/reward_collect.py:load_hotfiles_notes reads exactly this one file) and mirrors the same bullet into AGENTS.md's durable-notes block, so two live notes seats collide on BOTH files by construction. The 2026-10-01 pair resolved itself before this ticket dispatched: cli-20261001T020456Z-d30942a9 MERGED into main as 2fd35b887 (the rules/replacement_life.go note), and cli-20261001T020456Z-f497b076 was PARKED superseded (a91b4bf2f) after cli-20261001T024615Z-caad3047 (e6ab2d08d) re-landed its five notes on main — and one of its premises was stale (rules/nameuniverse_readers_test.go IS on main; its four azmcts files are main's one-sided four-file entry). NO HONEST SPLIT: the JSON is ONE list with ONE renderer feeding the ONE generated AGENTS.md embed (the same single-output argument recorded for internal/botobs/checklist.json + web/src/protocol.ts), and AGENTS.md's durable-notes section is one prose block every notes seat appends to; per-pair or per-seat note files would fragment the renderer's single input and rewire the embed pipeline for zero conflict reduction. OPERATING RULE: land notes seats in sequence (this ticket's own Depends-On chain is the mechanism); keep each change to append-only entries at the JSON tail plus one mirrored durable-notes bullet; and RE-MEASURE hotspots and re-verify a note's premises against current main BEFORE writing it — a stale premise is what minted the superseded f497b076 branch. The register copy of this table (.superpowers/ds4/gorge-context.md, git-excluded, read-only from a seat jail) is the controller's to re-render; a seat records only in this tracked file. This ticket itself landed as a record-only change; hotspots() lists no path for the briefed branch set. Dedupe against cli-20261001T022457Z-f9dc4b92.
- `rules/cast_commit.go`, `rules/testdata/oracle/spell-conditional/known-divergent.json`, `state/object.go` — measured 2026-10-02 against main for the briefed cr601-subtargets / fdn-fix2 branches: cr601-subtargets changes all three paths; fdn-fix2 changes only the spell-conditional known-divergent fixture. There are no issue files to express a Depends-On chain, so the controller must sequence those branch landings. NO HONEST SPLIT: cast_commit.go is part of the cohesive cast-commit legality flow; known-divergent.json is one oracle fixture document and splitting it would fragment its schema; state/object.go owns the central Object representation, not an independent concern these two branches can be separated into. Re-measured on the current main snapshot, hotspots no longer lists state/object.go or this old branch pair; cast_commit.go and the fixture remain contended by newer live branches (cr601-ship/fdn-fix5/fdn-fix6 and cr601-ship/reflex-ship respectively), so sequence those landings too rather than moving code or dividing the fixture.
- `cmd/botbench/spellbench.go` — the spellbench orchestration file: every bench-arm ticket lands its call-sites here. The per-arm logic already lives in its own new file (`ilgate.go` for the il-* cast gate, `azvariant.go` for the az variants), but sbResult's fields, registerSpellbenchFlags' flag line, sbPlay's Setup/stats loops and spellbenchExit's load/report call sequence are one integration flow, so two arms' additions are adjacent by construction. Measured 2026-10-02 for ticket cli-20261002T194322Z-c325a34c (branches wt/il-enc tip 851ffb06e and the mz workstream (mzrepro tip 077f93821, whose live worktree head wt/mzplay tip 08d9341ef carries the same spellbench.go blob cd5c9caf9; mzrepro is an ancestor of wt/mzplay, so the tool drops mzrepro as a stacked editor and lists only wt/mzplay), main 2d4e01009): an in-memory `git merge-tree --write-tree` of the two tips CONFLICTS ONLY in this file (three hunks — the -il-gate-checkpoint vs -spellbench-first-game-only flag lines, sbResult's gate/gated vs vcorpus/vvisits fields, and sbPlay's il-gate SetPlanner/Setup vs azVariant recorders); `cmd/botbench/main.go`, `spellbench_registry.go` and `internal/azmcts/candidates.go` AUTO-MERGE clean. NO HONEST SPLIT: the remaining edits are call-sites inside sbPlay and spellbenchExit, which both arms must edit in sequence, and a main-side carve-out would move exactly the lines both branches rebase onto and manufacture the resolver round it is meant to avoid (the deck/deck.go reasoning). Land the arms in sequence (the branches carry no ticket file, so no Depends-On chain is expressible — sequencing is a controller action), and keep each arm's spellbench.go change to its own flag line, its own sbResult fields and its own call inside the shared flow; put everything else in the arm's own file.
- `effects/registry.go`, `effects/trigger_referents.go`, `rules/resolution.go`, `rules/stack.go`, `rules/testdata/oracle/spell-conditional/known-divergent.json` — measured 2026-10-03 against main@bc7f0c94a for wt/fdn-fix8 (tip ec03eec1d) and wt/reflex-ship (tip fce77711d). The pair's exact main-relative intersection is these five paths, not the three a brief named. fdn-fix8 adds the layer-derived keyword/colour context and last-known P/T/LKI plumbing plus a suspended-resolution priority reset; reflex-ship adds CR 603.12 reflexive-trigger Host/TriggerContext plumbing bound during resumed resolution. These are separate behaviours that converge on the same public effects Host/Ctx interface, the TriggerContext/SpecContext field-copy and the single 926-line rules/resumeResolution. NO HONEST SPLIT while the branches are live: a main-side move cannot remove a live branch's already-committed footprint (`git diff main...ref` is merge-base..ref) and would move exactly the lines both branches rebase onto. The pair's `git merge-tree --write-tree` exits 0 and auto-merges every shared path — the contention is per-file granularity, not a conflicting edit; `rules/stack.go` and the spell-conditional fixture are also contended by wider sets and have their own notes. Neither branch has an issue file for a Depends-On chain, so the controller must sequence the landings manually (land fdn-fix8 first — it is the one conflicting with main — then rebase reflex-ship). The durable seam (carve reflexive-trigger and layer-table/LKI context into their own files once both branches land) is filed as a follow-up; this note records the rationale and does not hide the contention.
