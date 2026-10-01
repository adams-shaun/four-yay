<script lang="ts">
  import type { CommanderStatus } from '../lib/commander';
  import { nextCastCost } from '../lib/commander';
  import CardImage from './CardImage.svelte';
  import CardDetail from './CardDetail.svelte';
  import { HoverCard, type AnchorRect } from '../lib/carddetail.svelte';
  import OptionPicker from './OptionPicker.svelte';
  import { pointerRelease } from '../lib/pointer';

  /**
   * One commander, drawn as a card in its seat's CREATURES row (CZ2) — a
   * commander is a creature, and the tile reads the ambient `--card-w`
   * cascading over it rather than setting a scale of its own. Nothing here
   * decides the number; it only inherits it. Since fb-20260917T232202Z the
   * ambient value comes from the COMMAND PACK (CommandArea's own sub-row at
   * the creatures row's front), which carries the command zone's OWN layout
   * scale — at its default 1 that is exactly the creatures row's creature
   * scale the tile has always read.
   *
   * Commander identity is the premise of the format and a line of text is not
   * how anyone recognises a commander — you recognise it by its art. So this
   * is a card face, not a row: CardImage draws the Scryfall art when it
   * resolves and the typeset blank when it does not, and the blank is the
   * NORMAL state (cmd/gorged ships no catalog, and an offline box resolves
   * nothing). The name and the state therefore sit OUTSIDE the face, under
   * it, at full ink: they read the same whichever half CardImage drew, and
   * they survive the two states where the face is not full-strength — the
   * command zone's greyed art and the away state's empty box.
   *
   * THE TILE IS THE COMMAND ZONE, NOT A PICTURE OF A CARD. The box is always
   * there, drawn with a little dotted border, marking the place on the board
   * that is the command zone; what is inside it depends on where the
   * commander actually is. Art means "the card is somewhere you can see it",
   * full ink means "it is in play", grey means "it is in the zone", and an
   * empty dotted box means "it is not here".
   *
   * THE TAX SITS ON THE FACE, in the corner a printed mana cost occupies —
   * by explicit request, in place of an earlier design that deliberately
   * kept it OFF the face (a derived value in the printed cost's own spot can
   * read as though it IS the printed cost). That risk is real, so the tile
   * marks it as computed rather than printed: parenthesised, `(+N)` rather
   * than a bare number the way a real pip would be, in the initiative colour
   * (not the blank's own text colour) and inside its own translucent chip
   * rather than CardImage's opaque pip disc. It overlays BOTH halves
   * CardImage can draw — the Scryfall art has no DOM hook of its own to
   * anchor a corner value to, so this tile draws its own corner independent
   * of which half rendered.
   *
   * THREE STATES, and every one of them is inspectable. The state is a
   * property of where the card currently is (lib/commander.ts, presenceOf):
   *
   *   command      it sits in the command zone — greyed art, marked
   *                "command zone", plus the CR 903.8 next-cast tax when there
   *                is one — {2} per prior command-zone cast, the DERIVED
   *                number, never the raw count. The tax chip stays on this
   *                in-zone state (it is where the next cast is priced from),
   *                but it no longer justifies full ink: the art is greyed.
   *   battlefield  it is in play, so the box shows FULL art, and it is
   *                marked "on the battlefield". The PERMANENT is what the
   *                board draws for a commander in play — CommandArea filters
   *                battlefield commanders out (ui10, bug 1), so this state
   *                is no longer reached from the board's command area: a
   *                commander in play belongs in the creature row as the real
   *                permanent, not twice. The component still renders it, so
   *                a caller that deliberately hands it a battlefield status
   *                (the test harness) gets full art rather than a guess.
   *   away         anywhere else — graveyard, exile, hand, the stack, or a
   *                zone this viewer cannot browse. An EMPTY box: no art at
   *                all, because the card is not somewhere you can look. The
   *                box, the name and the state band all stay, and the tile
   *                stays inspectable. A commander in a zone this viewer
   *                cannot browse resolves to "library" and is drawn exactly
   *                the same — not being able to see into a zone is not an
   *                error state.
   *
   * Hover dwell or keyboard focus opens the existing CardDetail inspector, in
   * EVERY state including the empty one: an opponent's commander must be
   * readable at any time, including while it sits in a zone nobody can
   * otherwise browse, and that is the reason this feature is on the board
   * rather than in a rail. The wiring is CardTile's — one HoverCard, an
   * anchor rect captured on open, and superviseRendering keeping the panel's
   * lifetime tied to the object it was opened for.
   */
  // hover and anchor are injectable for the repo's SSR test harness, exactly
  // as CardTile takes them (CD1): that environment has no DOM, so no pointer
  // event can ever open the panel and $effect never runs. Production renders
  // never pass them and the defaults are what this component would have built
  // for itself.
  //
  // `seat` is carried only as a data attribute for the board test that counts
  // "one command area per commander seat" now that the tiles have no
  // wrapping element of their own (CZ2) — nothing here reads it.
  let { status, player, seat, hover = new HoverCard(), anchor: anchorProp = null, tileOptions = null, open0 = false }: {
    status: CommanderStatus;
    player: string;
    seat: number;
    hover?: HoverCard;
    anchor?: AnchorRect | null;
    /** tileOptions is this commander's own offers from the pending decision
     *  (see lib/cardoptions.ts), looked up by CommandArea the same way a
     *  battlefield CardTile's are: null when the decision offers this
     *  commander nothing. Casting from the command zone is keyed by the
     *  commander's object id like any other option, so it renders here with
     *  the exact same direct-button/menu affordance CardTile uses — this tile
     *  used to carry no options at all, which is why casting a commander was
     *  reachable only through the seat panel's generic ACTIONS list. */
    tileOptions?: import('../lib/cardoptions').TileOptions | null;
    /** open0 seeds the menu's open/closed state — same SSR-harness purpose as
     *  CardTile's own open0, since this environment has no DOM/pointer events
     *  for a test to click the badge with. */
    open0?: boolean;
  } = $props();

  const card = $derived(status.commander);

  // What the state band says. The zone's own name doubles as the label for
  // `away`, because "graveyard" is more use to the reader than "elsewhere".
  //
  // The printed cost is deliberately NOT re-typeset here: with no art the
  // blank already sets it on the face's title line, so a second copy under
  // the tile would print the same cost twice on a creature-scale card; with
  // art it is one hover away in the inspector, which sets it in the panel
  // header. The tax IS drawn on the face (in the mana-cost corner, see the
  // component doc comment) but as its own marked-derived `(+N)` chip, never
  // as a rewrite of the printed cost itself.
  const label = $derived(
    status.presence === 'command' ? 'command zone' : status.presence === 'battlefield' ? 'on the battlefield' : status.zone,
  );
  const description = $derived(
    status.presence === 'command'
      ? `${player} — ${card.name} is in the command zone${status.tax > 0 ? `, next cast pays ${status.tax} generic commander tax (CR 903.8) over its printed cost` : ', no commander tax'}`
      : status.presence === 'battlefield'
        ? `${player} — ${card.name} is on the battlefield`
        : `${player} — ${card.name} is in the ${status.zone}`,
  );

  let root = $state<HTMLElement | null>(null);
  // The anchor the fixed panel is placed against. In production only
  // capture() sets it, from this tile's rect once a pointer or focus event
  // arrives; the injected one is a seed for the SSR harness, read once at
  // mount so the compiler never sees a prop read in a reactive position.
  const initialAnchor = () => anchorProp ?? null;
  let anchor = $state<AnchorRect | null>(initialAnchor());

  // Same lifecycle tie as CardTile (CD1): the panel's lifetime follows the
  // OBJECT it was opened for, not the mounting. These tiles are keyed by the
  // commander's id so Svelte will not hand one instance a different
  // commander, but the id is re-fed on every card change anyway — the guard
  // is the contract, not an assumption about the keying above it.
  $effect(() => {
    hover.superviseRendering(card.id);
  });

  function capture(): void {
    const r = root?.getBoundingClientRect();
    anchor = r ? { left: r.left, top: r.top, right: r.right } : null;
  }
