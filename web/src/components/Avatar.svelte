<script lang="ts">
  import { images } from '../lib/images';

  /**
   * Avatar is a seat's round portrait: the art of its first commander when it
   * has one (cropped from the card image), otherwise the seat colour with the
   * name's initial. Decorative — the name beside it is the accessible label.
   */
  let { name, colour, art = null, size = 28 }: { name: string; colour: string; art?: string | null; size?: number } = $props();

  let url = $state<string | null>(null);
  $effect(() => {
    const n = art;
    let cancelled = false;
    url = null;
    if (n) void images.url(n).then((u) => { if (!cancelled) url = u; });
    return () => { cancelled = true; };
  });
  const initial = $derived((name.trim()[0] ?? '?').toUpperCase());
</script>

<span
  class="avatar"
  class:art={url !== null}
  style:--seat={colour}
  style:--size="{size}px"
  style:background-image={url ? `url(${url})` : undefined}
  aria-hidden="true"
>{#if url === null}{initial}{/if}</span>

<style>
  .avatar {
    flex: none;
    display: inline-grid;
    place-items: center;
    width: var(--size);
    height: var(--size);
    border-radius: 50%;
    background-color: color-mix(in srgb, var(--seat) 55%, var(--felt-sunk));
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--seat) 70%, transparent);
    color: var(--ink);
    font-family: var(--font-serif);
    font-weight: 700;
    font-size: calc(var(--size) * 0.5);
    line-height: 1;
  }
  /* The art box of a normal card image sits in its upper half: zoom there. */
  .avatar.art {
    background-size: 260%;
    background-position: 50% 24%;
  }
</style>
