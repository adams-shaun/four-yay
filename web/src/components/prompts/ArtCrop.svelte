<script lang="ts">
  import type { CardView } from '../../protocol';
  import { images } from '../../lib/images';

  /**
   * ArtCrop shows a card's artwork as a crop — the prompt's banner/spine and
   * a target chip's round thumbnail. The art proxy serves the whole card
   * image, so the crop is a zoomed background positioned on the art box.
   * While the image resolves (or when there is none) it is a quiet gradient,
   * never an error mark.
   */
  let { card, name = null, shape = 'banner' }: { card?: CardView | null; name?: string | null; shape?: 'banner' | 'thumb' } = $props();

  const lookup = $derived(card?.printing?.name ?? card?.name ?? name ?? null);
  let url = $state<string | null>(null);
  $effect(() => {
    const n = lookup;
    let cancelled = false;
    url = null;
    if (n === null) return;
    images.url(n).then((u) => {
      if (!cancelled) url = u;
    });
    return () => {
      cancelled = true;
    };
  });
</script>

<span class="art {shape}" class:resolved={url !== null} style={url ? `background-image:url("${url}")` : undefined} aria-hidden="true"></span>

<style>
  .art {
    display: block;
    background-color: var(--instrument-raised);
    background-image: linear-gradient(160deg, color-mix(in srgb, var(--gilt, #d4ad62) 14%, var(--instrument-raised)), var(--instrument));
    background-repeat: no-repeat;
  }
  /* The art box sits roughly between 11% and 55% of a card's height: a
     140% zoom anchored there shows the illustration without the frame. */
  .art.resolved {
    background-size: 140% auto;
    background-position: 50% 21%;
  }
  .banner {
    width: 100%;
    height: 100%;
  }
  .thumb {
    width: 1.6rem;
    height: 1.6rem;
    border-radius: 50%;
    flex: none;
  }
  .thumb.resolved {
    background-size: 260% auto;
    background-position: 50% 18%;
  }
</style>
