<script lang="ts">
  import type { CardView } from '../protocol';
  import CardImage from './CardImage.svelte';

  /**
   * ZoomCard is the zoom-card view action's overlay: the card under the
   * pointer, as large as the viewport allows. It is a modal dialog, so the
   * table's hotkey guard holds every other key while it is open; Escape, the
   * zoom key again, or a click anywhere closes it.
   */
  let { card, onClose }: { card: CardView | null; onClose: () => void } = $props();

  function focusOnOpen(node: HTMLElement): void {
    node.focus();
  }
</script>

{#if card}
  <div class="backdrop" aria-hidden="true" onclick={onClose}></div>
  <div
    class="zoom"
    role="dialog"
    aria-modal="true"
    aria-label={`${card.name}, zoomed`}
    tabindex="-1"
    data-zoom-card={card.id}
    use:focusOnOpen
    onclick={onClose}
    onkeydown={(e) => { if (e.key === 'Escape') { e.preventDefault(); onClose(); } }}
  >
    <CardImage {card} size="large" />
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 60;
    background: rgb(0 0 0 / 0.55);
  }
  .zoom {
    position: fixed;
    top: 50%;
    left: 50%;
    z-index: 61;
    transform: translate(-50%, -50%);
    --card-w-large: min(calc(82vh * 63 / 88), 90vw);
    --card-radius: 14px;
    outline: none;
    cursor: zoom-out;
    filter: drop-shadow(0 18px 40px rgb(0 0 0 / 0.6));
  }
</style>
