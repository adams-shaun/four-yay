<script lang="ts">
  // The ManaBrew adapter's visible voice (plan: "errors surface in a
  // banner"): a fallback to Native, a refused answer, a prompt the server
  // could not translate. Small and dismissable; it never covers the hand or
  // the action strip.
  let { text, onDismiss }: { text: string; onDismiss: () => void } = $props();
  import { pointerRelease } from '../lib/pointer';
</script>

<div class="wire-notice" role="status" data-wire-notice>
  <span>{text}</span>
  <button type="button" aria-label="Dismiss" use:pointerRelease onclick={onDismiss}>×</button>
</div>

<style>
  .wire-notice {
    position: absolute;
    top: var(--sp-2);
    left: 50%;
    transform: translateX(-50%);
    z-index: 9;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    max-width: min(40rem, calc(100% - var(--sp-8)));
    padding: var(--sp-1) var(--sp-2) var(--sp-1) var(--sp-3);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    background: var(--instrument-raised);
    color: var(--ink-inst);
    font: var(--t-12) var(--font-ui);
    box-shadow: 0 2px 8px rgb(0 0 0 / 0.35);
  }
  button {
    padding: 0 var(--sp-1);
    border: 0;
    background: none;
    color: var(--ink-dim);
    font-size: var(--t-14);
    cursor: pointer;
  }
</style>
