<script lang="ts">
  import type { PlayerView } from '../protocol';
  import type { CardOptions } from '../lib/cardoptions';
  import { pileTone } from '../lib/cardoptions';
  import { pileLabel, pileOpener } from '../lib/pileopener.svelte';
  import { pointerRelease } from '../lib/pointer';
  import { zonesFor } from '../lib/zones';
  import CardImage from './CardImage.svelte';

  /**
   * ZonePiles is a seat's zone-piles column (UI rework spec §2): library,
   * graveyard and exile as small piles beside the board, sized from the
   * panel height by cardsizing.pileWidth (the `width` prop). The graveyard
   * and exile show their top card and open the shared pile modal. The
   * column is not mounted at all when cards are art tiles or in a focus
   * side strip — the header counts replace it there.
   */
  let { player, who, width, options = null }: { player: PlayerView; who: string; width: number; options?: CardOptions | null } = $props();

  const zones = $derived(zonesFor(player));
</script>

<div class="piles" style:--pile-w="{width}px" data-zone-piles={player.seat}>
  <div class="pile back" data-motion-anchor={`${player.seat}:library`} title={`Library: ${player.library_size}`} aria-label={`Library, ${player.library_size} cards`}>
    <span class="oval" aria-hidden="true"></span>
    <span class="n">{player.library_size}</span>
  </div>
  {#each zones as z (z.zone)}
    {#if z.cards.length > 0}
      {@const tone = pileTone(options, z.cards)}
      <button
        type="button"
        class="pile face"
        data-pile={z.zone}
        data-motion-anchor={`${player.seat}:${z.zone}`}
        data-tone={tone === 'idle' ? undefined : tone}
        aria-label={pileLabel(who, z.zone, z.count)}
        title={pileLabel(who, z.zone, z.count)}
        use:pointerRelease
        onclick={(e) => pileOpener.open(player.seat, z.zone, e.currentTarget as HTMLElement, e.detail > 0)}
      >
        <CardImage card={z.cards[0]} pt={false} />
        <span class="n">{z.count}</span>
      </button>
    {:else}
      <div class="pile empty" data-motion-anchor={`${player.seat}:${z.zone}`} title={`${z.zone === 'graveyard' ? 'Graveyard' : 'Exile'}: ${z.count}`}>
        {#if z.count > 0}<span class="n">{z.count}</span>{/if}
      </div>
    {/if}
  {/each}
</div>

<style>
  .piles {
    flex: none;
    display: grid;
    grid-template-columns: repeat(2, var(--pile-w));
    grid-auto-rows: calc(var(--pile-w) * 88 / 63);
    gap: 6px;
    align-content: center;
    --card-w: var(--pile-w);
  }
  .pile {
    position: relative;
    width: var(--pile-w);
    aspect-ratio: 63 / 88;
    padding: 0;
    border: 0;
    border-radius: 4px;
    background: none;
    color: inherit;
    line-height: 0;
  }
  .pile.face {
    cursor: pointer;
  }
  .pile.back {
    display: grid;
    place-items: center;
    background: repeating-linear-gradient(135deg, #3a2a1c 0 6px, #33251a 6px 12px);
    box-shadow: inset 0 0 0 2px #1d140d;
  }
  .oval {
    width: 55%;
    height: 45%;
    border-radius: 50%;
    background: #8a6a3e;
    opacity: 0.8;
  }
  .pile.empty {
    border: 1px dashed var(--edge-felt);
  }
  .pile[data-tone] {
    box-shadow: 0 0 0 2px var(--verdigris);
  }
  .n {
    position: absolute;
    right: -4px;
    bottom: -4px;
    min-width: 1.5em;
    padding: 0 0.3em;
    border-radius: 999px;
    background: var(--felt-sunk);
    border: 1px solid var(--edge-felt);
    color: var(--ink);
    font-family: var(--font-data);
    font-size: var(--t-11);
    line-height: 1.5;
    text-align: center;
  }
</style>
