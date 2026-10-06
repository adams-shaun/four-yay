<script lang="ts">
  import { IMAGE_KEY } from '../lib/images';
  import type { CardView, Decision, Option, PlayerView, SeatInfo, View } from '../protocol';
  import { SeatPanelState } from '../lib/seatpanel.svelte';
  import { type DockPlacement } from '../lib/prompts/dock';
  import FeedbackButton from './FeedbackButton.svelte';
  import PromptDock from './prompts/PromptDock.svelte';
  import PromptRailSlot from './prompts/PromptRailSlot.svelte';

  /**
   * Geometry fixture for the prompt/Feedback non-intersection contract: the
   * REAL PromptDock, the REAL PromptRailSlot (the production lower-slot CSS
   * and its Feedback clearance) and the REAL FeedbackButton, over a plain
   * rail column shaped like Table.svelte's (log above, slot last).
   *
   * Query: `place` rail | rail-bottom | floating | table (default rail-bottom),
   * `side` right | left | hidden (rail side, default right), `ask` short | long
   * | huge, `fx`/`fy` the saved floating position in px.
   */
  localStorage.setItem(`${IMAGE_KEY}Lightning Bolt`, '');

  const q = new URLSearchParams(location.search);
  const place = (q.get('place') ?? 'rail-bottom') as DockPlacement;
  const side = q.get('side') ?? 'right';
  const askKind = q.get('ask') ?? 'short';
  const saved = q.has('fx') ? { x: Number(q.get('fx')), y: Number(q.get('fy')) } : null;

  const ctx = { seat: 0, token: 'tok' };
  const seats: SeatInfo[] = [{ name: 'Ari', deck: 'burn', colour: '#e5484d' }, { name: 'Mira', deck: 'stompy', colour: '#30a46c' }];
  const bolt = {
    id: 7, name: 'Lightning Bolt', types: 'Instant', printing: { name: 'Lightning Bolt' }, token: '#7',
    tapped: false, power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
  } as unknown as CardView;
  const player = (seat: number): PlayerView => ({
    seat, name: seats[seat].name, life: 20, lost: false, library_size: 53, hand_size: 0, graveyard_size: 0, hand: [],
    battlefield: [], graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [], completed_dungeons: 0,
  } as unknown as PlayerView);
  const opt = (index: number, label: string): Option => ({ index, kind: 'permanent', label, player: 1, obj: 30 + index });

  // min 1 / max 3: a row click toggles into `picked` and never posts, so the
  // fixture stays offline.
  const count = askKind === 'huge' ? 24 : askKind === 'long' ? 7 : 2;
  const ask = {
    seq: 12, player: 0, kind: 'choose', prompt: 'Choose attackers', min: 1, max: 3, source: 7,
    options: Array.from({ length: count }, (_, i) => opt(i, `Memnite ${i + 1} → Player 2`)),
  } as unknown as Decision;
  const view = {
    viewer: 0, visibility: 'seat', turn: 7, round: 4, step: 'main1', phase: 'main1', active: 0, priority: 0,
    over: false, draw: false, winner: null, players: [player(0), player(1)],
    stack: [{ id: 7, kind: 'spell', name: 'Lightning Bolt', text: '', controller: 0, targets: [], card: bolt, optional: false }],
    pending: [], decision: ask,
  } as unknown as View;
  const logic = new SeatPanelState('t1', 1, ctx, null, null);
  logic.adoptView(ask);

  let placement = $state<DockPlacement>(place);
  let position = $state(saved);
  const inRail = $derived(placement === 'rail' || placement === 'rail-bottom');
</script>

<div class="board" data-board></div>
<aside class="rail {side}" data-rail-side={side}>
  {#if placement === 'rail'}
    <PromptRailSlot placement="dock" clearFeedback={side !== 'left'}>
      <PromptDock {view} {logic} seat={0} {placement} onPlacementChange={(p) => (placement = p)} />
    </PromptRailSlot>
  {/if}
  <div class="main">rail</div>
  <section class="transcript">log</section>
  {#if placement === 'rail-bottom'}
    <PromptRailSlot placement="dock-bottom" clearFeedback={side !== 'left'}>
      <PromptDock {view} {logic} seat={0} {placement} onPlacementChange={(p) => (placement = p)} />
    </PromptRailSlot>
  {/if}
</aside>
{#if !inRail}
  <PromptDock {view} {logic} seat={0} {placement} {position} onPlacementChange={(p) => (placement = p)} onPositionChange={(p) => (position = p)} />
{/if}
<FeedbackButton />

<style>
  .board { position: fixed; inset: 0; background: #0d1015; }
  .rail {
    position: fixed; top: 0; bottom: 0; width: 340px;
    display: flex; flex-direction: column; background: #12151b; z-index: 5; color: #ddd;
  }
  .rail.right { right: 0; }
  .rail.left { left: 0; }
  /* The hidden drawer, peeked in (Table.svelte's rail-peek state). */
  .rail.hidden { right: 0; width: min(22rem, 85vw); z-index: 30; }
  .main { flex: 1 1 0; min-height: 0; }
  .transcript { flex: 0 0 38%; min-height: 0; border-top: 1px solid #333; }
</style>
