# Seat deck manifest: core projection and bot contract

Depends-On: none

Implement §Contract through §Interface mapping items 1–3 of
`docs/superpowers/specs/2026-09-29-seat-deck-manifest.md`.

Create one canonical immutable manifest representation with sorted,
positive name/count rows for the configured main deck, optional sideboard and
commander identities. Build it from genesis configuration in `rules.New`, not
from live zones or host deck files. It must not affect events, replay, or
rules legality.

Expose it only as `View.OwnDeck` for the authenticated/in-process actor's
seat projection. Thread that exact value to `botpolicy.Board` through both the
view and game adapters. Do not add a second native endpoint and do not add an
opponent-deck field.

Tests must cover deterministic canonicalization; owner-only view JSON;
absence from public and omniscient projections; stability across a shuffle,
draw and replay; and equality of `BoardFromView` and `BoardFromGame` over a
whole game. Update test `Chars` stubs for any necessary view interface method.

Run focused `go test` for `deck`, `rules`, `view`, `seat`, `botpolicy`, `host`
and native HTTP visibility tests. Report the commands and whether corpus tests
actually ran.
