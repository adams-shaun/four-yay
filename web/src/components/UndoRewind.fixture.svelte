<script lang="ts">
  import type { CardView, Decision, Frame, Intent, PaymentAction, PlayerView, SeatInfo, View } from '../protocol';
  import { MatchState } from '../lib/match.svelte';
  import { SeatPanelState } from '../lib/seatpanel.svelte';
  import HotButtonStrip from './HotButtonStrip.svelte';

  /**
   * UndoRewind.fixture.svelte composes the live seated route's decision path
   * exactly as Table.svelte wires it -- the real MatchState (seat-scoped view
   * fetches, rewind classification), the per-match SeatPanelState built in a
   * $derived, Table's onFrame glue (`if (m.apply(f)) panel.rewind()`), and the
   * real HotButtonStrip (the UNDO button, and the strip SeatPanel whose
   * effects adopt view.decision and poll /pending) -- in front of a simulated
   * server behind a stubbed window.fetch.
   *
   * The scenario is the 2026-09-26 demo report (table g3, round 7): the seat's
   * pending ask is the Lord Windgrace +2 discard at seq 1526; UNDO rewinds the
   * server to the seat's last intent, the priority window at seq 1519 with the
   * +2 still on the stack, where Harrow is castable through an Auto Mana
   * payment plan. `?autopay=0` runs the same flow with the Auto Mana toggle
   * off.
   */

  const TABLE = 'g3';
  const ctx = { seat: 0, token: 'fixture-token' };
  const autopay = new URLSearchParams(window.location.search).get('autopay') !== '0';

  const seats: SeatInfo[] = [
    { name: 'You', deck: 'hearthhull-worldseed-landfall', colour: '#e5484d', human: true },
    { name: 'Bot', deck: 'pro-shaper', colour: '#22c55e' },
  ];
  const card = (id: number, name: string, types: string): CardView => ({
    id, name, types, mana_cost: '', printing: { name }, token: '', tapped: false, power: 0, toughness: 0,
    damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
  });
  const hand = [
    card(11, 'Forest', 'Land'),
    card(12, 'Swamp', 'Land'),
    card(13, 'Harrow', 'Instant'),
    card(14, 'Cultivate', 'Sorcery'),
    card(15, 'Mountain', 'Land'),
  ];
  const player = (seat: number): PlayerView => ({
    seat, name: seats[seat].name, life: seat === 0 ? 39 : 40, lost: false, library_size: 80,
    hand_size: seat === 0 ? hand.length : 6, graveyard_size: 0, hand: seat === 0 ? hand : null,
    battlefield: seat === 0 ? [card(46, 'Lord Windgrace', 'Legendary Planeswalker')] : [],
    graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
  } as unknown as PlayerView);

  // The discard ask the pre-undo tail posed (seq 1526): five hand-card picks.
  const discard = (seq: number): Decision => ({
    seq, player: 0, kind: 'choose', prompt: 'Choose a card to discard', min: 1, max: 1, source: 46,
    options: hand.map((c, index) => ({ index, kind: 'discard', label: c.name, obj: c.id, player: 0 })),
  });

  // The window the undo restores (seq 1519): priority with the +2 on the
  // stack, one castable instant and its Auto Mana payment action. Payment
  // identities bind the decision seq (cast-payment-plans spec §4).
  const harrowPayment = (seq: number): PaymentAction => ({
    id: `pa-${seq}-13`, cast: { object: 13, face: 0, origin: 'hand' }, base_option_index: 0, label: 'Cast Harrow',
    plans: [{
      version: 1, id: `plan-${seq}-13`, cost: { generic: 2, mana: [0, 0, 0, 0, 1, 0] },
      activations: [], pool_spend: [0, 0, 0, 0, 0, 0], pool_after: [0, 0, 0, 0, 0, 0],
    }],
  });
  const priority = (seq: number): Decision => ({
    seq, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1,
    options: [
      { index: 0, kind: 'cast', label: 'Cast Harrow', obj: 13, player: 0 },
      { index: 1, kind: 'pass', label: 'Pass priority', player: 0 },
      { index: 2, kind: 'concede', label: 'Concede', player: 0 },
    ],
    payment_actions: [harrowPayment(seq)],
  });

  const windgraceOnStack: View['stack'] = [{
    id: 900, kind: 'ability', name: 'Lord Windgrace', text: '+2: Discard a card, then draw a card.',
    controller: 0, source: 46, targets: [], optional: false,
  }];
  // The seat view at head embeds the seat's parked decision (host/viewat.go);
  // the pushed snapshot/rewind body is the spectator projection, which never
  // does.
  const seatViewAt = (d: Decision, stack: View['stack']): View => ({
    viewer: 0, visibility: 'seat', turn: 13, round: 7, step: 'main1', phase: 'main1',
    active: 0, priority: 0, over: false, draw: false, winner: null,
    players: [player(0), player(1)], stack, pending: [], decision: d,
  });
  const spectator = (v: View): View => ({ ...v, viewer: -1, visibility: 'public', decision: null });

  // ---- the simulated server ---------------------------------------------
  // `live` is what the server holds right now: its head, the decision parked
  // for seat 0, and the seat view at that head.
  type Server = { head: number; pending: Decision; view: View };
  const preUndo: Server = { head: 1526, pending: discard(1526), view: seatViewAt(discard(1526), []) };
  const rewound: Server = { head: 1519, pending: priority(1519), view: seatViewAt(priority(1519), windgraceOnStack) };
  let live: Server = preUndo;

  const posts: { body: Intent; status: number; message?: string }[] = [];
  const undos: string[] = [];
  let rewoundPendingReads = 0;
  const json = (status: number, body: unknown) =>
    new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const u = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    const method = (init?.method ?? 'GET').toUpperCase();
    if (method === 'POST' && u.includes('/undo')) {
      undos.push(u);
      // host.Registry.Undo queues the request; the loop parked on this seat
      // services it at once and only then pushes the rewind frame, which the
      // test delivers with __deliverRewind.
      live = rewound;
      return new Response(null, { status: 204 });
    }
    if (method === 'POST' && u.includes('/intent')) {
      const body = JSON.parse(String(init?.body)) as Intent;
      // decision.Validate's stale-seq fence, surfaced as the 409 conflict
      // host/httpapi's writeSeatError sends.
      if (body.seq !== live.pending.seq) {
        const message = `intent seq ${body.seq}, pending decision seq ${live.pending.seq}`;
        posts.push({ body, status: 409, message });
        return json(409, { code: 'conflict', message });
      }
      posts.push({ body, status: 204 });
      return new Response(null, { status: 204 });
    }
    if (u.includes('/pending')) {
      if (live === rewound) rewoundPendingReads++;
      return json(200, live.pending);
    }
    if (u.includes('/view')) return json(200, live.view);
    if (u.includes('/events')) return json(200, []);
    return json(404, { code: 'not_found', message: u });
  };

  // ---- Table.svelte's composition ---------------------------------------
  const m = new MatchState(TABLE, ctx);
  let panelCache: { match: number; state: SeatPanelState } | null = null;
  const panel = $derived.by(() => {
    const mm = m.match;
    if (mm === null) return null;
    if (panelCache === null || panelCache.match !== mm) {
      const state = new SeatPanelState(TABLE, mm, ctx, null, null);
      state.adoptView(m.view?.decision ?? null);
      panelCache = { match: mm, state };
    }
    return panelCache.state;
  });
  // The table's auto_mana capability, and the player's Auto Mana toggle.
  $effect(() => {
    panel?.setAutoManaAvailable(true);
    if (autopay) panel?.setAutoPayMana(true);
  });
  // Table.svelte's onFrame body, verbatim.
  const onFrame = (f: Frame) => {
    if (m.apply(f)) panelCache?.state.rewind();
  };

  const frame = (t: string, seq: number, body: unknown): Frame => ({ v: 1, t, table: TABLE, match: 1, seq, body });
  onFrame(frame('match_start', 0, { seats, seed: 1, spectator: 'public', bot_policy: 'bot' }));
  onFrame(frame('snapshot', preUndo.head, { view: spectator(preUndo.view), turn_starts: [], head: preUndo.head, seats }));

  const w = window as unknown as {
    __deliverRewind: () => void;
    __state: () => { pending: number | null; kind: string | null; active: number | null; error: string | null; paused: boolean; busy: boolean };
    __rendered: () => number | null;
    __rewoundPendingReads: () => number;
    __posts: () => typeof posts;
    __undos: () => string[];
    __click: (index: number) => void;
  };
  // host/undo.go's pushRewind: the rewind frame (the full snapshot at the new
  // head), then the decision frame for the re-parked ask.
  w.__deliverRewind = () => {
    onFrame(frame('rewind', rewound.head, { view: spectator(rewound.view), turn_starts: [], head: rewound.head, seats }));
    onFrame(frame('decision', rewound.head, { player: 0, kind: rewound.pending.kind, prompt: rewound.pending.prompt }));
  };
  w.__state = () => ({
    pending: panel?.pending?.seq ?? null,
    kind: panel?.pending?.kind ?? null,
    active: panel?.active?.seq ?? null,
    error: panel?.error ?? null,
    paused: panel?.machinePaused ?? false,
    busy: panel?.busy ?? false,
  });
  w.__rendered = () => m.renderedSeq;
  w.__rewoundPendingReads = () => rewoundPendingReads;
  w.__posts = () => posts;
  w.__undos = () => undos;
  // Every answering surface (the panel's option buttons, the board tiles, the
  // hand fan) posts through SeatPanelState.click; with Auto Mana on, a cast
  // click is routed to the offered payment plan.
  w.__click = (index) => panel?.click(index);
</script>

{#if m.view && panel && m.match !== null}
  <HotButtonStrip view={m.view} seats={m.seats} state={panel} {ctx} table={TABLE} match={m.match} />
{/if}
