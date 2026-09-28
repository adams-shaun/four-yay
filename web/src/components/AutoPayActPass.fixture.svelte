<script lang="ts">
  import type { CardView, Decision, PaymentPlan, PlayerView, SeatInfo, View } from '../protocol';
  import { SeatPanelState } from '../lib/seatpanel.svelte';
  import { castableActions } from '../lib/announcepay';
  import SeatPanel from './SeatPanel.svelte';
  import HotButtonStrip from './HotButtonStrip.svelte';
  import HandFan from './HandFan.svelte';

  /**
   * AutoPayActPass.fixture.svelte mounts the UI entry points that submit a
   * planned cast around ONE real SeatPanelState with pass-after-acting ON and
   * the auto-pay preference ON (spec §8: "Auto-pay changes which witness an
   * explicit cast uses; it does not otherwise change Auto/Manual policy"):
   *
   *  - ?surface=panel — the seat-panel option list's plan button (SeatPanel);
   *  - ?surface=strip — the same list inside the hot strip's ACTIONS drop
   *    (HotButtonStrip's nested SeatPanel, where every in-game decision lives);
   *  - ?surface=hand  — the hand card's CAST shortcut (HandFan), wired to the
   *    panel exactly as Table.svelte wires onCastPayment, with the hot strip
   *    mounted beside it the way the table mounts it (its nested SeatPanel
   *    runs the autopilot loop that spends the armed token).
   *
   * The network is a window.fetch stub (the FeedbackButton / SeatPanelFollowUp
   * fixture trick): every POSTed intent is recorded, /pending answers the
   * current decision. A test clicks an entry point (with or without Ctrl),
   * delivers the next priority window with __deliver, and reads back whether
   * the machine passed it.
   */

  const surface = new URLSearchParams(window.location.search).get('surface') ?? 'panel';

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
  panel.setActPass(true);
  panel.settings = { ...panel.settings, pacing: { stepMs: 0, resolveMs: 0 } };
  panel.setAutoManaAvailable(true);
  panel.setAutoPayMana(true);

  const plan = (id: string): PaymentPlan => ({
    version: 1, id, cost: { generic: 0, mana: [0, 1, 0, 0, 0, 0] },
    activations: [{ source: 41, source_zone_seq: 9, ability: { kind: 'intrinsic', intrinsic: 'basic_land' }, produces: [0, 1, 0, 0, 0, 0] }],
    pool_spend: [0, 0, 0, 0, 0, 0], pool_after: [0, 0, 0, 0, 0, 0],
  });
  // A priority window: the Island's manual tap, Opt's legacy cast (index 7,
  // the base of its payment action), pass (8) and concede (9). The payment
  // identities bind the seq, as the engine's do.
  const decision = (seq: number): Decision => ({
    seq, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1,
    options: [
      { index: 0, kind: 'activate', label: 'Activate Island for mana', obj: 41, player: 0 },
      { index: 7, kind: 'cast', label: 'Cast Opt', obj: 22, player: 0 },
      { index: 8, kind: 'pass', label: 'Pass priority', player: 0 },
      { index: 9, kind: 'concede', label: 'Concede', player: 0 },
    ],
    payment_actions: [{
      id: `action-${seq}`, cast: { object: 22, face: 0, origin: 'hand' }, base_option_index: 7,
      label: 'Cast Opt', plans: [plan(`plan-${seq}-a`)],
    }],
  });
  const card = (id: number, name: string, types: string): CardView => ({
    id, name, types, mana_cost: types === 'Instant' ? 'U' : '', printing: { name }, token: '',
    tapped: false, power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
  });
  const me: PlayerView = {
    seat: 0, name: 'Ari', life: 20, lost: false, library_size: 40, hand_size: 1, graveyard_size: 0,
    hand: [card(22, 'Opt', 'Instant')], battlefield: [card(41, 'Island', 'Basic Land Island')],
    graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
  };
  const seats: SeatInfo[] = [{ name: 'Ari', deck: 'fixture', colour: '#e5484d', human: true }];

  current = decision(17);
  panel.adoptView(current);
  // The seat's own draw step, empty stack: no stop rule applies, so an armed
  // pass-after-acting token passes the next window.
  let view = $state<View>({
    viewer: 0, visibility: 'seat', turn: 2, round: 2, step: 'draw', phase: 'beginning', active: 0, priority: 0,
    over: false, draw: false, winner: null, players: [me], stack: [], pending: [], decision: current,
  });

  // window.* casts (not a `declare global`) keep svelte-check quiet.
  const w = window as unknown as {
    __posts: () => unknown[];
    __deliver: (seq: number) => void;
    __state: () => { active: number | null; postedSeq: number | null; actPassed: number; busy: boolean };
  };
  w.__posts = () => JSON.parse(JSON.stringify(posts)) as unknown[];
  w.__deliver = (seq) => {
    current = decision(seq);
    view = { ...view, decision: current };
  };
  w.__state = () => ({ active: panel.active?.seq ?? null, postedSeq: panel.postedSeq, actPassed: panel.actPassed, busy: panel.busy });
</script>

{#if surface === 'panel'}
  <SeatPanel {view} {seats} {ctx} table="fixture" match={1} state={panel} />
{:else}
  <HotButtonStrip {view} {seats} state={panel} {ctx} table="fixture" match={1} />
  {#if surface === 'hand'}
    <!-- Table.svelte's HandFan wiring, verbatim. -->
    <HandFan player={view.players[0]} width={900} paymentActions={castableActions(panel?.active ?? null, panel?.autoManaAvailable ?? false)} autoPay={panel?.autoPayMana ?? false} onCastPayment={(action, holdPriority) => panel?.castAction(action, holdPriority)} />
  {/if}
{/if}
