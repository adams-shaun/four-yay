<script lang="ts">
  import type { SeatPanelState } from '../../lib/seatpanel.svelte';

  /**
   * FilterInput is the picker filter the search and name-pick renderers
   * share: display-only text on the seat state (reset per decision).
   * Escape in the field clears it and never submits or closes anything; the
   * panel's window-capture Escape (the run's panic key) has already run.
   * Every table hotkey — the pick digits included — is guarded off while
   * focus is in an input, so typing filters, never answers.
   */
  let { logic, attr, placeholder, label }: { logic: SeatPanelState; attr: string; placeholder: string; label: string } = $props();
</script>

<input
  class="filter"
  type="search"
  {...{ [attr]: true }}
  {placeholder}
  aria-label={label}
  autocomplete="off"
  spellcheck="false"
  bind:value={logic.searchFilter}
  disabled={logic.busy}
  onkeydown={(e) => {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    logic.searchFilter = '';
  }}
/>

<style>
  .filter {
    width: 100%;
    box-sizing: border-box;
    background: var(--instrument-raised);
    color: var(--ink);
    border: 1px solid var(--edge-inst);
    border-radius: 7px;
    padding: var(--sp-1) var(--sp-2);
    font: var(--t-12) var(--font-ui);
  }
  .filter::placeholder { color: var(--ink-faint); }
  .filter:focus { outline: 1px solid var(--verdigris, #6fb7ae); outline-offset: -1px; }
</style>
