# Queue intake: seat deck manifest

The implementation authority is [the specification](../../specs/2026-09-29-seat-deck-manifest.md).
The briefs are intentionally small and sequential.

| File | Issue ID | Depends on |
|---|---|---|
| [01-core.md](01-core.md) | seat-deck-01-core | none |
| [02-manabrew.md](02-manabrew.md) | seat-deck-02-manabrew | seat-deck-01-core |
| [03-acceptance.md](03-acceptance.md) | seat-deck-03-acceptance | seat-deck-02-manabrew |

```sh
PYTHONPATH="$HOME/.agentctl/pins/current" python3 -m agentctl issue add . --id seat-deck-01-core --title 'Seat deck manifest: core projection and bot contract' --kind bot-api --priority 2 --brief-file docs/superpowers/plans/seat-deck-manifest/01-core.md
PYTHONPATH="$HOME/.agentctl/pins/current" python3 -m agentctl issue add . --id seat-deck-02-manabrew --title 'Seat deck manifest: ManaBrew state extension' --kind bot-api --priority 2 --depends seat-deck-01-core --brief-file docs/superpowers/plans/seat-deck-manifest/02-manabrew.md
PYTHONPATH="$HOME/.agentctl/pins/current" python3 -m agentctl issue add . --id seat-deck-03-acceptance --title 'Seat deck manifest: cross-interface privacy acceptance' --kind bot-api --priority 2 --depends seat-deck-02-manabrew --brief-file docs/superpowers/plans/seat-deck-manifest/03-acceptance.md
```

Run the commands from the Gorge checkout after checking that the IDs do not
already exist. The briefs are ready for direct intake; do not pass `--triage`.
