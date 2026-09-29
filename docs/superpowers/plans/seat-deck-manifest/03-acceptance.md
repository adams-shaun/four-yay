# Seat deck manifest: cross-interface privacy acceptance

Depends-On: seat-deck-02-manabrew

Audit the landed contract end to end against
`docs/superpowers/specs/2026-09-29-seat-deck-manifest.md`.

Add a compact fixture with duplicate names, a commander and a sideboard. At
the same match sequence compare the owner’s native HTTP view, ordinary seat
view/board, hosted EnvSeat input, and negotiated ManaBrew state extension.
They must agree on the owner manifest. Assert that the opponent, public view,
omniscient spectator, redacted events, and match metadata do not contain its
private card names. A feedback snapshot may carry only the reporting seat's
manifest, exactly as it carries that seat's private hand.

Exercise shuffle, tutor/search decision, undo or replay-at-sequence, and
persistence restore as applicable to demonstrate that the manifest remains
static while the actual library stays private. The tutor test must prove the
legal candidate prompt—not the manifest—is authoritative.

Run focused packages and the repository’s existing native/ManaBrew privacy
gates. Do not move golden heads unless a real event-level reason requires it.
