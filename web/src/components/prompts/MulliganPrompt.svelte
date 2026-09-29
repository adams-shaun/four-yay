<script lang="ts">
  import { onMount } from 'svelte';
  import type { CardView, Decision, Option, View } from '../../protocol';
  import { hotkeyAction } from '../../lib/hotkeys';
  import { keymapStore } from '../../lib/keymap.svelte';
  import { modalPickerOpen } from '../../lib/modals';
  import type { SeatPanelState } from '../../lib/seatpanel.svelte';
  import { mulliganPhase } from '../../lib/prompts/decision';
  import { digitMap, renderedOrder } from '../../lib/prompts/order';
  import CardImage from '../CardImage.svelte';
  import CardTile from '../CardTile.svelte';
  import Digit from './Digit.svelte';

  /**
   * MulliganPrompt is the London round's renderer: the keep/mulligan half
   * shows the opening hand and two numbered choices; the bottom half shows
   * the hand as toggles (bottoming is irreversible, so even a one-card
   * bottom toggles and commits with its button). Choices are resolved by
   * option KIND, never by count or label (FL-101).
   */
  let { decision, view, logic, seat }: { decision: Decision; view: View; logic: SeatPanelState; seat: number } = $props();

  const mull = $derived(mulliganPhase(decision));
  const mine = $derived(view.players.find((p) => p.seat === seat) ?? null);
  const handById = $derived(new Map((mine?.hand ?? []).map((c) => [c.id, c])));
  const digits = $derived(digitMap(renderedOrder(decision, { filter: '' })));
  const bottomCount = $derived(decision.min);

  // The engine labels these "keep" and "mulligan"; a button says what
  // pressing it does, with the server's label as the fallback.
  const CHOICE_LABEL: Record<string, string> = { keep: 'Keep this hand', mulligan: 'Mulligan' };
  function choiceLabel(o: Option): string {
    return CHOICE_LABEL[o.kind] ?? o.label;
  }
  function cardFor(o: Option): CardView | undefined {
    return o.obj === undefined ? undefined : handById.get(o.obj);
  }

  // The digit badges are the pick-N hotkeys. Their usual listener lives in
  // HotButtonStrip, which the mulligan round does not mount (controlsLive is
  // false until turn 1), so the round wires pick-N and confirm itself --
  // standing down whenever a strip IS mounted, so a key never acts twice.
  onMount(() => {
    const onKey = (e: KeyboardEvent): void => {
      if (keymapStore.capturing || document.querySelector('[data-hot-strip]') !== null) return;
      const action = hotkeyAction(e, modalPickerOpen, keymapStore.current);
      if (action === null) return;
      if (e.repeat) return;
      if (action.startsWith('pick-')) {
        if (!logic.pickHotkey(Number(action.slice(5)))) return;
      } else if (action === 'confirm' && mull?.phase === 'bottom') {
        if (!logic.canSubmit || logic.busy) return;
        logic.submit();
      } else return;
      e.preventDefault();
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  });
</script>

{#if mull !== null && mull.phase === 'keep'}
  <div class="hand" data-opening-hand data-card-count={(mine?.hand ?? []).length} style={`--n:${Math.max(1, (mine?.hand ?? []).length)}`} aria-label="Your opening hand">
    {#each mine?.hand ?? [] as c (c.id)}<CardTile card={c} />{/each}
  </div>
  <div class="choices" data-options>
    {#each mull.choices as opt (opt.index)}
      <button class="choice" class:keep={opt.kind === 'keep'} type="button" data-option={opt.index} onclick={() => logic.click(opt.index)} disabled={logic.busy}>
        <Digit n={digits.get(opt.index)} />{choiceLabel(opt)}
      </button>
    {/each}
  </div>
{:else if mull !== null && mull.phase === 'bottom'}
  <div class="hand picking" data-opening-hand data-card-count={mull.cards.length} style={`--n:${Math.max(1, mull.cards.length)}`} data-options aria-label="Choose cards to put on the bottom">
    {#each mull.cards as opt (opt.index)}
      {@const card = cardFor(opt)}
      {@const at = logic.picked.indexOf(opt.index)}
      <button class="pick" class:picked={at >= 0} type="button" data-option={opt.index} aria-pressed={at >= 0} aria-label={card ? card.name : opt.label} onclick={() => logic.toggle(opt.index)} disabled={logic.busy}>
        {#if card}<CardImage {card} />{:else}<span class="fallback">{opt.label}</span>{/if}
        <span class="key"><Digit n={digits.get(opt.index)} on={at >= 0} /></span>
        {#if at >= 0}<span class="order">{at + 1}</span>{/if}
      </button>
    {/each}
  </div>
  <div class="choices">
    <button class="choice keep" type="button" data-submit onclick={() => logic.submit()} disabled={!logic.canSubmit || logic.busy}>
      Bottom {bottomCount} {bottomCount === 1 ? 'card' : 'cards'}
    </button>
  </div>
{/if}

<style>
  /* The opening hand is one row by contract, from seven cards down to zero:
     the row divides its own width by the card count and hands the answer to
     CardTile as --card-w, capped at the shared gameplay size. Nothing wraps
     and nothing scrolls sideways. */
  .hand {
    display: flex;
    flex-wrap: nowrap;
    justify-content: center;
    align-items: flex-start;
    gap: var(--sp-2);
    padding: var(--sp-3);
    width: 100%;
    overflow: hidden;
    min-height: 0;
    container-type: inline-size;
    --card-w: min(var(--play-card-w), calc((100cqw - (var(--n) - 1) * var(--sp-2)) / var(--n)));
  }
  .hand > :global(.tile-wrap),
  .hand > .pick {
    flex: none;
    overflow: visible;
  }
  @container (max-width: 26rem) {
    .hand :global(.mana-symbols),
    .hand :global(.blank__foot) {
      display: none;
    }
  }
  .pick {
    position: relative;
    display: block;
    padding: 0;
    background: transparent;
    border: 0;
    border-radius: var(--radius-card);
    cursor: pointer;
    line-height: 0;
    flex: none;
  }
  .pick.picked {
    opacity: 0.55;
    box-shadow: 0 0 0 2px var(--gilt, #d4ad62);
  }
  .pick .key { position: absolute; bottom: 0.3em; left: 0.3em; line-height: 1; }
  .order {
    position: absolute;
    top: -0.4em;
    left: -0.4em;
    font: 600 0.6875rem/1.3 var(--font-data);
    color: var(--felt-sunk);
    background: var(--gilt, #d4ad62);
    border-radius: 2px;
    padding: 0.15em 0.3em;
  }
  .fallback {
    display: inline-block;
    padding: var(--sp-2);
    font-size: var(--t-12);
    line-height: 1.35;
    color: var(--ink-inst);
  }
  .choices {
    display: flex;
    justify-content: center;
    gap: var(--sp-2);
    padding: var(--sp-3);
    width: 100%;
  }
  .choice {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    background: transparent;
    color: var(--ink-inst);
    border: 1px solid var(--ink-faint);
    border-radius: 7px;
    padding: var(--sp-2) var(--sp-6);
    font: 600 var(--t-14) var(--font-ui);
    cursor: pointer;
  }
  .choice.keep {
    background: var(--gilt, #d4ad62);
    border-color: var(--gilt, #d4ad62);
    color: var(--felt-sunk);
  }
  .choice:hover:not(:disabled) { border-color: var(--ink-dim); }
  .choice.keep:hover:not(:disabled) { border-color: var(--gilt, #d4ad62); }
  .choice:disabled { opacity: 0.5; cursor: default; }
</style>
