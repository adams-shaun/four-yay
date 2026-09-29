<script lang="ts">
  import { onMount } from 'svelte';
  import { PROTOCOLS, activeWire, loadProtocol, saveProtocol, type WireProtocol } from '../lib/manabrew/pref';

  // The client-side "Protocol" setting (ManaBrew adapter plan): which wire a
  // seated table plays over. It is read when a table loads, so inside a
  // table (`active` given) a change offers a reconnect instead of switching
  // the live game's wire underneath the seat panel.
  const active = activeWire.current;
  let chosen = $state<WireProtocol>('native');
  onMount(() => {
    chosen = loadProtocol();
  });
  function choose(p: WireProtocol) {
    chosen = p;
    saveProtocol(p);
  }
</script>

<div class="protocol" data-protocol-setting>
  <span class="label" id="protocol-label">Protocol</span>
  <div class="segments" role="radiogroup" aria-labelledby="protocol-label">
    {#each PROTOCOLS as p (p.value)}
      <button type="button" role="radio" class="seg" class:on={chosen === p.value} aria-checked={chosen === p.value} data-protocol={p.value} onclick={() => choose(p.value)}>{p.label}</button>
    {/each}
  </div>
  {#if active !== null && active !== chosen}
    <p class="note">This table is playing over {active === 'manabrew' ? 'ManaBrew' : 'Native'}. <button type="button" class="link" data-protocol-reconnect onclick={() => location.reload()}>Reconnect now</button></p>
  {:else}
    <p class="note">Native is gorge's own stream; ManaBrew plays the seat over the ManaBrew protocol (server needs <code>-manabrew</code>). Applies when a table loads.</p>
  {/if}
</div>

<style>
  .protocol {
    display: grid;
    gap: var(--sp-1);
    font-size: var(--t-12);
    color: var(--ink-dim);
  }
  .label {
    color: var(--ink-dim);
  }
  .segments {
    display: flex;
    gap: 1px;
    border: 1px solid var(--edge-inst);
    background: var(--edge-inst);
    max-width: 16rem;
  }
  .seg {
    flex: 1 1 0;
    padding: var(--sp-1) var(--sp-2);
    border: 0;
    border-radius: 0;
    background: var(--instrument-raised);
    color: var(--ink-inst);
    font: var(--t-11) var(--font-ui);
    cursor: pointer;
  }
  .seg.on {
    background: var(--offered);
    color: var(--felt-sunk);
  }
  .note {
    margin: 0;
    font-size: var(--t-11);
  }
  .link {
    padding: 0;
    border: 0;
    background: none;
    color: var(--offered);
    text-decoration: underline;
    cursor: pointer;
    font: inherit;
  }
</style>
