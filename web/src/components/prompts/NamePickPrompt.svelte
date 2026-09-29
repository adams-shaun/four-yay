<script lang="ts">
  import type { Decision } from '../../protocol';
  import type { SeatPanelState } from '../../lib/seatpanel.svelte';
  import { NAME_PICK_RENDER_LIMIT, nameOptions } from '../../lib/name-pick';
  import { digitMap } from '../../lib/prompts/order';
  import Digit from './Digit.svelte';
  import FilterInput from './FilterInput.svelte';

  /**
   * NamePickPrompt is the "name a card" ask: a filter over the whole name
   * universe, rendering at most NAME_PICK_RENDER_LIMIT matches in A→Z
   * order. The first nine visible names are numbered, in the same order the
   * pick hotkeys resolve through.
   */
  let { decision, logic }: { decision: Decision; logic: SeatPanelState } = $props();

  const opts = $derived(nameOptions(decision, logic.searchFilter));
  const shown = $derived(opts.slice(0, NAME_PICK_RENDER_LIMIT));
  const digits = $derived(digitMap(shown.map((o) => o.index)));
</script>

<div class="name-pick" data-name-pick>
  <FilterInput {logic} attr="data-name-filter" placeholder="Filter names…" label="Filter card names" />
  <p class="count" data-name-count>{logic.searchFilter.trim() === '' ? `${opts.length.toLocaleString()} cards — type to filter` : `Showing first ${Math.min(opts.length, NAME_PICK_RENDER_LIMIT)} of ${opts.length.toLocaleString()} matches`}</p>
  <div class="list" data-name-list data-options>
    {#each shown as opt (opt.index)}
      <button class="option" type="button" data-option={opt.index} onclick={(e) => logic.click(opt.index, { holdPriority: e.ctrlKey })} disabled={logic.busy}><Digit n={digits.get(opt.index)} /><span class="label">{opt.label}</span></button>
    {/each}
    {#if opts.length === 0}<p class="empty">No card matches “{logic.searchFilter}”.</p>{/if}
  </div>
</div>

<style>
  .name-pick { display: flex; flex-direction: column; gap: var(--sp-2); width: 100%; }
  .count { margin: 0; font-size: var(--t-11); color: var(--ink-dim); }
  .list { display: flex; flex-direction: column; gap: 3px; max-height: 14rem; overflow-y: auto; min-height: 0; }
  .option {
    display: flex;
    gap: 0.6rem;
    align-items: center;
    text-align: left;
    background: var(--instrument-raised);
    color: var(--ink-inst);
    border: 1px solid transparent;
    border-radius: 6px;
    padding: 0.3rem 0.5rem;
    font: var(--t-12)/1.35 var(--font-ui);
    cursor: pointer;
  }
  .option:hover:not(:disabled) { border-color: var(--edge-inst); color: var(--ink); }
  .empty { margin: 0; padding: var(--sp-2); font-size: var(--t-12); color: var(--ink-dim); text-align: center; }
</style>
