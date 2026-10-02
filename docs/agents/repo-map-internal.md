# Repo map — `internal/` infrastructure and shared tooling

Part of the [repo map](repo-map.md) index. Research harnesses under
`internal/` live in [repo-map-research.md](repo-map-research.md).

## `internal/`

| Package | What it owns |
|---|---|
| `archtest` | Import-graph and structural tests (dependency order, `time` allowlist, no `math/rand`, 32-bit build, resume-state census, no engine leaks from host). |
| `testutil` | Corpus registry (`CorpusRegistry` — **skips when `.cards/` is absent**), repo decks (`testutil/decks/*.json`), shared invariants, and the AGENTS.md ratchet (`agentsdoc_test.go`). |
| `testutil/feedback` | Loads player feedback snapshots; `feedback.EngineAt`. |
| `tsgen` | Go→TypeScript for `web/src/protocol.ts`. |
| `bench` | Policy-agnostic game runner: `PlayGame`, `RunPairs`, watchdogs, livelock recovery. Shared by `botbench` and `policytune`. |
| `traceboard` | The redacted, map-free decision-trace board schema (`botbench -decision-trace`). |
