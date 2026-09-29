<script lang="ts">
  import type { SeatInfo, View } from '../protocol';
  import type { CardOptions } from '../lib/cardoptions';
  import { pileOpener, pileCards, pileTitle } from '../lib/pileopener.svelte';
  import PileModal from './PileModal.svelte';

  /**
   * PileHost is the ONE PileModal instance for the whole table (task
   * fb-20260916T225802Z). SeatTable's rail pile buttons and IdentityBar's
   * graveyard/exile icons both write the shared pileOpener store
   * (lib/pileopener.svelte.ts); this host — mounted once by Table.svelte —
   * reads it and renders the modal, so two affordances can never open two
   * dialogs at once and the modal's portalled markup exists exactly once.
   * The host also hands the modal the table's card-options bundle, so the
   * pile's cards become actionable (tone ring, badge, menu — each item
   * posting the option's own index, R-E4-1); null for a spectator or when
   * nothing is pending, exactly like the board tiles read it.
   */
  let { view, seats = [], options = null }: {
    view: View;
    seats?: SeatInfo[];
    options?: CardOptions | null;
  } = $props();

  const open = $derived(pileOpener.current);
  const player = $derived(open === null ? null : (view.players.find((p) => p.seat === open?.seat) ?? null));
  const cards = $derived(open === null || player === null ? [] : pileCards(player, open.zone));
  // The display name resolves the same way the rail's row does: the table's
  // registered seat name, falling back to the wire player name, then the
  // Seat N placeholder (the ui15 seed race).
  const name = $derived(
    open === null ? '' : (seats[open.seat]?.name ?? player?.name ?? `Seat ${open.seat}`),
  );
  const title = $derived(open === null ? '' : pileTitle(name, open.zone));
</script>

<PileModal
  open={open !== null && player !== null}
  {title}
  {cards}
  {options}
  returnFocus={open !== null && !open.byPointer ? open.trigger : null}
  onClose={() => pileOpener.close()}
/>
