# Mana-member carry (legal-walk §S4) — design

Date: 2026-10-07
Status: design approved, plan pending
Parent spec: [`2026-10-06-legal-walk-design.md`](2026-10-06-legal-walk-design.md) §S4
Worktree: `.worktrees/s4-census` (base `8c1bb84a9`)

## 1. Goal and metric

Cut per-object mana-ability membership recomputation on the **live priority
walk**, for the random and bot enginebench rows. Success metric is
`-row random` / `-row bot` **games/s** measured without any census hook.

This is a pure-read optimisation. It changes no rules contract, no option, and
no event.

## 2. Why the landed record path is not enough

The parent spec (`2026-10-06-legal-walk-design.md:412-431`) describes the
carry as reusing "the list `ownManaMembers` already records for PotentialMana
(`rules/walk_block_reuse.go:120-140`)". That list is only built when a walk is
*recording*: `own := w.rec != nil && z == ZBattlefield && zonePlayer == p`
(`rules/legal_walk_battlefield.go:66`), and `w.rec` is armed only at
`rules/legal.go:176` when
`forAsk && hyp == nil && !castsOnly && e.potentialFullDemand && e.WalkRecDemand`.

A throwaway census on the real enginebench rows (POC, since deleted) measured:

| run | decisions | gate calls | gate/dec | loop own | loop else | rec-armed walks | ownManaMembers |
|---|---|---|---|---|---|---|---|
| random A | 1,164,779 | 3,782,521 | 3.25 | 0 | 3,114,104 | 0 | 0 |
| random B | 1,151,206 | 6,348,688 | 5.51 | 0 | 5,698,850 | 0 | 0 |
| bot A (manual) | 788,927 | 2,277,792 | 2.89 | 0 | 2,048,858 | 0 | 0 |
| bot A `-autopay` | 493,539 | 3,199,065 | 6.48 | 0 | 1,429,252 | 0 | 0 |

The record path is **never armed** on these rows:

- `potentialFullDemand` is set only by `potentialWalkOf(p, true)`
  (`rules/potential_walk_cache.go:160`), whose only callers are the seat's
  view projection `PotentialActions` (`rules/potential.go:409`) and
  `PotentialPaymentPlans` (`rules/potential_plan.go:101`). enginebench
  random/bot never project.
- `WalkRecDemand` is set only by `paymentActionsForPriority`
  (`rules/payment_plan.go:466`), reached via host `EnsurePaymentActions`
  (`rules/engine_host.go:74`) only for seats that `WantsPaymentActions`
  (`internal/bench/bench.go:249`). The random row is inline; the bot row
  defaults to manual (`-autopay` off). `-autopay` on still leaves `rec` at 0,
  isolating `potentialFullDemand` as the sole blocker.

Therefore the carry must **populate membership from the priority walk itself**,
independent of `w.rec` and of the potential readers. That is the core change
this design makes relative to the parent spec's "reuse the recorded list".

## 3. Component and boundaries

- **New file `rules/mana_member_carry.go`.** One new Engine cluster
  `manaMemberCarry`, tagged `clone:"reset"`. It is a dense `ObjID`-indexed
  entry slice, a per-object touch slice and one board stamp.
- **Two operations:** `manaMemberLookup(id) ([]*cards.SA, bool)` and
  `manaMemberStore(id, all []*cards.SA)`.
- **One call site:** the mana loop in `rules/legal_walk_battlefield.go`
  (the only edit to a hot file).
- **Owns:** deferred-payability membership and its validity stamp.
- **Does not own:** payability (the caller re-applies it), and the existing
  `w.rec` potential record, which is kept in sync and not replaced.

## 4. Data structures and key

```go
type manaMemberEntry struct {
    objTouch uint64       // object's touch generation at write
    all      []*cards.SA  // deferred-payability membership; backing reused
    n        int32        // live length
    set      bool
}

type manaMemberCarry struct {
    owner   *Engine
    entries []manaMemberEntry
    touch   []uint64  // per-object generation, dense by id-1
    // board stamp at the last write
    stamp    manaMemberBoardStamp
    stampSet bool
}

type manaMemberBoardStamp struct {
    lineage           *events.Log // log pointer identity (Clone has its own)
    derivedSeq        uint64
    staticTouchGen    uint64
    crossWalkRetires  uint64
    continuousVersion int
    tapeEpoch         uint64 // restore rewinds state under the same log
    objs              int    // object-arena length
    turn              int32
}
```

