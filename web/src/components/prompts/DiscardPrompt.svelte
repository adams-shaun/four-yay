<script lang="ts">
  import type { Decision } from '../../protocol';
  import type { SeatPanelState } from '../../lib/seatpanel.svelte';
  import { discardCard } from '../../lib/discard';
  import { CardHover } from '../../lib/carddetail.svelte';
  import { digitMap, renderedOrder } from '../../lib/prompts/order';
  import { selectionStatus, submitLabel } from '../../lib/prompts/anatomy';
  import CardDetail from '../CardDetail.svelte';
  import DiscardModal from '../DiscardModal.svelte';
  import PickFace from './PickFace.svelte';
  import PromptFooter, { type FooterButton } from './PromptFooter.svelte';

  /**
   * DiscardPrompt is the discard-pick ask (fb-20260914T120705Z) — every engine
   * shape whose options are card picks: the Thoughtseize/Mind Rot discard
   * (KModes), the cleanup discard and the cast-cost discard (KChoose). The
   * posting path is the ordinary one (a min==max==1 click posts; otherwise
   * it toggles and the commit posts), so the wire intent is byte-identical
   * to a text list's. The big card view (DiscardModal) is presentational
   * and posts through the same path.
   */
  let { decision, logic }: { decision: Decision; logic: SeatPanelState } = $props();

  const hover = new CardHover();
  const cards = $derived(decision.options.map((o) => discardCard(decision, o)));
  $effect(() => {
    hover.supervise(cards);
  });
  const digits = $derived(digitMap(renderedOrder(decision, { filter: '' })));

  function submitDiscard(): void {
    logic.discardOpen = false;
    logic.submit();
  }

  const footer = $derived.by((): FooterButton[] => [
    { label: 'Open the card view', onclick: () => (logic.discardOpen = true), disabled: logic.busy, data: { 'data-discard-open': true } },
    ...(logic.showSubmit
      ? [{ label: submitLabel(decision), onclick: () => logic.submit(), disabled: !logic.canSubmit || logic.busy, primary: true, data: { 'data-submit': true } }]
      : []),
  ]);
</script>

<div class="discard" data-discard>
  <div class="row" data-discard-row data-options data-card-count={decision.options.length} style={`--n:${Math.max(1, decision.options.length)}`} aria-label="Cards to discard">
    {#each decision.options as opt, i (opt.index)}
      <PickFace card={cards[i]} index={opt.index} at={logic.picked.indexOf(opt.index)} digit={digits.get(opt.index)} label={cards[i].name} busy={logic.busy} {hover} press={false} onclick={() => logic.click(opt.index)} />
    {/each}
  </div>
  <PromptFooter status={selectionStatus(decision, logic.picked.length)} buttons={footer} />
  {#if hover.hover.show && hover.card && hover.anchor}<CardDetail card={hover.card} anchor={hover.anchor} />{/if}
</div>

{#if logic.discardOpen}
  <DiscardModal
    open={logic.discardOpen}
    {decision}
    picked={logic.picked}
    showSubmit={logic.showSubmit}
    canSubmit={logic.canSubmit}
    busy={logic.busy}
    onPick={(index) => logic.click(index)}
    onSubmit={submitDiscard}
    onClose={() => (logic.discardOpen = false)}
  />
{/if}

<style>
  .discard { display: flex; flex-direction: column; gap: var(--sp-2); width: 100%; }
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
