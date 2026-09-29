<script lang="ts">
  import type { PlayerView } from '../protocol';
  import type { CardOptions } from '../lib/cardoptions';
  import type { SeatGlow } from '../lib/seatglow';
  import Avatar from './Avatar.svelte';
  import SeatCounts from './SeatCounts.svelte';

  /**
   * SeatBox is the viewer's own seat (UI rework spec §3): avatar, serif
   * name, a big life total, the public counts and the floating mana pool,
   * in the hand row's left corner. Same glows and arrow anchor as an
   * opponent's header bar, in gilt because it is you.
   */
  let {
    player,
    name,
    colour,
    active = false,
    priority = false,
    glow = { target: false, attacked: false },
    options = null,
  }: {
    player: PlayerView;
    name: string;
    colour: string;
    active?: boolean;
    priority?: boolean;
    glow?: SeatGlow;
    options?: CardOptions | null;
  } = $props();

  const MANA = ['W', 'U', 'B', 'R', 'G', 'C'] as const;
  const pool = $derived(MANA.map((s) => ({ s, n: player.pool?.[s] ?? 0 })).filter((e) => e.n > 0));
  const art = $derived(player.commanders?.[0]?.name ?? null);
</script>

<section
  class="seat-box"
  class:target={glow.target}
  class:attacked={glow.attacked}
  class:priority
  class:lost={player.lost}
  style:--seat={colour}
  data-seat-anchor={player.seat}
  data-seat-box={player.seat}
  aria-label={`${name}${active ? ', your turn' : ''}${priority ? ', you have priority' : ''}: ${player.life} life`}
>
  <div class="top">
    <Avatar {name} {colour} {art} size={36} />
    <span class="name" title={name}>{name}</span>
    {#if active}<span class="turn">Turn</span>{/if}
    <span class="life" data-life>{player.life}</span>
  </div>
  <div class="bottom">
    <SeatCounts {player} who={name} {options} />
    {#if pool.length > 0}
      <span class="pool" data-mana-pool aria-label={`Mana pool: ${pool.map((e) => `${e.n} ${e.s}`).join(', ')}`}>
        {#each pool as e (e.s)}
          <span class="pip" style={`--pip: var(--mana-${e.s.toLowerCase()})`} title={`${e.n} ${e.s} mana`}>{e.n}</span>
        {/each}
      </span>
    {/if}
  </div>
</section>

<style>
  .seat-box {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: var(--sp-1);
    min-width: 0;
    padding: var(--sp-2) var(--sp-3);
    border: 1px solid var(--edge-inst);
    border-radius: 10px;
    background: var(--instrument);
    color: var(--ink);
  }
  .seat-box.priority {
    border-color: var(--gilt);
  }
  .top {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-width: 0;
  }
  .name {
    min-width: 0;
    font-family: var(--font-serif);
    font-weight: 700;
    font-size: var(--t-20);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .turn {
    flex: none;
    padding: 0 0.45em;
    border-radius: 999px;
    background: var(--gilt);
    color: var(--felt-sunk);
    font-size: var(--t-11);
    font-weight: 600;
  }
  .life {
    margin-left: auto;
    flex: none;
    font-family: var(--font-serif);
    font-weight: 700;
    font-size: var(--t-28);
    line-height: 1;
  }
  .bottom {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-width: 0;
  }
  .pool {
    display: inline-flex;
    gap: 3px;
    margin-left: auto;
  }
  .pip {
    display: inline-grid;
    place-items: center;
    min-width: 1.3em;
    height: 1.3em;
    border-radius: 50%;
    background: var(--pip);
    color: var(--felt-sunk);
    font-size: var(--t-11);
    font-weight: 700;
  }
  .seat-box.target {
    box-shadow: 0 0 0 2px var(--verdigris), 0 0 16px color-mix(in srgb, var(--verdigris) 55%, transparent);
  }
  .seat-box.attacked {
    box-shadow: 0 0 0 2px var(--ember), 0 0 16px color-mix(in srgb, var(--ember) 55%, transparent);
  }
  .seat-box.lost {
    opacity: 0.6;
  }
</style>
