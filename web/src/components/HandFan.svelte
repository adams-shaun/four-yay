<script lang="ts">
  import type { CardView, PaymentAction, PlayerView } from '../protocol';
  import { visibleHand } from '../lib/board';
  import { handFanLayout, PLAY_CARD_WIDTH, type HandFanSpec } from '../lib/handfan';
  import type { CardOptions } from '../lib/cardoptions';
  import { ACTION_GLYPHS, actionAccessibleLabel, singleActionIcon, tileScenario, tileOptions } from '../lib/cardoptions';
  import CardImage from './CardImage.svelte';
  import CardDetail from './CardDetail.svelte';
  import { HoverCard, type AnchorRect } from '../lib/carddetail.svelte';
  import { layoutStore } from '../lib/layoutsettings.svelte';
  import ZoneStepper from './ZoneStepper.svelte';

  /**
   * HandFan is the SEATED PLAYER'S OWN hand, drawn as real card faces along
   * the bottom edge — the competitive-client layout (Arena, MTGO, XMage:
   * full-size faces in a centred row that overlaps rather than shrinks as the
   * hand grows). In the resting state the fan sits half a card LOW and the
   * board clips the lower half (the peek): a hand card shows its top 50%
   * until it is hovered or keyboard-focused, when it rises the same half back
   * into the board (see the .handfan/.card:hover notes in the style block).
   * It is NOT the rail's HandList (see the note in
   * lib/cardoptions.ts' sibling, HandList.svelte), which stays text because it
   * has to fit four hands in a rail (survey #24); that reasoning is about the
   * rail and four hands, and has no purchase on the one hand you own.
   *
   * It renders ONLY for the seated player's own seat — `player` here is the
   * viewer's own PlayerView, whose hand is never redacted (view.go fills Hand
   * for the viewer's seat under every visibility). A spectator never mounts
   * it (Table.svelte gates on `seated`), but it still degrades honestly if it
   * is ever handed a null hand: a Go nil slice marshals to a literal JSON
   * `null` (view.go), and `handFan`/`visibleHand` both treat that as empty and
   * render nothing rather than throwing (the exact shape that once broke the
   * whole client — see the note in e2e/smoke.spec.ts).
   *
   * Hover/focus opens the SAME CardDetail panel every other card surface
   * opens, through the same HoverCard machinery — pointer dwell arms it,
   * keyboard focus opens it, leave/blur/Escape close it, and supervise keeps
   * a panel from hanging on a card that has since left the hand. The card
   * faces are CardImage (width from the `--card-w` custom property set on the
   * fan's container — never the `.card-image` width directly), so a hand card
   * gets exactly the same blank-vs-art treatment as a board tile.
   *
   * THE SAME OPTIONS AFFORDANCE AS A BOARD TILE (ui21), FROM THE SAME INDEX.
   * The board's tiles mark and menu the cards a pending decision offers
   * something to; a hand card was the blind spot — casting a card from hand
   * is the commonest decision a seat makes, and `tileOptions` (lib/cardoptions)
   * already returns exactly what a hand card needs: this card's options, its
   * picked ordinals, the tone, and the post callback. One mechanism, one
   * index, one post path (R-E4-1): the affordance here is a FAN-SPECIFIC
   * PRESENTATION of the same data, not a second index or a second post path.
   * Each hand card wears the tone ring marking; one offer is a direct icon,
   * while several get a count badge and menu of the server's own labels. Both
   * post each option's own index, with a picked chip in click order — exactly what a board tile gets,
   * adapted to the fan's geometry. See the task report for why the shape
   * differs (the fan overlaps; a menu must open UP, the hand sits at the
   * board's bottom edge; and the tile's all-four-corners signal hierarchy
   * has no purchase on a bare face, so the badge marks the card's top edge).
   *
   * The fan never covers the seat panel's decision UI (that panel floats at
   * the TOP of the board, this fan at the BOTTOM) and never intercepts a
   * click meant for anything else: the container is pointer-events:none and
   * only each card face accepts the pointer, so the empty gutter between
   * cards and the area behind them stay click-through to the board below.
   */

  let {
    player,
    /** the room the fan may occupy, px. Left at 0 the component measures its own container width; a unit test passes a concrete number. */
    width = 0,
    /** the pending decision's card-indexed offers (ui21's CardOptions bundle),
     *  or null for a spectator / when nothing is pending. A hand card the
     *  decision offers something to is marked and carries the same options
     *  menu as a board tile (R-E4-1: each item posts the option's own index). */
    options = null,
    paymentActions = [],
    onCastPayment = null,
    /** open0 seeds the fan's open menu (by card id), injectable for the repo's
     *  SSR test harness just as CardTile's `open0` is: this environment has no
     *  DOM and no pointer events, so a test cannot click a badge to open the
     *  menu; the affordance's own state is what a test drives by hand the way
     *  it drives the detail panel. Production never passes it and the default
     *  is that no menu is open. */
    open0 = null,
  }: { player: PlayerView; width?: number; options?: CardOptions | null; open0?: number | null; paymentActions?: PaymentAction[]; onCastPayment?: ((action: PaymentAction, holdPriority: boolean) => void) | null } = $props();

  const hand = $derived(visibleHand(player) ?? []);

  const GAP = 10;
  // The constraint is the room the BOARD has, so the fan never grows past the
  // viewport and the overlap tightens instead.
  //
  // This measures the full-width track element, NOT the fan row inside it.
  // Observing the fan itself measured this component's own output -- the row's
  // width is set from layout.rowWidth, so maxWidth settled at whatever width
  // the fan had already chosen and the constraint never bound. The overlap
  // path was reachable only from a unit test passing `width` explicitly; in a
  // real client a large hand simply ran off the board. The track is laid out
  // by the board and never by the fan, which is what makes it a real
  // measurement.
  let container = $state<HTMLElement | null>(null);
  let measured = $state(0);
  $effect(() => {
    if (container === null) return;
    const ro = new ResizeObserver((es) => {
      measured = es[0]?.contentRect.width ?? 0;
    });
    ro.observe(container);
    return () => ro.disconnect();
  });
  const room = $derived(width > 0 ? width : measured);
  const maxW = $derived(room > 0 ? Math.max(PLAY_CARD_WIDTH, room) : Number.MAX_SAFE_INTEGER);
  // The hand's card scale is a layout setting (fb-20260916T182801Z): the
  // layout MATH takes the scaled width so step/overlap tighten against the
  // real face size, and the CSS --card-w below takes the same multiplier so
  // the faces and the math agree.
  const handScale = $derived(layoutStore.scale('hand'));
  const spec: HandFanSpec = $derived({ cardWidth: Math.round(PLAY_CARD_WIDTH * handScale), gap: GAP, maxWidth: maxW });
  const layout = $derived(handFanLayout(hand.length, spec));
  // The dotted outline (brief decision 4): up while the hand's on-board
  // stepper is being used and for FLASH_MS after any hand adjustment —
  // including one made in the Game Options panel, which pulses the same
  // shared store this track reads.
  let handHover = $state(false);

  // One hover state for the whole fan, same contract as CardTile/HandList.
  const hover = new HoverCard();
  let hovered = $state<CardView | null>(null);
  let anchor = $state<AnchorRect | null>(null);

  // The options menu is this hand's own open/closed state: at most one card's
  // menu is open at a time (the same single-open contract a board tile has),
  // tracked here because a fan's cards are per-item instances and the fan
  // keeps per-card menu state in one place, keyed by card id. Nothing on the
  // wire drives it and nothing outside reads it.
  // svelte-ignore state_referenced_locally
  let openCard = $state<number | null>(open0);
  function toggleCard(id: number) {
    openCard = openCard === id ? null : id;
  }
  function openForCard(id: number) {
    return openCard === id;
  }
  function closeMenuOnEscape(event: KeyboardEvent): void {
    if (event.key !== 'Escape' || openCard === null) return;
    event.preventDefault();
    openCard = null;
  }

  // TIE THE PANEL TO THE OBJECT, NOT TO A POINTER EVENT: if the hovered card
  // leaves the hand (played, discarded, exiled) while the pointer is still
  // over it, its element is removed and pointerleave never fires — the panel
  // would hang on a card that no longer exists. Re-feed the present set on
  // every change; HoverCard closes when the one it shows is gone.
  $effect(() => {
    if (hovered !== null) hover.supervise(hovered.id, hand);
  });

  function rectOf(el: HTMLElement): AnchorRect {
    const r = el.getBoundingClientRect();
    return { left: r.left, top: r.top, right: r.right };
  }
  function armFor(c: CardView, el: HTMLElement) {
    hovered = c;
    hover.arm(c.id, () => {
      anchor = rectOf(el);
    });
  }
  function openFor(c: CardView, el: HTMLElement) {
    hovered = c;
    hover.open(c.id, () => {
      anchor = rectOf(el);
    });
  }
