<script lang="ts">
  import { getContext } from 'svelte';
  import { REPEAT_ARM, type RepeatArmContext } from '../lib/repeat-context';
  import { REPEAT_MAX, repeatTarget } from '../lib/repeat';

  let { source, stackId }: { source?: number; stackId?: number } = $props();
  const controls = getContext<RepeatArmContext | undefined>(REPEAT_ARM);
  const candidate = $derived(controls?.candidate());
  const shown = $derived(candidate && (source !== undefined ? candidate.source === source : candidate.stackId === stackId));
  let count = $state<number | undefined>(10);
  function arm() {
    count = repeatTarget(count ?? 10);
    controls?.arm(count);
  }
</script>

{#if shown}
  <span class="repeat-arm" data-repeat-arm>
    <button type="button" disabled={controls?.disabled()} onclick={arm} title="The manual activation counts as iteration 1">Repeat ×N</button>
    <input type="number" min="1" max={REPEAT_MAX} step="1" bind:value={count}
      aria-label={`Repeat count for ${candidate?.sourceName}`} onchange={() => count = repeatTarget(count ?? 10)} />
  </span>
{/if}

<style>
  .repeat-arm { display: inline-flex; gap: .2rem; align-items: center; font-size: .7rem; }
  button, input { font: inherit; }
  input { width: 3.5rem; }
</style>
