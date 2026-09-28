import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { CardView, Decision, PaymentAction, PaymentPlan, PlayerView, View } from '../protocol';
import {
  announceActions, castableActions, manaAmountText, manaOptionAccessibleName, manaOptionPips, manaSourceRows,
  manaWindow, owesNothing, paymentCostText, windowAction,
} from './announcepay';
import { optionsByObj, optionsByPlayer, postSingleAction, scenarioIconOf, singleTapOptionOf, tileOptions, tileOptionsMany, type CardOptions } from './cardoptions';
import { SeatPanelState } from './seatpanel.svelte';

const { postIntentMock, fetchPendingMock } = vi.hoisted(() => ({ postIntentMock: vi.fn(), fetchPendingMock: vi.fn() }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postIntent: postIntentMock,
  fetchPending: fetchPendingMock,
}));

const ctx = { seat: 0, token: 'tok' };
const plan: PaymentPlan = {
  version: 1, id: 'plan-a', cost: { generic: 0, mana: [0, 0, 0, 1, 0, 0] },
  activations: [{ source: 41, source_zone_seq: 9, ability: { kind: 'intrinsic', intrinsic: 'basic_land' }, produces: [0, 0, 0, 1, 0, 0] }],
  pool_spend: [0, 0, 0, 0, 0, 0], pool_after: [0, 0, 0, 0, 0, 0],
};
const action = (id: string, base: number | null, plans: PaymentPlan[] = [plan]): PaymentAction => ({
  id, cast: { object: 22, face: 0, origin: 'hand' }, base_option_index: base, label: `Cast ${id}`, plans,
});
const priority = (seq: number, actions: PaymentAction[]): Decision => ({
  seq, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
  options: [{ index: 0, player: 0, kind: 'activate', label: 'Activate Mountain for mana', obj: 41 }, { index: 1, player: 0, kind: 'pass', label: 'Pass' },
    { index: 7, player: 0, kind: 'cast', label: 'Cast pooled' }],
  payment_actions: actions,
});

// The announced window for a {1}{B}{U} spell over a Swamp and a Badlands.
const windowDecision = (seq = 30): Decision => ({
  seq, player: 0, kind: 'choose', prompt: 'Pay for Doom Test', min: 1, max: 1, source: 22,
  options: [
    { index: 0, player: 0, kind: 'mana', label: 'Add B', obj: 41 },
    { index: 1, player: 0, kind: 'mana', label: 'Add B', obj: 42, mana_symbol: 'B' },
    { index: 2, player: 0, kind: 'mana', label: 'Add R', obj: 42, mana_symbol: 'R' },
    { index: 3, player: 0, kind: 'mana', label: 'Pay 1 life: Add U', obj: 43 },
    { index: 4, player: 0, kind: 'autofill', label: 'Auto-fill: tap Swamp, Badlands' },
    { index: 5, player: 0, kind: 'undo_tap', label: 'Undo tapping Island', obj: 44 },
    { index: 6, player: 0, kind: 'cancel_cast', label: 'Cancel cast' },
  ],
  mana_payment: { card: 22, cost: { generic: 1, mana: [0, 1, 1, 0, 0, 0] }, owed: { generic: 1, mana: [0, 0, 1, 0, 0, 0] },
    pool: [0, 1, 0, 0, 0, 0], autofill: [41, 42] },
});

const land = (id: number, name: string): CardView => ({
  id, name, types: 'Land', printing: { name }, token: '', tapped: false, power: 0, toughness: 0,
  damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
});
const view = (d: Decision | null): View => {
  const me: PlayerView = { seat: 0, name: 'You', life: 20, lost: false, library_size: 40, hand_size: 0, graveyard_size: 0,
  completed_dungeons: 0,
    hand: [], battlefield: [land(41, 'Swamp'), land(42, 'Badlands'), land(43, 'Horizon Test')], graveyard: [], exile: [],
    pool: {}, command: [], commanders: [], commander_casts: [] };
  return { viewer: 0, visibility: 'seat', turn: 3, round: 3, step: 'main1', phase: 'main1', active: 0, priority: 0,
    over: false, draw: false, winner: null, players: [me], stack: [], pending: [], decision: d } as View;
};

