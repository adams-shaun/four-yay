<script lang="ts">
  import { untrack } from 'svelte';
  import { motionStore } from '../lib/motion/settings.svelte';
  import { TIMINGS } from '../lib/motion/settings';

  /**
   * TickNumber counts a number (a life total) to its new value instead of
   * jumping (spec sub-project 5, "life changes tick"). Motion off or reduced
   * motion shows the new value at once. The server-rendered and first paint
   * is the value itself; only a later change ticks. The parent keeps its own
   * accessible label with the true value.
   */
  let { value }: { value: number } = $props();

  // svelte-ignore state_referenced_locally
  let shown = $state(value);
  let dir = $state<'up' | 'down' | null>(null);

  $effect(() => {
    const target = value;
    const speed = motionStore.effective;
    const from = untrack(() => shown);
    if (speed === 'off' || from === target) {
      shown = target;
      dir = null;
      return;
    }
    const dur = TIMINGS[speed].tick;
    dir = target > from ? 'up' : 'down';
    let raf = 0;
    let t0: number | null = null;
    const frame = (now: number) => {
      t0 ??= now;
      const k = Math.min(1, (now - t0) / dur);
      const eased = 1 - (1 - k) * (1 - k);
      shown = Math.round(from + (target - from) * eased);
      if (k < 1) raf = requestAnimationFrame(frame);
      else dir = null;
    };
    raf = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(raf);
  });
</script>

<span class="tick" class:up={dir === 'up'} class:down={dir === 'down'} data-tick>{shown}</span>

<style>
  .tick {
    font-variant-numeric: tabular-nums;
    transition: color 0.2s;
  }
  .up {
    color: #9fe0a8;
  }
  .down {
    color: #ff8a78;
  }
</style>
