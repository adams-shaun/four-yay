import { describe, expect, it, vi } from 'vitest';
import type { Decision, Option } from '../protocol';
import { SeatPanelState } from './seatpanel.svelte';
import { defaultKeymap, matchKeymap } from './keymap';

const { postIntentMock } = vi.hoisted(() => ({ postIntentMock: vi.fn() }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postIntent: postIntentMock,
}));

const ctx = { seat: 0, token: 'tok' };
const settle = async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); };
const opt = (index: number, kind: string, extra: Partial<Option> = {}): Option => ({ index, kind, label: `${kind} ${index}`, player: 1, ...extra });
const state = (d: Decision) => {
  const s = new SeatPanelState('t', 1, ctx, null, null);
  s.adoptView(d);
  return s;
};

const attackers: Decision = {
  seq: 30, player: 0, kind: 'attackers', prompt: 'turn 5 — declare attackers', min: 0, max: 3,
  options: [opt(0, 'attacker', { obj: 10, group: 'a10' }), opt(1, 'attacker', { obj: 10, player: 2, group: 'a10' }), opt(2, 'attacker', { obj: 11, group: 'a11' })],
};
const blockers: Decision = {
  seq: 31, player: 0, kind: 'blockers', prompt: 'turn 5 — declare blockers', min: 0, max: 2,
  options: [opt(0, 'block', { obj: 10, attacker: 30, player: 0 })],
};

describe('the prompt quick answers', () => {
  it('attack with all selects one pairing per creature and does not post', async () => {
    postIntentMock.mockReset();
    const s = state(attackers);
    expect(s.attackWithAll()).toBe(true);
    expect(s.picked).toEqual([0, 2]);
    await settle();
    expect(postIntentMock).not.toHaveBeenCalled();
    expect(state(blockers).attackWithAll()).toBe(false);
  });

  it('no blocks posts the empty declaration', async () => {
    postIntentMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
    const s = state(blockers);
    expect(s.noBlocks()).toBe(true);
    await settle();
    expect(postIntentMock).toHaveBeenCalledWith('t', 1, { seq: 31, player: 0, choices: [] }, ctx);
  });

  it('no blocks refuses a forced block and a non-blockers ask', async () => {
    postIntentMock.mockReset();
    expect(state({ ...blockers, options: [{ ...blockers.options[0], required: true }] }).noBlocks()).toBe(false);
    expect(state(attackers).noBlocks()).toBe(false);
    await settle();
    expect(postIntentMock).not.toHaveBeenCalled();
  });

  it('auto-pay presses Auto-fill in the select-mana window and nowhere else', async () => {
    postIntentMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
    const pay: Decision = {
      seq: 32, player: 0, kind: 'choose', prompt: 'Pay for Bolt', min: 1, max: 1,
      mana_payment: { card: 9, cost: { generic: 0, mana: [0, 0, 0, 1, 0, 0] }, owed: { generic: 0, mana: [0, 0, 0, 1, 0, 0] }, pool: [0, 0, 0, 0, 0, 0] },
      options: [opt(0, 'mana', { obj: 3, label: 'Add R' }), opt(1, 'autofill', { label: 'Auto-fill' }), opt(2, 'cancel_cast', { label: 'Cancel' })],
    };
    expect(state(blockers).autoPay()).toBe(false);
    const s = state(pay);
    expect(s.autoPay()).toBe(true);
    await settle();
    expect(postIntentMock).toHaveBeenCalledWith('t', 1, { seq: 32, player: 0, choices: [1] }, ctx);
  });
});

describe('the keymap carries the quick answers', () => {
  it('binds Shift+A, Shift+N and Shift+P by default and leaves bare letters free', () => {
    const k = defaultKeymap();
    const ev = (code: string, shift: boolean) => ({ key: code.slice(3).toLowerCase(), code, shiftKey: shift, ctrlKey: false, metaKey: false, altKey: false });
    expect(matchKeymap(k, ev('KeyA', true))).toBe('attack-all');
    expect(matchKeymap(k, ev('KeyN', true))).toBe('no-blocks');
    expect(matchKeymap(k, ev('KeyP', true))).toBe('auto-pay');
    expect(matchKeymap(k, ev('KeyA', false))).toBeNull();
  });
});
