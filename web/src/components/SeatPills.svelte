<script lang="ts">
  import type { PlayerView, SeatInfo, View } from '../protocol';
  import type { CardOptions, OptionTone } from '../lib/cardoptions';
  import { pileTone } from '../lib/cardoptions';
  import { pileLabel, pileOpener } from '../lib/pileopener.svelte';
  import { zonesFor, type ZoneSummary } from '../lib/zones';
  import { seatColour } from '../lib/colours';

  /**
   * SeatPills is the compact, always-visible player ledger beside the control
   * strip. The board used to duplicate this information in large corner
   * plates, which consumed card space and made the phase/action instrument
   * compete with player identity. Each player now owns one non-wrapping pill:
   * priority, name, life, and public zone counts stay readable in a single
   * scan line. Full names and every count remain available to assistive tech
   * and hover readers; graveyard/exile remain real pile buttons.
   */
  let { view, seats = [], options = null, seat = null }: {
    view: View;
    seats?: SeatInfo[];
    options?: CardOptions | null;
    /** A board dock renders one player's pill; null keeps the reusable all-seat ledger. */
    seat?: number | null;
  } = $props();

  const displayedPlayers = $derived(seat === null ? view.players : view.players.filter((player) => player.seat === seat));

  const DISPLAY_MAX = 12;
  const MANA_ORDER = ['W', 'U', 'B', 'R', 'G', 'C'] as const;
  function nameFor(player: PlayerView): string {
    return seats[player.seat]?.name || player.name || `Seat ${player.seat}`;
  }
  function displayName(name: string): string {
    return name.length > DISPLAY_MAX ? `${name.slice(0, DISPLAY_MAX)}…` : name;
  }
  function zoneTone(zone: ZoneSummary): OptionTone | undefined {
    const tone = pileTone(options, zone.cards);
    return tone === 'idle' ? undefined : tone;
  }
  function openPile(player: PlayerView, zone: ZoneSummary, event: MouseEvent): void {
    pileOpener.open(player.seat, zone.zone, event.currentTarget as HTMLElement);
  }
</script>

