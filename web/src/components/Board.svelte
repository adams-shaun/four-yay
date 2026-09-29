<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { View, SeatInfo } from '../protocol';
  import type { CardOptions } from '../lib/cardoptions';
  import { arrangeTable, type SeatCell } from '../lib/tablearrange';
  import { regionRows, slotUnits, type BoardRow } from '../lib/boardregions';
  import { seatSize, shareOpponentSize, splitHeights, type RegionDemand, type SeatSize } from '../lib/cardsizing';
  import { rowCount } from '../lib/layoutprofile';
  import { layoutStore } from '../lib/layouts.svelte';
  import { seatColour } from '../lib/colours';
  import { seatGlow } from '../lib/seatglow';
  import Quadrant from './Quadrant.svelte';

  /**
   * Board is the table: the opponents' area, the centre strip, the viewer's
   * own board and the hand row, as one CSS grid whose row heights come from
   * cardsizing.splitHeights. Seats are placed by the ONE arrangement function
   * (lib/tablearrange) and sized by the sizing rules — every full-size
   * opponent shares the smallest opponent card size, the viewer's board is
   * sized on its own. The board is measured once (bind:clientWidth/Height);
   * everything else is arithmetic, so dragging the centre bar re-computes
   * numbers rather than re-measuring. An SSR render (no measurement) lays the
   * table out at a nominal 1280×760.
   *
   * The centre strip and the hand row are snippets the stage passes in, so
   * this component knows seats and sizes, not controls. During the mulligan
   * round the strip moves to the top and there is no hand row (the opening
   * hand owns the board centre, ui26).
   *
   * The Arrows overlay is mounted at the table root (Table.svelte), not here:
   * the felt clips its content and an arrow must reach the rail.
   */
  let {
    view,
    seats,
    options = null,
    centre = null,
    below = null,
    stripH = 0,
    hand = false,
    mulligan = false,
    liveSplit = null,
    ownHeader = null,
  }: {
    /** draw a header bar on the bottom seat; default: only when the viewer is not that seat (a spectator). The stage turns it off when a seat box names the seat instead. */
    ownHeader?: boolean | null;
    /** the centre bar's split while it is being dragged: only the grid rows follow it */
    liveSplit?: number | null;
    view: View;
    seats: SeatInfo[];
    options?: CardOptions | null;
    centre?: Snippet | null;
    below?: Snippet<[{ height: number; handCardH: number }]> | null;
    stripH?: number;
    /** whether the viewer's hand row is drawn */
    hand?: boolean;
    mulligan?: boolean;
  } = $props();

  let w = $state(0);
  let h = $state(0);
  const W = $derived(w > 0 ? w : 1280);
  const H = $derived(h > 0 ? h : 760);
  const profile = $derived(layoutStore.profile);

  // Focus is per table and per session: which opponent matters changes every game.
  let focus = $state<number | null>(null);

  const arr = $derived(arrangeTable(view.players.map((p) => p.seat), view.viewer, profile.table.arrangement, focus));
  const byseat = $derived(new Map(view.players.map((p) => [p.seat, p])));
  const heights = $derived(splitHeights({
    boardH: H,
    stripH,
    split: arr.opponents.length === 0 ? 0 : profile.table.split,
    ownRows: rowCount(profile),
    hand: hand && !mulligan,
    handVisible: profile.hand.visible,
  }));

  // While the centre bar is dragged only the grid rows move (the spec's
  // "update only CSS while dragging"); card sizes follow on release.
  const gridHeights = $derived(liveSplit === null || arr.opponents.length === 0 ? heights : splitHeights({
    boardH: H,
    stripH,
    split: liveSplit,
    ownRows: rowCount(profile),
    hand: hand && !mulligan,
    handVisible: profile.hand.visible,
  }));

  const OPP_GAP = 8;
  const colCount = $derived(Math.max(1, ...arr.cells.map((c) => c.col)));
  const rowCountOpp = $derived(Math.max(1, ...arr.cells.map((c) => c.row + c.rowSpan - 1)));

  function rowsFor(seat: number, strip: boolean): BoardRow[] {
    const p = byseat.get(seat);
    return regionRows(p?.battlefield ?? [], profile, { stacking: profile.cards.stacking, creaturesOnly: strip });
  }
  function demands(rows: BoardRow[], seat: number): RegionDemand[][] {
    // a focus side strip draws no command zone (its header counts stand in)
    const cmd = strips.has(seat) ? 0 : (byseat.get(seat)?.commanders?.length ?? 0);
    return rows.map((r) => r.map((s) => ({ weight: s.weight, units: slotUnits(s) + (s.key === 'creatures' ? cmd : 0) })));
  }
  function cellBox(c: SeatCell): { w: number; h: number } {
    if (arr.mode === 'focus') {
      const mainW = ((W - OPP_GAP) * 2.2) / 3.2;
      const k = Math.max(1, rowCountOpp);
      return c.strip ? { w: W - OPP_GAP - mainW, h: (heights.oppH - (k - 1) * OPP_GAP) / k } : { w: mainW, h: heights.oppH };
    }
    return { w: (W - (colCount - 1) * OPP_GAP) / colCount, h: (heights.oppH - (rowCountOpp - 1) * OPP_GAP) / rowCountOpp };
  }

  const strips = $derived(new Set(arr.cells.filter((c) => c.strip).map((c) => c.seat)));
  const oppRows = $derived(arr.cells.map((c) => rowsFor(c.seat, c.strip)));
  const oppSizes = $derived.by((): SeatSize[] => {
    const raw = arr.cells.map((c, i) => {
      const b = cellBox(c);
      return seatSize({ panelW: b.w, panelH: b.h, header: true, strip: c.strip, rows: demands(oppRows[i], c.seat), artBelow: profile.cards.artBelow });
    });
    return shareOpponentSize(raw, arr.cells.map((c) => c.strip), profile.cards.artBelow);
  });
  const showOwnHeader = $derived(ownHeader ?? !arr.seated);
  const ownRows = $derived(arr.own === null ? [] : rowsFor(arr.own, false));
  const ownSize = $derived(arr.own === null ? null : seatSize({
    panelW: W,
    panelH: heights.ownH,
    header: showOwnHeader,
    strip: false,
    rows: demands(ownRows, arr.own),
    artBelow: profile.cards.artBelow,
  }));
  const mirrored = $derived(profile.table.orientation === 'mirrored');
  const nameOf = (seat: number) => seats[seat]?.name || byseat.get(seat)?.name || `Seat ${seat}`;
