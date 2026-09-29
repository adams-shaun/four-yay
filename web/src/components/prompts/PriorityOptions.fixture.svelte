<script lang="ts">
  import type { Decision, PaymentAction, PlayerView, SeatInfo, View } from '../../protocol';
  import { SeatPanelState } from '../../lib/seatpanel.svelte';
  import PriorityOptions from './PriorityOptions.svelte';

  /**
   * PriorityOptions fixture mounts the real PriorityOptions against a real
   * SeatPanelState so a mounted browser test can CLICK the rendered control
   * and read the intent it posts. `case` picks the seat's Auto-pay
   * preference:
   *
   *  - 'manabrew' — the reported ManaBrew seat (Aether Vial, Auto-pay OFF):
   *    the wire offers only a plan-less `pay-<id>` payment action, which the
   *    announce button must render and post as `announce`.
   *  - 'manabrew-autopay' — Auto-pay ON: castAction has no route for a
   *    plan-less action, so no CAST affordance may render.
   *
   * window.fetch is the AutoPayActPass / FeedbackButton recording stub: every
   * POSTed intent lands in window.__posts without a server.
   */

  const posts: unknown[] = [];
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    if (url.includes('/intent') && init?.method === 'POST') {
      posts.push(JSON.parse(String(init.body)));
      return new Response(null, { status: 204 });
    }
    return new Response(JSON.stringify({ code: 'not_found', message: url }), {
      status: 404, headers: { 'Content-Type': 'application/json' },
    });
  };
  (window as unknown as { __posts: () => unknown[] }).__posts = () => JSON.parse(JSON.stringify(posts)) as unknown[];
  (window as unknown as { __state: () => Record<string, unknown> }).__state = () => ({
    error: logic.error, busy: logic.busy, pending: logic.pending?.seq ?? null, postedSeq: logic.postedSeq,
    paymentPlanCounts: logic.pending?.payment_actions?.map((action) => action.plans.length) ?? [],
  });

  const planless: PaymentAction = {
    id: 'pay-4ad785b7', cast: { object: 54, face: 0, origin: 'hand' }, label: 'Cast Aether Vial', plans: [],
  };
  const decision: Decision = {
    seq: 109, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
    options: [{ index: 0, player: 0, kind: 'activate', label: 'Activate Rishadan Port for mana', obj: 15 }],
    payment_actions: [planless],
  };

  const ctx = { seat: 0, token: 'fixture-token' };
  const logic = new SeatPanelState('fixture', 1, ctx, null, null);
  logic.setAutoManaAvailable(true);
  const autoPayMana = new URLSearchParams(window.location.search).get('case') === 'manabrew-autopay';
  logic.setAutoPayMana(autoPayMana);
  logic.adoptView(decision);

  const card = (id: number, name: string, types: string): PlayerView['hand'][number] => ({
    id, name, types, printing: { name }, token: '', tapped: false, power: 0, toughness: 0,
    damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
  });
  const me: PlayerView = {
    seat: 0, name: 'Ari', life: 20, lost: false, library_size: 40, hand_size: 0, graveyard_size: 0,
    completed_dungeons: 0,
    hand: [], battlefield: [card(15, 'Rishadan Port', 'Land')], graveyard: [], exile: [], pool: {},
    command: [], commanders: [], commander_casts: [],
  };
  const seats: SeatInfo[] = [{ name: 'Ari', deck: 'fixture', colour: '#e5484d', human: true }];
  const view: View = {
    viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'main1', phase: 'main1', active: 0, priority: 0,
    over: false, draw: false, winner: null, players: [me], stack: [], pending: [], decision,
  };

  // Production passes the panel's OWN pending decision (SeatPanel.svelte and
  // PromptDock.svelte both derive it from logic), never the raw view object:
  // submitAnnounce re-resolves the clicked action against that same identity,
  // so the fixture must hand PriorityOptions the same proxy or the guard
  // (offered !== action) rejects an honest click.
  const rendered = $derived(logic.active);
</script>

{#if rendered}
  <PriorityOptions decision={rendered} {view} {logic} seat={0} placement="board" />
{/if}