</script>

<div class="tile-wrap">
<div
  class="cmd-tile cmd-tile--{status.presence}"
  data-commander={status.index}
  data-obj={card.id}
  data-cmd-state={status.presence}
  data-cmd-zone={status.zone}
  data-seat={seat}
  data-tone={tileOptions?.tone ?? ''}
  data-options={tileOptions ? tileOptions.list.length : undefined}
  data-next-cost={nextCastCost(card.mana_cost, status.casts)}
  bind:this={root}
  tabindex="0"
  role="button"
  use:pointerRelease
  title={description}
  aria-label={description}
  onpointerenter={() => hover.arm(card.id, capture)}
  onpointerleave={() => hover.close()}
  onfocus={() => hover.open(card.id, capture)}
  onblur={() => hover.close()}
  onkeydown={(e) => hover.keydown(e)}
  aria-describedby={hover.show ? `card-detail-${card.id}` : undefined}
>
  <div class="face cmd-zone-box">
    {#if status.presence !== 'away'}
      <CardImage {card} pt={false} />
    {/if}
    {#if status.presence === 'command' && status.tax > 0}
      <span
        class="tax data"
        data-tax={status.tax}
        data-casts={status.casts}
        title="Commander tax (CR 903.8): {status.tax} generic on the next cast, for {status.casts} prior cast{status.casts === 1 ? '' : 's'}"
      >(+{status.tax})</span>
    {/if}
  </div>
  <div class="who">{card.name}</div>
  <div class="band">
    <span class="state">{label}</span>
  </div>
</div>

{#if tileOptions}
  <OptionPicker {tileOptions} subject="for {card.name}" {open0} />
{/if}
</div>

{#if hover.show && anchor}
  <CardDetail {card} anchor={anchor} />
{/if}

<style>
  /* A card lying in its own spot on the felt, with a state band under it
     rather than over it: the band is the one thing that must stay readable
     when the face is greyed (in the zone) or an empty box (away), so it
     never sits on the art. */
  /* Width comes from the ambient --card-w — since fb-20260917T232202Z that
     is the command pack's own scale (CommandArea's .cmd-pack), whose
     default 1 is the creatures row's scale (104px) the tile has always
     read. No fallback is set here: outside a container that defines
     --card-w, CardImage's own 90px default applies, which only ever happens
     in a test render. */
  .cmd-tile {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    width: var(--card-w, 104px);
    flex: none;
  }
  /* The box IS the command zone — a place on the board, not a picture of a
     card — so it is always there, card-sized (--card-w, the row's own
     creature scale, CZ2), and framed by a little dotted border. `position:`
     relative gives the tax chip below a corner to anchor to, and `overflow:`
     hidden clips the card art to the box. What is inside the box depends on
     where the commander actually is: the art when it is somewhere you can
     see it, nothing when it is not here. */
  .face {
    position: relative;
    width: var(--card-w, 104px);
    aspect-ratio: 63 / 88;
    border: 1px dotted var(--ink-faint);
    border-radius: var(--card-radius, var(--radius-card));
    box-sizing: border-box;
    overflow: hidden;
    line-height: 0;
  }
  /* In the command zone: the card is HERE, in the box, but not in play, so
     the art is greyed — greying says "in the zone". The CR 903.8 tax chip
     still belongs to this in-zone state (it is where the next cast is priced
     from), but it no longer justifies full ink; the chip is drawn outside the
     greyed art, in its own colour, so it does not go grey with it. */
  .cmd-tile--command .face :global(.card-image) {
    filter: grayscale(1);
  }
  /* In play: the real permanent is in the creatures row of this quadrant and
     it is on the battlefield, so the box shows FULL art — the same card is
     visible twice only because both are true of one object, and this is the
     command-zone slot the card has left, drawn the way a filled slot reads.
     No dimming rule here: full ink is the default. */
  .cmd-tile--battlefield .face :global(.card-image) {
    filter: none;
    opacity: 1;
  }
  /* Away: the card is in a zone this tile cannot show — graveyard, exile,
     hand, the stack, or one the viewer cannot browse — so the box is EMPTY:
     no art element at all, because the card is not somewhere you can look.
     (The #if above simply does not mount CardImage in this state.) The box,
     the name and the state band all stay, and the tile stays inspectable. */
  .cmd-tile:focus-visible {
    outline: 2px solid var(--initiative);
    outline-offset: 2px;
  }

  /* The name, OUTSIDE the box and at full ink. It is not a duplicate of the
     blank's own title line: the blank sets the name beside the mana pips and
     clips it to a few characters even at creature scale, and in the command
     zone's greyed art and the away state's empty box the face is not
     full-strength — which is exactly when the reader most needs to know which
     commander the box is. One line, clipped, with the full name (and where it
     is) in the tile's title and accessible name. */
  .who {
    max-width: 100%;
    font-size: var(--t-10);
    line-height: 1.3;
    font-weight: 600;
    color: var(--ink);
    text-align: center;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* The band wraps rather than clips: "on the battlefield" is three words
     against a narrow card and half of it ("on the battlef…") is not a state
     anyone can read. A second line costs 13px at the rim, where there is
     room; an ellipsis costs the meaning. */
  .band {
    display: flex;
    align-items: baseline;
    justify-content: center;
    flex-wrap: wrap;
    gap: 0 0.35em;
    max-width: 100%;
    font-size: var(--t-10);
    line-height: 1.3;
    text-align: center;
  }
  .state {
    color: var(--ink-faint);
    min-width: 0;
  }
  /* The one castable state carries the initiative colour the client reserves
     for what can be acted on. */
  .cmd-tile--command .state {
    color: var(--initiative);
  }
  /* The tax sits where a printed mana cost lives — the corner the reader
     already scans for "what does this cost" — but it is not typeset like a
     printed pip, because it is not one: CardImage's own pips are opaque
     colour discs, printed values, one per card, permanent. This is a
     parenthesised, computed, CHANGING number, so it gets its own translucent
     chip, its own colour (the initiative hue this tile already reserves for
     "castable now" on the state band below), and parentheses no printed
     cost would ever carry — three signals that this is what the NEXT cast
     costs on top of the print, not the print itself. `line-height:0` on
     `.face` would otherwise collapse this to nothing, so it sets its own. */
  .tax {
    position: absolute;
    top: 2px;
    right: 2px;
    line-height: 1.3;
    font-family: var(--font-data);
    font-variant-numeric: tabular-nums;
    font-size: var(--t-10);
    font-weight: 600;
    color: var(--initiative);
    background: color-mix(in srgb, var(--felt-sunk) 85%, transparent);
    border-radius: 2px;
    padding: 0 3px;
  }

  /* The options affordance, ported from CardTile verbatim (same fact, same
     corner, same rules): the wrapper anchors it outside the role="button"
     tile so it can overlay the face's top-right corner — here that corner is
     shared with the CR 903.8 tax chip (top-left, .tax is top:2px/right:2px on
     `.face` which is narrower than the wrap only via its own inset — the two
     do not collide in practice since the tax chip is drawn inside `.face`,
     inset from its own corner, while this sits at the outer wrap's edge). */
  .tile-wrap {
    position: relative;
    display: inline-flex;
    flex-direction: column;
    width: var(--card-w, 104px);
    flex: none;
  }
</style>
