import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import type { CardView, Decision, PaymentAction, PlayerView, SeatInfo, View } from '../protocol';
import { SeatPanelState } from '../lib/seatpanel.svelte';
import ManaPaymentPanel from './ManaPaymentPanel.svelte';
import SeatPanel from './SeatPanel.svelte';
import HandFan from './HandFan.svelte';

// SSR via svelte/server, the repo's component-test pattern (see
// SeatPanel.svelte.test.ts): no DOM, no effects, nothing reaches the network.
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  postIntent: vi.fn(),
  fetchPending: vi.fn(),
}));
vi.mock('../lib/images', () => ({ images: { url: () => new Promise<string | null>(() => {}), offline: () => false } }));

const ctx = { seat: 0, token: 'tok' };
const seats: SeatInfo[] = [{ name: 'you', deck: 'grixis', colour: '#e5484d' }, { name: 'bot', deck: 'stompy', colour: '#30a46c' }];
const land = (id: number, name: string, tapped = false): CardView => ({
  id, name, types: 'Land', printing: { name }, token: '', tapped, power: 0, toughness: 0,
  damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
});
const spell = (id: number, name: string): CardView => ({
  id, name, types: 'Instant', mana_cost: 'B B U', printing: { name }, token: '', tapped: false, power: 0, toughness: 0,
  damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
});
const player = (seat: number, hand: CardView[], battlefield: CardView[]): PlayerView => ({
  seat, name: seats[seat].name, life: 20, lost: false, library_size: 40, hand_size: hand.length, graveyard_size: 0,
  completed_dungeons: 0,
  hand, battlefield, graveyard: [], exile: [], pool: {}, command: [], commanders: [], commander_casts: [],
});
const view = (d: Decision | null, hand: CardView[] = []): View => ({
  viewer: 0, visibility: 'seat', turn: 3, round: 3, step: 'main1', phase: 'main1', active: 0, priority: 0,
  over: false, draw: false, winner: null,
  players: [player(0, hand, [land(41, 'Swamp'), land(42, 'Swamp'), land(43, 'Underground Sea'), land(44, 'Island', true)]), player(1, [], [])],
  stack: [], pending: [], decision: d,
} as View);

// The walkthrough's {B}{B}{U} spell after one Island was tapped for {U}.
const payWindow: Decision = {
  seq: 40, player: 0, kind: 'choose', prompt: 'Pay for Grixis Test', min: 1, max: 1, source: 22,
  options: [
    { index: 0, player: 0, kind: 'mana', label: 'Add B', obj: 41 },
    { index: 1, player: 0, kind: 'mana', label: 'Add B', obj: 42 },
    { index: 2, player: 0, kind: 'mana', label: 'Add U', obj: 43, mana_symbol: 'U' },
    { index: 3, player: 0, kind: 'mana', label: 'Add B', obj: 43, mana_symbol: 'B' },
    { index: 4, player: 0, kind: 'autofill', label: 'Auto-fill: tap Swamp, Swamp' },
    { index: 5, player: 0, kind: 'undo_tap', label: 'Undo tapping Island', obj: 44 },
    { index: 6, player: 0, kind: 'cancel_cast', label: 'Cancel cast' },
  ],
  mana_payment: { card: 22, cost: { generic: 0, mana: [0, 1, 2, 0, 0, 0] }, owed: { generic: 0, mana: [0, 0, 2, 0, 0, 0] },
    pool: [0, 1, 0, 0, 0, 0], autofill: [41, 42] },
};

