import { describe, expect, it } from 'vitest';
import type { Decision, Option } from '../../protocol';
import { digitMap, orderedOptions, renderedOrder } from './order';
import { rendererFor } from './renderer';

const opt = (index: number, kind: string, label: string, extra: Partial<Option> = {}): Option => ({ index, kind, label, player: 0, ...extra });
const dec = (kind: string, options: Option[], extra: Partial<Decision> = {}): Decision => ({ seq: 1, player: 0, kind, prompt: 'p', min: 1, max: 1, options, ...extra });
const none = { filter: '' };

describe('rendererFor', () => {
  it('maps each kind to its renderer, option-shape families first', () => {
    expect(rendererFor(dec('priority', [opt(0, 'pass', 'Pass')]))).toBe('priority');
    expect(rendererFor(dec('target', [opt(0, 'permanent', 'Bear (Bo)', { obj: 5 })]))).toBe('target');
    expect(rendererFor(dec('attackers', [opt(0, 'attacker', 'Bear → Bo', { obj: 5 })]))).toBe('attackers');
    expect(rendererFor(dec('blockers', [opt(0, 'block', 'Bear blocks Elk', { obj: 5 })]))).toBe('blockers');
    expect(rendererFor(dec('modes', [opt(0, 'mode', 'Mode A')]))).toBe('list');
    expect(rendererFor(dec('modes', [opt(0, 'discard', 'Discard Bolt', { obj: 9 })]))).toBe('discard');
    expect(rendererFor(dec('choose', [opt(0, 'search', 'Forest', { obj: 9 })]))).toBe('search');
    expect(rendererFor(dec('choose', [opt(0, 'name', 'Forest')]))).toBe('name');
    expect(rendererFor(dec('arrange', [opt(0, 'bottom', 'Forest', { obj: 9 })]))).toBe('arrange');
    expect(rendererFor(dec('mulligan', [opt(0, 'keep', 'keep'), opt(1, 'mulligan', 'mulligan')]))).toBe('mulligan');
    for (const k of ['choose', 'replacement', 'trigger_order', 'trigger_optional', 'commander_zone', 'starting_player']) {
      expect(rendererFor(dec(k, [opt(0, 'x', 'X')]))).toBe('list');
    }
  });
});

describe('orderedOptions / renderedOrder', () => {
  it('refuses priority: the action list is not numbered', () => {
    expect(renderedOrder(dec('priority', [opt(0, 'cast', 'Cast Bolt'), opt(1, 'pass', 'Pass')]), none)).toBeNull();
    expect(renderedOrder(null, none)).toBeNull();
  });

  it('numbers library search in its sorted, filtered on-screen order', () => {
    const d = dec('choose', [opt(0, 'search', 'Swamp', { obj: 1 }), opt(1, 'search', 'forest', { obj: 2 }), opt(2, 'search', 'Island', { obj: 3 })], { min: 0 });
    expect(renderedOrder(d, none)).toEqual([1, 2, 0]);
    expect(renderedOrder(d, { filter: 's' })).toEqual([1, 2, 0]);
    expect(renderedOrder(d, { filter: 'sw' })).toEqual([0]);
  });

  it('numbers a name pick in its filtered, sorted order', () => {
    const d = dec('choose', [opt(0, 'name', 'Opt'), opt(1, 'name', 'Lightning Bolt'), opt(2, 'name', 'Ancestral Recall')]);
    expect(renderedOrder(d, none)).toEqual([2, 1, 0]);
    expect(renderedOrder(d, { filter: 'l' })).toEqual([2, 1]);
  });

  it('numbers the payment window as the panel draws it: sources grouped, then its buttons', () => {
    const d = dec('choose', [
      opt(0, 'mana', 'Add R', { obj: 10 }),
      opt(1, 'mana', 'Add G', { obj: 11 }),
      opt(2, 'mana', 'Add B', { obj: 10 }),
      opt(3, 'cancel_cast', 'Cancel'),
      opt(4, 'autofill', 'Auto-fill'),
      opt(5, 'undo_tap', 'Undo'),
    ], { mana_payment: { card: 9, cost: { generic: 1, mana: [0, 0, 0, 1, 0, 0] }, owed: { generic: 1, mana: [0, 0, 0, 1, 0, 0] }, pool: [0, 0, 0, 0, 0, 0], autofill: [] } } as Partial<Decision>);
    expect(rendererFor(d)).toBe('payment');
    expect(renderedOrder(d, none)).toEqual([0, 2, 1, 4, 5, 3]);
  });

  it('leaves a separately drawn pass/resolve and concede out of a generic list', () => {
    const d = dec('choose', [opt(0, 'x', 'A'), opt(1, 'resolve', 'Resolve'), opt(2, 'y', 'B'), opt(3, 'concede', 'Concede')]);
    expect(orderedOptions(d, none)?.map((o) => o.label)).toEqual(['A', 'B']);
  });

  it('numbers mulligan keep choices and bottom cards in wire order', () => {
    expect(renderedOrder(dec('mulligan', [opt(0, 'keep', 'keep'), opt(1, 'mulligan', 'mulligan')]), none)).toEqual([0, 1]);
    expect(renderedOrder(dec('mulligan', [opt(0, 'bottom', 'A', { obj: 1 }), opt(1, 'bottom', 'B', { obj: 2 })]), none)).toEqual([0, 1]);
  });
});

describe('digitMap', () => {
  it('numbers the first nine rows 1-based and leaves the rest bare', () => {
    const m = digitMap([10, 11, 12, 13, 14, 15, 16, 17, 18, 19]);
    expect(m.get(10)).toBe(1);
    expect(m.get(18)).toBe(9);
    expect(m.has(19)).toBe(false);
    expect(digitMap(null).size).toBe(0);
  });
});
