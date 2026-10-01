<script lang="ts">
  import type { SeatInfo, View } from '../protocol';
  import type { Stops, TurnSide } from '../lib/autopilot';
  import type { SeatCtx } from '../lib/seat';
  import type { SeatPanelState } from '../lib/seatpanel.svelte';
  import { layoutStore } from '../lib/layouts.svelte';
  import { pointerRelease } from '../lib/pointer';
  import { SPLIT_MAX, SPLIT_MIN } from '../lib/layoutprofile';
  import HotButtonStrip from './HotButtonStrip.svelte';
  import PhaseTrack from './PhaseTrack.svelte';

  /**
   * CentreStrip is the bar between the opponents and the viewer's board (UI
   * rework spec §3): the turn label, the phase track with the seat's stops
   * dotted, the seat's prompt controls (the hot strip without its transport
   * tabs — the gilt action cluster owns those), and two pills naming the
   * active flow profile and layout profile, each opening its drawer.
   *
   * It is also the SPLITTER: dragging the bar's empty felt moves the
   * opponents' share of the height (only the grid rows move while dragging;
   * the new split is saved on release), double-click resets it for the seat
   * count, and the grip is a keyboard separator (arrow keys) so dragging is
   * never required.
   */
  let {
    view,
    seats,
    seat = null,
    stops = null,
    onToggle = null,
    controls = null,
    controlsLive = false,
    onOpenFlow = null,
    onDrag = null,
    board = null,
  }: {
    view: View;
    seats: SeatInfo[];
    seat?: number | null;
    stops?: Stops | null;
    onToggle?: ((step: string, side: TurnSide) => void) | null;
    controls?: { state: SeatPanelState; ctx: SeatCtx; table: string; match: number; onToggleOptions?: () => void } | null;
    controlsLive?: boolean;
    onOpenFlow?: (() => void) | null;
    /** a live split while dragging (null on release) */
    onDrag?: ((split: number | null) => void) | null;
    /** the board element the split is measured against */
    board?: HTMLElement | null;
  } = $props();

  const nameOf = (s: number) => seats[s]?.name || view.players.find((p) => p.seat === s)?.name || `Seat ${s}`;
  const whose = $derived(seat !== null && view.active === seat ? 'your turn' : `${nameOf(view.active)}'s turn`);
  const PRESET_LABEL: Record<string, string> = { casual: 'Casual', 'no-tells': 'No tells', 'full-control': 'Full control', custom: 'Custom' };
  const flowLabel = $derived(
    controls ? (controls.state.activeProfileName ?? PRESET_LABEL[controls.state.settings.preset] ?? 'Custom') : null,
  );
  const split = $derived(layoutStore.profile.table.split);

  let dragging = $state(false);
  let el = $state<HTMLElement | null>(null);
  function splitAt(clientY: number): number | null {
    const b = board?.getBoundingClientRect();
    const stripH = el?.getBoundingClientRect().height ?? 0;
    if (!b || b.height <= stripH) return null;
    const v = (clientY - b.top - stripH / 2) / (b.height - stripH);
    return Math.min(SPLIT_MAX, Math.max(SPLIT_MIN, v));
  }
  function down(e: PointerEvent): void {
    if ((e.target as HTMLElement).closest('button, a, input, select, [role="toolbar"], [data-phase-track]')) return;
    dragging = true;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  }
  function move(e: PointerEvent): void {
    if (!dragging) return;
    const v = splitAt(e.clientY);
    if (v !== null) onDrag?.(v);
  }
  function up(e: PointerEvent): void {
    if (!dragging) return;
    dragging = false;
    const v = splitAt(e.clientY);
    onDrag?.(null);
    if (v !== null) layoutStore.setSplit(v);
  }
  function key(e: KeyboardEvent): void {
    if (e.key === 'ArrowUp') layoutStore.setSplit(split - 0.02);
    else if (e.key === 'ArrowDown') layoutStore.setSplit(split + 0.02);
    else if (e.key === 'Home') layoutStore.resetSplit(view.players.length);
    else return;
    e.preventDefault();
  }
</script>

<div
  class="centre-strip"
  class:dragging
  bind:this={el}
  role="presentation"
  data-centre-strip
  data-phase-lane
  onpointerdown={down}
  onpointermove={move}
  onpointerup={up}
  onpointercancel={up}
  ondblclick={(e) => { if (!(e.target as HTMLElement).closest('button')) layoutStore.resetSplit(view.players.length); }}
