<script lang="ts">
  import type { CardView } from '../protocol';
  import { stackFaces, type CardStackGroup } from '../lib/board';
  import type { CardOptions, TileOptions } from '../lib/cardoptions';
  import { tileOptions, tileOptionsMany } from '../lib/cardoptions';
  import CardTile from './CardTile.svelte';

  /**
   * CardStack renders one stackIdentical group: a bare CardTile for a group
   * of one — pixel-for-pixel what the quadrant rendered before stacking — or
   * a single tile with a shallow fan and a count badge for a group of many.
   * Activating a stacked group expands it in place to every member;
   * activating again collapses. That state is this component's alone: nothing
   * on the wire drives it, and nothing outside reads it. Attachments only
   * ever reach a group of one — a permanent with an attachment is individual
   * by definition and never stacks — so the fan has no riders to compose.
   *
   * THE PILE. Six identical Islands are one pile of cards on a table, and a
   * pile of cards is square: offset, not splayed. The two ghost layers step
   * up and to the left by a fixed amount, exposing a spine along the left
   * edge, and the count sits ON that spine like the tab on a pile you have
   * squared up — not floating in a corner as a badge. Which is also why the
   * spine is on the left: every other corner of the tile already means
   * something (keyword marks, counters, the combat numbers), and the gutter
   * outside the card's left edge is the only place left that is the pile's
   * own rather than the card's.
   *
   * THE AFFORDANCE. Pointing at the pile pushes the ghosts further out and
   * warms the tab: the gesture the tile is about to make, made small first.
   * That is the discoverability — motion answering a pointer, not an ambient
   * animation. Deliberately NOT a title tooltip: pointing at the pile already
   * opens the card inspector, and a second floating box over it is noise.
   *
   * The first card keeps its data-obj anchor (on the CardTile itself), and
   * the group's every member id is on `data-obj-group` so a later task can
   * draw arrows to each member of a stacked tile.
   */
  let { group, size = 'tile', attachments = [], options = null, art = false }: { group: CardStackGroup; size?: 'tile' | 'large'; attachments?: CardView[]; options?: CardOptions | null; /** render every face as an art tile (CardTile's `art`) */ art?: boolean } = $props();

  let expanded = $state(false);
  const faces = $derived(stackFaces(group, expanded));
  const accessibleLabel = $derived(
    expanded
      ? `${group.cards.length} copies of ${group.cards[0].name}, expanded`
      : `${group.cards.length} copies of ${group.cards[0].name}`,
  );
  const memberIds = $derived(group.cards.map((c) => c.id).join(','));

  // fb-20260916T201423Z: the lands row merges tapped and untapped members
  // into one pile (Quadrant passes ignoreTapped), so the readiness the split
  // used to carry moves onto the tab: a collapsed pile whose members MIX
  // tapped states shows the untapped count as a second mini-plate above the
  // count. Derived from the group itself, not from a flag — a creature (or
  // others-row) group can never mix, because its stack key still includes
  // tapped, so this plate fires only where the merge happens. Uniform piles
  // — all untapped or all tapped — keep the shipped xN tab alone. Members
  // stay id-sorted, so a tap/untap never moves the lead and never churns the
  // render key (fb-20260915T182335Z).
  const tappedCount = $derived(group.cards.filter((c) => c.tapped).length);
  const mixedTapped = $derived(tappedCount > 0 && tappedCount < group.cards.length);
  const readyCount = $derived(group.cards.length - tappedCount);

  // fb-20260917T004545Z: the collapsed face presents the PILE's readiness,
  // not the lead member's rotation. A merged lands pile mixes tapped states
  // (only the lands row merges tapped), and rendering the lead's own rotation
  // made ONE member's tap rotate the whole pile's silhouette while the tap
  // badge (the union: the next tap takes the next READY member) stayed up —
  // "i clicked one and it rotated both cards, retaining a tap icon". So the
  // collapsed face reads rotated only when EVERY member is tapped (the pile
  // is then genuinely inert — the union offers nothing, so no badge shows);
  // while any member is ready, the face presents ready and the ready plate
  // carries the tapped/ready counts. For a uniform pile the value always
  // equals the lead's own state, so every non-lands row (whose key still
  // includes tapped, hence can never mix) is byte-identical to before.
  // Deliberately the lead stays the face: the data-obj anchor (what arrows
  // target) and the inspector's subject are untouched, and only the rotation
  // is presentation — pinned in CardStack.test.ts.
  const collapsedFaceTapped = $derived(readyCount === 0);

  // fb-20260918T010805Z: on a COLLAPSED MIXED pile the ready plate alone was
  // the tapped signal, and the player overlooked it twice — "should show
  // tapped card in the same pile". So the pile renders a SECOND face: the
  // quarter-turned representative (the lowest-id tapped member — cards are
  // id-sorted, so this is deterministic and a tap/untap never re-keys it)
  // lying across the pile's corner, while the lead face above keeps its ready
  // presentation, its data-obj anchor and the inspector's subject. The
  // historical constraints all hold: the pile never splits into two piles by
  // tapped state (fb-20260916T201423Z — this is one pile with a visible tapped
  // member in it), the whole-pile silhouette never rotates from one member's
  // tap (fb-20260917T004545Z — the lead face stays ready until EVERY member
  // is tapped), and the render key is still the lead id so nothing
  // unmounts (fb-20260915T182335Z). Uniform piles and expanded piles never
  // mix, so they never render the representative and are byte-identical to
  // before. Deliberately a full CardTile, not a third small plate: the plate
  // is the thing the player already overlooked.
  const tappedRep = $derived(mixedTapped ? (group.cards.find((c) => c.tapped) ?? null) : null);

  // One tile's options depends on the pending decision offered THIS object
  // (per member when expanded, or the whole pile when collapsed — stack
  // members are interchangeable, so the pile's options are the union).
  const ids = $derived(group.cards.map((c) => c.id));
  // When the group is collapsed the single rendered tile speaks for every
  // member, so it carries the aggregate of all members' options (the pile
  // is marked and actionable if any member is offered anything). Expanded,
  // each member tile carries its own options.
  const collapsedOptions = $derived(options ? tileOptionsMany(options, ids) : null);
  const singleOptions = $derived(options ? tileOptions(options, group.cards[0].id) : null);
  function memberOptions(id: number): TileOptions | null {
    return options ? tileOptions(options, id) : null;
  }
