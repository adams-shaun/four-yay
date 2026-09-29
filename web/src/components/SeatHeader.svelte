<script lang="ts">
  import type { PlayerView } from '../protocol';
  import type { CardOptions } from '../lib/cardoptions';
  import type { SeatGlow } from '../lib/seatglow';
  import Avatar from './Avatar.svelte';
  import SeatCounts from './SeatCounts.svelte';
  import TickNumber from './TickNumber.svelte';

  /**
   * SeatHeader is an opponent's header bar (UI rework spec §3): avatar,
   * serif name, a Turn badge on the active seat, the public counts and a
   * big life total. It glows verdigris when the pending decision offers this
   * player as a choice and ember while creatures attack them; the whole bar
   * is the player's arrow anchor ([data-seat-anchor]). In a focus side strip
   * the bar is the button that brings that opponent forward.
   */
  let {
    player,
    name,
    colour,
    active = false,
    priority = false,
    glow = { target: false, attacked: false },
    options = null,
    onFocus = null,
  }: {
    player: PlayerView;
    name: string;
    colour: string;
    active?: boolean;
    priority?: boolean;
    glow?: SeatGlow;
    options?: CardOptions | null;
    /** set on a focus side strip: clicking the bar focuses this seat */
    onFocus?: (() => void) | null;
  } = $props();

  const art = $derived(player.commanders?.[0]?.name ?? null);
  const label = $derived(`${name}${player.lost ? ', out of the game' : ''}${active ? ', their turn' : ''}${priority ? ', has priority' : ''}: ${player.life} life`);
</script>

<header
  class="seat-header"
  class:target={glow.target}
  class:attacked={glow.attacked}
  class:active
  class:lost={player.lost}
  style:--seat={colour}
  data-seat-anchor={player.seat}
  data-seat-header={player.seat}
  aria-label={label}
>
  {#if onFocus}
    <button type="button" class="focus-hit" aria-label={`Focus ${name}'s board`} onclick={onFocus}></button>
  {/if}
  <Avatar {name} {colour} {art} size={24} />
  <span class="name" title={name}>{name}</span>
  {#if active}<span class="turn">Turn</span>{/if}
  {#if priority && !active}<span class="prio" title="Has priority" aria-hidden="true"></span>{/if}
  {#if player.lost}<span class="out">Out</span>{/if}
  <SeatCounts {player} who={name} {options} />
  <span class="life" data-life data-motion-anchor={`${player.seat}:life`}><TickNumber value={player.life} /><small>life</small></span>
</header>

<style>
  .seat-header {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    height: var(--seat-header-h, 34px);
    padding: 0 var(--sp-3);
    min-width: 0;
    border-bottom: 1px solid color-mix(in srgb, var(--seat) 30%, var(--edge-felt));
    color: var(--ink);
  }
  .focus-hit {
    position: absolute;
    inset: 0;
    border: 0;
    background: none;
    cursor: pointer;
  }
  .name {
    min-width: 0;
    flex: 0 1 auto;
    font-family: var(--font-serif);
    font-weight: 700;
    font-size: var(--t-16);
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
  .prio {
    flex: none;
    width: 0.5rem;
    height: 0.5rem;
    border-radius: 50%;
    background: var(--gilt);
  }
  .out {
    flex: none;
    color: var(--ember);
    font-size: var(--t-11);
    font-weight: 600;
    text-transform: uppercase;
  }
  .life {
    margin-left: auto;
    flex: none;
    font-family: var(--font-serif);
    font-weight: 700;
    font-size: var(--t-20);
    line-height: 1;
  }
  .life small {
    margin-left: 0.25em;
    color: var(--ink-faint);
    font-family: var(--font-ui);
    font-size: var(--t-10);
    font-weight: 400;
  }
  /* The counts and the name yield before the life total does. */
  .seat-header :global([data-seat-counts]) {
    flex: 1 1 auto;
    position: relative;
  }
  .seat-header.target {
    box-shadow: inset 0 0 0 2px var(--verdigris), 0 0 14px color-mix(in srgb, var(--verdigris) 55%, transparent);
  }
  .seat-header.attacked {
    box-shadow: inset 0 0 0 2px var(--ember), 0 0 14px color-mix(in srgb, var(--ember) 55%, transparent);
  }
  .seat-header.lost .name,
  .seat-header.lost .life {
    color: var(--ink-faint);
  }
</style>
