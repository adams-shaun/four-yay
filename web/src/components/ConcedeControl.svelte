<script lang="ts">
  import { pointerRelease } from '../lib/pointer';
  /**
   * The seated player's page-level concede control: the ARM-then-CONFIRM
   * two-step (R-E4-1). Presentational only — the state (SeatPanelState:
   * which option is the pending concede, the arm click, the confirm post by
   * its own wire index) stays with the route, Table.svelte, which passes the
   * two callbacks in. Owning the markup here (not inlined in Table's snippet)
   * is what lets the geometry fixture (SeatTable.geometry) render the REAL
   * control through Rail's real `logbar` snippet seam and measure it, rather
   * than probing a hand-copied replica of it (task fb-53bd45b9).
   *
   * fb-53bd45b9: the control used to be absolutely positioned at top: 3rem
   * inside the rail — an offset calibrated to clear the logbar that landed on
   * seat row 0 once the zone counts stacked two-high, its z-index: 9 painting
   * over that row's life and pile counts and eating the pile buttons' clicks.
   * It now renders inside the rail's logbar row (Table passes it to Rail as
   * the logbar snippet), in normal flex flow at the row's right edge — the
   * row is the anchor, so "floating" is structurally impossible and nothing
   * here is absolutely positioned.
   */
  let {
    confirming = false,
    busy = false,
    onArm,
    onConfirm,
  }: {
    /** confirming is the ARMED state: the second, wider button that posts. */
    confirming?: boolean;
    busy?: boolean;
    /** onArm arms the concede (Table: panel.click(concede.index)). */
    onArm: () => void;
    /** onConfirm posts the concede option (Table: panel.confirmConcede()). */
    onConfirm: () => void;
  } = $props();
</script>

<div class="concede-control">
  {#if confirming}
    <button class="confirm" type="button" data-confirm-concede use:pointerRelease onclick={onConfirm} disabled={busy}>
      Concede — confirm
    </button>
  {:else}
    <button type="button" data-concede-control use:pointerRelease onclick={onArm} disabled={busy}>
      Concede
    </button>
  {/if}
</div>

<style>
  .concede-control {
    flex: none;
  }
  .concede-control button {
    padding: var(--sp-1) var(--sp-2);
    border: 1px solid color-mix(in srgb, var(--danger) 42%, var(--edge-inst));
    border-radius: var(--radius);
    background: var(--instrument);
    color: color-mix(in srgb, var(--danger) 68%, var(--ink));
    font-size: var(--t-12);
    cursor: pointer;
  }
  .concede-control button.confirm {
    background: var(--danger);
    color: var(--felt-sunk);
    font-weight: 600;
  }
</style>