</script>

<!-- The menu may be open while focus is on a card face or the document body,
     so Escape belongs at window scope. The capture-phase table hotkey guard
     sees the still-open role=menu first; this bubble handler then closes only
     the hand picker, leaving an End Turn/hard-skip run intact. -->
<svelte:window onkeydown={closeMenuOnEscape} />

{#if hand.length > 0}
  <!-- The track spans the board and is the measured element; the fan row is
       packed per the layout setting (data-align, default centred) inside it.
       Both are pointer-transparent; only a face claims the pointer — and the
       resize stepper (ZoneStepper), which re-enables the pointer on itself. -->
  <div class="handtrack" class:zone-outline={layoutStore.flash.hand || handHover} bind:this={container}>
  <div class="handfan" style:width="{layout.rowWidth}px" style:--hand-scale={handScale} data-peek={layoutStore.peek} data-align={layoutStore.align('hand')}>
    {#each hand as c, i (c.id)}
      <!-- A hand card is NOT a board permanent: no tapped/attacking/counters
           chrome, just the face plus the shared hover inspector. When the
           pending decision offers this card something it also carries the
           same options affordance a board tile gets — the tone ring, the
           options badge + menu (each item posting its own index, R-E4-1),
           and the picked chip — adapted to the fan. -->
      {@const opt = options ? tileOptions(options, c.id) : null}
      {@const payment = paymentActions.find((action) => action.cast.object === c.id && action.plans.length > 0)}
      {@const landPlay = opt?.list.find((action) => action.kind === 'play_land')}
      <!-- Auto Mana already supplies CAST for its matching base cast. Keep
           unrelated offers visible, but remove that exact legacy duplicate. -->
      {@const legacyActions = opt?.list.filter((action) => action.index !== payment?.base_option_index) ?? []}
      <div class="card" class:marked={!!opt} data-obj={c.id} style:left="{i * layout.step}px">
        <div
          class="face"
          data-tone={opt?.tone ?? ''}
          data-options={opt ? opt.list.length : undefined}
          data-selected={opt && opt.pickedOrder.length > 0 ? opt.pickedOrder.join(',') : undefined}
          tabindex="0"
          role="button"
          onpointerenter={(e) => armFor(c, e.currentTarget)}
          onpointerleave={() => hover.close()}
          onfocus={(e) => openFor(c, e.currentTarget)}
          onblur={() => hover.close()}
          onkeydown={(e) => hover.keydown(e)}
          aria-describedby={hover.show && hovered?.id === c.id ? `card-detail-${c.id}` : undefined}
        >
          <CardImage card={c} />
        </div>
        {#if opt && legacyActions.length > 0 && !(legacyActions.length === 1 && landPlay)}
          <!-- The options affordance sits OUTSIDE the role="button" face so a
               real button is never nested inside one; it anchors to the card's
               TOP EDGE (a bare face has no corner meaning to preserve, and the
               icon/badge clears the card in front of it on the overlap fan). -->
          {@const scenario = tileScenario(opt)}
          <div class="tile-actions">
            {#if legacyActions.length === 1}
              {@const action = legacyActions[0]}
              {@const icon = singleActionIcon(action)}
              <button
                class="action-icon badge--{opt.tone}"
                class:selected={opt.pickedOrder.length > 0}
                type="button"
                data-single-action
                data-action-icon={icon}
                aria-label={actionAccessibleLabel(action)}
                title={actionAccessibleLabel(action)}
                onclick={(event) => opt.post(action.index, false, event.ctrlKey)}
              >
                <span aria-hidden="true">{ACTION_GLYPHS[icon]}</span>
              </button>
            {:else}
              <button
                class="badge badge--{opt.tone}"
                class:selected={opt.pickedOrder.length > 0}
                type="button"
                aria-haspopup="menu"
                aria-expanded={openForCard(c.id)}
                aria-label={scenario
                  ? `${legacyActions.length} ${scenario.noun} for ${c.name}`
                  : `${legacyActions.length} actions for ${c.name}`}
                title="Options for {c.name}"
                data-action-icon={scenario?.icon}
                onclick={() => toggleCard(c.id)}
              >
                {#if scenario}
                  <span class="badge__icon" aria-hidden="true">{ACTION_GLYPHS[scenario.icon]}</span>
                {/if}
                <span class="badge__n data">{legacyActions.length}</span>
              </button>
            {/if}
            {#if opt.pickedOrder.length > 0}
              <span class="sel data" aria-label="picked {opt.pickedOrder.join(', ')}">{opt.pickedOrder.join(',')}</span>
            {/if}
            {#if opt.list.length > 1 && openForCard(c.id)}
              <ul class="menu" role="menu" aria-label="Options for {c.name}">
                {#each legacyActions as o (o.index)}
                  <li role="none">
                    <button class="menu__item" type="button" role="menuitem" onclick={(event) => opt.post(o.index, false, event.ctrlKey)}>
                      {o.label}
                    </button>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        {/if}
        <!-- Gate on the object ID, never object identity: every live view
             refresh hands this keyed each a FRESH CardView for the same id,
             so `hovered === c` flipped false and unmounted the open panel
             while the pointer never left the (kept) element. Render the
             fresh `c` so the open inspector shows the refreshed data, the
             same contract a board tile's CardDetail has. -->
        {#if hover.show && hovered?.id === c.id && anchor}
          <CardDetail card={c} anchor={anchor} />
        {/if}
        {#if payment}
          <button class="payment-shortcut" type="button" data-payment-card={payment.id} aria-label="Cast {c.name} with suggested mana" title="Cast with suggested mana" onclick={(event) => onCastPayment?.(payment, event.ctrlKey)}>CAST</button>
        {:else if landPlay && opt}
          <button class="payment-shortcut" type="button" data-play-land={landPlay.index} aria-label={landPlay.label} title={landPlay.label} onclick={(event) => opt.post(landPlay.index, false, event.ctrlKey)}>PLAY</button>
        {/if}
      </div>
    {/each}
  </div>
  {#if layoutStore.steppersOnBoard}
    <ZoneStepper zone="hand" label="hand" onhover={(h) => (handHover = h)} />
  {/if}
  </div>
{/if}

<style>
  /* The fan floats along the bottom of the felt, centred, above the viewer's
     own board (their quadrant's lands row), the bottom edge of the play area.
     pointer-events:none on the row means only a card face (set back to auto
     below) ever claims the pointer — the empty gutter and the space behind
     the fan stay click-through, and the fan can never eat a click or hover
     meant for the seat panel's decision UI (which floats at the TOP of the
     board, the opposite edge). */
  .handtrack {
    position: absolute;
    left: var(--own-seat-w, 0px);
    right: 0;
    bottom: 0;
    z-index: 6;
    pointer-events: none;
  }
  /* The dotted outline (fb-20260916T182801Z): up while the hand's resize
     stepper is being used (handHover) and for FLASH_MS after any hand
     adjustment — including one made in the Game Options panel, which pulses
     the same shared store this track reads. */
  .handtrack.zone-outline {
    outline: 2px dashed var(--ink-dim);
    outline-offset: 3px;
  }
  /* The fan owns the bottom edge after the fixed identity bay; together they
     make one seat strip. The track, not the cards, consumes --own-seat-w, so
     overlap tightening still follows the actual room available.

     The peek (fb-20260916T024357Z-9005ad6a): the whole fan sits half a card
     LOW so the board's own overflow:hidden (Table.svelte .board) clips the
     lower half — the normal state exposes exactly the top 50% of every hand
     card. The offset is relative (50% of the fan's own height, which is one
     card height in the 63:88 ratio), so it stays exactly half whatever
     --play-card-w becomes; no pixel constant. A card's hovered/focused
     translateY(-50%) (below) pays exactly that half back, so its full face
     returns inside the board on hover/focus. */
  .handfan {
    position: relative;
    margin-inline: auto;
    --card-w: calc(var(--play-card-w) * var(--hand-scale, 1));
    height: calc(var(--card-w) * 88 / 63);
    transform: translateY(50%);
    pointer-events: none;
  }
  /* The peek modes (fb-20260916T182801Z, brief decision 2 — the three
     readings of "slide up"):
     - hover (the shipped default): the half-clipped rest above, raised on
       hover/focus — the base rules, unchanged.
     - always: full cards, never clipped — the fan rests inside the board.
       The hover raise stays as emphasis.
     - never: no slide-up at all — the half-clipped rest stays and the hover
       raise is suppressed (the pointer arm below); the keyboard arm
       (.card:has(:focus-visible)) deliberately KEEPS its raise, because a
       keyboard user must still be able to bring the focused card into view.
       The hover CardDetail inspector still reads a card in this mode. */
  .handfan[data-peek='always'] {
    transform: translateY(0);
  }
  .handfan[data-peek='never'] .card:hover {
    transform: translateY(0);
  }
  /* The fan's own alignment (data-align, layout settings): centre is the
     shipped margin-inline: auto; left/right pin the packed row to that edge
     of the track. */
  .handfan[data-align='left'] {
    margin-inline: 0 auto;
  }
  .handfan[data-align='right'] {
    margin-inline: auto 0;
  }
  /* Each face is absolutely positioned by the layout's step, then that step
     is also the negative margin so a face slides UNDER the one ahead of it
     when the fan overlaps: the first card is front-most (highest stacking),
     the last is behind it. Hovering raises the face. */
  .card {
    position: absolute;
    top: 0;
    width: var(--card-w);
    aspect-ratio: 63 / 88;
    pointer-events: auto;
    cursor: pointer;
    transition: transform 0.12s ease-out, filter 0.12s ease-out;
    transform: translateY(0);
    filter: brightness(0.92);
  }
  /* The hovered / focused face lifts off the row, the competitive raise, and
     the faces behind it go under rather than over it. The raise is the same
     relative half the fan shifted down (the fan's height IS one card height),
     so a hovered card's full face sits back inside the board — the peek is
     repaid in full, and the card keeps the z-index that holds it above the
     neighbours it overlaps.

     The focus arm is `.card:has(:focus-visible)`, NOT `.card:focus-visible`:
     since ui23 moved tabindex off the card onto the inner .face (so a real
     options button is never nested inside a role=button), the element the
     keyboard focuses is the FACE, and a :focus-visible on .card itself could
     never match again — the old selector was dead, leaving a keyboard user
     with neither the raise nor the outline (caught by the geometry fixture,
     HandFan.geometry.test.ts). :has() restores the parity structurally, off
     the element that is actually focused.

     The descendant form has NO .face qualifier on purpose: a card's options
     affordance puts MORE focusables inside the card — the direct-action
     icon, the count badge and (once open) its menu items, all real buttons
     beside the role="button" face. Tabbing from the face onto any of them
     blurs the face, so a `.face:focus-visible`-qualified :has() would drop
     the card back to its half-clipped rest while keyboard focus stayed on
     that card's action — the raise must follow ANY focused descendant, which
     is also what keeps the badge and its open menu usable inside the raised
     card (caught by the geometry fixture's action-control steps). A mouse
     click on those controls does not set :focus-visible, but the pointer arm
     `.card:hover` already holds the card up then. The outline stays on the
     face for the same reason as before (below). */
  .card:hover,
  .card:has(:focus-visible) {
    transform: translateY(-50%);
    filter: brightness(1);
    z-index: 10;
  }
  .face:focus-visible {
    outline: 2px solid var(--initiative);
    outline-offset: 1px;
  }
  /* The board marking, restated on a hand card: a card the pending decision
     offers something to wears the seat panel's own initiative/offered
     register (see lib/seatpanel toneOf) — blocked (targets/blocks/modes)
     warms to --initiative, an open window (cast/activate) cools to
     --offered. This is a ring around the face, the same grammar the board
     tiles use. */
  .face[data-tone='initiative'] {
    box-shadow: 0 0 0 2px var(--initiative);
    border-radius: var(--radius-card);
  }
  .face[data-tone='offered'] {
    box-shadow: 0 0 0 2px var(--offered);
    border-radius: var(--radius-card);
  }
  .face[data-tone=''] {
    box-shadow: none;
  }
  /* KEYBOARD FOCUS MUST NOT SCROLL THE BOARD. The resting face's lower half
     lies outside the board's overflow:hidden box, and a browser reveals a
     newly focused element by scrolling every scrollable ancestor — an
     overflow:hidden box IS one — so Tab-focusing a hand card scrolled the
     whole board up by the clip depth (measured 73-75px: the felt jumps, the
     board's top row is cut). A negative scroll-margin shrinks the reveal rect
     past the clipped half, so the face is already fully "revealed" and the
     board never scrolls; it changes nothing else (no other scroll-into-view
     targets a hand card — the transcript scroller is unrelated). The margin
     is deliberately more than the exact half (44/63) so sub-pixel rounding at
     the clip edge cannot leave a 1-2px scroll behind, and it stays relative
     to the card width like every other offset here. Measured via
     HandFan.geometry.test.ts. */
  .face {
    scroll-margin-bottom: calc(var(--card-w) * -50 / 63);
  }
  .face[data-selected] {
    box-shadow: 0 0 0 2px var(--ink), 0 0 0 4px var(--felt-sunk);
    border-radius: var(--radius-card);
  }
  .face[data-tone='initiative'][data-selected] {
    box-shadow: 0 0 0 2px var(--ink), 0 0 0 4px var(--initiative);
  }
  .face[data-tone='offered'][data-selected] {
    box-shadow: 0 0 0 2px var(--ink), 0 0 0 4px var(--offered);
  }

  /* The options affordance, anchored to the card's TOP edge. Unlike a board
     tile, a bare face has no corner that already means something (no keyword
     marks, no state band), and the hand must OPEN UPWARD — it sits at the
     board's bottom, so a menu that opened down would leave the felt. The
     badge clears the card in front of it (the overlap fan's front-most card
     is the lowest index; the cards ahead are behind it), so it is never
     covered. */
  .tile-actions {
    position: absolute;
    top: 1px;
    right: 1px;
    z-index: 20;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 2px;
    line-height: 1;
  }
  .badge,
  .action-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 2px;
    /* +25% over the pre-fb-9946410e 1.1rem, matching OptionPicker's badge. */
    min-width: 1.375rem;
    height: 1.375rem;
    padding: 0 0.25rem;
    border-radius: 3px;
    border: var(--edge-w) solid var(--edge-inst);
    background: var(--instrument);
    color: var(--ink);
    font-family: var(--font-data);
    font-size: var(--t-12);
    font-weight: 600;
    cursor: pointer;
  }
  .badge--initiative {
    background: var(--initiative);
    border-color: var(--initiative);
    color: var(--felt-sunk);
  }
  .badge--offered {
    background: var(--offered);
    border-color: var(--offered);
    color: var(--felt-sunk);
  }
  .badge.selected,
  .action-icon.selected {
    outline: 2px solid var(--ink);
    outline-offset: 1px;
  }
  .action-icon {
    width: 1.6875rem;
    padding: 0;
    font-size: calc(var(--t-14) * 1.25);
    line-height: 1;
  }
  .sel {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 1rem;
    height: 1rem;
    padding: 0 0.2rem;
    border-radius: 2px;
    background: var(--ink);
    color: var(--felt-sunk);
    font-size: var(--t-10);
    font-weight: 600;
  }
  .badge__n {
    font-size: inherit;
  }
  .badge:hover,
  .badge[aria-expanded='true'],
  .action-icon:hover {
    border-color: var(--ink-dim);
    color: var(--ink);
  }
  /* The payment shortcut is deliberately a word, not the generic star
     glyph: it is the card's primary action and should stay legible in a
     densely fanned hand.  Land plays use the same direct affordance so a
     hand land never requires opening the ACTIONS panel. */
  .payment-shortcut {
    position: absolute;
    top: 1px;
    right: 1px;
    z-index: 21;
    min-width: 2.6rem;
    height: 1.375rem;
    padding: 0 0.25rem;
    border: var(--edge-w) solid var(--offered);
    border-radius: 3px;
    background: var(--offered);
    color: var(--felt-sunk);
    font-family: var(--font-data);
    font-size: var(--t-10);
    font-weight: 700;
    cursor: pointer;
  }
  .payment-shortcut:hover { border-color: var(--ink); color: var(--felt-sunk); }
  /* The menu opens UP, above the fan, so it is never clipped by the board's
     bottom edge (the fan's own row is at the bottom of the felt). */
  .menu {
    position: absolute;
    bottom: calc(100% + 3px);
    right: 0;
    z-index: 20;
    margin: 0;
    padding: 2px;
    list-style: none;
    min-width: 9rem;
    max-width: 14rem;
    max-height: 12rem;
    overflow-y: auto;
    background: var(--instrument);
    border: var(--edge-w) solid var(--edge-inst);
    border-radius: var(--radius);
    box-shadow: var(--shadow-lift);
  }
  .menu__item {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: 0;
    border-left: 2px solid transparent;
    border-radius: 0;
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    line-height: 1.35;
    padding: var(--sp-1) var(--sp-2);
    cursor: pointer;
  }
  .menu__item:hover,
  .menu__item:focus-visible {
    background: color-mix(in srgb, var(--ink) 7%, var(--instrument));
    border-left-color: var(--ink-dim);
    color: var(--ink);
  }
</style>
