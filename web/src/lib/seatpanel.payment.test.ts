import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Decision, PaymentAction, PaymentPlan, View } from '../protocol';
import { ApiError } from './api';
import { SeatPanelState, actedPayment, autoNoteText } from './seatpanel.svelte';

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
// `base` is the legacy cast option's wire index; null is the PLAN-ONLY shape
// (no legacy cast option: the engine offers it only once floating mana can
// pay). It is null rather than undefined on purpose: an explicit undefined
// argument takes the parameter's default, so `priority(seq, undefined)` used to
// build a window WITH the legacy cast at index 7.
const action = (base: number | null = 7): PaymentAction => ({
  id: 'action-a', cast: { object: 22, face: 0, origin: 'hand' }, base_option_index: base,
  label: 'Cast Test Spell', plans: [plan('plan-a'), plan('plan-b')],
});
const priority = (seq = 17, base: number | null = 7): Decision => ({
  seq, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
  options: base === null
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

/**
 * offer is priority() with payment identities that bind the decision seq, as
 * the engine's do (spec §4: action and plan IDs hash the seq): action-<seq>,
 * plan-<seq>-a and plan-<seq>-b. Two seqs' offers are then distinguishable by
 * ID, not only by object identity, so "N's plan attached to N+1" is visible
 * in the posted body. base null is the plan-only shape.
 */
const offer = (seq: number, base: number | null = 7): Decision => ({
  ...priority(seq, base),
  payment_actions: [{ ...action(base), id: `action-${seq}`, plans: [plan(`plan-${seq}-a`), plan(`plan-${seq}-b`)] }],
});

/** mainView is the seat's own Main 1 with the given stack; no seat projection, so potential_actions never makes a window actionable here. */
const mainView = (stack: { id: number; controller: number; kind?: string }[] = []): View =>
  ({ active: 0, step: 'main1', turn: 2, stack }) as unknown as View;

/**
 * seat builds a panel on a table that OFFERS auto-pay (the auto_mana
 * capability is on), with the seat preference as given. Pass-after-acting is
 * off and pacing is zero, so every machine pass posts synchronously; `smart`
 * names the steps of the seat's own turn that carry a smart stop.
 */
function seat(o: { auto: boolean; autoPay: boolean; smart?: string[] }): SeatPanelState {
  const p = new SeatPanelState('table', 1, ctx, null, null);
  p.stops = { yours: new Set(o.smart ?? []), opponents: new Set() };
  p.setAuto(o.auto);
  p.setActPass(false);
  p.settings = { ...p.settings, pacing: { stepMs: 0, resolveMs: 0 } };
  p.setAutoManaAvailable(true);
  p.setAutoPayMana(o.autoPay);
  return p;
}

async function settle(predicate: () => boolean, maxTicks = 200): Promise<void> {
  for (let i = 0; i < maxTicks; i++) {
    if (predicate()) return;
    await Promise.resolve();
  }
  throw new Error(`settle: condition still false after ${maxTicks} microtask ticks`);
}

/** deferred is a promise the test settles by hand: an HTTP response still on the wire. */
function deferred<T>(): { promise: Promise<T>; resolve: (v: T) => void; reject: (e: unknown) => void } {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

// Spec §8 as amended 2026-09-26 (aph-web-autopass): auto-pay reaches the
// autopilot only through the SEAT preference. The table capability makes the
// switch available and nothing else: with the preference off, a capability
// table's pass policy is exactly a capability-less table's.
describe('auto-pass follows the seat preference, not the table capability', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  it('own spell on the stack resolves with capability on and either auto-pay preference', async () => {
    const ownSpell = mainView([{ id: 9, controller: 0, kind: 'spell' }]);
    expect(ownSpell.stack.at(-1)).toMatchObject({ id: 9, controller: 0, kind: 'spell' });
    const off = seat({ auto: true, autoPay: false, smart: ['main1'] });
    expect(off.autoManaAvailable).toBe(true);
    off.adoptView(priority(21));
    off.considerAuto(ownSpell);
    await settle(() => off.postedSeq === 21);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(postIntentMock.mock.calls[0][2]).toEqual({ seq: 21, player: 0, choices: [1] });

    postIntentMock.mockClear();
    const on = seat({ auto: true, autoPay: true, smart: ['main1'] });
    on.adoptView(priority(22));
    on.considerAuto(ownSpell);
    await settle(() => on.postedSeq === 22);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    // Both preference values submit the pass option by its wire index.
    expect(postIntentMock.mock.calls[0][2]).toEqual({ seq: 22, player: 0, choices: [1] });
  });

  it('plan-only window, Manual seat: the empty-window floor leaves it to the player while the preference is on, and passes it while off', async () => {
    const on = seat({ auto: false, autoPay: true });
    on.adoptView(offer(31, null));
    on.considerAuto(mainView());
    await settle(() => !on.busy);
    expect(postIntentMock).not.toHaveBeenCalled();
    expect(on.emptySkipped).toBe(0);
    expect(on.active?.seq).toBe(31);

    const off = seat({ auto: false, autoPay: false });
    off.adoptView(offer(32, null));
    off.considerAuto(mainView());
    await settle(() => off.postedSeq === 32);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(postIntentMock.mock.calls[0][2]).toEqual({ seq: 32, player: 0, choices: [1] });
    expect(off.emptySkipped).toBe(1);
  });

  it('plan-only window, Auto with a smart Main 1 stop: stops and names the planned cast while on, passes while off', async () => {
    const on = seat({ auto: true, autoPay: true, smart: ['main1'] });
    on.adoptView(offer(41, null));
    on.considerAuto(mainView());
    await settle(() => !on.busy);
    expect(postIntentMock).not.toHaveBeenCalled();
    expect(autoNoteText(on.note)).toBe('Auto stopped here: you set a stop on this step and you can act — Cast Test Spell (with suggested mana).');

    const off = seat({ auto: true, autoPay: false, smart: ['main1'] });
    off.adoptView(offer(42, null));
    off.considerAuto(mainView());
    await settle(() => off.postedSeq === 42);
    expect(postIntentMock.mock.calls[0][2]).toEqual({ seq: 42, player: 0, choices: [1] });
  });

  it('merely toggling the preference on a held plan-only window posts nothing: no cast is submitted and the window is not passed', async () => {
    const p = seat({ auto: true, autoPay: true, smart: ['main1'] });
    p.adoptView(offer(51, null));
    p.considerAuto(mainView());
    p.setAutoPayMana(false);
    p.setAutoPayMana(true);
    p.setAutoPayMana(false);
    await settle(() => !p.busy);
    expect(postIntentMock).not.toHaveBeenCalled();
    expect(p.active?.seq).toBe(51);
  });
});

// PP-18 / PP-19 (spec §8): toggling after a submission affects future
// submissions only; a stale response must never attach an old plan to a new
// seq. submitPayment re-resolves the action and plan on the CURRENT pending
// decision by identity, and adopt()'s seq fence refuses an older decision.
describe('in-flight and stale payment submissions (PP-18, PP-19)', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  it.each([
    ['the legacy cast click (the first plan)', 0, (p: SeatPanelState) => p.click(7)],
    ['the list button for the second plan', 1, (p: SeatPanelState, d: Decision) => p.submitPayment(d.payment_actions![0], d.payment_actions![0].plans[1])],
  ] as const)('PP-18: toggling after %s is submitted leaves exactly one post, carrying the plan chosen at click time', async (_, planAt, submit) => {
    const wire = deferred<undefined>();
    postIntentMock.mockReturnValueOnce(wire.promise);
    const p = seat({ auto: false, autoPay: true });
    const d = offer(61);
    p.adoptView(d);
    submit(p, d);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    const chosen = d.payment_actions![0].plans[planAt];
    const atClick = JSON.stringify(postIntentMock.mock.calls[0][2]);

    // While the post is on the wire: toggle off (a click would now be a
    // legacy manual cast), click, toggle on, click and pick the other plan,
    // toggle off again. The request in flight owns the seat.
    p.setAutoPayMana(false);
    p.click(7);
    p.setAutoPayMana(true);
    p.click(7);
    p.submitPayment(d.payment_actions![0], d.payment_actions![0].plans[1 - planAt]);
    p.setAutoPayMana(false);
    wire.resolve(undefined);
    await settle(() => !p.busy);

    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(JSON.stringify(postIntentMock.mock.calls[0][2])).toBe(atClick);
    expect(postIntentMock.mock.calls[0][2]).toEqual({
      seq: 61, player: 0, choices: [], payment: { action_id: 'action-61', plan: chosen },
    });
    expect(p.postedSeq).toBe(61);
    // The answered seq is done: a late click posts nothing for it.
    p.click(7);
    await Promise.resolve();
    expect(postIntentMock).toHaveBeenCalledTimes(1);
  });

  it('PP-19: a delayed /pending response for seq N, arriving after N+1 was adopted, attaches no plan and posts nothing for N', async () => {
    const wire = deferred<Decision>();
    fetchPendingMock.mockReturnValueOnce(wire.promise);
    const p = seat({ auto: false, autoPay: true });
    const n = offer(71);
    const next = offer(72);
    p.adoptView(n);
    const poll = p.refreshPending(); // the 1s poll, issued while N is the ask
    p.adoptView(next); // the stream delivers N+1 first
    wire.resolve(n); // ...and the poll's answer for N lands late
    await poll;

    expect(p.active).toBe(next);
    expect(p.active?.payment_actions).toBe(next.payment_actions);
    expect(postIntentMock).not.toHaveBeenCalled();
    // A control still closed over N's offer is inert on N+1.
    p.submitPayment(n.payment_actions![0], n.payment_actions![0].plans[0]);
    expect(postIntentMock).not.toHaveBeenCalled();
    // The next cast click answers N+1 with N+1's own plan.
    p.click(7);
    await settle(() => !p.busy);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(postIntentMock.mock.calls[0][2]).toEqual({
      seq: 72, player: 0, choices: [], payment: { action_id: 'action-72', plan: next.payment_actions![0].plans[0] },
    });
  });

  it.each([
    ['accepted', (wire: ReturnType<typeof deferred<undefined>>) => wire.resolve(undefined)],
    ['rejected as stale', (wire: ReturnType<typeof deferred<undefined>>) => wire.reject(new ApiError(409, 'conflict', 'intent seq 81, pending decision seq 82'))],
  ] as const)('PP-19: N\'s planned post %s after N+1 was adopted posts nothing further and leaves N+1 its own plan', async (_, answer) => {
    const wire = deferred<undefined>();
    postIntentMock.mockReturnValueOnce(wire.promise);
    const p = seat({ auto: false, autoPay: true });
    const n = offer(81);
    const next = offer(82);
    fetchPendingMock.mockResolvedValue(next); // the rejection's recovery read
    p.adoptView(n);
    p.click(7); // N's cast, paid by N's first plan, on the wire
    p.adoptView(next); // the stream moves on before the response lands
    answer(wire);
    await settle(() => !p.busy && (p.error === null || fetchPendingMock.mock.calls.length > 0));
    await Promise.resolve();

    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(postIntentMock.mock.calls[0][2]).toMatchObject({ seq: 81, payment: { action_id: 'action-81', plan: { id: 'plan-81-a' } } });
    expect(p.active).toBe(next);
    expect(p.active?.payment_actions).toBe(next.payment_actions);
    // The next cast click answers N+1 with N+1's own plan.
    p.click(7);
    await settle(() => !p.busy);
    expect(postIntentMock).toHaveBeenCalledTimes(2);
    expect(postIntentMock.mock.calls[1][2]).toEqual({
      seq: 82, player: 0, choices: [], payment: { action_id: 'action-82', plan: next.payment_actions![0].plans[0] },
    });
  });
});

// Spec §8: "Auto-pay changes which witness an explicit cast uses; it does not
// otherwise change Auto/Manual policy." A planned cast posts choices=[] plus a
// payment selection, so pass-after-acting must arm on the selection exactly as
// it arms on the same cast's legacy option: on the ACCEPTED post, skipped with
// Ctrl (holdPriority), and never on a rejected one. The entry points below are
// the exact calls the UI makes: SeatPanel.svelte's plan button (the seat-panel
// list and the hot strip's nested list) calls submitPayment with the plan it
// lists, Table.svelte's HandFan CAST shortcut calls submitPayment with the
// first plan, and a legacy cast click on a base option goes through click().
describe('pass after acting arms on a planned cast exactly as on its legacy cast (spec §8)', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  /** acting is a Manual seat (auto off, no stops, zero pacing) with pass-after-acting ON, on an auto-pay table. */
  function acting(autoPay: boolean): SeatPanelState {
    const p = new SeatPanelState('table', 1, ctx, null, null);
    p.stops = { yours: new Set(), opponents: new Set() };
    p.setAuto(false);
    p.setActPass(true);
    p.settings = { ...p.settings, pacing: { stepMs: 0, resolveMs: 0 } };
    p.setAutoManaAvailable(true);
    p.setAutoPayMana(autoPay);
    return p;
  }
  /** drawView is the seat's own draw step with an empty stack: no stop rule applies, so an armed token passes. */
  const drawView = { active: 0, step: 'draw', turn: 2, stack: [] } as unknown as View;

  type Entry = readonly [
    name: string,
    autoPay: boolean,
    base: number | null,
    submit: (p: SeatPanelState, d: Decision, holdPriority: boolean) => void,
    posted: (d: Decision) => unknown,
  ];
  const planned = (planAt: number) => (d: Decision) =>
    ({ seq: d.seq, player: 0, choices: [], payment: { action_id: d.payment_actions![0].id, plan: d.payment_actions![0].plans[planAt] } });
  const entries: readonly Entry[] = [
    ['the seat-panel / hot-strip plan button (submitPayment, the listed plan)', true, 7,
      (p, d, hold) => p.submitPayment(d.payment_actions![0], d.payment_actions![0].plans[1], hold), planned(1)],
    ['the hand CAST shortcut (Table onCastPayment: submitPayment with the first plan)', true, 7,
      (p, d, hold) => p.submitPayment(d.payment_actions![0], d.payment_actions![0].plans[0], hold), planned(0)],
    ['the legacy cast click (click() on the base option submits its first plan)', true, 7,
      (p, _d, hold) => p.click(7, { holdPriority: hold }), planned(0)],
    ['a plan-only cast (no legacy option) from its plan button', true, null,
      (p, d, hold) => p.submitPayment(d.payment_actions![0], d.payment_actions![0].plans[0], hold), planned(0)],
    // The control: the same cast through its legacy option with auto-pay off.
    ['the legacy cast with auto-pay off (the control)', false, 7,
      (p, _d, hold) => p.click(7, { holdPriority: hold }), (d: Decision) => ({ seq: d.seq, player: 0, choices: [7] })],
  ];

  it.each(entries)('%s arms: the next priority window is machine-passed exactly once', async (_, autoPay, base, submit, posted) => {
    const p = acting(autoPay);
    const d = offer(17, base);
    p.adoptView(d);
    submit(p, d, false);
    await settle(() => p.postedSeq === 17);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(postIntentMock.mock.calls[0][2]).toEqual(posted(d));

    p.adoptView(offer(18));
    p.considerAuto(drawView);
    await settle(() => p.postedSeq === 18);
    expect(postIntentMock).toHaveBeenCalledTimes(2);
    expect(postIntentMock.mock.calls[1][2]).toEqual({ seq: 18, player: 0, choices: [1] }); // the PASS option's wire index
    expect(p.actPassed).toBe(1);
    expect(autoNoteText(p.note)).toBe('Passed 1 priority window after your action.');

    // One shot: the window after that is the player's again.
    p.adoptView(offer(19));
    p.considerAuto(drawView);
    await settle(() => !p.busy);
    expect(postIntentMock).toHaveBeenCalledTimes(2);
    expect(p.active?.seq).toBe(19);
  });

  it.each(entries)('%s with Ctrl held (holdPriority) posts but does not arm', async (_, autoPay, base, submit, posted) => {
    const p = acting(autoPay);
    const d = offer(17, base);
    p.adoptView(d);
    submit(p, d, true);
    await settle(() => p.postedSeq === 17);
    expect(postIntentMock.mock.calls[0][2]).toEqual(posted(d));

    p.adoptView(offer(18));
    p.considerAuto(drawView);
    await settle(() => !p.busy);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(p.actPassed).toBe(0);
    expect(p.active?.seq).toBe(18);
  });

  it.each(entries)('%s rejected: nothing arms, the error surfaces and the current decision is re-read', async (_, autoPay, base, submit) => {
    const p = acting(autoPay);
    postIntentMock.mockRejectedValueOnce(new ApiError(409, 'conflict', 'intent seq 17 is stale'));
    fetchPendingMock.mockResolvedValue(offer(17, base)); // the recovery read: the same ask is still pending
    const d = offer(17, base);
    p.adoptView(d);
    submit(p, d, false);
    await settle(() => !p.busy && fetchPendingMock.mock.calls.length === 1);
    await Promise.resolve();
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(p.error).toBe('intent seq 17 is stale');
    expect(p.postedSeq).toBeNull();
    expect(p.picked).toEqual([]);
    expect(p.active?.seq).toBe(17);

    // The same window is still the player's: nothing was armed to pass it...
    p.considerAuto(drawView);
    await settle(() => !p.busy);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    // ...and neither is the next one.
    p.adoptView(offer(18));
    p.considerAuto(drawView);
    await settle(() => !p.busy);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(p.actPassed).toBe(0);
    expect(p.active?.seq).toBe(18);
  });
});

describe('actedPayment — the arming test for a payment selection', () => {
  const d = offer(90);
  const sel = (action_id: string, planID: string) => ({ action_id, plan: { ...d.payment_actions![0].plans[0], id: planID } });

  it('is true exactly when the selection names a payment action and one of its plans offered on this priority decision', () => {
    expect(actedPayment(d, sel('action-90', 'plan-90-a'))).toBe(true);
    expect(actedPayment(d, sel('action-90', 'plan-90-b'))).toBe(true);
  });

  it('is false for no selection, an unknown action or plan, or a non-priority decision', () => {
    expect(actedPayment(d, undefined)).toBe(false);
    expect(actedPayment(d, null)).toBe(false);
    expect(actedPayment(d, sel('action-89', 'plan-90-a'))).toBe(false);
    expect(actedPayment(d, sel('action-90', 'plan-89-a'))).toBe(false);
    expect(actedPayment({ ...d, kind: 'target' }, sel('action-90', 'plan-90-a'))).toBe(false);
    expect(actedPayment({ ...d, payment_actions: undefined }, sel('action-90', 'plan-90-a'))).toBe(false);
  });
});