describe('announce-then-pay helpers', () => {
  it('lists only plan-payable, pool-short casts while Auto-pay is off on an auto-mana table', () => {
    const d = priority(5, [action('plan-only', null), action('pooled', 7), action('planless', null, [])]);
    expect(announceActions(d, true, false).map((a) => a.id)).toEqual(['plan-only']);
    expect(announceActions(d, true, true)).toEqual([]);
    expect(announceActions(d, false, false)).toEqual([]);
    expect(castableActions(d, true).map((a) => a.id)).toEqual(['plan-only', 'pooled']);
    expect(announceActions(windowDecision(), true, false)).toEqual([]);
  });

  it('reads the window readout and groups the offered abilities by source', () => {
    const d = windowDecision();
    expect(manaWindow(d)).toBe(d);
    expect(manaWindow(priority(1, []))).toBeNull();
    expect(paymentCostText(d.mana_payment!.cost)).toBe('1 U B');
    expect(paymentCostText(d.mana_payment!.owed)).toBe('1 B');
    expect(manaAmountText(d.mana_payment!.pool)).toBe('U');
    expect(owesNothing(d.mana_payment!.owed)).toBe(false);
    expect(owesNothing({ generic: 0, mana: [0, 0, 0, 0, 0, 0] })).toBe(true);
    const rows = manaSourceRows(d, view(d));
    expect(rows.map((r) => [r.name, r.options.map((o) => o.index), r.suggested])).toEqual([
      ['Swamp', [0], true], ['Badlands', [1, 2], true], ['Horizon Test', [3], false],
    ]);
    expect(manaOptionPips('Add B')).toBe('B');
    expect(manaOptionPips('Add CC')).toBe('C C');
    expect(manaOptionPips('Add any color')).toBeNull();
    expect(manaOptionPips('Pay 1 life: Add U')).toBeNull();
    expect(manaOptionAccessibleName('Badlands', d.options[2])).toBe('Tap Badlands for R');
    expect(manaOptionAccessibleName('Horizon Test', d.options[3])).toBe('Horizon Test: Pay 1 life: Add U');
    expect(windowAction(d, 'autofill')?.index).toBe(4);
    expect(windowAction(d, 'done')).toBeNull();
  });

  it('marks a window mana option as a tap on the board tile', () => {
    expect(scenarioIconOf('mana')).toBe('tap');
  });

  // The board half of the window (spec §4.2, §8): every option carrying an
  // Obj marks its source, and a tile answers the window with its own wire
  // index. A lone Swamp and a pile of interchangeable Swamps are one tap; a
  // dual land's two colours keep the picker.
  it('lets a battlefield tile answer the window by its wire index', () => {
    const d: Decision = {
      ...windowDecision(32),
      options: [
        { index: 0, player: 0, kind: 'mana', label: 'Add B', obj: 41 },
        { index: 1, player: 0, kind: 'mana', label: 'Add B', obj: 45 },
        { index: 2, player: 0, kind: 'mana', label: 'Add B', obj: 42, mana_symbol: 'B' },
        { index: 3, player: 0, kind: 'mana', label: 'Add R', obj: 42, mana_symbol: 'R' },
        { index: 4, player: 0, kind: 'autofill', label: 'Auto-fill: tap Swamp' },
        { index: 5, player: 0, kind: 'cancel_cast', label: 'Cancel cast' },
      ],
    };
    const post = vi.fn();
    const bundle: CardOptions = { source: d.source, byObj: optionsByObj(d), byPlayer: optionsByPlayer(d), picked: [], tone: 'offered', post };
    expect([...bundle.byObj.keys()]).toEqual([41, 45, 42]);
    const swamp = tileOptions(bundle, 41)!;
    postSingleAction(swamp);
    expect(post).toHaveBeenLastCalledWith(0, false, false);
    const pile = tileOptionsMany(bundle, [45, 41])!;
    expect(singleTapOptionOf(pile)?.index).toBe(0);
    const badlands = tileOptions(bundle, 42)!;
    expect(singleTapOptionOf(badlands)).toBeNull();
    post.mockClear();
    postSingleAction(badlands);
    expect(post).not.toHaveBeenCalled();
  });
});

describe('announce-then-pay seat routing', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  it('with Auto-pay off, CAST announces a pool-short cast with empty choices', async () => {
    const p = new SeatPanelState('table', 1, ctx, null);
    p.setAutoManaAvailable(true);
    const d = priority(17, [action('plan-only', null)]);
    p.adoptView(d);
    p.castAction(d.payment_actions![0]);
    await Promise.resolve();
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(postIntentMock.mock.calls[0][2]).toEqual({ seq: 17, player: 0, choices: [], announce: { action_id: 'plan-only' } });
  });

  it('with Auto-pay off, CAST of a cast the pool already pays posts the legacy option', async () => {
    const p = new SeatPanelState('table', 1, ctx, null);
    p.setAutoManaAvailable(true);
    const d = priority(18, [action('pooled', 7)]);
    p.adoptView(d);
    p.castAction(d.payment_actions![0]);
    await Promise.resolve();
    expect(postIntentMock.mock.calls[0][2]).toEqual({ seq: 18, player: 0, choices: [7] });
  });

  it('with Auto-pay on, CAST submits the suggested plan exactly as before', async () => {
    const p = new SeatPanelState('table', 1, ctx, null);
    p.setAutoManaAvailable(true);
    p.setAutoPayMana(true);
    const d = priority(19, [action('plan-only', null)]);
    p.adoptView(d);
    p.castAction(d.payment_actions![0]);
    await Promise.resolve();
    expect(postIntentMock.mock.calls[0][2]).toMatchObject({ seq: 19, choices: [], payment: { action_id: 'plan-only', plan: { id: 'plan-a' } } });
    expect(postIntentMock.mock.calls[0][2].announce).toBeUndefined();
  });

  it('a stale or planless announce button is inert, and double clicks post once', async () => {
    const p = new SeatPanelState('table', 1, ctx, null);
    p.setAutoManaAvailable(true);
    const old = priority(20, [action('plan-only', null)]);
    p.adoptView(old);
    p.adoptView(priority(21, [action('plan-only', null)]));
    p.submitAnnounce(old.payment_actions![0]);
    const planless = priority(22, [action('planless', null, [])]);
    p.adoptView(planless);
    p.submitAnnounce(planless.payment_actions![0]);
    await Promise.resolve();
    expect(postIntentMock).not.toHaveBeenCalled();
    const d = priority(23, [action('plan-only', null)]);
    p.adoptView(d);
    p.submitAnnounce(d.payment_actions![0]);
    p.submitAnnounce(d.payment_actions![0]);
    await Promise.resolve();
    expect(postIntentMock).toHaveBeenCalledTimes(1);
  });

  it('a window button posts its own offered index', async () => {
    const p = new SeatPanelState('table', 1, ctx, null);
    p.adoptView(windowDecision(31));
    p.click(2);
    await Promise.resolve();
    expect(postIntentMock.mock.calls[0][2]).toEqual({ seq: 31, player: 0, choices: [2] });
  });
});