</script>

<div
  class="board"
  class:mulligan
  bind:clientWidth={w}
  bind:clientHeight={h}
  style:grid-template-rows={mulligan
    ? `${stripH}px ${gridHeights.oppH}px minmax(0, 1fr)`
    : `${gridHeights.oppH}px ${stripH}px ${gridHeights.ownH}px ${gridHeights.handH}px`}
  data-arrangement={arr.mode}
  data-seat-count={view.players.length}
>
  <div class="opps" style:grid-template-columns={arr.columns} style:grid-template-rows={arr.rows} data-opponents>
    {#each arr.cells as c, i (c.seat)}
      {@const p = byseat.get(c.seat)}
      {#if p}
        <div class="cell" class:strip={c.strip} style:grid-column="{c.col}" style:grid-row="{c.row} / span {c.rowSpan}" data-cell-seat={c.seat}>
          <Quadrant
            player={p}
            colour={seatColour(p.seat, seats)}
            name={nameOf(p.seat)}
            stack={view.stack}
            {options}
            rows={oppRows[i]}
            size={oppSizes[i]}
            {mirrored}
            header={true}
            strip={c.strip}
            active={view.active === p.seat}
            priority={view.priority === p.seat}
            glow={seatGlow(view, options, p.seat)}
            onFocus={c.strip ? () => (focus = c.seat) : null}
          />
        </div>
      {/if}
    {/each}
  </div>
  <div class="centre">{@render centre?.()}</div>
  <div class="own" data-own-board>
    {#if arr.own !== null && byseat.get(arr.own) && ownSize}
      {@const p = byseat.get(arr.own)!}
      <Quadrant
        player={p}
        colour={seatColour(p.seat, seats)}
        name={nameOf(p.seat)}
        stack={view.stack}
        {options}
        rows={ownRows}
        size={ownSize}
        header={showOwnHeader}
        active={view.active === p.seat}
        priority={view.priority === p.seat}
        glow={seatGlow(view, options, p.seat)}
      />
    {/if}
  </div>
  {#if !mulligan}
    <div class="below">{@render below?.({ height: heights.handH, handCardH: heights.handCardH })}</div>
  {/if}
</div>

<style>
  .board {
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    width: 100%;
    height: 100%;
    min-width: 0;
    /* clip, not hidden: a hidden box is still a scroll container, and a
       raised hand card taking focus scrolled the whole board up under the
       top seat's header bar. */
    overflow: clip;
  }
  .opps {
    display: grid;
    gap: 8px;
    min-height: 0;
    padding: 0 0 0 0;
  }
  .board.mulligan .opps { grid-row: 2; }
  .board.mulligan .centre { grid-row: 1; }
  .board.mulligan .own { grid-row: 3; }
  .cell {
    min-width: 0;
    min-height: 0;
  }
  .centre {
    position: relative;
    z-index: 7;
    min-width: 0;
  }
  .own {
    min-height: 0;
    min-width: 0;
  }
  .below {
    position: relative;
    min-height: 0;
    min-width: 0;
    z-index: 6;
  }
</style>
