<script lang="ts">
  import type { PlayerView } from '../protocol';
  import type { CardOptions } from '../lib/cardoptions';
  import type { SeatGlow } from '../lib/seatglow';
  import Avatar from './Avatar.svelte';
  import SeatCounts from './SeatCounts.svelte';
  import TickNumber from './TickNumber.svelte';
  import ManaPool from './ManaPool.svelte';

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
    <span class="life" data-life data-motion-anchor={`${player.seat}:life`}><TickNumber value={player.life} /></span>
  </div>
  <div class="bottom">
    <SeatCounts {player} who={name} {options} />
    <ManaPool pool={player.pool} poolRestrictions={player.pool_restrictions} />
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
  /* The counts line and the mana pool each get a full-width row. The own
     seat slot is only ~160–240px wide, and the counts alone need ~200px, so
     the counts WRAP rather than clip (SeatCounts' own overflow:hidden would
     cut the right-hand end — the Grave and Exile pile buttons — off), and
     the pool, whose persistent restriction text is ~220px, wraps too instead
     of forcing the row wider than the box. */
  .bottom {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: var(--sp-1);
    min-width: 0;
  }
  .bottom :global([data-seat-counts]) {
    flex-wrap: wrap;
    row-gap: 0.15em;
    overflow: visible;
  }
  .bottom :global([data-mana-readout]) {
    justify-content: flex-start;
  }
  .bottom :global([data-mana-pool]) {
    flex-wrap: wrap;
    min-width: 0;
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
