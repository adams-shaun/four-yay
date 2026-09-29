<script lang="ts">
  import { onMount } from 'svelte';
  import type { View } from '../../protocol';
  import type { SeatPanelState } from '../../lib/seatpanel.svelte';
  import { promptAnatomy } from '../../lib/prompts/anatomy';
  import { dockAnswers } from '../../lib/prompts/renderer';
  import { promptHover, type HoverEnd } from '../../lib/prompts/hover.svelte';
  import { clampPosition, dockFromProfile, fractionOf, nextPlacement, profilePlacement, tableAnchor, type DockPlacement, type DockPoint, type TableAnchor } from '../../lib/prompts/dock';
  import { layoutStore } from '../../lib/layouts.svelte';
  import ArtCrop from './ArtCrop.svelte';
  import PromptBody from './PromptBody.svelte';

  /**
   * PromptDock is the prompt system's shell (UI rework spec §4): the source
   * card's art as a banner (rail) or spine (floating), the source line, a
   * serif title that asks the question, one plain-language line, the
   * decision's renderer (numbered options), and the renderer's footer.
   *
   * Placement: near the table by default -- pinned just above the gilt
   * action button, growing up over the board, so answering never means a
   * trip to the screen's top edge (operator feedback 2026-09-29). Docked at
   * the top of the rail and floating (dragged by its grip) are settings. The layout profile owns both (`layoutStore.prompt`: the
   * placement, and the floating spot as viewport fractions, saved on drag);
   * the `placement`/`position` props override it for fixtures and tests.
   * Client-side only, never posted.
   *
   * It answers every decision except priority (the action button) and the
   * London mulligan (dockAnswers). While mounted it bumps the seat's
   * dockCount, so the ACTIONS strip points here instead of drawing a second
   * answer surface.
   */
  let { view, logic, seat, placement: placementProp = undefined, position: positionProp = undefined, onPlacementChange = undefined, onPositionChange = undefined }: {
    view: View;
    logic: SeatPanelState;
    seat: number;
    placement?: DockPlacement;
    position?: DockPoint | null;
    onPlacementChange?: (p: DockPlacement) => void;
    onPositionChange?: (p: DockPoint) => void;
  } = $props();

  const fromProfile = $derived(dockFromProfile(layoutStore.prompt, typeof window === 'undefined' ? { w: 0, h: 0 } : viewport()));
  const placement = $derived(placementProp ?? fromProfile.placement);
  const saved = $derived(positionProp !== undefined ? positionProp : fromProfile.position);
  function setPlacement(p: DockPlacement): void {
    if (onPlacementChange) onPlacementChange(p);
    else layoutStore.edit((q) => (q.panels.prompt.placement = profilePlacement(p)));
  }
  function setPosition(p: DockPoint): void {
    if (onPositionChange) onPositionChange(p);
    else {
      const f = fractionOf(p, viewport());
      layoutStore.setPromptPosition(f.x, f.y);
    }
  }

  onMount(() => {
    logic.dockCount += 1;
    return () => {
      logic.dockCount -= 1;
      promptHover.clear();
    };
  });

  const decision = $derived(dockAnswers(logic.active) ? logic.active : null);
  const anatomy = $derived(decision ? promptAnatomy(decision, view) : null);

  // A hover belongs to the decision it was made on.
  $effect(() => {
    void decision?.seq;
    promptHover.clear();
  });

  // ---- the board highlight for a hovered option ------------------------
  // The anchors are the ones Arrows.svelte resolves ([data-obj] tiles and
  // the seat box / header bar [data-seat-anchor]). The attribute is display-only; the global
  // rule below rings it in verdigris ("a legal choice").
  function anchorEl(end: HoverEnd): Element | null {
    return 'obj' in end ? document.querySelector(`[data-obj="${end.obj}"]`) : document.querySelector(`[data-seat-anchor="${end.seat}"]`);
  }
  $effect(() => {
    const link = promptHover.link;
    if (link === null || typeof document === 'undefined') return;
    const els = [link.to, ...(link.from ? [link.from] : [])].map(anchorEl).filter((e): e is Element => e !== null);
    for (const el of els) el.setAttribute('data-prompt-hover', '');
    return () => {
      for (const el of els) el.removeAttribute('data-prompt-hover');
    };
  });

  // ---- floating placement and drag -------------------------------------
  let root = $state<HTMLElement | null>(null);
  let pos = $state<DockPoint | null>(null);
  let drag: { dx: number; dy: number; id: number } | null = null;

  function viewport() {
    return { w: window.innerWidth, h: window.innerHeight };
  }
  function size() {
    const r = root?.getBoundingClientRect();
    return { w: r?.width ?? 420, h: r?.height ?? 320 };
  }
  // Resolve the floating position once the dock has a size: the saved spot
  // clamped into this viewport, or a default over the lower board.
  $effect(() => {
    if (placement !== 'floating' || root === null || decision === null) return;
    if (drag !== null) return;
    pos = clampPosition(saved ?? { x: 0, y: 0 }, size(), viewport());
  });
  onMount(() => {
    const onResize = () => {
      if (placement === 'floating' && pos !== null) pos = clampPosition(pos, size(), viewport());
    };
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  });

  function gripDown(e: PointerEvent): void {
    if (placement !== 'floating' || root === null) return;
    const r = root.getBoundingClientRect();
    drag = { dx: e.clientX - r.left, dy: e.clientY - r.top, id: e.pointerId };
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    e.preventDefault();
  }
  function gripMove(e: PointerEvent): void {
    if (drag === null || e.pointerId !== drag.id) return;
    pos = clampPosition({ x: e.clientX - drag.dx, y: e.clientY - drag.dy }, size(), viewport());
  }
  function gripUp(e: PointerEvent): void {
    if (drag === null || e.pointerId !== drag.id) return;
    drag = null;
    if (pos !== null) setPosition(pos);
  }
  /** Arrow keys nudge a floating dock, so moving it never requires dragging (spec principle 3). */
  function gripKey(e: KeyboardEvent): void {
    if (placement !== 'floating' || pos === null) return;
    const step = e.shiftKey ? 40 : 10;
    const d = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] }[e.key];
    if (!d) return;
    e.preventDefault();
    pos = clampPosition({ x: pos.x + d[0], y: pos.y + d[1] }, size(), viewport());
    setPosition(pos);
  }

  // ---- near-table placement -------------------------------------------
  // Pinned above the action button ([data-action-cluster]) inside the board;
  // re-measured whenever either resizes or the window does.
  let anchor = $state<TableAnchor | null>(null);
  $effect(() => {
    if (placement !== 'table' || decision === null || typeof document === 'undefined') return;
    const measure = () => {
      const board = document.querySelector('.board');
      if (board === null) return;
      const action = document.querySelector('[data-action-cluster]');
      anchor = tableAnchor(action?.getBoundingClientRect() ?? null, board.getBoundingClientRect(), viewport());
    };
    measure();
    const ro = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure);
    for (const el of [document.querySelector('.board'), document.querySelector('[data-action-cluster]')]) if (el) ro?.observe(el);
    window.addEventListener('resize', measure);
    return () => {
      ro?.disconnect();
      window.removeEventListener('resize', measure);
    };
  });

  const floatStyle = $derived(
    placement === 'floating' && pos !== null
      ? `left:${pos.x}px;top:${pos.y}px`
      : placement === 'table' && anchor !== null
        ? `right:${anchor.right}px;bottom:${anchor.bottom}px;max-height:${anchor.maxHeight}px`
        : undefined,
  );
  const toggleTitle = $derived(
    { table: 'Dock the prompt in the rail', rail: 'Float the prompt over the board', floating: 'Pin the prompt above the action button' }[placement],
  );
