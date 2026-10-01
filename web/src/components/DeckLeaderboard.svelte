<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchDeckStats, type DeckRecord } from '../lib/api';

  // Live deck win-rate tally: finished bot-vs-bot games only, computed by the
  // server on read (GET /api/stats/decks). Polled every POLL_MS while the page
  // is visible; a hidden tab stops polling and refreshes once when it returns.
  const POLL_MS = 5000;
  const TOP = 10;
  let decks = $state<DeckRecord[]>([]);
  let minGames = $state(10);
  let loaded = $state(false);
  let failed = $state(false);
  const rows = $derived(decks.slice(0, TOP));

  async function refresh() {
    try {
      const t = await fetchDeckStats();
      decks = t.decks;
      minGames = t.min_games;
      failed = false;
    } catch {
      failed = true;
    } finally {
      loaded = true;
    }
  }

  const pct = (r: number) => `${(r * 100).toFixed(1)}%`;
  const record = (d: DeckRecord) => `${d.wins}-${d.losses}${d.draws > 0 ? `-${d.draws}` : ''}`;

  onMount(() => {
    let timer: ReturnType<typeof setInterval> | undefined;
    const start = () => {
      if (timer !== undefined) return;
      void refresh();
      timer = setInterval(() => void refresh(), POLL_MS);
    };
    const stop = () => {
      if (timer !== undefined) clearInterval(timer);
      timer = undefined;
    };
    const onVis = () => (document.visibilityState === 'visible' ? start() : stop());
    document.addEventListener('visibilitychange', onVis);
    onVis();
    return () => {
      document.removeEventListener('visibilitychange', onVis);
      stop();
    };
  });
</script>

<section class="leaderboard" aria-label="deck win rates">
  <h2>Deck win rates</h2>
  {#if !loaded}
    <p class="note">loading</p>
  {:else if failed && rows.length === 0}
    <p class="note">tally unavailable</p>
  {:else if rows.length === 0}
    <p class="note">no finished bot games yet</p>
  {:else}
    <table>
      <thead>
        <tr><th>#</th><th class="deck">deck</th><th>win %</th><th>W-L-D</th><th>games</th></tr>
      </thead>
      <tbody>
        {#each rows as d, i (d.deck)}
          <tr>
            <td>{i + 1}</td>
            <td class="deck">{d.deck}</td>
            <td>{pct(d.win_rate)}</td>
            <td>{record(d)}</td>
            <td>{d.games}</td>
          </tr>
        {/each}
      </tbody>
    </table>
    <p class="note">bot-vs-bot games, decks with at least {minGames} games</p>
  {/if}
</section>

<style>
  .leaderboard {
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
  .note {
    margin: 0;
    font-size: var(--t-12);
    color: var(--ink-dim);
  }
  table {
    border-collapse: collapse;
    font-family: var(--font-data);
    font-size: var(--t-12);
  }
  th,
  td {
    padding: 0 var(--sp-2) 0 0;
    text-align: right;
    white-space: nowrap;
  }
  th {
    font-weight: 400;
    color: var(--ink-dim);
  }
  .deck {
    text-align: left;
    width: 100%;
    max-width: 14rem;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
