# Seat deck manifest: ManaBrew state extension

Depends-On: seat-deck-01-core

Implement §Interface mapping item 4 of
`docs/superpowers/specs/2026-09-29-seat-deck-manifest.md` after the core
manifest contract is landed.

Add an additive, documented `gameView.ownDeck` `x_gorge_own_deck_v1` extension
to Gorge's ManaBrew DTO and translator. Its payload must contain the same
ordered name/count manifest as native `own_deck`, not card instances, object
ids, library order, Forge scripts, or opponent data. It is present on every
authenticated seat state; old clients may ignore the optional JSON member.

Ensure stream, one-shot state, reconnect, and state-after-intent all agree.
Prove a seat token cannot obtain another seat's manifest, and that existing
prompt messages and input validation are unchanged. Do not attempt lobby,
deck upload, feature negotiation, or generic upstream ManaBrew Deck submission.

Run `go test ./internal/manabrew ./host/manabrewhttp` plus the relevant host
seat-authorization and view tests.
