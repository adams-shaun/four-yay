<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchDecks, type DeckInfo } from '../lib/api';
  import ProtocolSetting from './ProtocolSetting.svelte';
  import { VS_BOT_FORMATS, VS_BOT_MULLIGANS, startPlayVsBot, type VsBotFormat, type VsBotMulligans } from '../lib/playvsbot';

  // The landing-page entry point that sits the player down 1v1 against a bot.
  // Deck ids come only from the server catalogue; empty values deliberately
  // stay the original random-assignment path.
  let format: VsBotFormat = $state('constructed');
  let humanDeck = $state('');
  let botDeck = $state('');
  let mulligans: VsBotMulligans = $state(6);
  let decks = $state<DeckInfo[]>([]);
  let loadingDecks = $state(true);
  let busy = $state(false);
  let error = $state<string | null>(null);
  const formatDecks = $derived(decks.filter((d) => d.format === format));

  onMount(() => {
    void loadDecks();
  });

  async function loadDecks() {
    try {
      decks = await fetchDecks();
    } catch (e) {
      error = e instanceof Error ? `Could not load decks: ${e.message}` : String(e);
    } finally {
      loadingDecks = false;
    }
  }

  function chooseFormat(next: VsBotFormat) {
    format = next;
    humanDeck = '';
    botDeck = '';
  }

  function deckLabel(d: DeckInfo): string {
    const details = [d.archetype, d.commander ? `Commander: ${d.commander}` : ''].filter(Boolean);
    return details.length === 0 ? d.name : `${d.name} — ${details.join(' · ')}`;
  }

  async function start() {
    busy = true;
    error = null;
    try {
      const join = await startPlayVsBot(format, humanDeck, botDeck, mulligans);
      window.location.href = join;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      busy = false;
    }
  }
</script>

<div class="playvsbot" data-testid="playvsbot">
  <h2>Play 1v1 vs a bot</h2>
  <p class="hint">Choose the matchup, or leave either seat random. The match replays from its seed.</p>
  <div class="formats" role="radiogroup" aria-label="format">
    {#each VS_BOT_FORMATS as f (f.value)}
      <label class="format">
        <input type="radio" name="vsbot-format" value={f.value} checked={format === f.value} onchange={() => chooseFormat(f.value)} />
        <span>{f.label}</span>
      </label>
    {/each}
  </div>
  <label class="mulligans">
    <span>Mulligans</span>
    <select aria-label="Mulligans" bind:value={mulligans} data-testid="mulligans">
      {#each VS_BOT_MULLIGANS as n (n)}
        <option value={n}>{n}</option>
      {/each}
    </select>
    <span class="mulligan-hint">each redraw adds one card you put on the bottom</span>
  </label>
  <div class="pickers" aria-busy={loadingDecks}>
    <label class="picker">
      <span>Your deck</span>
      <select aria-label="Your deck" bind:value={humanDeck} data-testid="human-deck">
        <option value="">Random</option>
        {#each formatDecks as d (d.id)}
          <option value={d.id}>{deckLabel(d)}</option>
        {/each}
      </select>
    </label>
    <label class="picker">
      <span>Bot deck</span>
      <select aria-label="Bot deck" bind:value={botDeck} data-testid="bot-deck">
        <option value="">Random</option>
        {#each formatDecks as d (d.id)}
          <option value={d.id}>{deckLabel(d)}</option>
        {/each}
      </select>
    </label>
  </div>
  <ProtocolSetting />
  <button type="button" onclick={start} disabled={busy}>
    {busy ? 'Starting…' : 'Start game'}
  </button>
  {#if error}<p class="error">{error}</p>{/if}
</div>

<style>
  .playvsbot {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    padding: var(--sp-4);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    background: var(--instrument);
    color: var(--ink-inst);
    max-width: 32rem;
    flex: none;
  }
  h2 {
    margin: 0;
    font-size: var(--t-16);
    font-weight: 600;
  }
  .hint {
    margin: 0;
    font-size: var(--t-12);
    color: var(--ink-dim);
  }
  .formats {
    display: flex;
    gap: var(--sp-3);
  }
  .mulligans {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    font-size: var(--t-12);
    color: var(--ink-dim);
  }
  .mulligans select {
    width: auto;
    padding: var(--sp-1) var(--sp-2);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    background: var(--instrument-raised);
    color: var(--ink-inst);
    font: var(--t-14) var(--font-ui);
  }
  .mulligan-hint {
    color: var(--ink-dim);
  }
  .format {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    font-size: var(--t-14);
  }
  .pickers {
    display: grid;
    gap: var(--sp-2);
  }
  .picker {
    display: grid;
    gap: var(--sp-1);
    font-size: var(--t-12);
    color: var(--ink-dim);
  }
  select {
    width: 100%;
    padding: var(--sp-2);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    background: var(--instrument-raised);
    color: var(--ink-inst);
    font: var(--t-14) var(--font-ui);
  }
  button {
    align-self: flex-start;
    padding: var(--sp-2) var(--sp-3);
    border: 1px solid var(--edge-inst);
    border-radius: var(--radius);
    background: var(--felt);
    color: var(--ink);
    font-size: var(--t-14);
    cursor: pointer;
  }
  button:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .error {
    margin: 0;
    font-size: var(--t-12);
    color: var(--danger);
  }
</style>
