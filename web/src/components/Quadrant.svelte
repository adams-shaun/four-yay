<script lang="ts">
  import type { PlayerView, StackView } from '../protocol';
  import type { CardOptions } from '../lib/cardoptions';
  import { attachedTo } from '../lib/board';
  import { regionRows, type BoardRow, type RegionSlot } from '../lib/boardregions';
  import { rowFit, CARD_RATIO, GAP, type SeatSize } from '../lib/cardsizing';
  import { ANCHOR_LABELS, ORDER_LABELS, REGION_LABELS, type Overflow } from '../lib/layoutprofile';
  import type { SeatGlow } from '../lib/seatglow';
  import { layoutStore } from '../lib/layouts.svelte';
  import CardStack from './CardStack.svelte';
  import CommandArea from './CommandArea.svelte';
  import SeatHeader from './SeatHeader.svelte';
  import ZonePiles from './ZonePiles.svelte';

  /**
   * Quadrant is ONE seat's panel on the table (the name predates the 2–8
   * seat arrangement; it is no longer a quarter of anything): an optional
   * header bar, the zone-piles column, and the board template's rows of
   * regions (lib/boardregions). It has no rules knowledge: grouping,
   * ordering and stacking come from regionRows, sizes from cardsizing, and
   * placement on the table from Board's one arrangement function.
   *
   * Every seat uses the viewer's template, so an opponent's creatures are
   * always where the viewer expects them. `mirrored` flips the row order so
   * row 1 (creatures, by default) sits nearest the centre on the far side of
   * the table — the default orientation; "same as mine" leaves it unflipped.
   *
   * The command zone draws at the front of the creatures region (CZ2,
   * fb-20260917T232202Z), never in the piles column, so a castable commander
   * cannot be hidden when the column collapses.
   *
   * The keyed-each key is each group's RENDER key (the lead object's id), not
   * the mutable stacking key: a changing DOM key unmounts the tile and closes
   * an inspector the reader had open (fb-20260915T182335Z).
   */
  let {
    player,
    colour,
    name = '',
    stack = [],
    options = null,
    rows = null,
    size = null,
    mirrored = false,
    header = false,
    strip = false,
    active = false,
    priority = false,
    glow = { target: false, attacked: false },
    onFocus = null,
  }: {
    player: PlayerView;
    colour: string;
    name?: string;
    stack?: StackView[];
    options?: CardOptions | null;
    /** the seat's template rows; computed from the layout store when absent */
    rows?: BoardRow[] | null;
    /** the seat's computed card size; a nominal size when absent (SSR, fixtures) */
    size?: SeatSize | null;
    mirrored?: boolean;
    header?: boolean;
    strip?: boolean;
    active?: boolean;
    priority?: boolean;
    glow?: SeatGlow;
    onFocus?: (() => void) | null;
  } = $props();

  const profile = $derived(layoutStore.profile);
  const board = $derived(rows ?? regionRows(player.battlefield ?? [], profile, { stacking: profile.cards.stacking, creaturesOnly: strip }));
  /** the commander tile's caption (name + zone) under its face, px */
  const CMD_CAPTION = 32;
  const NOMINAL: SeatSize = { cardH: 145, cardW: 104, compact: false, pileW: 0, rowsW: 1200 };
  const sz = $derived(size ?? NOMINAL);
  const overflow = $derived<Overflow>(profile.cards.overflow);
  const who = $derived(name || player.name || `Seat ${player.seat}`);

  /** the width one group takes: a tapped pile is a landscape slot, a stacked pile adds its spine gutter */
  function groupWidth(slot: RegionSlot, i: number): number {
    const g = slot.groups[i];
    const w = g.cards.every((c) => c.tapped) ? sz.cardW / CARD_RATIO : sz.cardW;
    return w + (g.cards.length > 1 ? 24 : 0);
  }
  function fitFor(row: BoardRow, slot: RegionSlot) {
    const total = row.reduce((s, r) => s + r.weight, 0) || 1;
    const regionW = ((sz.rowsW - (row.length - 1) * GAP) * slot.weight) / total;
    const cmd = slot.key === 'creatures' && !strip ? (player.commanders?.length ?? 0) : 0;
    const widths = [...Array.from({ length: cmd }, () => sz.cardW), ...slot.groups.map((_, i) => groupWidth(slot, i))];
    return rowFit(regionW, widths, overflow);
  }
</script>

<section
  class="quadrant"
  class:lost={player.lost}
  class:mirrored
  class:strip
  class:compact={sz.compact}
  class:outlines={profile.cards.outlines}
  style:--seat={colour}
  style:--card-w="{sz.cardW}px"
  data-seat={player.seat}
  data-lost={player.lost}