describe('ManaPaymentPanel — the select-mana prompt', () => {
  it('shows cost, owed and pool, one row per source with one button per ability, and the window controls', () => {
    const picks: number[] = [];
    const { html } = render(ManaPaymentPanel, { props: { decision: payWindow as Decision & { mana_payment: NonNullable<Decision['mana_payment']> }, view: view(payWindow), onPick: (i: number) => picks.push(i) } });
    expect(html).toContain('data-mana-payment');
    expect(html).toContain('aria-label="Pay for Grixis Test"');
    for (const cell of ['data-mp-cost', 'data-mp-owed', 'data-mp-pool']) expect(html).toContain(cell);
    // Rows in offer order, names from the battlefield, suggested marked.
    const rows = [...html.matchAll(/data-mana-source="(\d+)"/g)].map((m) => Number(m[1]));
    expect(rows).toEqual([41, 42, 43]);
    expect(html).toMatch(/class="mp-source[^"]*suggested[^"]*" data-mana-source="41"/);
    expect(html).not.toMatch(/class="mp-source[^"]*suggested[^"]*" data-mana-source="43"/);
    // The dual land offers both abilities, each with an accessible name.
    expect(html).toContain('aria-label="Tap Underground Sea for U"');
    expect(html).toContain('aria-label="Tap Underground Sea for B"');
    expect(html).toContain('aria-label="Tap Swamp for B"');
    expect(html).toContain('data-mp-autofill="4"');
    expect(html).toContain('data-mp-undo="5"');
    expect(html).toContain('data-mp-cancel="6"');
    expect(html).toContain('Auto-fill: tap Swamp, Swamp');
    expect(html).not.toContain('data-mp-pay');
  });

  it('says so when no source can help, leaving undo and cancel', () => {
    const stuck: Decision = { ...payWindow, options: [payWindow.options[5], payWindow.options[6]].map((o, i) => ({ ...o, index: i })) };
    const { html } = render(ManaPaymentPanel, { props: { decision: stuck as Decision & { mana_payment: NonNullable<Decision['mana_payment']> }, view: view(stuck), onPick: () => {} } });
    expect(html).toContain('data-mp-no-sources');
    expect(html).toContain('data-mp-undo="0"');
    expect(html).toContain('data-mp-cancel="1"');
    expect(html).not.toContain('data-mp-autofill');
  });

  it('is the seat panel body for the announced window, never the generic option list', () => {
    const state = new SeatPanelState('t1', 1, ctx, null, null);
    state.adoptView(payWindow);
    const { html } = render(SeatPanel, { props: { view: view(payWindow), seats, ctx, table: 't1', match: 1, state } });
    expect(html).toContain('data-mana-payment');
    expect(html).not.toContain('class="list"');
  });
});

describe('announce-then-pay casts at priority', () => {
  const action: PaymentAction = {
    id: 'act-1', cast: { object: 22, face: 0, origin: 'hand' }, base_option_index: null, label: 'Cast Grixis Test',
    plans: [{ version: 1, id: 'p', cost: { generic: 0, mana: [0, 1, 2, 0, 0, 0] }, activations: [], pool_spend: [0, 0, 0, 0, 0, 0], pool_after: [0, 0, 0, 0, 0, 0] }],
  };
  const prio: Decision = {
    seq: 39, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1,
    options: [{ index: 0, player: 0, kind: 'pass', label: 'Pass priority' }], payment_actions: [action],
  };

  it('lists a plan-only cast as an announce row while Auto-pay is off, and not while it is on', () => {
    const off = new SeatPanelState('t1', 1, ctx, null, null);
    off.setAutoManaAvailable(true);
    off.adoptView(prio);
    const a = render(SeatPanel, { props: { view: view(prio, [spell(22, 'Grixis Test')]), seats, ctx, table: 't1', match: 1, state: off } });
    expect(a.html).toContain('data-announce="act-1"');
    expect(a.html).toContain('Cast Grixis Test');
    const on = new SeatPanelState('t1', 1, ctx, null, null);
    on.setAutoManaAvailable(true);
    on.setAutoPayMana(true);
    on.adoptView(prio);
    const b = render(SeatPanel, { props: { view: view(prio, [spell(22, 'Grixis Test')]), seats, ctx, table: 't1', match: 1, state: on } });
    expect(b.html).not.toContain('data-announce=');
    expect(b.html).toContain('data-payment-plan="p"');
  });

  it('gives the hand card a CAST shortcut worded for the route the click takes', () => {
    const me = player(0, [spell(22, 'Grixis Test')], []);
    const off = render(HandFan, { props: { player: me, width: 1180, paymentActions: [action], autoPay: false } });
    expect(off.html).toContain('data-payment-card="act-1"');
    expect(off.html).toContain('aria-label="Cast Grixis Test, then choose mana"');
    const on = render(HandFan, { props: { player: me, width: 1180, paymentActions: [action], autoPay: true } });
    expect(on.html).toContain('aria-label="Cast Grixis Test with suggested mana"');
  });
});