- Indexed by `id-1`; `entries` and `touch` grow like `walkObjCls`
  (`rules/walk_objclass.go`).
- The `all` backing arrays are reused across writes (derived-memo style,
  `rules/derivedmemo.go`), so the steady state allocates nothing.
- **Hit condition:** `stampSet` and the current board stamp equals `stamp`;
  the entry is `set`; `entry.objTouch` equals the object's current touch
  generation; the walk's board facts show no grants and no `AddAbility`
  carriers (`walkBoardFacts.hasGrants` / `addAbility`); and every recorded SA
  is all-plain `manaSAFacts`
  (`noActivation`, `noPhaseGate`, `noIsPresent`, `noCheckSVar`, `noLimit`;
  `rules/mana_safacts.go`).
- **The board stamp follows `BoardReadKey`** (`rules/board_read_key.go:30`),
  the derived memo's own cross-walk key: `lineage`, `derivedSeq`,
  `continuousVersion` and `objs`, plus `tapeEpoch` folded for a restore that
  rewinds state under the same log (`board_read_key.go:35-38`). It adds
  `staticTouchGen`, `crossWalkRetires` and `turn` for the mana-specific
  inputs.
- **Per-object touch:** the `touch` generation is bumped in `walkClassTouch`
  (`rules/walk_objclass.go:359`) for any touched object that is not provably
  static-cold — the same conservative rule under which `staticTouchGen`
  already bumps (`rules/static_zoneskip.go:201`, `walk_objclass.go:363`). It
  catches an object touched while static-cold, which the global `staticTouchGen`
  does not. This mirrors the parent spec's "extend `staticZonesCatchUp`'s
  touch pattern with a per-object generation stamp"
  (`2026-10-06-legal-walk-design.md:416`).

## 5. Data flow

For an own-battlefield-`p` object in the mana loop
(`rules/legal_walk_battlefield.go`):

1. If the walk is carryable (own battlefield `p`, no grants/`AddAbility`) and
   `all, ok := e.manaMemberLookup(id); ok` — serve `all`.
2. Else compute `all := e.appendAvailableManaAbilitiesGate(dst, &w.actionStatics, p, id, true)`
   (the payability-deferred gate `ownManaMembers` uses,
   `rules/walk_block_reuse.go:122`), then `e.manaMemberStore(id, all)`.
3. `mas := filterPayableMana(all)` — the payability loop currently inline in
   `ownManaMembers` (`rules/walk_block_reuse.go:124-133`) extracted into a
   shared helper so the carry and the record agree exactly.
4. If `w.rec != nil`, still call `w.rec.recordMembers(zi, all)` on both hit
   and miss, so PotentialMana's record is byte-for-byte what it is today.

The `else` branch (objects that are not own-battlefield-`p`) is unchanged.

## 6. Correctness argument

Every input of the deferred membership is one of:

- the object's own fields — covered by `entry.objTouch`;
- the derived characteristics — covered by `derivedSeq`;
- the static registry — covered by `staticTouchGen`;
- the continuous registry — covered by `continuousVersion`;
- the turn (summoning sickness via `pay.TapFlagsSick`) — covered by `turn`;
- the walk's board facts (grants / `AddAbility`) — gated before use;
- each ability's own gates — the all-plain `manaSAFacts` gate proves none
  reads phase, board presence, SVars, or activation counts;
- the pool — **not** cached: `manaCostPayable` is re-applied every walk by
  `filterPayableMana`, exactly as `ownManaMembers` does today.

`crossWalkRetires` is compared because `retireCrossWalkMemo`
(`rules/derivedmemo.go:382`) bumps `derivedSeq` for no-event probes; including
it keeps the key conservative when such a probe runs. `tapeEpoch` and
`lineage` cover a kernel restore that rewinds state under the same log, which
`derivedSeq`/`continuousVersion` alone cannot see (`board_read_key.go:35-38`).

