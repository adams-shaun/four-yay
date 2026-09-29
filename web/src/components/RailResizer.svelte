<script lang="ts">
  import { RAIL_MAX, RAIL_MIN, railWidthAfterArrow } from '../lib/layoutprofile';

  let { side, width, onWidth, onDrag, onReset }: {
    side: 'left' | 'right';
    width: number;
    onWidth: (width: number) => void;
    onDrag: (width: number | null) => void;
    onReset: () => void;
  } = $props();
  let el = $state<HTMLElement | null>(null);
  let dragging = $state(false);
  let pointer: number | null = null;
  let draft = $state(0);
  const shown = $derived(dragging ? draft : width);

  function move(e: PointerEvent): void {
    if (pointer !== e.pointerId || window.innerWidth <= 0) return;
    const edge = side === 'left' ? e.clientX : window.innerWidth - e.clientX;
    draft = Math.min(RAIL_MAX, Math.max(RAIL_MIN, edge / window.innerWidth));
    onDrag(draft);
  }
  function start(e: PointerEvent): void {
    if (e.button !== 0 || el === null) return;
    pointer = e.pointerId;
    dragging = true;
    draft = width;
    el.setPointerCapture(e.pointerId);
  }
  function end(e: PointerEvent): void {
    if (pointer !== e.pointerId) return;
    move(e);
    pointer = null;
    dragging = false;
    onDrag(null);
    onWidth(draft);
  }
  function cancel(e: PointerEvent): void {
    if (pointer !== e.pointerId) return;
    pointer = null;
    dragging = false;
    onDrag(null);
  }
  function key(e: KeyboardEvent): void {
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      onWidth(railWidthAfterArrow(side, e.key, width));
      e.preventDefault();
    } else if (e.key === 'Home') {
      onReset();
      e.preventDefault();
    }
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<span
  bind:this={el}
  class="rail-resizer"
  class:dragging
  role="separator"
  tabindex="0"
  aria-orientation="vertical"
  aria-label="Rail width: drag, or use arrow keys; Home resets"
  aria-valuemin={Math.round(RAIL_MIN * 100)}
  aria-valuemax={Math.round(RAIL_MAX * 100)}
  aria-valuenow={Math.round(shown * 100)}
  data-rail-resizer
  onpointerdown={start}
  onpointermove={move}
  onpointerup={end}
  onpointercancel={cancel}
  onkeydown={key}
  ondblclick={onReset}
></span>

<style>
  .rail-resizer {
    position: absolute;
    z-index: 35;
    top: 0;
    bottom: 0;
    width: 12px;
    cursor: col-resize;
    touch-action: none;
  }
  :global(.rail-right) .rail-resizer { left: -6px; }
  :global(.rail-left) .rail-resizer { right: -6px; }
  .rail-resizer:focus-visible { outline: 2px solid var(--gilt); outline-offset: -2px; }
  .rail-resizer.dragging { background: color-mix(in srgb, var(--gilt) 30%, transparent); }
</style>
