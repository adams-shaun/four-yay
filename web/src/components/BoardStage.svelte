<script lang="ts">
  import type { SeatInfo, View } from '../protocol';
  import type { Stops, TurnSide } from '../lib/autopilot';
  import type { CardOptions } from '../lib/cardoptions';
  import type { SeatCtx } from '../lib/seat';
  import type { SeatPanelState } from '../lib/seatpanel.svelte';
  import Board from './Board.svelte';
  import HotButtonStrip from './HotButtonStrip.svelte';
  import PhaseTrack from './PhaseTrack.svelte';

  /**
   * BoardStage owns the one piece of geometry Board and PhaseTrack share: the
   * clock's lane through the felt. During play Board reserves the lane and the
   * painted track is centred in it. Mulligan keeps ui26's separate top lane so
   * the opening-hand panel retains the board centre.
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
  }: {
    view: View;
    seats: SeatInfo[];
    options?: CardOptions | null;
    seat?: number | null;
    stops?: Stops | null;
    onToggle?: ((step: string, side: TurnSide) => void) | null;
    mulligan?: boolean;
    controlsLive?: boolean;
    /** A live seated route supplies the one seat state every tab delegates to. */
    controls?: {
      state: SeatPanelState;
      ctx: SeatCtx;
      table: string;
      match: number;
      onToggleOptions?: () => void;
    } | null;
  } = $props();
</script>

<div class="board-stage" class:has-controls={controlsLive} data-board-stage>
  <Board {view} {seats} {options} reserveCentre={!mulligan} />
  <div class="phase-shard" class:mulligan data-phase-lane>
    <div class="phase-instrument" data-centre-instrument>
      <PhaseTrack {view} {seats} {seat} {stops} {onToggle} />
      {#if controlsLive}
        <HotButtonStrip {view} {seats} state={controls!.state} ctx={controls!.ctx} table={controls!.table} match={controls!.match} onToggleOptions={controls!.onToggleOptions} />
      {/if}
    </div>
  </div>
</div>

<style>
  .board-stage {
    position: relative;
    width: 100%;
    height: 100%;
    min-width: 0;
    /* Spectators reserve only the clock. A live seat reserves the attached
       tabs as well; Board reads this same value, so paint and geometry cannot
       drift apart. */
    --phase-lane-h: var(--phase-track-row-h);
  }
  .board-stage.has-controls {
    --phase-lane-h: var(--phase-instrument-h);
  }
  .phase-shard {
    position: absolute;
    top: 50%;
    left: 0;
    right: 0;
    transform: translateY(-50%);
    z-index: 7;
    min-width: 0;
  }
  .phase-instrument {
    width: 100%;
    height: var(--phase-lane-h);
    min-width: 0;
  }
  /* Mulligan keeps the centre for its hand; the clock retains ui26's compact
     top lane for that one decision instead of competing for the same pixels. */
  .phase-shard.mulligan {
    top: 0;
    transform: none;
  }
</style>
