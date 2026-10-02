# Repo map — research harnesses and bench/training tools

Part of the [repo map](repo-map.md) index. Infrastructure and shared tooling
live in [repo-map-internal.md](repo-map-internal.md).

## `internal/` research harnesses

| Package | What it owns |
|---|---|
| `policynet` | Pure-Go learned policy: hashed features, MLP, value head, `.gpol` checkpoints, label-corpus loaders. |
| `searchbench` | Native search-benchmark replication: the sealed item manifest and scoring, `Materialize` (StateSpec → staged engine), `Reach`, canonical options (`BuildCanon`, `Canon.Project`) and 17lands label matching (`LabelItem`). Research harness. |
| `searchbench/statespec` | Strict Go mirror of Draft Zero's StateSpec v1 JSON (validation, aliases, typed labels). |
| `searchprobe` | Hidden-information world sampler (`Collector`, `Sample`, `Redealer`, known-card tracking) and PIMC teacher scoring. Research harness, not production. |
| `searchseat` | The PIMC search decision function (`Choose`) shared by the teacher corpus generator and `SearchBot`. |
| `hindsight` | Deterministic hindsight-branch mechanics (no clock, no files). |
| `paymirror` | A/B check of planned vs manual cast payment. |

## `cmd/` bench and training tools

| Command | Purpose |
|---|---|
| `botbench` | Head-to-head policy evaluation: deck-pair matrices, CIs, decision traces, search/az front doors. |
| `searchteacher` / `searchprobe` | PIMC label-corpus generator / search calibration (also the engine perf oracle). |
| `policytrain` / `policytune` / `exitloop` / `hindsight` | Train `policynet` / SPSA-fit cast weights / expert-iteration loop / hindsight labels. |
| `traindash` | Read-only training dashboard over `/mnt/sata/gorge-training` (`make traindash`). |
| `enginebench` | The draft-zero docs/015 engine-speed rows (random/bot play, Clone, one step, search rates); builds at a4af596 too. Results: `docs/superpowers/reports/2026-09-30-gorge-engine-speed.md`. |
