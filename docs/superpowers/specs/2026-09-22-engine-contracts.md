# Engine contracts (not approximations)

These are deliberate, decided behaviours of the gorge engine. None of them is
a defect and none of them is owed to a milestone. They lived in AGENTS.md's
"Known approximations" table, which is a CLOSING REGISTER of real debt; a
contract sitting in that table made the table look longer than the debt is
and invited a ticket that would have been wrong to write.

If one of these ever stops being true, change it here, in the same commit as
the code.

## R-9: the no-ask host degradation contract

A host that cannot answer a decision — the fuzzer, a test harness, a seat
that has left — must never wedge the engine. Every asking primitive
therefore carries a deterministic fallback that completes the resolution.
The fallback is the CONTRACT, not a stand-in for a missing ask: the ask
itself is real for a host that can answer. The named fallbacks are:

- `Charm` takes its first mode and records a Note
  (`effects/misc.go`, `effCharm`'s fallback).
- An exact-`Origin$ Library` `ChangeZone` takes the deterministic
  first-`Min` pick (a quantity-only search) or fails to find (a
  stated-quality search) — `effects/zone.go`
  (`effSearchLibrary`, `applyLibrarySearch`).
- `Scry`/`Surveil` keep every card on top in its existing order (Scry's
  pile B empty, Surveil's graveyard pile empty), recorded as one
  `events.LibraryOrder` — `effects/cardflow.go` (`effLookAndArrange`).
- `RearrangeTopOfLibrary` keeps the existing order (pile A = the offered
  options in offered order) — `effects/cardflow.go`
  (`effRearrangeTopOfLibrary`), `rules/arrange.go` (`handleArrange`).
- `api:TimeTravel` declines every add/remove election —
  `effects/time_travel.go` (`effTimeTravel`).
- Fixed `Choices$` and `VoteCard$` ballots take the first option —
  `effects/vote.go`.
- Any decision kind with no bot-policy arm of its own takes option 0
  through `botpolicy`'s clamp fallback (`botpolicy/policy.go`, `Decide`).
  Where that produces a BAD but legal play rather than merely a neutral one,
  it is real debt and carries a ticket; the clamp itself is the contract.

## api:TimeTravel is implemented, not approximated

Every repetition re-enumerates the affected objects (owned suspended exile
cards, then the controller's battlefield permanents with a TIME counter) in
zone order and poses one `KChoose` add/remove/skip election per object,
`Amount$` repetitions deep. Each ask rides an immutable snapshot
(`Decision.ResumeObjects`) with its cursor and repetition in `ResumeTarget`
and `ResumeRound`, so an answer that drops a counter cannot shift the next
object's cursor. `effects/time_travel.go` (each answer is applied in
place). The only stand-in is R-9 above.

## ActivationLimit$ is enforced for every shape the corpus carries

`resolveActivationLimit` (`rules/legal.go`) reports `ok=false` for a value
that is neither a literal, an SVar reference nor an inline `Count$`, and
`activationLimitReached` then does not enforce. The compiled corpus carries
no such shape, so this is unreachable. It becomes debt only if a future
corpus pin introduces one — the census would surface it.

## A Class level's granted body depends on its own primitive

`cards/kw_class.go` (`addLevelGate`) and `rules/class_level.go`
(`classBandGateHolds`) implement the level machinery. A level whose
`AddStaticAbility$`/`AddTrigger$`/`AddReplacementEffect$` names an
unimplemented mode or effect is exactly as dead as that primitive, and the
debt belongs to that primitive's own ticket, not to Class.

One decision to honour: a `ClassBand$` band must NOT be folded into
`IsPresent$`/`IsPresent2$`. The trigger gate reads those as a UNION and a
granted replacement reads no `IsPresent2$` at all, so folding silently
widens the gate.

## Regeneration

A permanent with an unused this-turn Shield replaces lethal damage and
`Destroy`/`DestroyAll` through the destruction-replacement path: consume the
shield, clear damage, tap, remove from combat. Shields expire at cleanup.
`NoRegen$` and the Effect-registered `Mode$ CantRegenerate` restriction are
honoured. `effects/regeneration.go`, `effects/zone.go`, `rules/sba.go`,
`rules/combat.go`.

## TargetingPlayer$ Opponent selection

When `TargetingPlayer$` names `Opponent` or `Player.Opponent` and at least two
opponents are alive, the ability's controller first chooses which living
opponent answers the target ask. The selected opponent then receives the
original ask; target legality and `TargetEffect` remain relative to the
ability's controller. A sole living opponent receives the ask directly, with
no redundant selection, and no living opponent fails closed to the controller
rather than leaving an unanswered decision. This rule covers cast and
activation announcement, trigger placement and resolution-sub asks, and
mid-resolution `ValidTgts$` asks (`chosenTargetsFor` and
`changeZoneChosenTargets`). The selection is transient engine flow state, not
a logged event. Trigger-relative `TargetingPlayer$` referents continue to
fail closed to the controller when their binding is absent.

## Chain targets are announced on cast (CR 601.2c)

Every target of a spell or activated ability is chosen as it is cast or
activated: the root declaration's and each `SubAbility$` link's that declares
`ValidTgts$`. The cast flow asks the root first (`targetAsk`), then each link
in chain order (`subTargetAsk`), all before payment. A link's ask is an
ordinary `KTarget` decision with `ResumeKind` `cast_sub`; it carries the
link's `TargetEffect`, routes `TargetingPlayer$` to the named chooser, and
honours `TargetUnique$` against the targets already announced.

- The answer is a targeting: one `TargetsChosen` per target with
  `Text == events.SubTargetNotice`, folded into `Object.SubTargets` (the
  root's stay in `Object.Targets`). Ward, "becomes the target" and the crime
  check match it by kind. `view.StackView.Targets` lists root targets, then
  chain targets.
- Which link chose which target is engine flow state
  (`Engine.castSubTargets`, keyed by the link's line), rebuilt by replaying
  the answers. Resolution reads it through `Ctx.SubPreAsk` and never
  re-asks; CR 608.2b prunes illegal chain targets (`recheckCastSubTargets`).
- The offer census (`targetsAvailable`) withholds a cast or activation whose
  chain has a mandatory target with no legal candidate.
- A link behind `Condition$ Kicked` / `OptionalCost` announces nothing when
  that cost is not being paid.

Not announced by the cast flow, and still asked as the link resolves (debt,
not contract; each is a later stage): ChangeZone-family links, a modal
(Charm) mode's deeper links, Fuse, copies, a trigger's chain (CR 603.3d), and
a link whose legality reads an earlier target of the same spell
(`castSubPreAskable`). An overloaded or face-down cast announces no targets
at all. `rules/cr601_subtargets_test.go`.

## The board clock's round number is exact

`view.RoundOf` folds the ordered event stream, anchored on the starting
player and tracking eliminations, so an elimination alone never moves the
round. Every log-bearing caller (host fan-out, `viewAt`, the seat view,
mtgsim, botbench, keywordbench) uses it; `roundOf` is only the snapshot
fallback. It never touches engine state. Revisit if a same-seat repeat turn
(`AddTurn`) ever lands.

## Staged hypothetical engines (`rules.NewStaged`)

`rules.NewStaged` builds a two-player engine at an arbitrary position from a
rules-level `Stage` (research harnesses only: `internal/searchbench`). Every
placement after the genesis-style object arena is an event folded by
`events.Apply`, emitted raw, so staging queues no trigger, runs no
replacement or ETB effect and writes no rules-side "this turn" ledger. A
permanent is summoning sick exactly when the Stage says so and nothing
"entered this turn". The sickness of the active player's sick permanents is
set by parking them under the other seat across the last `TurnChange`, so
they sit at the end of their controller's battlefield list. The engine is
hypothetical: its later shuffles are seeded by `Config.Seed` and it is not
replayable from `Config`. The full contract is the doc comment in
`rules/staging.go`.