</script>

{#if decision && anatomy}
  {#key decision.seq}
  <section
    class="prompt-dock {placement}"
    bind:this={root}
    style={floatStyle}
    data-prompt-dock
    data-placement={placement}
    data-renderer={anatomy.renderer}
    data-answer-surface="true"
    aria-label={anatomy.title}
  >
    <div class="art"><ArtCrop card={anatomy.source?.card ?? null} shape="banner" /></div>
    <div class="body">
      <div class="tools">
        {#if placement === 'floating'}
          <button
            class="tool grip"
            type="button"
            title="Drag to move (arrow keys nudge)"
            aria-label="Move the prompt"
            data-dock-grip
            onpointerdown={gripDown}
            onpointermove={gripMove}
            onpointerup={gripUp}
            onpointercancel={gripUp}
            onkeydown={gripKey}
          >⋮⋮</button>
        {/if}
        <button
          class="tool"
          type="button"
          data-dock-placement-toggle
          title={toggleTitle}
          aria-label={toggleTitle}
          onclick={() => setPlacement(nextPlacement(placement))}
        >{placement === 'table' ? '⇥' : placement === 'rail' ? '⧉' : '⤓'}</button>
      </div>
      {#if anatomy.source}<p class="src" data-prompt-source>{anatomy.source.line}</p>{/if}
      <h2 class="title" class:long={anatomy.title.length > 32} data-prompt-title>{anatomy.title}</h2>
      {#if anatomy.plain}<p class="plain" data-prompt-plain>{anatomy.plain}</p>{/if}
      {#if logic.error}<p class="error" role="alert">{logic.error}</p>{/if}
      <PromptBody {decision} {view} {logic} {seat} placement="dock" />
    </div>
  </section>
  {/key}
{/if}

<style>
  .prompt-dock {
    position: relative;
    display: grid;
    background: linear-gradient(180deg, #1d2129, #171a20);
    color: var(--ink);
    font-family: var(--font-ui);
    min-width: 0;
  }
  /* The gilt rule on the leading edge and a gilt ring: this is YOUR move.
     A new question announces itself with a short glow pulse (each decision
     remounts the section), still under reduced motion. */
  .prompt-dock::before {
    content: '';
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 4px;
    background: var(--gilt, #d4ad62);
    z-index: 2;
  }
  .prompt-dock {
    outline: 2px solid color-mix(in srgb, var(--gilt, #d4ad62) 75%, transparent);
    outline-offset: -2px;
    box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.5), 0 0 28px color-mix(in srgb, var(--gilt, #d4ad62) 35%, transparent);
    animation: prompt-arrive 1.4s ease-out 1;
  }
  @keyframes prompt-arrive {
    0% { box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.5), 0 0 0 0 color-mix(in srgb, var(--gilt, #d4ad62) 80%, transparent); }
    35% { box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.5), 0 0 0 10px color-mix(in srgb, var(--gilt, #d4ad62) 30%, transparent); }
    100% { box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.5), 0 0 28px color-mix(in srgb, var(--gilt, #d4ad62) 35%, transparent); }
  }
  @media (prefers-reduced-motion: reduce) {
    .prompt-dock { animation: none; }
  }
  /* Docked: the art is a banner across the top, the body overlaps its fade. */
  .prompt-dock.rail {
    grid-template-rows: 5.5rem auto;
    padding: 0;
    border-bottom: 1px solid var(--edge-inst);
    max-height: 70vh;
    overflow-y: auto;
  }
  .rail .art { position: relative; min-height: 0; }
  .rail .art::after {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(180deg, rgba(29, 33, 41, 0.1) 30%, #1d2129);
  }
  .rail .body { padding: 0 var(--sp-4) var(--sp-3); margin-top: -1.6rem; position: relative; min-width: 0; }
  /* Floating: the art is a spine down the left edge. */
  .prompt-dock.floating {
    position: fixed;
    z-index: 40;
    left: 50%;
    top: 55%;
    width: min(36rem, calc(100vw - 2rem));
    max-height: min(70vh, 40rem);
    grid-template-columns: 7rem 1fr;
    border: 1px solid #394150;
    border-radius: 14px;
    overflow: hidden;
  }
  /* Near the table: pinned above the action button (right/bottom/max-height
     come from tableAnchor), the art a spine like floating.

     The panel deliberately covers the board, so its chrome is
     pointer-transparent (ui24): a click aimed at a board target underneath
     reaches the board. `pointer-events: none` on the root inherits into
     every descendant; the dock's OWN interactive surfaces opt back in — the
     tools buttons, and the renderer's controls (option rows, target chips,
     submit buttons, filter inputs). Everything else — the art spine, the
     title/plain text, the body's padding — passes clicks and wheel through
     to the board.

     The trade (ui24 fix): the body keeps `overflow-y: auto` but cannot be
     wheel-scrolled over its padding, and its scrollbar thumb is not
     draggable, because the scrollable element itself is transparent. A wheel
     over any option row or control still scrolls it (the wheel scrolls the
     nearest scrollable ancestor of the event target), keyboard scrolling of
     a focused control still works, and the option lists the dock answers are
     short — so every non-control pixel is bought for the board. Floating is
     untouched: the player chose to put a panel there. */
  .prompt-dock.table {
    position: fixed;
    z-index: 40;
    right: 1rem;
    bottom: 6rem;
    width: min(30rem, calc(100vw - 2rem));
    max-height: 60vh;
    grid-template-columns: 6rem 1fr;
    border-radius: 12px;
    overflow: hidden;
    pointer-events: none;
  }
  .prompt-dock.table .tool,
  .prompt-dock.table .body :global(button),
  .prompt-dock.table .body :global(input),
  .prompt-dock.table .body :global(select),
  .prompt-dock.table .body :global(textarea) {
    pointer-events: auto;
  }
  .table .art { position: relative; }
  .table .art::after {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(90deg, transparent 55%, #1d2129);
  }
  .table .body { padding: var(--sp-3) var(--sp-4) var(--sp-3) var(--sp-3); min-width: 0; overflow-y: auto; }
  .floating .art { position: relative; }
  .floating .art::after {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(90deg, transparent 55%, #1d2129);
  }
  .floating .body { padding: var(--sp-3) var(--sp-4) var(--sp-3) var(--sp-3); min-width: 0; overflow-y: auto; }
  .tools {
    position: absolute;
    top: 0.4rem;
    right: 0.6rem;
    display: flex;
    gap: 4px;
    z-index: 3;
  }
  .rail .tools { top: -3.6rem; }
  .tool {
    border: 0;
    border-radius: 5px;
    background: rgba(20, 23, 28, 0.7);
    color: var(--ink-dim);
    font: 600 var(--t-12)/1.4 var(--font-ui);
    padding: 1px 7px;
    cursor: pointer;
  }
  .tool:hover { color: var(--ink); }
  .grip { cursor: grab; letter-spacing: 1px; touch-action: none; }
  .grip:active { cursor: grabbing; }
  .src { margin: 0; font-size: var(--t-12); color: var(--gilt, #d4ad62); }
  .title {
    margin: 0.15rem 0 0.1rem;
    font: 600 1.5rem/1.15 var(--font-serif, 'Cormorant Garamond', Georgia, 'Times New Roman', serif);
    letter-spacing: 0.005em;
    color: var(--ink);
    overflow-wrap: anywhere;
  }
  /* A server-worded question (a `choose`) can run long; it stays the title
     but steps down a size so it never pushes the options off the dock. */
  .title.long { font-size: 1.15rem; line-height: 1.2; }
  .plain { margin: 0 0 var(--sp-2); font-size: var(--t-12); line-height: 1.45; color: var(--ink-dim); }
  .error {
    margin: 0 0 var(--sp-2);
    padding: var(--sp-1) var(--sp-2);
    border-left: 3px solid var(--danger);
    background: color-mix(in srgb, var(--danger) 20%, var(--instrument));
    font-size: var(--t-12);
  }
  /* The board anchor of the hovered option (see the hover effect above). */
  :global([data-prompt-hover]) {
    outline: 2px solid var(--verdigris, #6fb7ae);
    outline-offset: 2px;
    border-radius: var(--radius-card);
  }
</style>
