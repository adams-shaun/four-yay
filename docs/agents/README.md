# Agent guide

Documentation for coding agents (and humans) working in gorge. `AGENTS.md` at
the repo root is the always-loaded summary. This directory holds the detail,
so it can stay out of every turn's context.

| Read | When |
|---|---|
| [repo-map.md](repo-map.md) (the index) and its per-domain files (`repo-map-engine.md`, `repo-map-internal.md`, `repo-map-research.md`, `repo-map-paths.md`) | Finding where something lives, or where a kind of change goes. |
| [invariants.md](invariants.md) | Before changing engine code. It lists each design invariant, what enforces it, and the legitimate way to change it, and ends with the contributor workflow. |
| [do-not.md](do-not.md) | Before you touch git, run a server, or merge. Every entry has happened here. |
| [README § Bot player training and adoption](../../README.md#bot-player-training-and-adoption-guidelines) | Building, training or evaluating a bot policy. |
| `docs/superpowers/specs/2026-09-22-engine-contracts.md` | Behaviour that looks like a bug but is a deliberate contract. |

## The six rules that matter most

1. Every state write goes through `events.Apply`, and `events.Kind` is
   append-only.
2. No wall clock, no unseeded randomness, no map order reaching an event. The
   engine is a pure function of `(Config, intents)`.
3. Never commit Forge scripts (GPL-3.0). No cgo, no third-party dependencies.
4. Seats see a `view.View`, never the engine. Clairvoyance never reaches a
   hosted or rated game.
5. Work in a `scripts/agent-worktree.sh` worktree that has `.cards/` linked,
   and leave `main`'s checkout alone.
6. Ratchets fail both ways, the Known-approximations table only shrinks, and
   golden heads move only with a named cause.
