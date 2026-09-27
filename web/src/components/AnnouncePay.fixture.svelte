<script lang="ts">
  import type { CardView, Decision, PaymentPlan, PlayerView, SeatInfo, View } from '../protocol';
  import { SeatPanelState, toneOf } from '../lib/seatpanel.svelte';
  import { optionsByObj, optionsByPlayer, type CardOptions } from '../lib/cardoptions';
  import { castableActions } from '../lib/announcepay';
  import SeatPanel from './SeatPanel.svelte';
  import HandFan from './HandFan.svelte';
  import Board from './Board.svelte';

  /**
   * AnnouncePay.fixture.svelte mounts announce-then-pay's surfaces around ONE
   * real SeatPanelState with Auto-pay OFF on an auto-mana table
   * (docs/superpowers/specs/2026-09-27-announce-then-pay.md §8): the hand
   * card's CAST shortcut and the board tiles wired exactly as Table.svelte
   * wires them, and the seat panel. The network is a window.fetch stub that
   * records every POSTed intent; __deliver(seq) hands the panel the
   * announced select-mana window the engine would pose next.
   */

  const posts: unknown[] = [];
  let current: Decision | null = null;
  const json = (body: unknown, status: number) =>
    new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    if (url.includes('/intent') && init?.method === 'POST') {
      posts.push(JSON.parse(String(init.body)));
      return new Response(null, { status: 204 });
    }
    if (url.includes('/pending')) {
      return current === null ? json({ code: 'conflict', message: 'nothing pending' }, 409) : json(current, 200);
    }
    return json({ code: 'not_found', message: url }, 404);
  };

  const ctx = { seat: 0, token: 'fixture-token' };
  const panel = new SeatPanelState('fixture', 1, ctx, null, null);
  panel.stops = { yours: new Set(), opponents: new Set() };
  panel.setAuto(false);
  panel.setAutoManaAvailable(true);

  const plan: PaymentPlan = {
    version: 1, id: 'plan-20', cost: { generic: 0, mana: [0, 1, 2, 0, 0, 0] },
    activations: [
      { source: 41, source_zone_seq: 1, ability: { kind: 'intrinsic', intrinsic: 'basic_land' }, produces: [0, 0, 1, 0, 0, 0] },
      { source: 42, source_zone_seq: 2, ability: { kind: 'intrinsic', intrinsic: 'basic_land' }, produces: [0, 0, 1, 0, 0, 0] },
      { source: 43, source_zone_seq: 3, ability: { kind: 'intrinsic', intrinsic: 'basic_land' }, produces: [0, 1, 0, 0, 0, 0] },
    ],
    pool_spend: [0, 0, 0, 0, 0, 0], pool_after: [0, 0, 0, 0, 0, 0],
  };
  // Priority: the lands' manual taps, pass; the {B}{B}{U} spell is payable
  // only with the lands, so it is a plan-only payment action.
  const priority: Decision = {
    seq: 20, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1,
    options: [
      { index: 0, kind: 'activate', label: 'Activate Swamp for mana', obj: 41, player: 0 },
      { index: 1, kind: 'activate', label: 'Activate Swamp for mana', obj: 42, player: 0 },
      { index: 2, kind: 'activate', label: 'Activate Underground Sea for mana', obj: 43, player: 0 },
      { index: 3, kind: 'activate', label: 'Activate Badlands for mana', obj: 44, player: 0 },
      { index: 4, kind: 'pass', label: 'Pass priority', player: 0 },
    ],
    payment_actions: [{ id: 'action-20', cast: { object: 22, face: 0, origin: 'hand' }, base_option_index: null, label: 'Cast Grixis Charm Test', plans: [plan] }],
  };
  // The announced window the engine poses after the announce.
  const payWindow: Decision = {
    seq: 21, player: 0, kind: 'choose', prompt: 'Pay for Grixis Charm Test', min: 1, max: 1, source: 22,
    options: [
      { index: 0, kind: 'mana', label: 'Add B', obj: 41, player: 0 },
      { index: 1, kind: 'mana', label: 'Add B', obj: 42, player: 0 },
      { index: 2, kind: 'mana', label: 'Add U', obj: 43, mana_symbol: 'U', player: 0 },
      { index: 3, kind: 'mana', label: 'Add B', obj: 43, mana_symbol: 'B', player: 0 },
      { index: 4, kind: 'mana', label: 'Add B', obj: 44, mana_symbol: 'B', player: 0 },
      { index: 5, kind: 'mana', label: 'Add R', obj: 44, mana_symbol: 'R', player: 0 },
      { index: 6, kind: 'autofill', label: 'Auto-fill: tap Swamp, Swamp, Underground Sea', player: 0 },
      { index: 7, kind: 'cancel_cast', label: 'Cancel cast', player: 0 },
    ],
    mana_payment: { card: 22, cost: { generic: 0, mana: [0, 1, 2, 0, 0, 0] }, owed: { generic: 0, mana: [0, 1, 2, 0, 0, 0] },
      pool: [0, 0, 0, 0, 0, 0], autofill: [41, 42, 43] },
  };
  const card = (id: number, name: string, types: string, cost = ''): CardView => ({
    id, name, types, mana_cost: cost, printing: { name }, token: '',
    tapped: false, power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
  });
  const me: PlayerView = {
    seat: 0, name: 'Ari', life: 20, lost: false, library_size: 40, hand_size: 1, graveyard_size: 0,
    hand: [card(22, 'Grixis Charm Test', 'Instant', 'B B U')],
    battlefield: [card(41, 'Swamp', 'Basic Land Swamp'), card(42, 'Swamp', 'Basic Land Swamp'),
      card(43, 'Underground Sea', 'Land Island Swamp'), card(44, 'Badlands', 'Land Swamp Mountain')],
    graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
  };
  const bot: PlayerView = { ...me, seat: 1, name: 'Bot', hand: [], hand_size: 5, battlefield: [] };
  const seats: SeatInfo[] = [{ name: 'Ari', deck: 'grixis', colour: '#e5484d', human: true }, { name: 'Bot', deck: 'stompy', colour: '#30a46c' }];

  current = priority;
  panel.adoptView(current);
  let view = $state<View>({
    viewer: 0, visibility: 'seat', turn: 3, round: 3, step: 'main1', phase: 'main1', active: 0, priority: 0,
    over: false, draw: false, winner: null, players: [me, bot], stack: [], pending: [], decision: current,
  });

  // Table.svelte's boardOptions, reduced to what a board tile reads.
  const boardOptions = $derived.by((): CardOptions | null => {
    const d = panel.active;
    if (d === null) return null;
    return { source: d.source, byObj: optionsByObj(d), byPlayer: optionsByPlayer(d), picked: [...panel.picked], tone: toneOf(d),
      post: (index: number, _f = false, holdPriority = false) => panel.click(index, { holdPriority }) };
  });

  const w = window as unknown as { __posts: () => unknown[]; __deliver: () => void; __state: () => { active: number | null; kind: string | null } };
  w.__posts = () => JSON.parse(JSON.stringify(posts)) as unknown[];
  w.__deliver = () => {
    current = payWindow;
    panel.adoptView(current);
    view = { ...view, decision: current };
  };
  w.__state = () => ({ active: panel.active?.seq ?? null, kind: panel.active?.kind ?? null });
</script>

<div class="stage">
  <div class="felt"><Board {view} {seats} options={boardOptions} /></div>
  <SeatPanel {view} {seats} {ctx} table="fixture" match={1} state={panel} />
  <!-- Table.svelte's HandFan wiring, verbatim. -->
  <HandFan player={view.players[0]} options={boardOptions} paymentActions={castableActions(panel?.active ?? null, panel?.autoManaAvailable ?? false)} autoPay={panel?.autoPayMana ?? false} onCastPayment={(action, holdPriority) => panel?.castAction(action, holdPriority)} />
</div>

<style>
  .stage { position: relative; width: 100vw; height: 100vh; overflow: hidden; background: var(--felt); }
  .felt { position: absolute; inset: 0; }
</style>
