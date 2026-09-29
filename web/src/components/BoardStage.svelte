<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { SeatInfo, View } from '../protocol';
  import type { Stops, TurnSide } from '../lib/autopilot';
  import type { CardOptions } from '../lib/cardoptions';
  import type { SeatCtx } from '../lib/seat';
  import type { SeatPanelState } from '../lib/seatpanel.svelte';
  import { CARD_RATIO } from '../lib/cardsizing';
  import { layoutStore } from '../lib/layouts.svelte';
  import { seatColour } from '../lib/colours';
  import { seatGlow } from '../lib/seatglow';
  import Board from './Board.svelte';
  import CentreStrip from './CentreStrip.svelte';
  import ActionCluster from './ActionCluster.svelte';
  import SeatBox from './SeatBox.svelte';

  /**
   * BoardStage is the felt: Board (the arranged seats) with the centre strip
   * between the opponents and the viewer, and the viewer's hand row below —
   * the seat box in its left corner, the hand fan (passed in by the route,
   * sized here from the profile's row unit) in the middle, and the gilt
   * action cluster in its right corner. A spectator gets no hand row and no
   * action button; the mulligan round keeps the strip at the top and no hand
   * row, so the opening hand owns the board centre (ui26).
   */
  let {
    view,
    seats,
    options = null,
    seat = null,
    stops = null,
    onToggle = null,
    mulligan = false,
    controls = null,
    controlsLive = false,
    onOpenFlow = null,
    hand = null,
  }: {
    view: View;
    seats: SeatInfo[];
    options?: CardOptions | null;
    seat?: number | null;
    stops?: Stops | null;
    onToggle?: ((step: string, side: TurnSide) => void) | null;
    mulligan?: boolean;
    controlsLive?: boolean;
    /** A live seated route supplies the one seat state every control delegates to. */
    controls?: {
      state: SeatPanelState;
      ctx: SeatCtx;
      table: string;
      match: number;
      onToggleOptions?: () => void;
    } | null;
    onOpenFlow?: (() => void) | null;
    /** the viewer's hand fan, rendered at the width and peek the layout gives it */
    hand?: Snippet<[{ cardWidth: number; visible: number; raise: boolean }]> | null;
  } = $props();

  const STRIP_H = 40;
  let stage = $state<HTMLElement | null>(null);
  let liveSplit = $state<number | null>(null);
  const own = $derived(seat !== null ? (view.players.find((p) => p.seat === seat) ?? null) : null);
  const hasHand = $derived(own !== null && hand !== null);
  const nameOf = (s: number) => seats[s]?.name || view.players.find((p) => p.seat === s)?.name || `Seat ${s}`;
</script>

<div class="board-stage" class:has-controls={controlsLive} bind:this={stage} data-board-stage>
  <Board {view} {seats} {options} stripH={STRIP_H} hand={hasHand} {mulligan} {liveSplit} ownHeader={own === null || mulligan}>
    {#snippet centre()}
      <CentreStrip
        {view}
        {seats}
        {seat}
        {stops}
        {onToggle}
        {controls}
        {controlsLive}
        {onOpenFlow}
        board={stage}
        onDrag={(v) => (liveSplit = v)}
      />
    {/snippet}
    {#snippet below({ handCardH })}
      <div class="hand-row">
        <div class="seat-slot">
          {#if own}
            <SeatBox
              player={own}
              name={nameOf(own.seat)}
              colour={seatColour(own.seat, seats)}
              active={view.active === own.seat}
              priority={view.priority === own.seat}
              glow={seatGlow(view, options, own.seat)}
              {options}
            />
          {/if}
        </div>
        <div class="hand-slot" data-hand-slot>
          {#if own && hand}
            {@render hand({ cardWidth: Math.round(handCardH * CARD_RATIO), visible: layoutStore.profile.hand.visible, raise: layoutStore.profile.hand.raise })}
          {/if}
        </div>
        <div class="action-slot">
          {#if controlsLive && controls}
            <ActionCluster {view} {seats} state={controls.state} ctx={controls.ctx} table={controls.table} match={controls.match} />
          {/if}
        </div>
      </div>
    {/snippet}
  </Board>
</div>

<style>
  .board-stage {
    position: relative;
    width: 100%;
    height: 100%;
    min-width: 0;
    /* legacy lane tokens some fixtures still read */
    --phase-lane-h: 40px;
  }
  .hand-row {
    position: relative;
    display: grid;
    grid-template-columns: minmax(10rem, 15rem) minmax(0, 1fr) minmax(11rem, 15rem);
    gap: var(--sp-3);
    height: 100%;
    padding: 0 var(--sp-3) var(--sp-2);
    align-items: end;
  }
  .seat-slot,
  .action-slot {
    position: relative;
    z-index: 8;
    align-self: end;
    min-width: 0;
  }
  .action-slot {
    display: flex;
    justify-content: flex-end;
  }
  .hand-slot {
    position: relative;
    height: 100%;
    min-width: 0;
    --own-seat-w: 0px;
  }
</style>