</script>

{#if group.cards.length === 1}
  <CardTile card={group.cards[0]} {size} {attachments} {art} tileOptions={singleOptions} />
{:else}
  <button
    type="button"
    class="stacked"
    class:expanded
    data-obj-group={memberIds}
    aria-expanded={expanded}
    aria-label={accessibleLabel}
    onclick={() => (expanded = !expanded)}
  >
    {#if !expanded}
      <!-- the fan is exactly two ghost layers, never one per card: a fan of
           40 tiles is noise, and two edges say "stack" in one glance. -->
      <span class="ghost ghost--2" aria-hidden="true"></span>
      <span class="ghost ghost--1" aria-hidden="true"></span>
    {/if}
    {#each faces as c (c.id)}
      <CardTile card={c} {size} {art} faceTapped={expanded ? undefined : collapsedFaceTapped} tileOptions={expanded ? memberOptions(c.id) : collapsedOptions} />
    {/each}
    {#if !expanded && tappedRep}
      <!-- fb-20260918T010805Z: the collapsed mixed pile's tapped member, shown
           IN the pile as the quarter-turned card it is. Presentation only: the
           representative carries no options affordance of its own (the pile's
           union lives on the lead tile) and the lead face stays the anchor. -->
      <span class="tapped-rep">
        <CardTile card={tappedRep} {size} {art} faceTapped={true} tileOptions={null} />
      </span>
    {/if}
    <span class="count" data-stack-count aria-hidden="true">×{group.cards.length}</span>
    {#if !expanded && mixedTapped}
      <span class="count ready" data-stack-ready aria-hidden="true">{readyCount} ready</span>
    {/if}
  </button>
{/if}

<style>
  /* The whole group is one focusable control; the first tile inside it is
     still the data-obj anchor CardTile already drew. */
  .stacked {
    --spine: 5px;
    position: relative;
    display: inline-flex;
    align-items: flex-start;
    gap: var(--sp-2);
    /* room for the spine and for the tab that hangs off it, so the pile never
       leans into whatever is beside it in the row */
    margin-left: var(--sp-6);
    padding: 0;
    border: none;
    border-radius: var(--radius-card);
    background: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }
  /* Ghosts trace the face's own box in both states — the CardTile slot
     changes shape when a permanent is tapped, and inset:0 follows it — so a
     pile of tapped lands is still a pile and not two loose rectangles. */
  .ghost {
    position: absolute;
    inset: 0;
    background: var(--felt-sunk);
    /* The lit edges are the ones the pile exposes, so the step reads even
       when the top card is a blank in nearly the same colour. */
    border: var(--edge-w) solid var(--edge-felt);
    border-top-color: var(--ink-faint);
    border-left-color: var(--ink-faint);
    border-radius: var(--radius-card);
    transition: transform 0.12s ease-out;
  }
  .ghost--1 {
    transform: translate(calc(var(--spine) * -1), calc(var(--spine) * -0.75));
  }
  .ghost--2 {
    background: var(--felt);
    border-top-color: var(--ink-dim);
    border-left-color: var(--ink-dim);
    transform: translate(calc(var(--spine) * -2), calc(var(--spine) * -1.5));
  }
  .stacked:hover .ghost--1,
  .stacked:focus-visible .ghost--1 {
    transform: translate(calc(var(--spine) * -1.75), calc(var(--spine) * -1.5));
  }
  .stacked:hover .ghost--2,
  .stacked:focus-visible .ghost--2 {
    transform: translate(calc(var(--spine) * -3.5), calc(var(--spine) * -3));
  }

  /* Open, the members are a group rather than N loose tiles that happen to
     match, so the set keeps one rule around it and the tab stays put. */
  .stacked.expanded {
    padding: var(--sp-1);
    margin-left: var(--sp-6);
    box-shadow: inset 0 0 0 1px var(--edge-felt);
  }

  /* The tab: a real plate on the spine, in the instrument's voice because a
     count is a value. It is the only thing on the pile that is not a card. */
  .count {
    position: absolute;
    /* Hung entirely in the gutter, off the pile's exposed edge: the card's
       own bottom-left corner belongs to the counters band. translateX keeps
       it outside whatever width the number needs. */
    left: calc(var(--spine) * -2);
    bottom: var(--sp-2);
    transform: translateX(-100%);
    background: var(--felt-sunk);
    color: var(--gilt);
    border: 1px solid var(--gilt);
    border-radius: 999px;
    font-family: var(--font-data);
    font-variant-numeric: tabular-nums;
    font-size: var(--t-11);
    font-weight: 500;
    line-height: 1.4;
    letter-spacing: -0.02em;
    padding: 0 0.35em;
    transition: border-color 0.12s ease-out, color 0.12s ease-out;
  }
  .stacked.expanded .count {
    left: calc(var(--sp-1) * -1);
    bottom: auto;
    top: calc(var(--sp-1) * -1);
  }
  .stacked:hover .count,
  .stacked:focus-visible .count {
    border-color: var(--initiative);
    color: var(--initiative);
  }
  /* The readiness plate (fb-20260916T201423Z): the second mini-plate on a
     mixed pile, stacked directly above the count tab with the same hang and
     right-edge alignment. The offset is the count tab's own height (~1.8em
     at --t-11/1.4 plus the 2px edges) plus a small gap. It only ever exists
     on a collapsed pile, so the expanded repositioning below cannot reach
     it. */
  .count.ready {
    bottom: calc(var(--sp-2) + 2.1em);
  }

  /* The tapped representative (fb-20260918T010805Z): a quarter-turned card
     lying across the pile's bottom-right corner and sticking out past it,
     like the tapped card it stands in for. Absolutely positioned, so it is
     out of the flex flow and the ghost layers still trace the lead face's
     own box — the pile stays square and its geometry is otherwise untouched.
     The offsets clear the pile's corner without reaching the tab gutter on
     the left or overlapping a neighbouring pile materially. */
  .tapped-rep {
    position: absolute;
    right: calc(var(--sp-2) * -1.5);
    bottom: calc(var(--sp-2) * -1);
  }
</style>
