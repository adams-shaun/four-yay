<script lang="ts">
  import type { PlayerView } from '../protocol';
  import type { CardOptions, OptionTone } from '../lib/cardoptions';
  import { pileTone } from '../lib/cardoptions';
  import { pileLabel, pileOpener } from '../lib/pileopener.svelte';
  import { zonesFor, type ZoneSummary } from '../lib/zones';
  import { pointerRelease } from '../lib/pointer';

  /**
   * SeatCounts is the one line of public counts a seat's header bar and seat
   * box share: Hand, Lib, Grave, Exile. Graveyard and exile are real pile
   * buttons (the shared pileOpener, so the one PileModal opens), wearing the
   * pending decision's tone when a card in them is offered something.
   */
  let { player, who, options = null }: { player: PlayerView; who: string; options?: CardOptions | null } = $props();

  const zones = $derived(zonesFor(player));
  function tone(z: ZoneSummary): OptionTone | undefined {
    const t = pileTone(options, z.cards);
    return t === 'idle' ? undefined : t;
  }
  const LABEL: Record<string, string> = { graveyard: 'Grave', exile: 'Exile' };
</script>

<span class="counts" data-seat-counts>
  <span title={`Hand: ${player.hand_size}`} data-motion-anchor={`${player.seat}:hand`}>Hand <b>{player.hand_size}</b></span>
  <span title={`Library: ${player.library_size}`} data-motion-anchor={`${player.seat}:library`}>Lib <b>{player.library_size}</b></span>
  {#each zones as z (z.zone)}
    {#if z.cards.length > 0}
      <button
        type="button"
        class="pile"
        data-pile={z.zone}
        data-motion-anchor={`${player.seat}:${z.zone}`}
        data-tone={tone(z)}
        aria-label={pileLabel(who, z.zone, z.count)}
        title={pileLabel(who, z.zone, z.count)}
        use:pointerRelease
        onclick={(e) => pileOpener.open(player.seat, z.zone, e.currentTarget as HTMLElement, e.detail > 0)}
      >{LABEL[z.zone]} <b>{z.count}</b></button>
    {:else if z.zone === 'graveyard' || z.count > 0}
      <span title={`${LABEL[z.zone]}: ${z.count}`} data-motion-anchor={`${player.seat}:${z.zone}`}>{LABEL[z.zone]} <b>{z.count}</b></span>
    {/if}
  {/each}
</span>

<style>
  .counts {
    display: inline-flex;
    align-items: baseline;
    gap: 0.7em;
    min-width: 0;
    color: var(--ink-dim);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    white-space: nowrap;
    overflow: hidden;
  }
  b {
    color: var(--ink);
    font-weight: 600;
  }
  .pile {
    padding: 0 0.2em;
    border: 1px solid transparent;
    border-radius: var(--radius);
    background: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }
  .pile:hover,
  .pile:focus-visible {
    border-color: var(--edge-felt);
    color: var(--ink);
  }
  .pile[data-tone='initiative'],
  .pile[data-tone='offered'] {
    border-color: var(--verdigris);
    color: var(--ink);
  }
</style>
