<script lang="ts">
  import type { PlayerView, StackView } from '../protocol';
  import { commandZoneOf, stackIdsOf } from '../lib/commander';
  import type { CardOptions } from '../lib/cardoptions';
  import { tileOptions } from '../lib/cardoptions';
  import CommanderTile from './CommanderTile.svelte';

  /**
   * CommandArea projects one seat's command zone into the CREATURES row of
   * its own Quadrant (CZ2): one CommanderTile per roster commander, in
   * genesis order (CR 903.6), rendered directly as siblings of the creature
   * CardStacks that already live there — not into a private area of its own.
   *
   * WHY THE PRIVATE AREA IS GONE. It used to be a recessed, sticky band
   * pinned to the seat's own rim at land scale (80px): a commander is a
   * creature, and drawing it smaller than the row it competes with in combat
   * was the defect this component existed to fix. Quadrant.svelte now places
   * `<CommandArea>` as the first children of `.row.creatures`, so a commander
   * tile inherits that row's `--card-w: 104px` exactly the way a CardStack
   * does — same scale, same flex-wrap, same row. Losing the old area also
   * loses its rim-pinned stickiness: a commander now scrolls with the rest of
   * the creature row like any other permanent, which is the direct
   * consequence of no longer having a private layout to pin.
   *
   * ONE TILE PER COMMANDER, WHEREVER IT IS — EXCEPT THE BATTLEFIELD (ui10).
   * A commander actually IN PLAY is a battlefield permanent, and the real
   * one is already drawn in this same creatures row by the CardStack that
   * `groupBattlefield` produced from the seat's battlefield list. Drawing the
   * command-zone tile for it as well put the same card on the board twice —
   * the duplication the live demo showed and the user told us to remove.
   * So a commander whose presence resolves to 'battlefield' is SKIPPED here:
   * the tile exists to show the command zone, and a commander in play is not
   * in the command zone. A commander that is genuinely in the command zone
   * (not yet cast, or returned there) still gets its tile — removing the
   * tile in the zone is the opposite defect — and so does a commander only
   * reachable through some other zone ('away'): graveyard, exile, hand, the
   * stack, or a zone the viewer cannot browse. Those are nowhere else on the
   * board, so the command-zone tile is the one place the reader can find and
   * inspect them.
   *
   * This component renders ONE wrapping element — the command pack
   * (`.cmd-pack`, fb-20260917T232202Z) — a sub-container inside the
   * creatures row that carries the command zone's OWN layout settings: its
   * scale resolves the tiles' `--card-w` (the creatures row's resolution of
   * that property has already happened on the `.row`, so a tile-level value
   * could not reach its children) and its `data-align` packs the pack's
   * main axis independently of the creatures row's. It is a flex-wrap row
   * exactly like the row that contains it, sits FIRST among the creatures
   * row's children (the CZ2 "commanders draw first" order preserved) and
   * its tiles wrap inside it. With both settings at their defaults the pack
   * is content-sized at the row's front and renders indistinguishably from
   * the pre-pack interleaved tiles. A seat with no roster (a Constructed
   * game) still renders literally nothing — not an empty node, not a frame,
   * not a heading — so the pack is conditional on a non-empty roster (an
   * empty wrapper would insert a stray flex gap into the creatures row).
   *
   * `options` (the pending decision's card-indexed offers, forwarded from
   * Quadrant the same way every CardStack in this row already gets it) is
   * looked up per commander by its object id and handed to CommanderTile as
   * `tileOptions` — the same fact CardTile renders as a direct cast button
   * or menu badge. Before this, a commander's cast option was still posted
   * on the wire (keyed by its id like any other object) but never reached
   * this tile, so casting a commander was reachable only through the seat
   * panel's generic ACTIONS list, never by clicking the card itself.
   */
  let { player, stack = [], options = null }: { player: PlayerView; stack?: StackView[]; options?: CardOptions | null } = $props();

  const commanders = $derived(
    commandZoneOf(player, stackIdsOf(stack)).filter((c) => c.presence !== 'battlefield'),
  );
</script>

{#if commanders.length > 0}
  <!-- The command pack: the seat's command zone, rendered as the creatures
       region's first flex item at the region's own card size (the layout
       profile's sizing rules replaced the zone's separate scale). Conditional
       on a non-empty roster so a constructed seat (or a moment when every
       commander is on the battlefield) still renders literally nothing — an
       empty wrapper would insert a stray row gap. -->
  <div class="cmd-pack" data-cmd-pack="">
    {#each commanders as c (c.commander.id)}
      <CommanderTile status={c} player={player.name} seat={player.seat} tileOptions={options ? tileOptions(options, c.commander.id) : null} />
    {/each}
  </div>
{/if}

<style>
  /* The command pack is one flex item at the front of the creatures region:
     a sub-row of commander tiles reading the region's ambient --card-w. */
  .cmd-pack {
    display: flex;
    flex: none;
    gap: var(--sp-2);
    align-items: flex-start;
    min-width: 0;
  }
</style>
