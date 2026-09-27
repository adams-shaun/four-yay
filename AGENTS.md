# AGENTS.md — gorge

Pure-Go Magic rules engine.

## What this is

The rules engine that will replace `mtgplay`'s XMage bridge. Card behaviour is
compiled from Forge card scripts into an IR; the engine implements the
primitives that IR references. See
`docs/superpowers/specs/2026-09-03-mtgcore-go-engine-design.md`.

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

**M2r closed the coverage ratchet.** `rules/acceptance_test.go`'s
`knownUnsupported` (Ruling P12/D2-a) names exactly the cards the measured
deck set does not fully support, and
`TestEveryRepoDeckIsFullySupported` asserts the measured gap set equals the
table in both directions (Ruling R-20): a card the build newly cannot fully
support fails and is named together with the primitives it is missing, and a
table entry the build now supports is stale and fails too. Measured
2026-09-17 at the avengers-assemble Commander import, the ratchet stands at
**3 of 791** -- every one of the 791 distinct cards across the repo deck
files (`internal/testutil/decks/*.json`: 14 60-card constructed decks plus
10 Commander decks) is fully supported and plays except the three the table
holds (Avengers Quinjet's crew, Captain Marvel's mirror-counter trigger,
Speed's pay-on-trigger -- the Marvel Super Heroes Commander import's own
remaining backlog). The 12 pinned Legacy decks (`legacyDeckNames`)
round-robin across 2/4/6/8 seats
(`TestRepoDecksPlayAtEverySeatCount`), replay byte-identically
(`TestRepoDeckGamesReplayExactly`), and `TestHeads` pins the chain heads as
goldens in `rules/heads_test.go`:

| seats | 2 | 4 | 6 | 8 |
|---|---|---|---|---|
| chain head | `3ddcc4e3ba5bb799` | `b3bf75f0c56512ae` | `a93593d452866261` | `beac5729b0e63dcc` |

`TestEveryRepoDeckParamsAreRead` (`rules/paramcensus_test.go`) is the
companion ratchet over the same decks' parameters: measured at the same
date, its `knownUnsupportedParams` table holds 25 repo-deck cards carrying
an unread param or unmodelled cost token (19 of them from the
avengers-assemble import; the desc-only `ValidTgtsDesc` family the new
deck's cards carry several of).

`make sim` plays 20 verified 4-seat games from the same seed set, every one
replaying byte-identically (20/20 `replay OK`).

Measured at the corpus pin `master @
95f04e8a04c8925fa97cb226fc3341cabcc90a53` (`FORGE_REF` in the Makefile):
`make report` prints `cards: 33667  playable: 29820 (88.6%)` with `tokens:
839` (re-measured 2026-09-24 at fuzz-cov3, which made the gate count the
`count:<head>` value heads a card's referenced SVars read -- 30050 before it;
`effects.modelledValueHeads` lists the modelled heads and rules'
`TestValueHeadRegistryMatchesEvaluator` holds that list to the evaluator. The
2026-09-17 figure was 24727/73.4%; the jump from the 2026-09-14 figure of
21108/62.7% is the parameter-read registrations merged since -- the
param-census task wave -- and the pw1 figure 20635/61.3% predated api:Untap,
api:ManaReflected, stat:ManaConvert, stat:UntapOtherPlayer and
kw:Cumulative upkeep registering. `repl:Untap` is out of scope and remains
unsupported. The registered primitive set is measured by `make report`.
M1's 37/8/8/8/1 = 62 was the count before M2r registered the keyword,
trigger, static and mid-game primitives the ratchet's card work needed.)

A seat's clients must be able to answer every `decision.Kind`, and the set is
closed: the M1 kinds (`priority`, `target`, `attackers`, `blockers`,
`trigger_order`, `trigger_optional`), M2r's `choose` (an {X} value, delve
exiles, cost sacrifices, "as it enters" name/type/number, miracle-style
yes/no), the two M2d closures -- `mulligan`, the London keep/mulligan
and bottoming round `Config.Mulligans` runs between the deal and turn 1, and
`modes`, the modal pick and unless-pay ask a mid-resolution answer serves
(the `ModeChosen` event carries the answer into the log) -- and the three
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
`TriggeredDefendingPlayer`, `TriggeredPlayer` and `TriggeredCard`; an absent
binding fails closed (also under `!`); `Targeted*` and `Spawner>...` chains
stay unknown. `effects.TriggerContext` carries event roles separately from the
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
