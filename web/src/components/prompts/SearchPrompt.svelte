<script lang="ts">
  import type { Decision } from '../../protocol';
  import type { SeatPanelState } from '../../lib/seatpanel.svelte';
  import { searchCard, searchOptions } from '../../lib/search';
  import { CardHover } from '../../lib/carddetail.svelte';
  import { digitMap } from '../../lib/prompts/order';
  import { selectionStatus, submitLabel } from '../../lib/prompts/anatomy';
  import CardDetail from '../CardDetail.svelte';
  import PickFace from './PickFace.svelte';
  import PromptFooter, { type FooterButton } from './PromptFooter.svelte';
  import FilterInput from './FilterInput.svelte';

  /**
   * SearchPrompt is the library-search ask (fb-20260916T181754Z): a filter
   * above the card faces SORTED A→Z. The filter is display-only state on
   * the shared SeatPanelState, reset on every newly adopted decision.
   * Selection is untouched — the click path and the picked indexes are the
   * generic ones, in click order. The faces are numbered in their sorted,
   * filtered order, the same order the pick hotkeys resolve through.
   */
  let { decision, logic }: { decision: Decision; logic: SeatPanelState } = $props();

  const hover = new CardHover();
  const opts = $derived(searchOptions(decision, logic.searchFilter));
  const cards = $derived(opts.map((o) => searchCard(decision, o)));
  $effect(() => {
    hover.supervise(cards);
  });
  const digits = $derived(digitMap(opts.map((o) => o.index)));

  const footer = $derived.by((): FooterButton[] => logic.showSubmit
    ? [{ label: submitLabel(decision), onclick: () => logic.submit(), disabled: !logic.canSubmit || logic.busy, primary: true, data: { 'data-submit': true } }]
    : []);
</script>

<div class="search" data-search>
  <FilterInput {logic} attr="data-search-filter" placeholder={`Filter ${decision.options.length} cards…`} label="Filter the offered cards by name" />
  <div class="grid" data-search-grid data-options data-card-count={opts.length} aria-label="Cards the search offers, sorted and filtered">
    {#each opts as opt, i (opt.index)}
      <PickFace card={cards[i]} index={opt.index} at={logic.picked.indexOf(opt.index)} digit={digits.get(opt.index)} label={cards[i].name} busy={logic.busy} {hover} onclick={(e) => logic.click(opt.index, { holdPriority: e.ctrlKey })} />
    {/each}
    {#if cards.length === 0}
      <p class="empty">No card matches “{logic.searchFilter}”.</p>
    {/if}
  </div>
  <PromptFooter status={selectionStatus(decision, logic.picked.length)} buttons={footer} />
  {#if hover.hover.show && hover.card && hover.anchor}<CardDetail card={hover.card} anchor={hover.anchor} />{/if}
</div>

<style>
  .search { display: flex; flex-direction: column; gap: var(--sp-2); width: 100%; }
  .grid {
    display: flex;
    flex-wrap: wrap;
    align-content: flex-start;
    justify-content: center;
    gap: var(--sp-2);
    padding: var(--sp-1) 0;
    width: 100%;
    max-height: 16rem;
    overflow-y: auto;
    min-height: 0;
    --card-w: 72px;
  }
  .empty { margin: 0; padding: var(--sp-2); font-size: var(--t-12); color: var(--ink-dim); width: 100%; text-align: center; }
</style>
