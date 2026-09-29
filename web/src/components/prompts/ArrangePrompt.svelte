<script lang="ts">
  import type { Decision } from '../../protocol';
  import type { SeatPanelState } from '../../lib/seatpanel.svelte';
  import { arrangeCard } from '../../lib/arrange';
  import { CardHover } from '../../lib/carddetail.svelte';
  import { digitMap, renderedOrder } from '../../lib/prompts/order';
  import { selectionStatus, submitLabel } from '../../lib/prompts/anatomy';
  import ArrangeModal from '../ArrangeModal.svelte';
  import CardDetail from '../CardDetail.svelte';
  import PickFace from './PickFace.svelte';
  import PromptFooter, { type FooterButton } from './PromptFooter.svelte';

  /**
   * ArrangePrompt is the ordered-subset ask (kind `arrange`: a top-of-library
   * reorder, or Scry/Surveil): card faces in OFFERED order, each click
   * toggling it into or out of the keep pile (the picked array, in its
   * order, IS the keep pile), and the card view (ArrangeModal) for drag and
   * drop. The popup's open flag lives on the shared SeatPanelState so every
   * surface mounted against it agrees and a new decision closes it.
   */
  let { decision, logic }: { decision: Decision; logic: SeatPanelState } = $props();

  const hover = new CardHover();
  const cards = $derived(decision.options.map((o) => arrangeCard(decision, o)));
  // PANEL LIFETIME FOLLOWS THE ASK, NOT THE POINTER: when the shown card is
  // no longer offered (a seq swap under a stationary pointer), close it.
  $effect(() => {
    hover.supervise(cards);
  });
  const digits = $derived(digitMap(renderedOrder(decision, { filter: '' })));

  /** Submit both ordered piles when offered; legacy arrange answers still use the ordinary picked-order path. */
  function submitArrange(order: number[], rest?: number[]): void {
    logic.arrangeOpen = false;
    if (rest !== undefined) logic.submitArrange(order, rest);
    else {
      logic.setPicked(order);
      logic.submit();
    }
  }

  const footer = $derived.by((): FooterButton[] => [
    { label: 'Open the card view', onclick: () => (logic.arrangeOpen = true), disabled: logic.busy, data: { 'data-arrange-open': true } },
    ...(logic.showSubmit
      ? [{ label: submitLabel(decision), onclick: () => logic.submit(), disabled: !logic.canSubmit || logic.busy, primary: true, data: { 'data-submit': true } }]
      : []),
  ]);
</script>

<div class="arrange" data-arrange>
  <div class="row" data-arrange-row data-options data-card-count={decision.options.length} style={`--n:${Math.max(1, decision.options.length)}`} aria-label="Top of the library, in keep order">
    {#each decision.options as opt, i (opt.index)}
      <PickFace card={cards[i]} index={opt.index} at={logic.picked.indexOf(opt.index)} digit={digits.get(opt.index)} label={opt.label} busy={logic.busy} {hover} onclick={() => logic.toggle(opt.index)} />
    {/each}
  </div>
  <PromptFooter status={selectionStatus(decision, logic.picked.length)} buttons={footer} />
  {#if hover.hover.show && hover.card && hover.anchor}<CardDetail card={hover.card} anchor={hover.anchor} />{/if}
</div>

{#if logic.arrangeOpen}
  <ArrangeModal open={logic.arrangeOpen} {decision} seed={logic.picked} onSubmit={submitArrange} onClose={() => (logic.arrangeOpen = false)} />
{/if}

<style>
  .arrange { display: flex; flex-direction: column; gap: var(--sp-2); width: 100%; }
  .row {
    display: flex;
    flex-wrap: nowrap;
    justify-content: center;
    align-items: flex-start;
    gap: var(--sp-2);
    padding: var(--sp-2) var(--sp-1);
    width: 100%;
    overflow: hidden;
    container-type: inline-size;
    --card-w: min(var(--play-card-w), calc((100cqw - (var(--n) - 1) * var(--sp-2)) / var(--n)));
  }
  @container (max-width: 26rem) {
    .row :global(.mana-symbols),
    .row :global(.blank__foot) {
      display: none;
    }
  }
</style>
