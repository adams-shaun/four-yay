# Search-probe engine reset: measured outcome

Design: `docs/superpowers/specs/2026-10-07-searchprobe-engine-reset-design.md`.
Plan: `docs/superpowers/plans/2026-10-07-searchprobe-engine-reset.md`.

Landed (kept):

- **C3 — transfer a once-selected world engine** (`e2f0b3059`): a proposal
  selected once hands its engine to the result; only a repeated proposal is
  cloned (`materializeWorlds`, `internal/searchprobe/sample.go`).
- **C4 — recycle the parallel teacher's rollout engines** (`5d31d32c5`): the
  parallel path mirrors the sequential one, reusing one `rules.Spare` per
  worker (`teacher.go`).

Parked (not landed): **C1 `rules.Engine.Reset`** and **C2 the per-worker
engine pool**. Both were implemented and committed, then removed after
measurement (history preserved on `backup/park34-20261007`, tip `f63c598c5`).

## Measurements (2026-10-07, shared box)

Output-identical is the hard requirement and holds: hosted-root sampler lines
(attempts/accepted/prefix-rejected, ESS weighting) match the pre-change binary
exactly on the same seeds, for every run below.

- **Hosted-root botbench, 10 games x 2 reps ABBA, pinned core 8,
  GOMAXPROCS=1** (C1+C2 vs pre-change):
  - user CPU: base 11.12 / 11.73 s, cand 11.53 / 11.57 s (cand ~1% slower).
  - `ms/asked` mean: base 102.6 / 108.1, cand 107.1 / 107.2.
- **enginebench pair, GOMAXPROCS=1, pinned, 2 reps**: `clone` 0.999,
  `sampler` 1.005 (wall-neutral). Over the 5 s sampler run, alloc_space
  1652.6 MB -> 1571.8 MB (~5% lower).

## Why C1/C2 do not pay

The spec's premise was that deep engine cloning dominates the search-probe
path. Reuse is real but undersized: in the sampler workload only 0-2 worlds
are accepted per run, so the pool recycles mostly **rejected** attempts, whose
event log is short. `events.NewLogIntoHint` and `state.NewGameInto` only adopt
a Spare array when `cap(spare) >= target`; a rejected attempt's log/arena cap
is below `expectedEventsPerGame`, so genesis reallocates regardless
(162 MB in `NewLogIntoHint` over 5 s in both binaries). `Engine.Reset` also
pays `Release` + zeroing + rebuild for no net array saving, which is the
observed ~1% CPU regression.

C3/C4 are small, touch no `rules/genesis.go` (no cpu-redeal contention), and
keep output byte-identical; they land alone. If the clone path is revisited,
the fix to try first is reserving `expectedEventsPerGame` capacity on a
recycled engine (or having rejected attempts grow their log) so the Spare is
actually large enough to be adopted.
