<script lang="ts">
  import { getContext } from 'svelte';
  import { STICKY_SOURCES, type StickySources } from '../lib/sticky-context';
  import StickyMarker from './StickyMarker.svelte';
  import type { CardView } from '../protocol';
  import { images } from '../lib/images';
  import ManaSymbols from './ManaSymbols.svelte';

  /**
   * Renders a card's Scryfall image when the lookup resolves, else the card
   * typeset as a blank: name and cost on the title line, the type line under
   * it, power/toughness in the corner a printed card puts it. This component
   * has no rules knowledge: it only displays fields already on `card` and asks
   * `images` to resolve `card.printing.name`.
   *
   * The blank is the NORMAL state, not a failure state. cmd/gorged ships no
   * catalog and a machine with no route to Scryfall resolves nothing, so the
   * blank is what most players will actually look at most of the time. It is
   * therefore drawn as a card — warm felt ground, a title rule, the corner
   * where the numbers live — and not as an error placeholder. The one thing
   * that does say something went wrong is the backed-off marker, and it is
   * the quietest mark on the face: twenty copies of the word "offline" across
   * a board is noise, and the fact is the same fact twenty times.
   *
   * `fallback="none"` suppresses the blank entirely, for a caller that is
   * already typesetting the card itself and would only duplicate it (the
   * hover inspector). `pt={false}` suppresses just the blank's corner
   * numbers, for a caller that overlays the CURRENT ones in the same corner
   * (the board tile) — printing both put two different 5/5s on one card.
   *
   * Width comes from --card-w / --card-w-large so a caller can scale a row of
   * tiles without this component needing to know about board layout.
   */
  let { card, size = 'tile', fallback = 'text', pt = true, onresolved }: {
    card: CardView;
    size?: 'tile' | 'large';
    fallback?: 'text' | 'none';
    pt?: boolean;
    /** Lets a detail surface choose its layout only after real art resolves. */
    onresolved?: (resolved: boolean) => void;
  } = $props();

  const stickySources = getContext<StickySources | undefined>(STICKY_SOURCES);
  const sticky = $derived(stickySources?.().includes(card.name) ?? false);

  let url = $state<string | null>(null);
  let offline = $state(false);

  $effect(() => {
    const name = card.printing.name;
    let cancelled = false;
    url = null;
    onresolved?.(false);
    images.url(name).then((u) => {
      if (!cancelled) {
        url = u;
        onresolved?.(u !== null);
      }
    });
    offline = images.offline();
    const poll = setInterval(() => (offline = images.offline()), 1000);
    return () => {
      cancelled = true;
      clearInterval(poll);
    };
  });

  const isCreature = $derived(card.types.includes('Creature'));
</script>

{#if url}
  <div class="card-image card-image--{size}">
    <img src={url} alt={card.name} loading="lazy" />
    {#if sticky}<StickyMarker label={`Sticky choices active for ${card.name}`} overlay />{/if}
  </div>
{:else if fallback === 'text'}
  <div class="card-image card-image--{size}">
    {#if sticky}<StickyMarker label={`Sticky choices active for ${card.name}`} overlay />{/if}
    <div class="blank">
      <div class="blank__title">
        <span class="blank__name">{card.name}</span>
        {#if card.mana_cost}<ManaSymbols cost={card.mana_cost} />{/if}
      </div>
      <div class="blank__well" aria-hidden="true">
        {#if offline}<span class="blank__offline" title="Card art unavailable: the image source is backed off."></span>{/if}
      </div>
      <div class="blank__types">{card.types}</div>
      <div class="blank__foot">
        {#if isCreature && pt}<span class="blank__pt data">{card.power}/{card.toughness}</span>{/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .card-image {
    position: relative;
    display: inline-flex;
    flex: none;
    aspect-ratio: 63 / 88;
    border-radius: var(--card-radius, var(--radius-card));
    overflow: hidden;
  }
  .card-image--tile {
    width: var(--card-w, 90px);
    --face-t: var(--t-10);
  }
  .card-image--large {
    width: var(--card-w-large, 220px);
    --face-t: var(--t-12);
  }
  .card-image img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  /* A card blank, not a grey box: warm felt ground in the board's own
     register, an inset rule standing in for the printed border, and the three
     things a player needs off a face they cannot see the art of. */
  .blank {
    position: relative;
    width: 100%;
    height: 100%;
    display: flex;
    flex-direction: column;
    gap: 0.3em;
    padding: 0.5em;
    box-sizing: border-box;
    /* A card is an object lying ON the felt, so the blank's ground is one step
       above the board it sits on — at --felt-raised it read as a hole cut in
       the quadrant rather than a card placed on it — with a lit edge for the
       card's own rim. */
    background: var(--edge-felt);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--ink-faint) 40%, transparent);
    color: var(--ink);
    font-size: var(--face-t, var(--t-10));
    line-height: 1.25;
  }
  .blank__title {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.4em;
    padding-bottom: 0.35em;
    border-bottom: 1px solid color-mix(in srgb, var(--ink-faint) 30%, transparent);
  }
  .blank__name {
    font-weight: 600;
    /* Two lines of name, then clip: a long name must not push the type line
       off a 90px face. */
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    overflow: hidden;
  }
  /* The art window, empty. A card blank has a place where the art goes, and
     drawing that place is what stops the middle of the face reading as a
     rendering failure: the well is recessed, so the eye takes it as part of
     the blank rather than as something that did not load. */
  .blank__well {
    position: relative;
    flex: 1;
    min-height: 0;
    background: var(--felt-sunk);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--felt) 70%, transparent);
    border-radius: 1px;
  }
  /* A printed card puts the type line under the art and the numbers in the
     bottom corner, with the text box between them. The blank keeps that
     skeleton, and the empty strip at the foot is the text box: it is also
     exactly where a board tile lays its own current-state band, so the two
     do not fight over the same pixels. */
  .blank__types {
    min-width: 0;
    padding-top: 0.35em;
    border-top: 1px solid color-mix(in srgb, var(--ink-faint) 30%, transparent);
    color: var(--ink-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .blank__foot {
    display: flex;
    align-items: flex-end;
    justify-content: flex-end;
    min-height: 1.5em;
  }
  .blank__pt {
    flex: none;
    font-size: 1.15em;
    font-weight: 500;
    color: var(--ink);
  }
  /* The whole marker is one dim dot in the corner of the empty well. */
  .blank__offline {
    position: absolute;
    right: 0.35em;
    bottom: 0.35em;
    width: 0.35em;
    height: 0.35em;
    border-radius: 999px;
    background: var(--ink-faint);
  }
</style>
