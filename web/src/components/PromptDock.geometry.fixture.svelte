<script lang="ts">
  import { IMAGE_KEY } from '../lib/images';
  import type { CardView, Decision, Option, PlayerView, SeatInfo, StackView, View } from '../protocol';
  import { SeatPanelState } from '../lib/seatpanel.svelte';
  import { type DockPlacement } from '../lib/prompts/dock';
  import PromptDock from './prompts/PromptDock.svelte';

  /**
   * Geometry fixture body for PromptDock's near-table placement (the ui24
   * leaf): the real component mounted over a fake board, so the measured
   * hit-testing is what a real prompt produces.
   *
   * The board is a full-viewport fixed surface with the action cluster at
   * its bottom-right — the dock pins above that cluster, right-aligned with
   * it, so its rect covers the board's lower-right corner. The geometry test
   * pins the pile badge against the dock's MEASURED rect before clicking, so
   * the overlap precondition can never silently pass.
   */

  // The shared images resolver reads the exact-name localStorage key first,
  // so a stored '' is a known no-image: no /art/named request ever fires.
  localStorage.setItem(`${IMAGE_KEY}Lightning Bolt`, '');
  localStorage.setItem(`${IMAGE_KEY}Tarmogoyf`, '');

  const ctx = { seat: 0, token: 'tok' };
  const seats: SeatInfo[] = [
    { name: 'Ari', deck: 'burn', colour: '#e5484d' },
    { name: 'Mira', deck: 'stompy', colour: '#30a46c' },
  ];
  const bolt: CardView = {
    id: 7, name: 'Lightning Bolt', types: 'Instant', printing: { name: 'Lightning Bolt' }, token: '#7',
    tapped: false, power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
  } as unknown as CardView;
  const goyf: CardView = {
    id: 20, name: 'Tarmogoyf', types: 'Creature — Lhurgoyf', printing: { name: 'Tarmogoyf' }, token: '#20',
    tapped: false, power: 4, toughness: 5, damage: 0, attacking: false, counters: {}, keywords: [],
    controller: 1, owner: 1, summon_sick: false,
  } as unknown as CardView;
  const player = (seat: number, battlefield: CardView[] = []): PlayerView => ({
    seat, name: seats[seat].name, life: 20, lost: false, library_size: 53, hand_size: 0, graveyard_size: 0, hand: [],
    battlefield, graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [], completed_dungeons: 0,
  } as unknown as PlayerView);
  const opt = (index: number, kind: string, label: string, extra: Partial<Option> = {}): Option => ({ index, kind, label, player: 1, ...extra });

  // min 1 / max 3: a multi-target ask, so a chip click TOGGLES into `picked`
  // and never posts — the fixture stays offline.
  const target: Decision = {
    seq: 11, player: 0, kind: 'target', prompt: 'Choose targets for Lightning Bolt', min: 1, max: 3, source: 7,
    target_effect: { api: 'DealDamage', damage: { amount: 3 } },
    options: [opt(0, 'player', 'Mira', { player: 1 }), opt(1, 'permanent', 'Tarmogoyf (Mira)', { obj: 20 })],
  } as unknown as Decision;

  // The long-list ask (promptdock2): the ui24 repro needs a tall dock whose
  // numbered rows reach down over the board. min 1 / max 3 keeps a row click
  // a toggle, so the fixture never posts.
  const long: Decision = {
    seq: 12, player: 0, kind: 'choose', prompt: 'Choose attackers', min: 1, max: 3,
    options: [0, 1, 2, 3, 4, 5, 6].map((i) => opt(i, 'permanent', `Memnite ${i + 1} → Player 2`, { obj: 30 + i })),
  } as unknown as Decision;

  const longAsk = typeof location === 'undefined' ? false : new URLSearchParams(location.search).get('ask') === 'long';
  const ask = longAsk ? long : target;

  const view: View = {
    viewer: 0, visibility: 'seat', turn: 7, round: 4, step: 'main1', phase: 'main1', active: 0, priority: 0,
    over: false, draw: false, winner: null, players: [player(0), player(1, [goyf])],
    stack: [{ id: 7, kind: 'spell', name: 'Lightning Bolt', text: '', controller: 0, targets: [], card: bolt, optional: false } as StackView],
    pending: [],
    decision: ask,
  } as unknown as View;

  const logic = new SeatPanelState('t1', 1, ctx, null, null);
  logic.adoptView(ask);

  // The placement prop pins the dock at its near-table default for the
  // fixture; the toggle writes back through onPlacementChange (as
  // Table.svelte's layout profile does), so the test can still exercise the
  // tools button.
  let placement = $state<DockPlacement>('table');

  // The badge's own effect: the board answering the click. The test asserts
  // THIS, not just that Playwright's actionability let go of the pointer.
  function badgeClicked(): void {
    document.body.setAttribute('data-badge-clicked', '1');
  }
</script>

<div class="board">
  <div class="tile" data-obj="20" data-options="7">Tarmogoyf <button type="button" class="badge" data-pile-badge aria-haspopup="menu" onclick={badgeClicked}>3</button></div>
  <div class="cluster" data-action-cluster><button type="button">Pass turn</button></div>
</div>
<aside class="rail">
  <PromptDock {view} {logic} seat={0} {placement} onPlacementChange={(p) => (placement = p)} />
</aside>

<style>
  .board {
    position: fixed;
    inset: 0 340px 0 0;
    background: #0d1015;
  }
  /* The rail column the dock falls back to when it yields: the near-table
     dock is position:fixed and ignores it; the yielded dock is in flow here,
     clear of the board. */
  .rail {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: 340px;
    background: #12151b;
    z-index: 5;
    overflow-y: auto;
  }
  .cluster {
    position: absolute;
    right: 2rem;
    bottom: 1rem;
    width: 280px;
    height: 56px;
    background: #2a2117;
  }
  .tile {
    position: absolute;
    right: 4rem;
    bottom: 7rem;
    width: 9rem;
    height: 12.4rem;
    background: #20303c;
    color: #eee;
  }
  .badge {
    display: inline-block;
    width: 40px;
    height: 24px;
    line-height: 24px;
    text-align: center;
    background: #7a4d1d;
    border-radius: 6px;
    border: 0;
    color: #fff;
    cursor: pointer;
    padding: 0;
  }
</style>
