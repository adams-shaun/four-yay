<script lang="ts">
  /**
   * Digit is a numbered option's key cap: the `1`-`9` the pick hotkeys
   * answer (UI rework spec §4, numbered options). The number comes from the
   * decision's rendered order (lib/prompts/order.ts), so the cap and the key
   * cannot disagree. Absent past the ninth row.
   */
  let { n, on = false }: { n: number | undefined; on?: boolean } = $props();
</script>

{#if n !== undefined}<kbd class="digit" class:on data-digit={n} aria-hidden="true">{n}</kbd>{/if}

<style>
  .digit {
    flex: none;
    display: inline-block;
    min-width: 1.25rem;
    height: 1.25rem;
    padding: 0 0.2rem;
    box-sizing: border-box;
    font: 600 0.6875rem/1.15rem var(--font-data);
    text-align: center;
    border-radius: 5px;
    background: var(--instrument);
    border: 1px solid var(--edge-inst);
    color: var(--ink-dim);
  }
  .digit.on {
    background: var(--gilt, #d4ad62);
    border-color: var(--gilt, #d4ad62);
    color: var(--felt-sunk);
  }
</style>
