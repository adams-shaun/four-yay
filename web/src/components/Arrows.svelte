<script lang="ts">
  import type { View } from '../protocol';
  import type { CardOptions } from '../lib/cardoptions';
  import { arrowsFor, hoverArrowFor, previewArrowsFor } from '../lib/arrows';
  import { promptHover } from '../lib/prompts/hover.svelte';
  import type { Arrow, End } from '../lib/arrows';
  import { hovered } from '../lib/hovered.svelte';

  /**
   * Arrows overlays the board with one line per arrowsFor(view) result. It
   * has no rules knowledge itself — arrows.ts already decided which ends to
   * connect; this component only resolves each End to its DOM anchor
   * ([data-obj] / [data-seat] — some of which live outside the board's own
   * subtree: the stack is rendered in the rail, identity bars are siblings
   * of Board) and draws a line relative to this overlay's own box. A
   * missing anchor (not rendered yet, or already gone) simply draws
   * nothing for that arrow rather than throwing.
   */
  let { view, options = null }: { view: View; options?: CardOptions | null } = $props();

  interface Line { x1: number; y1: number; x2: number; y2: number; kind: Arrow['kind'] }

  // Arrow colour encodes the KIND of relationship, XMage's convention
  // (survey #9, #16): red attacks, blue blocks, the initiative colour targets.
  // The values live in CSS so they stay the palette's, not this file's — the
  // three hexes here were hand-picked and drifted from every token.

  let root = $state<HTMLDivElement | undefined>(undefined);
  let lines = $state<Line[]>([]);

  function anchorEl(end: End): Element | null {
    // `[data-seat]` is not unique: Quadrant's whole board box carries it too
    // (for its seat-colour rule), and querySelector returns whichever comes
    // first in document order — a player arrow was landing on that huge box's
    // corner instead of the player's own name plate. `.identity` is
    // IdentityBar's own root and is the only element this arrow should ever
    // point at.
    // A player end lands on the seat's header bar or seat box (their
    // [data-seat-anchor] root); `.identity` is the retired identity bar's.
    return 'obj' in end
      ? document.querySelector(`[data-obj="${end.obj}"]`)
      : document.querySelector(`[data-seat-anchor="${end.seat}"]`) ?? document.querySelector(`.identity[data-seat="${end.seat}"]`);
  }

  function centre(el: Element, base: DOMRect): { x: number; y: number } {
    const r = el.getBoundingClientRect();
    return { x: r.left + r.width / 2 - base.left, y: r.top + r.height / 2 - base.top };
  }

  function recompute(): void {
    if (!root) return;
    const base = root.getBoundingClientRect();
    const next: Line[] = [];
    // Proposed arrows belong to a pending TARGET choice only: fanning lines
    // from a resolving spell to every card a discard or search offers is the
    // spaghetti the spec rules out.
    const preview = view.decision?.kind === 'target' ? previewArrowsFor(options) : [];
    const hover = hoverArrowFor(options, promptHover.link);
    for (const arrow of [...arrowsFor(view, hoveredStack), ...preview, ...(hover ? [hover] : [])]) {
      const from = anchorEl(arrow.from);
      const to = anchorEl(arrow.to);
      if (!from || !to) continue;
      const a = centre(from, base);
      const b = centre(to, base);
      next.push({ x1: a.x, y1: a.y, x2: b.x, y2: b.y, kind: arrow.kind });
    }
    lines = next;
  }

  // Redraw whenever the view changes. requestAnimationFrame defers the
  // measurement past this tick's DOM update so the new frame's tiles have
  // laid out before we read their positions.
  // Stack target arrows follow the pointer: only the hovered stack item's.
  const hoveredStack = $derived(hovered.obj !== null && view.stack.some((s) => s.id === hovered.obj) ? hovered.obj : null);

  $effect(() => {
    void view;
    void options;
    void hoveredStack;
    void promptHover.link;
    const id = requestAnimationFrame(recompute);
    return () => cancelAnimationFrame(id);
  });

  // Redraw on layout changes that don't themselves change `view` — window
  // resize, a panel collapsing, fonts loading in — anything that moves the
  // board or its anchors without a new frame arriving.
  $effect(() => {
    if (!root) return;
    const ro = new ResizeObserver(() => recompute());
    ro.observe(root);
    return () => ro.disconnect();
  });

  // Redraw when any internal scroller scrolls (fb-20260914T121642Z): the
  // stack rail's section.stack is a real overflow-y container, scroll does
  // not bubble, and a scrolled tile moves under a line whose endpoints were
  // measured at the old scroll offset. A capture-phase listener on
  // `document` sees every descendant scroller's scroll (capture propagation
  // reaches the target even for non-bubbling events) and the document's own
  // scroll; passive, never polling, cleaned up with the component.
  $effect(() => {
    if (!root) return;
    const onScroll = () => recompute();
    document.addEventListener('scroll', onScroll, { capture: true, passive: true });
    return () => document.removeEventListener('scroll', onScroll, { capture: true });
  });
</script>

<div class="arrows" bind:this={root}>
  <svg>
    <defs>
      <marker id="arrow-target" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
        <path class="head head--target" d="M0,0 L10,5 L0,10 z" />
      </marker>
      <marker id="arrow-attack" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
        <path class="head head--attack" d="M0,0 L10,5 L0,10 z" />
      </marker>
      <marker id="arrow-target-hover" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
        <path class="head head--target-hover" d="M0,0 L10,5 L0,10 z" />
      </marker>
      <marker id="arrow-block" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
        <path class="head head--block" d="M0,0 L10,5 L0,10 z" />
      </marker>
    </defs>
    {#each lines as l, i (i)}
      <line class="line line--{l.kind}" x1={l.x1} y1={l.y1} x2={l.x2} y2={l.y2} stroke-width={l.kind === 'target-hover' ? 3 : 2} stroke-linecap="round" marker-end={`url(#arrow-${l.kind === 'target-preview' ? 'target' : l.kind})`} />
    {/each}
  </svg>
</div>

<style>
  .arrows { position: absolute; inset: 0; pointer-events: none; overflow: visible; }
  svg { position: absolute; inset: 0; width: 100%; height: 100%; overflow: visible; }
  /* Verdigris for a choice being made, ember for attacks (spec §3). */
  .line--target { stroke: var(--verdigris); }
  .line--target-preview {
    stroke: var(--verdigris);
    stroke-dasharray: 7 6;
    opacity: 0.58;
  }
  /* The hovered prompt option's arrow: verdigris, "a choice being made"
     (UI rework spec §3), solid and a step heavier than the dashed previews. */
  .line--target-hover { stroke: var(--verdigris, #6fb7ae); }
  .head--target-hover { fill: var(--verdigris, #6fb7ae); }
  .line--attack { stroke: var(--ember); }
  .line--block { stroke: var(--ink-dim); }
  .head--target { fill: var(--verdigris); }
  .head--attack { fill: var(--ember); }
  .head--block { fill: var(--ink-dim); }
</style>
