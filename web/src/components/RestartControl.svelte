<script lang="ts">
  import { pointerRelease } from '../lib/pointer';
  /**
   * The seated player's page-level Restart control: the ARM-then-CONFIRM
   * two-step (the same R-E4-1 shape as ConcedeControl, because confirming
   * navigates the player away from the current table). Presentational only —
   * the armed state, the busy flag and the POST itself stay with the route,
   * Table.svelte, which passes the two callbacks in.
   *
   * It deliberately does NOT wear the danger colour ConcedeControl wears:
   * restart creates a new game, it destroys nothing. It renders inside the
   * rail's logbar row next to the concede control, in normal flex flow (the
   * fb-53bd45b9 ruling: no absolutely-positioned controls in this row), and
   * owning the markup here is what lets a geometry fixture render the REAL
   * control through Rail's `logbar` snippet seam.
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
    /** onArm arms the restart (Table: restartConfirming = true). */
    onArm: () => void;
    /** onConfirm creates the new game and navigates to its join path. */
    onConfirm: () => void;
  } = $props();
</script>

<div class="restart-control">
  {#if confirming}
    <button class="confirm" type="button" data-confirm-restart use:pointerRelease onclick={onConfirm} disabled={busy}>
      Restart — confirm
    </button>
  {:else}
    <button type="button" data-restart-control use:pointerRelease onclick={onArm} disabled={busy}>
      Restart
    </button>
  {/if}
</div>

<style>
  .restart-control {
    flex: none;
  }
  .restart-control button {
    padding: var(--sp-1) var(--sp-2);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    background: var(--instrument);
    color: var(--ink);
    font-size: var(--t-12);
    cursor: pointer;
  }
  .restart-control button.confirm {
    border-color: color-mix(in srgb, var(--accent, var(--edge-inst)) 55%, var(--edge-inst));
    font-weight: 600;
  }
</style>
