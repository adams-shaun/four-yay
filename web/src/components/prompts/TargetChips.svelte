<script lang="ts">
  import type { Decision, Option, View } from '../../protocol';
  import type { SeatPanelState } from '../../lib/seatpanel.svelte';
  import { findCardAnywhere } from '../../lib/board';
  import { seatColour } from '../../lib/colours';
  import { digitMap, orderedOptions } from '../../lib/prompts/order';
  import { selectionStatus, submitLabel } from '../../lib/prompts/anatomy';
  import { hoverLinkOf, promptHover } from '../../lib/prompts/hover.svelte';
  import { pointerRelease } from '../../lib/pointer';
  import ArtCrop from './ArtCrop.svelte';
  import Digit from './Digit.svelte';
  import PromptFooter, { type FooterButton } from './PromptFooter.svelte';

  /**
   * TargetChips is the `target` renderer (UI rework spec §4): one chip per
   * legal target with the card's art as a round thumbnail and its P/T (or a
   * player's life) beside the name. The board already rings the same legal
   * targets (the shared card-options index); hovering a chip sets the prompt
   * hover hook, which rings that one anchor and draws its arrow. A chip
   * answers through the seat's ordinary click path, so a single target posts
   * on the click and a multi-target ask toggles and commits.
   */
  let { decision, view, logic }: { decision: Decision; view: View; logic: SeatPanelState } = $props();

  const rows = $derived(orderedOptions(decision, { filter: '' }) ?? []);
  const digits = $derived(digitMap(rows.map((o) => o.index)));

  interface Chip { name: string; detail: string | null; card: ReturnType<typeof findCardAnywhere>; seat: number | null }
  function chipOf(o: Option): Chip {
    if (o.obj !== undefined && o.obj !== 0) {
      const card = findCardAnywhere(view, o.obj);
      if (card) {
        const creature = /\bCreature\b/.test(card.types);
        const whose = card.controller === view.viewer ? 'yours' : view.players.find((p) => p.seat === card.controller)?.name ?? null;
        return { name: card.name, detail: creature ? `${card.power}/${card.toughness}` : whose, card, seat: null };
      }
      return { name: o.label, detail: null, card: null, seat: null };
    }
    const p = view.players.find((x) => x.seat === o.player);
    if (o.kind === 'player' && p) return { name: p.seat === view.viewer ? `${p.name} (you)` : p.name, detail: `${p.life} life`, card: null, seat: p.seat };
    return { name: o.label, detail: null, card: null, seat: null };
  }

  const footer = $derived.by((): FooterButton[] => logic.showSubmit
    ? [{ label: submitLabel(decision), onclick: (e) => logic.submit(e.ctrlKey), disabled: !logic.canSubmit || logic.busy, primary: true, data: { 'data-submit': true } }]
    : []);
</script>

<div class="targets" data-target-chips>
  <div class="chips" data-options>
    {#each rows as opt (opt.index)}
      {@const chip = chipOf(opt)}
      {@const at = logic.picked.indexOf(opt.index)}
      {@const link = hoverLinkOf(opt)}
      <button
        class="chip"
        class:picked={at >= 0}
        type="button"
        data-option={opt.index}
        title={opt.label}
        aria-label={opt.label}
        aria-pressed={decision.max > 1 ? at >= 0 : undefined}
        use:pointerRelease
        onclick={(e) => logic.click(opt.index, { holdPriority: e.ctrlKey })}
        onpointerenter={() => link && promptHover.set(link)}
        onpointerleave={() => promptHover.clear()}
        onfocus={() => link && promptHover.set(link)}
        onblur={() => promptHover.clear()}
        disabled={logic.busy}
      >
        <Digit n={digits.get(opt.index)} on={at >= 0} />
        {#if chip.card}
          <ArtCrop card={chip.card} shape="thumb" />
        {:else if chip.seat !== null}
          <span class="seat-dot" style={`--seat:${seatColour(chip.seat, [])}`} aria-hidden="true">{chip.name.charAt(0)}</span>
        {/if}
        <span class="name">{chip.name}</span>
        {#if chip.detail}<small class="detail">{chip.detail}</small>{/if}
      </button>
    {/each}
  </div>
  <PromptFooter status={selectionStatus(decision, logic.picked.length)} buttons={footer} />
</div>

<style>
  .targets { display: flex; flex-direction: column; gap: var(--sp-2); width: 100%; }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    max-height: 16rem;
    overflow-y: auto;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 0.45rem;
    max-width: 100%;
    padding: 4px 11px 4px 4px;
    border-radius: 999px;
    background: var(--instrument-raised);
    border: 1px solid var(--edge-inst);
    color: var(--ink);
    font: 500 var(--t-12)/1.3 var(--font-ui);
    cursor: pointer;
  }
  .chip:hover:not(:disabled),
  .chip:focus-visible {
    border-color: var(--verdigris, #6fb7ae);
  }
  .chip.picked {
    border-color: var(--gilt, #d4ad62);
    background: color-mix(in srgb, var(--gilt, #d4ad62) 10%, var(--instrument-raised));
  }
  .name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .detail { color: var(--ink-dim); font-size: var(--t-11); white-space: nowrap; }
  .seat-dot {
    flex: none;
    width: 1.6rem;
    height: 1.6rem;
    border-radius: 50%;
    display: inline-grid;
    place-items: center;
    font: 600 var(--t-11) var(--font-ui);
    color: var(--felt-sunk);
    background: var(--seat);
  }
</style>
