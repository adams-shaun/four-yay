<script lang="ts">
  import { MOTION_SPEEDS, MOTION_SPEED_LABELS } from '../lib/motion/settings';
  import { motionStore } from '../lib/motion/settings.svelte';

  /**
   * The motion speed setting (spec sub-project 5): off / fast / normal,
   * normal by default, saved in this browser. When the OS asks for reduced
   * motion, that wins and the control says so.
   */
</script>

<div class="motion-speed" data-motion-speed>
  <span id="motion-speed-label">Animations</span>
  <div class="segments" role="group" aria-labelledby="motion-speed-label">
    {#each MOTION_SPEEDS as s (s)}
      <button
        type="button"
        class="seg"
        class:on={motionStore.speed === s}
        aria-pressed={motionStore.speed === s}
        data-motion-speed-option={s}
        onclick={() => motionStore.setSpeed(s)}
      >{MOTION_SPEED_LABELS[s]}</button>
    {/each}
  </div>
</div>
{#if motionStore.reduced}
  <p class="note" data-motion-reduced>Your system asks for reduced motion, so animations are off.</p>
{/if}

<style>
  .motion-speed {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
    padding: var(--sp-2);
    border: 1px solid var(--edge-inst);
    background: var(--instrument-raised);
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    margin-top: 1px;
  }
  .segments {
    display: flex;
    gap: 1px;
    border: 1px solid var(--edge-inst);
    background: var(--edge-inst);
  }
  .seg {
    min-width: 0;
    padding: var(--sp-1) var(--sp-2);
    border: 0;
    background: var(--instrument-raised);
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-11);
    cursor: pointer;
  }
  .seg:hover { color: var(--ink); }
  .seg.on {
    background: var(--offered);
    color: var(--felt-sunk);
  }
  .note {
    margin: var(--sp-1) 0 0;
    color: var(--ink-dim);
    font-size: var(--t-11);
  }
</style>
