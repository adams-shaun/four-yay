# Repo map — every other path

Part of the [repo map](repo-map.md) index. The engine chain is in
[repo-map-engine.md](repo-map-engine.md), `internal/` in
[repo-map-internal.md](repo-map-internal.md), and the research/bench tools in
[repo-map-research.md](repo-map-research.md).

## `cmd/`

| Command | Purpose |
|---|---|
| `forgec` | Fetch and compile the Forge corpus; `report`, `coverage`. |
| `gorged` | The server: perpetual bot tables, vs-bot games, web client. |
| `mtgsim` | Headless self-play over the repo decks with replay verification (`make sim`). |
| `repro` | Replays a player feedback snapshot; `-emit-test` writes a failing test. |
| `headdiff` | Plays the `TestHeads` acceptance games; `-dump` records their event streams, `-against` names the first divergent event between two builds. |
| `cardfuzz` | Random mono-colour decks over the supported corpus; hunts panics, livelocks, divergences. |
| `testbudget` / `gcgate` | Per-test wall and peak-RSS budget check (post-merge, `scripts/postmerge_full.sh`); by-hand GC-share check. |
| `gentypes` | Regenerates `web/src/protocol.ts` (`make gentypes`; `-check` in lint). |
| `deckimport` | Plain-text decklist → repo deck JSON. |
| `ledger` | Rebuilds the derived issue ledger (`.ds4/ledger.json`). |
| `keywordbench` / `oraclepacket` / `autopayaudit` / `paymirror` | Keyword presence stats / Oracle text for audit authors (write to a gitignored path) / auto-pay audit (`-tags autopayaudit`) / payment A/B CLI. |

## Everything else

| Path | What it is |
|---|---|
| `web/` | Svelte + Vite client. `web/src/protocol.ts` is **generated** — never hand-edit. |
| `docs/superpowers/specs/` | Design specs, `YYYY-MM-DD-<slug>[-design].md`. The founding design is `2026-09-03-mtgcore-go-engine-design.md`; deliberate contracts (not debt) are in `2026-09-22-engine-contracts.md`. |
| `docs/superpowers/plans/` | Implementation plans (single files or dirs with a `README.md`). |
| `docs/superpowers/reports/` | Measured results. Numbers you quote should come from here, with the file named. |
| `docs/agents/` | This guide. |
| `orchestrator/hooks.py` | agentctl pipeline hooks. Stdlib-only Python; the old daemon is gone. |
| `.agentctl/config.toml` | agentctl pipeline config: tiers, gates, landing. |
| `.githooks/` | pre-commit, commit-msg, pre-push (`core.hooksPath=.githooks`). |
| `scripts/` | `agent-worktree.sh` (the only sanctioned worktree creator), `fleet.sh` (port allocation), `smoke.sh`, `cleanup.sh`, `deploy-demo.sh` (operator only). |
| `.github/workflows/coverage.yml` | On push to main: runs `make coverage` and publishes `.coverage/summary.md` as the job summary and `.coverage/` as the `card-coverage` artifact. Commits nothing. |
| `internal/testutil/testdata/wall_exceptions.txt`, `rss_exceptions.txt` | Shrink-only inventories of tests over the per-test wall / RSS budget. |

## Untracked but load-bearing

| Path | Notes |
|---|---|
| `.cards/` | Forge corpus (`cardsfolder/`, `tokenscripts/`) and the IR cache `ir-<fingerprint>.gob.gz` with its per-card segment file `ir-<fingerprint>.seg` (written by `Registry.Save`, read by the subset loader). GPL-3.0 — never tracked. Without it, corpus tests **skip and read green**. |
| `.ds4/` | Pipeline scratch (issues, briefs, `ledger.json`). `TestNothingUnderDS4IsTracked` keeps it out of git. |
| `.superpowers/` | Orchestration records and seat prompt context. |
| `.worktrees/` | Sibling task checkouts. Exclude from searches or you get duplicate hits. |
| `gorged-data/` | Default server persistence. Never share one directory between servers. |
| `feedback/` | Player bug reports (agentctl intake). Excluded via `.git/info/exclude`. |
| `cmd/repro/testdata/.tokens/` | GPL token text stripped from committed feedback fixtures. |
| `/mnt/sata/gorge-training` | Training corpora, checkpoints, `GOTMPDIR`. Never `/tmp` (RAM-backed). |
