# Seat deck manifest contract

Status: implementation specification, ready for agentctl intake.

## Decision

Every Gorge player interface receives an immutable, seat-private **own deck
manifest**. It is the unordered, counted list registered for that seat at
match creation. It is not a view of the library: it contains neither current
membership nor order, object ids, known positions, random seed, or any card
identity belonging to another seat.

This makes a deck-aware bot able to reason about its tutor and search choices
from the same information a player may bring to a match. It can derive a
belief such as `deck manifest − cards it has observed`; the engine continues to
be the sole authority on the actual library and supplies the legal candidates
when a search is posed.

## Contract

`deck.Manifest` is a value with the following semantics:

| field | meaning |
|---|---|
| `name` | the seat's assigned deck name; presentation only |
| `main` | every original main-deck card, collapsed to canonical-name/count rows and sorted by canonical name |
| `sideboard` | same representation; omitted when empty |
| `commanders` | the one or two commander identities, in declared commander order; they also remain counted in `main` |

Canonical names are the compiled card face names used by `rules.Config.Decks`.
Deck entry order, object ids, printing/set metadata, Forge text, tokens and
the current zone of any entry are deliberately absent. A manifest is immutable
for the life of its match and is copied before publication; consumers must not
retain a mutable backing slice owned by the engine or host.

The manifest is **only for the credential's/actor's own seat**. It is not made
visible because a spectator is omniscient, because another player controls the
seat, or because a table is a bot table. It never appears in a public view,
another seat's view, redacted events, logs, persistence event payloads, replay
hashes, or `protocol.MatchInfo`. A feedback capture may contain the named
reporting seat's ordinary `View` and therefore that seat’s manifest; it must
never contain one for any other seat.

The existing library redaction rule is unchanged: `library_size` is a current
count; optional `library_top` is an actual permitted reveal; neither becomes a
deck manifest substitute.

## One source of truth

`rules.Config.Decks`, `Sideboards`, and `Commanders` are the genesis source.
`rules.New` builds a per-seat manifest once from that configuration and stores
an immutable copy on the engine. This keeps direct engine users, host tables,
replay, and test fixtures in agreement without asking the host to re-open a
deck file or reconstruct a deck from mutable zones.

`host.Deck.Name` is supplied as the manifest name where the host has it. For
direct `rules.Config` callers with no display deck name, use the existing
`Config.Names[seat]` identity. This is presentation metadata only and cannot
change gameplay or the event stream.

The manifest is derived data, not an event and not mutable game state. It must
therefore leave event kinds, logs, deterministic replay, and golden heads
unchanged.

## Interface mapping

1. **Native view and HTTP/SSE.** Add `View.OwnDeck *deck.Manifest` with JSON
   key `own_deck`. `view.ProjectFor` fills it only for `Visibility == Seat`
   and an in-range viewer. `ViewAtSeat`, the native `/view` endpoint, seat
   stream frames, and a named seat feedback capture consequently carry it
   without a second, drift-prone endpoint. Public and omniscient spectator
   views omit it.
2. **Ordinary in-process seats.** `seat.Seat.Decide` already receives
   `view.View`; `seat.BoardFromView` copies `OwnDeck` into
   `botpolicy.Board.OwnDeck`. `botpolicy.BoardFromGame(Into)` obtains the
   same value from the engine. The existing whole-game adapter-parity test is
   extended to compare it. Thus `bot`, `sb-tactical`, policy-net and any
   third-party `seat.Seat` have the same information.
3. **Hosted search seats.** `bots.Env.View` and `Env.Board` gain the same
   manifest through the preceding mapping. `searchseat.Env.Setup` keeps its
   declared-game data for deterministic world reconstruction, but implementations
   must use the actor's `OwnDeck` rather than treating all `Setup.Decks` as
   universally visible. Existing benchmark configurations that intentionally
   declare opponent lists remain explicit test-only/public-list modes.
4. **ManaBrew.** The published ManaBrew deck type exists, but no standard
   lifecycle message carries a deck. Gorge therefore adds a documented,
   additive `gameView.ownDeck` `x_gorge_own_deck_v1` extension. It is the
   same manifest, rendered as name/count rows (not `DeckCard` instances) and
   is sent only on the authenticated seat's state. It is always present for a
   valid seat state (including an empty main list), so a reconnect cannot lose
   setup information; clients that do not know the optional JSON member simply
   ignore it. The adapter's stream and poll paths must produce identical data.
   This is a Gorge extension, not a claim that the upstream protocol defines
   deck submission.

No interface gains an endpoint for arbitrary seat manifests. A host-side
client wants its own manifest by authenticating as that seat and reading the
normal initial view/state.

## Tutor and search behavior

When a card asks a player to search or choose from their library, the engine's
pending decision remains the authority and contains the legal choices. The
manifest is advisory static setup data; it cannot make an absent card legal,
make an illegal card selectable, or expose the current library order. A bot
can use the named candidates in the prompt together with `OwnDeck` to choose
well. The same rule applies to a declared deck that has been changed by
ante-like effects, control effects, copying, tokens, or other zone changes:
the manifest does not claim those cards are currently in the library.

## Security and regression requirements

- A seat sees exactly its own manifest; attempts to request another seat still
  fail through the existing claim boundary.
- Public and omniscient projections contain no manifest card names. The
  omniscient setting continues to show hands but never turns a deck manifest
  into spectator data.
- JSON has a stable row order. Duplicate card names collapse to one positive
  count. A zero-card main deck uses `main: []`, not `null`; absent sideboard
  stays omitted.
- The manifest remains identical through shuffles, draws, tutors, mulligans,
  control changes, replay, persistence restore, undo, and a finished-match
  view at any sequence.
- Engine/view board parity holds for ordinary bots. A search seat cannot get
  an opponent list through the newly standard manifest path.
- The ManaBrew stream, poll state, reconnect, and wrong-seat-token tests prove
  the extension is owner-only and does not alter prompt validation.

## Non-goals

- Deck submission, lobby/match creation, custom deck upload, and importing a
  ManaBrew `Deck` are not part of this work.
- No change to library visibility, tutor legality, hidden-zone event redaction,
  object ids, game state mutation, or replay schema is authorized.
- No UI decklist panel is required. Clients may ignore `own_deck`.

## Queue

The direct briefs are in
[`../plans/seat-deck-manifest/`](../plans/seat-deck-manifest/README.md). They
are intentionally sequential: the ManaBrew extension must consume the proven
core contract, and the final slice audits every interface without broadening
the protocol further.
