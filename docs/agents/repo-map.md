# Repo map — what lives where

A lookup table for agents and new contributors. One line per package or
directory, plus where to go when you need to change a particular kind of
thing. Verified against `main` on 2026-09-28; where this file and the code
disagree, the code wins — fix this file in the same commit.

See also: [invariants.md](invariants.md) (the rules every change must keep)
and [do-not.md](do-not.md) (the mistakes that have actually happened here).

This is the index. The package and path rows live in the per-domain files
below; this file changes only when a DOMAIN appears, not when a package does.

| Domain | What it covers |
|---|---|
| [repo-map-engine.md](repo-map-engine.md) | The engine chain: `cards` … `host/httpapi`, and the one-way import direction. |
| [repo-map-internal.md](repo-map-internal.md) | `internal/` infrastructure and shared tooling packages. |
| [repo-map-research.md](repo-map-research.md) | Research harnesses and bench/training tools (`internal/` and `cmd/`). |
| [repo-map-paths.md](repo-map-paths.md) | Everything else, plus the untracked-but-load-bearing paths. |

## Where do I change…

| I want to… | Go to |
|---|---|
| Implement an `api:` effect | `effects/`, `effects.Register("Name", fn)` in an `init()`. If it needs more than `effects.Host` offers, it is rules work. |
| Implement a keyword/trigger/static/replacement | `rules/`, `effects.RegisterNonAPI("kw:X", …)`. A keyword that expands to script lines is a `cards/kw_*.go` expander instead (moves the fingerprint). |
| Add a `count:` value head | The evaluator arm in `effects/` **and** `effects.modelledValueHeads` (`TestValueHeadRegistryMatchesEvaluator`). |
| Add an event kind | Append to `events/event.go` after the last constant and add its one `kindInfo` entry in `events/kindinfo.go` (name, trigger class, optional Describe template). Behaviour (`events.Apply`, a custom `view.Describe` case) is separate. |
| Add a decision kind | Don't, without an operator decision — the set is closed; every seat, bot, client and the protocol must answer it. |
| Add a bot policy | Bench-only: `cmd/botbench`. Hosted: `host/bot_policy.go` (closed vocabulary). See the README's *Bot player training and adoption guidelines*. |
| Add a repo deck | `internal/testutil/decks/*.json` via `cmd/deckimport`; it must be fully supported (the acceptance ratchet). |
| Turn a player report into a test | `go run ./cmd/repro -emit-test <pkg> <feedback-dir>`. |
| Change the wire | `protocol/`, then `make gentypes`. |
| Change a seat's projected view | The family file for the seam: a card/battlefield projection in `view/card.go`, a stack projection in `view/stack.go`. Only an assembler change (`Project`/`project`) belongs in `view/view.go`, which two projection tickets once collided on. |