<nav class="seat-pills" aria-label="Players" data-seat-pills>
  {#each displayedPlayers as player (player.seat)}
    {@const who = nameFor(player)}
    {@const graveyard = zonesFor(player)[0]}
    {@const exile = zonesFor(player)[1]}
    {@const pool = MANA_ORDER.map((symbol) => ({ symbol, amount: player.pool[symbol] ?? 0 })).filter((entry) => entry.amount > 0)}
    <div
      class="seat-pill"
      class:active={view.active === player.seat}
      class:priority={view.priority === player.seat}
      class:lost={player.lost}
      data-player-pill={player.seat}
      style={`--seat:${seatColour(player.seat, seats)}`}
      aria-label={`${who}${player.lost ? ', eliminated' : ''}${view.priority === player.seat ? ', has priority' : ''}: ${player.life} life, library ${player.library_size}, hand ${player.hand_size}, graveyard ${graveyard.count}, exile ${exile.count}`}
    >
      <div class="pill-summary">
        <span class="priority-dot" aria-hidden="true"></span>
        <span class="who" data-player-name={who} title={who}>{displayName(who)}</span>
        <span class="life" data-life title={`${player.life} life`}>{player.life}</span>
        <span class="counts">
        <span title={`Library: ${player.library_size}`}>L{player.library_size}</span>
        <span title={`Hand: ${player.hand_size}`}>H{player.hand_size}</span>
        {#if graveyard.cards.length > 0}
          <button
            type="button"
            class="pile-count"
            data-pile="graveyard"
            data-tone={zoneTone(graveyard)}
            aria-label={pileLabel(who, 'graveyard', graveyard.count)}
            title={pileLabel(who, 'graveyard', graveyard.count)}
            onclick={(event) => openPile(player, graveyard, event)}
          >G{graveyard.count}</button>
        {:else}
          <span title={`Graveyard: ${graveyard.count}`}>G{graveyard.count}</span>
        {/if}
        {#if exile.cards.length > 0}
          <button
            type="button"
            class="pile-count"
            data-pile="exile"
            data-tone={zoneTone(exile)}
            aria-label={pileLabel(who, 'exile', exile.count)}
            title={pileLabel(who, 'exile', exile.count)}
            onclick={(event) => openPile(player, exile, event)}
          >E{exile.count}</button>
        {:else}
          <span title={`Exile: ${exile.count}`}>E{exile.count}</span>
        {/if}
        </span>
      </div>
      <div class="mana-line" data-mana-pool aria-label={pool.length === 0 ? 'Mana pool empty' : `Mana pool: ${pool.map((entry) => `${entry.amount} ${entry.symbol}`).join(', ')}`}>
        <span class="mana-label">POOL</span>
        {#if pool.length === 0}
          <span class="mana-empty">—</span>
        {:else}
          {#each pool as entry (entry.symbol)}
            <span class="mana-entry" title={`${entry.amount} ${entry.symbol} mana`}>
              <span class="mana-pip" style={`--pip: var(--mana-${entry.symbol.toLowerCase()})`} aria-hidden="true"></span>{entry.amount}
            </span>
          {/each}
        {/if}
      </div>
    </div>
  {/each}
</nav>

<style>
  .seat-pills {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    gap: var(--sp-2);
    min-width: 0;
    max-width: 100%;
    overflow-x: auto;
    scrollbar-width: thin;
    pointer-events: auto;
  }
  .seat-pill {
    display: inline-flex;
    flex-direction: column;
    align-items: stretch;
    gap: 0.16rem;
    min-width: max-content;
    padding: var(--sp-1) var(--sp-2);
    border: 1px solid var(--edge-inst);
    border-left: 3px solid var(--seat);
    border-radius: 999px;
    background: color-mix(in srgb, var(--instrument) 94%, transparent);
    color: var(--ink-inst);
    font-family: var(--font-data);
    font-size: var(--t-11);
    font-variant-numeric: tabular-nums;
    line-height: 1;
    white-space: nowrap;
  }
  .pill-summary {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    min-width: 0;
  }
  .mana-line {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    min-height: 0.6rem;
    color: var(--ink-dim);
    font-size: 0.56rem;
    line-height: 1;
  }
  .mana-label {
    font-size: 0.5rem;
    letter-spacing: 0.06em;
    opacity: 0.75;
  }
  .mana-empty { opacity: 0.55; }
  .mana-entry {
    display: inline-flex;
    align-items: center;
    gap: 0.16rem;
    font-variant-numeric: tabular-nums;
  }
  .mana-pip {
    width: 0.45rem;
    height: 0.45rem;
    border-radius: 50%;
    background: var(--pip);
    box-shadow: inset 0 0 0 1px rgb(0 0 0 / 0.35);
  }
  .seat-pill.active {
    border-color: var(--seat);
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--seat) 45%, transparent);
  }
  .seat-pill.lost { opacity: 0.5; }
  .priority-dot {
    width: 0.42rem;
    height: 0.42rem;
    border-radius: 50%;
    background: var(--initiative);
    opacity: 0;
    flex: none;
  }
  .seat-pill.priority .priority-dot { opacity: 1; }
  .who {
    max-width: 8rem;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--ink);
    font-family: var(--font-ui);
    font-weight: 650;
  }
  .seat-pill.priority .who { color: var(--initiative); }
  .life {
    padding: 0.12rem 0.35rem;
    border: 1px solid var(--edge-inst);
    border-radius: 999px;
    color: var(--ink);
    font-size: var(--t-12);
    font-weight: 700;
  }
  .counts {
    display: inline-flex;
    gap: var(--sp-1);
    color: var(--ink-dim);
  }
  .pile-count {
    border: 0;
    padding: 0;
    background: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }
  .pile-count:hover { color: var(--ink); }
  .pile-count[data-tone='initiative'] { color: var(--initiative); }
  .pile-count[data-tone='offered'] { color: var(--offered); }
</style>