>
  {#if header}
    <SeatHeader {player} name={who} {colour} {active} {priority} {glow} {options} {onFocus} />
  {/if}
  <div class="body">
    {#if sz.pileW > 0 && !strip}
      <ZonePiles {player} {who} width={sz.pileW} {options} />
    {/if}
    <div class="rows">
      {#each board as row, ri (ri)}
        <div class="row" data-board-row={ri}>
          {#each row as slot (slot.key)}
            {@const fit = fitFor(row, slot)}
            <div
              class="region"
              data-zone-row={slot.key}
              data-anchor={slot.anchor}
              data-fit={fit.mode}
              style:flex-grow={slot.weight}
              style:--spacing="{fit.spacing}px"
              style:--card-w={fit.scale !== 1 ? `${Math.round(sz.cardW * fit.scale)}px` : undefined}
            >
              {#if profile.cards.outlines}
                <span class="region-label">{REGION_LABELS[slot.key]} · {ORDER_LABELS[slot.order]} · {ANCHOR_LABELS[slot.anchor].toLowerCase()}</span>
              {/if}
              {#if slot.key === 'creatures' && !strip}
                <!-- A commander tile carries a caption under its face, so its
                     face is shrunk by that caption to keep the tile inside the row. -->
                <div class="cmd-slot" style:--card-w="{Math.max(24, Math.round(sz.cardW * Math.max(0.4, (sz.cardH - CMD_CAPTION) / sz.cardH)))}px">
                  <CommandArea {player} {stack} {options} />
                </div>
              {/if}
              {#each slot.groups as g (g.render)}
                <div class="item">
                  <CardStack group={g} attachments={g.cards.length === 1 ? attachedTo(player.battlefield, g.cards[0].id) : []} {options} art={sz.compact} />
                </div>
              {/each}
            </div>
          {/each}
        </div>
      {/each}
    </div>
  </div>
</section>

<style>
  .quadrant {
    position: relative;
    box-sizing: border-box;
    width: 100%;
    height: 100%;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    border-radius: 8px;
    background: color-mix(in srgb, var(--felt-raised) 70%, transparent);
  }
  /* An eliminated seat reads as greyed out without lying about its life
     total. A ::after scrim with backdrop-filter dims what is painted below it
     while leaving the (body-portalled) inspector untouched, and
     pointer-events:none keeps every card inspectable through it. */
  .quadrant.lost::after {
    content: '';
    position: absolute;
    inset: 0;
    z-index: 2;
    pointer-events: none;
    background: color-mix(in srgb, var(--felt-sunk) 62%, transparent);
    backdrop-filter: grayscale(0.9) brightness(0.62);
  }
  .body {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: 6px;
    padding: 6px;
  }
  .rows {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  /* Mirrored: the far side of the table reads from its rim inward, so row 1
     sits nearest the centre bar. */
  /* A focus side strip: a slimmer bar and a hairline of padding (the
     sizing maths uses the same numbers, cardsizing STRIP_*). */
  .quadrant.strip {
    --seat-header-h: 28px;
  }
  .quadrant.strip .body {
    padding: 2px;
  }
  .quadrant.strip .region {
    padding-top: 0;
  }
  .quadrant.mirrored .rows {
    flex-direction: column-reverse;
  }
  .row {
    flex: 1 1 0;
    min-height: 0;
    display: flex;
    gap: 6px;
  }
  .region {
    position: relative;
    flex: 1 1 0;
    min-width: 0;
    display: flex;
    align-items: center;
    padding-top: 4px;
  }
  .region[data-anchor='start'] { justify-content: flex-start; }
  .region[data-anchor='center'] { justify-content: safe center; }
  .region[data-anchor='end'] { justify-content: safe flex-end; }
  .region > .cmd-slot:not(:empty) + .item,
  .region > .item + .item {
    margin-left: var(--spacing, 6px);
  }
  .region[data-fit='scroll'] {
    overflow-x: auto;
    overflow-y: hidden;
    justify-content: flex-start;
    scrollbar-width: thin;
  }
  .region[data-fit='overlap'] {
    justify-content: flex-start;
  }
  .region[data-fit='wrap'] {
    flex-wrap: wrap;
    align-content: center;
    row-gap: 4px;
  }
  .region[data-fit='wrap'] > .item + .item {
    margin-left: 6px;
  }
  .item,
  .cmd-slot {
    flex: none;
    position: relative;
  }
  .cmd-slot:empty {
    display: none;
  }
  .item:hover,
  .item:focus-within {
    z-index: 3;
  }
  .quadrant.outlines .region {
    outline: 1px dashed color-mix(in srgb, var(--ink-faint) 70%, transparent);
    outline-offset: -1px;
    border-radius: 6px;
  }
  .region-label {
    position: absolute;
    top: 2px;
    left: 8px;
    z-index: 1;
    color: var(--ink-dim);
    font-size: var(--t-11);
    pointer-events: none;
  }
</style>