**Verify mode** (`walkCacheVerify`): on every hit, recompute
`appendAvailableManaAbilitiesGate(…, true)` and panic on any difference, the
same contract `ownManaMembers` already enforces
(`rules/walk_block_reuse.go:134-138`). This is the empirical half of the
argument above and is what the `enginebench-verify` and rules test binaries
run.

## 7. Invariants kept

1. **Byte-identical offers.** The carry serves the same membership; labels,
   modes, `Cost` markers and `Index` are produced by the unchanged code.
2. **Heads unchanged.** Pure read: no event, no `state.Game` write.
3. **Determinism.** Dense slice, no `map` range; keyed on event-log counters,
   the log lineage and a per-object generation, never wall clock or pointer
   order. A restore that rewinds state under the same log is detected by
   `tapeEpoch` (`board_read_key.go:35-38`), the same guard the derived memo
   uses.
4. **Hot-path shape.** No maps, dense `ObjID`-indexed slices, per-walk facts
   stay bitfields; zero allocation once warm.
5. **Verify mode** as above.
6. **Clone policy.** `manaMemberCarry` is `clone:"reset"`, so hypothetical and
   search clones start cold; an owner guard (`owner != e`, the `walkObjCls`
   pattern, `rules/walk_objclass.go`) stops a by-value Engine copy from
   writing the original's arrays. `TestClonePolicyEveryFieldTagged` must pass.

## 8. Measurement and kill

- Instrument hit/miss counters, surfaced on the existing `-walkstats` read (or
  a small dedicated `-manacarry` row read) so the hit rate is visible before
  and while landing.
- Measure the walk's cumulative CPU with `-cpuprofile` on random A, random B
  and bot A, with and without the carry.
- **Gate / kill** (from the parent spec, `2026-10-06-legal-walk-design.md:429-430`):
  expected ≤ the mana-gate cum (random B ≈ 12% of the walk; random A ≈ 4%;
  bot A ≈ 6%), minus the residual payability check. **Kill if the walk cut is
  < 4% on random B**, or any verify trip on the repo decks.

### Approach 2 contingency (not built now)

If the board stamp is too coarse and the hit rate is poor — e.g.
any event touching a static-hot object invalidates the whole carry between two
priority walks of the same seat — replace the board stamp with a single
`manaMemberGen` bumped only by mana-relevant events (turn change, a static-hot
object's move/control change, ability gain/loss, `continuousVersion`). That
narrows the key and should raise the hit rate, at the cost of a fail-closed
event classifier. It is deliberately not part of this ticket; it is the
fallback if Approach 1's measurement fails the kill.

## 9. Test plan / done means

- `TestLegalWalkDigest` byte-identical over every repo deck, 2-seat, 4-seat and
  Commander.
- `-run 'TestPotentialWalkSharedAcrossReaders|Mana|TestHeads$' ./rules ./view`.
- `make sim` replays; `TestHeads` unchanged.
- Verify mode live over the repo decks with no panic.
- `TestClonePolicyEveryFieldTagged`.
- New unit test: a hit serves membership identical to a fresh gate; the entry
  invalidates on a turn change and on a board change; a clone starts cold.
- Measurement reported for random A, random B, bot A; walk cut vs the kill
  threshold.

## 10. Files

- **New:** `rules/mana_member_carry.go`, `rules/mana_member_carry_test.go`.
- **Edited (one call site each):** `rules/legal_walk_battlefield.go`,
  `rules/walk_block_reuse.go` (extract `filterPayableMana`),
  `rules/walk_objclass.go` (per-object touch bump),
  `rules/engine_scratch.go` (the cluster and its `clone:"reset"` tag),
  plus the bench instrument read.
- **Hot-file note:** `rules/legal_walk_battlefield.go` is a one call-site edit;
  new logic goes in the new file.

## 11. Open risks

- **Hit rate.** The board-stamp key may invalidate too often between
  consecutive same-seat walks. This is the parent spec's own uncertainty; the
  measurement gate above is how it is resolved, with Approach 2 as the
  fallback.
- **In-stride versus cross-decision hits.** Most hits may be within one
  decision's multiple walks (the intra-decision case the landed record
  already covers). The instrument must separate the two so the payoff is not
  overstated.
- **Link-time verify flag.** `walkCacheVerify` must be reachable in the
  `enginebench-verify` build, as it already is for `ownManaMembers`.
