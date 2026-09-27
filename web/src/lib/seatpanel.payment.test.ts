import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Decision, PaymentAction, PaymentPlan, View } from '../protocol';
import { SeatPanelState } from './seatpanel.svelte';

const { postIntentMock, fetchPendingMock } = vi.hoisted(() => ({ postIntentMock: vi.fn(), fetchPendingMock: vi.fn() }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postIntent: postIntentMock,
  fetchPending: fetchPendingMock,
}));

const ctx = { seat: 0, token: 'tok' };
const plan = (id = 'plan-a'): PaymentPlan => ({
  version: 1, id, cost: { generic: 0, mana: [0, 1, 0, 0, 0, 0] },
  activations: [{ source: 41, source_zone_seq: 9, ability: { kind: 'intrinsic', intrinsic: 'basic_land' }, produces: [0, 1, 0, 0, 0, 0] }],
  pool_spend: [0, 0, 0, 0, 0, 0], pool_after: [0, 0, 0, 0, 0, 0],
});
const action = (base: number | undefined = 7): PaymentAction => ({
  id: 'action-a', cast: { object: 22, face: 0, origin: 'hand' }, base_option_index: base,
  label: 'Cast Test Spell', plans: [plan('plan-a'), plan('plan-b')],
});
const priority = (seq = 17, base: number | undefined = 7): Decision => ({
  seq, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
  options: base === undefined
    ? [{ index: 0, kind: 'activate', label: 'Tap Island', player: 0 }, { index: 1, kind: 'pass', label: 'Pass', player: 0 }]
    : [{ index: base, kind: 'cast', label: 'Cast Test Spell', player: 0 }, { index: 1, kind: 'pass', label: 'Pass', player: 0 }],
  payment_actions: [action(base)],
});

describe('payment plan seat preference', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  it('is off by default, is local to this seat-match, and toggling sends no intent', () => {
    const p = new SeatPanelState('table', 1, ctx, null);
    expect(p.autoPayMana).toBe(false);
    p.setAutoPayMana(true);
    expect(p.autoPayMana).toBe(false);
    p.setAutoManaAvailable(true);
    p.setAutoPayMana(true); p.setAutoPayMana(false); p.setAutoPayMana(true);
    expect(p.autoPayMana).toBe(true);
    expect(postIntentMock).not.toHaveBeenCalled();
    expect(new SeatPanelState('table', 2, ctx, null).autoPayMana).toBe(false);
  });

  it('submits the exact selected offered plan with empty choices and no rest', async () => {
    const p = new SeatPanelState('table', 1, ctx, null);
    const d = priority();
    p.adoptView(d);
    p.submitPayment(d.payment_actions![0], d.payment_actions![0].plans[1]);
    await Promise.resolve();
    expect(postIntentMock).toHaveBeenCalledWith('table', 1, {
      seq: 17, player: 0, choices: [], payment: { action_id: 'action-a', plan: d.payment_actions![0].plans[1] },
    }, ctx);
  });

  it('uses the first plan for an ordinary cast only while enabled, without duplicate posts', async () => {
    const p = new SeatPanelState('table', 1, ctx, null);
    p.adoptView(priority());
    p.click(7);
    await Promise.resolve();
    expect(postIntentMock.mock.calls[0][2]).toMatchObject({ choices: [7] });
    p.adoptView(priority(18));
    p.setAutoManaAvailable(true);
    p.setAutoPayMana(true);
    p.click(7); p.click(7);
    await Promise.resolve();
    expect(postIntentMock).toHaveBeenCalledTimes(2);
    expect(postIntentMock.mock.calls[1][2]).toMatchObject({ seq: 18, choices: [], payment: { action_id: 'action-a', plan: { id: 'plan-a' } } });
  });

  it('does not retain a plan after a fresh decision replaces the old sequence', () => {
    const p = new SeatPanelState('table', 1, ctx, null);
    const old = priority(17);
    p.adoptView(old);
    p.adoptView(priority(18));
    p.submitPayment(old.payment_actions![0], old.payment_actions![0].plans[0]);
    expect(postIntentMock).not.toHaveBeenCalled();
  });

});
