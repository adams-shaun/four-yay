import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Decision, View } from '../protocol';
import { SeatPanelState } from './seatpanel.svelte';
import { rememberKey } from './remembered';
import { clear, ruleFromAnswer, saveSticky } from './sticky';

const { postIntent } = vi.hoisted(() => ({ postIntent: vi.fn() }));
vi.mock('./api', async (original) => ({
  ...(await original<typeof import('./api')>()), postIntent,
}));
const d: Decision = {
  seq: 10, kind: 'trigger_optional', player: 0, prompt: 'Return Miner?', source: 20,
  min: 1, max: 1, options: [
    { index: 0, kind: 'yes', label: 'Yes', player: 0 },
    { index: 1, kind: 'no', label: 'No', player: 0 },
  ],
};
const view = { players: [{ graveyard: [{ id: 20, name: 'Miner', controller: 0 }] }], stack: [], turn: 1, over: false, decision: d } as unknown as View;
const ctx = { seat: 0, token: 'test' };

function panel(table: string, decision = d, choices = [1]): SeatPanelState {
  const rule = ruleFromAnswer(decision, view, choices)!;
  saveSticky(table, 1, new Map([[rule.key, rule]]), null);
  const p = new SeatPanelState(table, 1, ctx, null, null);
  p.settings = { ...p.settings, autoPass: false };
  return p;
}

beforeEach(() => { postIntent.mockReset(); postIntent.mockResolvedValue(undefined); });

describe('sticky answer post wiring', () => {
  it('wins over global remembered answers, posts once through the normal intent endpoint, and respects per-game scope', async () => {
    const p = panel('sticky-post');
    p.remembered = { version: 1, entries: [{ key: rememberKey(d.kind, d.prompt), choice: 0, label: d.prompt, savedAt: 1 }] };
    p.adoptView(d);
    expect(postIntent).not.toHaveBeenCalled(); // needs the fresh view for the source name
    p.considerAuto(view);
    p.considerAuto(view);
    expect(postIntent).toHaveBeenCalledExactlyOnceWith('sticky-post', 1, { seq: 10, player: 0, choices: [1] }, ctx);
    await Promise.resolve();
    expect(p.postedSeq).toBe(10);
    p.considerAuto(view);
    expect(postIntent).toHaveBeenCalledTimes(1);
    const next = new SeatPanelState('sticky-post', 2, ctx, null, null);
    next.adoptView(d);
    next.considerAuto(view);
    expect(postIntent).toHaveBeenCalledTimes(1);
  });

  it('sticky ordering wins over the default auto-order, not just auto-pass', () => {
    const order: Decision = { ...d, kind: 'trigger_order', min: 2, max: 2, options: [
      { index: 4, kind: 'trigger', label: 'Miner: Return', obj: 20, player: 0 },
      { index: 8, kind: 'trigger', label: 'Miner: Draw', obj: 20, player: 0 },
    ] };
    const p = panel('sticky-order', order, [8, 4]);
    p.settings = { ...p.settings, autoOrderAllTriggers: true };
    p.adoptView(order);
    p.considerAuto({ ...view, decision: order });
    expect(postIntent.mock.calls[0][2].choices).toEqual([8, 4]);
  });

  it('a miss falls through to remembered answers', () => {
    const p = panel('sticky-miss');
    p.remembered = { version: 1, entries: [{ key: rememberKey(d.kind, d.prompt), choice: 0, label: d.prompt, savedAt: 1 }] };
    p.adoptView({ ...d, options: [d.options[0]] });
    p.considerAuto(view);
    expect(postIntent.mock.calls[0][2].choices).toEqual([0]);
  });

  it('clear prevents firing and rewind leaves the restored decision manual', () => {
    const p = panel('sticky-clear');
    clear('sticky-clear', 1, null);
    p.adoptView(d);
    p.considerAuto(view);
    expect(postIntent).not.toHaveBeenCalled();
    const paused = panel('sticky-paused');
    paused.rewind();
    paused.adoptView(d);
    paused.considerAuto(view);
    expect(postIntent).not.toHaveBeenCalled();
  });

  it('a rejected sticky answer is not retried or replaced by a competing auto-answer', async () => {
    postIntent.mockRejectedValue(new Error('rejected'));
    const p = panel('sticky-error');
    p.remembered = { version: 1, entries: [{ key: rememberKey(d.kind, d.prompt), choice: 0, label: d.prompt, savedAt: 1 }] };
    p.adoptView(d);
    p.considerAuto(view);
    await Promise.resolve();
    expect(p.error).toBeTruthy();
    p.considerAuto(view);
    expect(postIntent).toHaveBeenCalledTimes(1);
  });
});
