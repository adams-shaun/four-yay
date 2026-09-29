<script lang="ts">
  import type { CardView } from '../../protocol';
  import type { CardHover } from '../../lib/carddetail.svelte';
  import CardImage from '../CardImage.svelte';
  import Digit from './Digit.svelte';

  /**
   * PickFace is one card-face option of the card-pick renderers (arrange,
   * discard, library search): the face, its key cap, and — once picked —
   * its pick ordinal. The shared CardHover gives it the dwell/focus
   * inspector every card surface has (fb-20260914T063020Z Job 1); `press`
   * adds the touch long-press the arrange and search rows had.
   */
  let { card, index, at, digit, label, busy, hover, press = true, onclick }: {
    card: CardView;
    index: number;
    /** Position in the picked list, or -1. */
    at: number;
    digit: number | undefined;
    label: string;
    busy: boolean;
    hover: CardHover;
    press?: boolean;
    onclick: (e: MouseEvent) => void;
  } = $props();
</script>

<button
  class="pick"
  class:picked={at >= 0}
  type="button"
  data-option={index}
  aria-pressed={at >= 0}
  aria-label={label}
  onpointerenter={(e) => hover.arm(card, e.currentTarget)}
  onpointerleave={() => hover.leave(card)}
  onpointerdown={(e) => press && hover.pointerdown(card, e.currentTarget)}
  onpointerup={() => press && hover.pointerup(card)}
  onfocus={(e) => hover.open(card, e.currentTarget)}
  onblur={() => hover.blur(card)}
  onkeydown={(e) => hover.keydown(e)}
  aria-describedby={hover.hover.show && hover.card?.id === card.id ? `card-detail-${card.id}` : undefined}
  {onclick}
  disabled={busy}
>
  <CardImage {card} />
  {#if digit !== undefined}<span class="key"><Digit n={digit} on={at >= 0} /></span>{/if}
  {#if at >= 0}<span class="order">{at + 1}</span>{/if}
</button>

<style>
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
    overflow: visible;
  }
  /* A chosen card reads as leaving: dimmed, ringed, with its ordinal. */
  .pick.picked {
    opacity: 0.55;
    box-shadow: 0 0 0 2px var(--gilt, #d4ad62);
  }
  .key { position: absolute; bottom: 0.3em; left: 0.3em; line-height: 1; }
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
</style>