>
  <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions (a focusable separator is the ARIA window-splitter pattern) -->
  <span
    class="grip"
    role="separator"
    tabindex="0"
    aria-orientation="horizontal"
    aria-label="Board split: drag, or use the arrow keys; Home resets"
    aria-valuemin={Math.round(SPLIT_MIN * 100)}
    aria-valuemax={Math.round(SPLIT_MAX * 100)}
    aria-valuenow={Math.round(split * 100)}
    title="Drag to share height between the boards; double-click resets"
    onkeydown={key}
  ></span>
  <span class="turn" data-turn-label>Turn <b>{view.round}</b><small>{whose}</small></span>
  <div class="track" data-centre-instrument>
    <PhaseTrack {view} {seats} {seat} {stops} {onToggle} clock={false} />
  </div>
  {#if controlsLive && controls}
    <div class="controls">
      <HotButtonStrip {view} {seats} state={controls.state} ctx={controls.ctx} table={controls.table} match={controls.match} onToggleOptions={controls.onToggleOptions} transport={false} />
    </div>
  {/if}
  <span class="pills">
    {#if flowLabel !== null && onOpenFlow}
      <button type="button" class="pill flow" data-flow-pill use:pointerRelease title="Flow profile: open game options" onclick={() => onOpenFlow?.()}>{flowLabel}</button>
    {/if}
    <button type="button" class="pill" data-layout-pill use:pointerRelease aria-expanded={layoutStore.drawerOpen} title="Layout profile: open the Layout drawer" onclick={() => (layoutStore.drawerOpen = !layoutStore.drawerOpen)}>Layout: {layoutStore.label}</button>
  </span>
</div>

<style>
  .centre-strip {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    height: 100%;
    padding: 0 var(--sp-3);
    border-top: 1px solid var(--edge-felt);
    border-bottom: 1px solid var(--edge-felt);
    background: linear-gradient(180deg, color-mix(in srgb, var(--felt) 70%, #000) 0%, var(--felt) 50%, color-mix(in srgb, var(--felt) 70%, #000) 100%);
    cursor: row-resize;
    user-select: none;
    touch-action: none;
  }
  .centre-strip.dragging {
    border-color: var(--gilt);
  }
  .grip {
    position: absolute;
    top: -3px;
    left: 50%;
    width: 44px;
    height: 5px;
    transform: translateX(-50%);
    border-radius: 3px;
    background: var(--edge-inst);
  }
  .grip:focus-visible,
  .centre-strip:hover .grip {
    background: var(--gilt);
  }
  .turn {
    flex: none;
    display: inline-flex;
    align-items: baseline;
    gap: 0.35em;
    font-family: var(--font-serif);
    font-weight: 600;
    font-size: var(--t-16);
    color: var(--ink);
    cursor: auto;
  }
  .turn b {
    font-weight: 700;
    font-size: var(--t-20);
  }
  .turn small {
    color: var(--ink-dim);
    font-family: var(--font-ui);
    font-size: var(--t-12);
  }
  .track {
    flex: 1 1 auto;
    min-width: 0;
    cursor: auto;
  }
  .controls {
    flex: none;
    height: 100%;
    display: flex;
    align-items: center;
    cursor: auto;
    --hot-strip-h: 1.75rem;
  }
  .pills {
    flex: none;
    display: inline-flex;
    gap: var(--sp-2);
  }
  .pill {
    padding: 0.15rem 0.7rem;
    border: 1px solid var(--edge-inst);
    border-radius: 999px;
    background: var(--instrument);
    color: var(--ink-inst);
    font-family: var(--font-ui);
    font-size: var(--t-12);
    cursor: pointer;
    white-space: nowrap;
  }
  .pill.flow {
    border-color: color-mix(in srgb, var(--verdigris) 70%, transparent);
    color: var(--verdigris);
  }
  .pill:hover,
  .pill[aria-expanded='true'] {
    border-color: var(--gilt);
    color: var(--ink);
  }
  /* The hot strip's tabs hang from the old phase row; in the strip they are
     free-standing buttons. */
  .controls :global(.hot-strip .tab),
  .controls :global(.hot-strip .mode-chip) {
    border: 1px solid var(--edge-inst);
    border-radius: 6px;
  }
  .controls :global(.hot-strip) {
    gap: 4px;
  }
  @media (max-width: 70rem) {
    .turn small { display: none; }
  }
</style>
