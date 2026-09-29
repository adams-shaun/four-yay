import { describe, expect, it } from 'vitest';
import type { CardView, Decision, Option, PlayerView, StackView, View } from '../protocol';
import { clear, loadSticky, ruleFromAnswer, saveSticky, stickyAnswer, stickyKey, stickyStorageKey, type StickyKind, type StickyRule } from './sticky';

const card = (id: number, name = 'Miner', controller = 0) => ({ id, name, controller }) as CardView;
const view = (cards = [card(10, 'Altar'), card(20)]) => ({
  players: [{ battlefield: cards }] as PlayerView[], stack: [],
}) as unknown as View;
const option = (index: number, label = 'Miner', obj: number | undefined = 20): Option =>
  ({ index, label, obj, kind: 'pick', player: 0 });
const decision = (kind: StickyKind = 'target', options = [option(7)], over: Partial<Decision> = {}): Decision =>
  ({ seq: 1, player: 0, kind, source: 10, prompt: 'Choose one', min: 1, max: 1, options, ...over });
const stored = (d: Decision, v: View, chosen = [7]) => {
  const rule = ruleFromAnswer(d, v, chosen)!;
  expect(rule).not.toBeNull();
  return new Map([[rule.key, rule]]);
};

function storage(): Storage {
  const data = new Map<string, string>();
  return {
    get length() { return data.size; }, clear: () => data.clear(),
    getItem: (k) => data.get(k) ?? null, setItem: (k, v) => { data.set(k, v); },
    removeItem: (k) => { data.delete(k); }, key: (i) => [...data.keys()][i] ?? null,
  };
}

describe('sticky label answers', () => {
  it.each(['target', 'choose', 'modes', 'trigger_optional'] as const)('%s hits by label, misses safely, and takes the first duplicate in wire order', (kind) => {
    const v = view();
    const d = decision(kind);
    const rules = stored(d, v);
    expect(stickyAnswer(d, v, rules)).toEqual([7]);
    // Position and wire index both change; the first match is NOT the lowest index.
    const next = { ...d, options: [option(12, 'Other'), option(9), option(2)] };
    expect(stickyAnswer(next, v, rules)).toEqual([9]);
    expect(stickyAnswer({ ...d, options: [option(7, 'Other')] }, v, rules)).toBeNull();
    expect(stickyAnswer({ ...d, prompt: 'Different ability' }, v, rules)).toBeNull();
    expect(stickyAnswer(d, v, new Map())).toBeNull();
    expect([...rules.values()][0].answer).toEqual({ label: 'Miner', controller: kind === 'target' ? 0 : null });
  });

  it.each(['target', 'choose', 'modes', 'trigger_optional'] as const)('%s keys use source name, not id, and unknown names fall back to prompt-only', (kind) => {
    const d = decision(kind);
    const v = view();
    expect(stickyKey(d, v)).toBe(`${kind}\u0000Altar\u0000Choose one`);
    const unknown = { ...d, source: 999 };
    expect(stickyKey(unknown, v)).toBe(`${kind}\u0000\u0000Choose one`);
    expect(stickyAnswer({ ...unknown, source: undefined }, v, stored(unknown, v))).toEqual([7]);
    const returned = { ...d, source: 30 };
    expect(stickyAnswer(returned, view([card(30, 'Altar'), card(20)]), stored(d, v))).toEqual([7]);
    expect(stickyAnswer(d, view([card(10, 'Other'), card(20)]), stored(d, v))).toBeNull();
  });

  it('targets require the recorded current controller, including seat zero', () => {
    const d = decision();
    const rules = stored(d, view());
    expect(stickyAnswer(d, view([card(10, 'Altar'), card(20, 'Miner', 1)]), rules)).toBeNull();
    expect(stickyAnswer(d, view([card(10, 'Altar')]), rules)).toBeNull();
    expect(ruleFromAnswer(d, view([card(10, 'Altar')]), [7])).toBeNull();
    const next = { ...d, options: [option(11, 'Miner', 21), option(12, 'Miner', 22)] };
    expect(stickyAnswer(next, view([card(10, 'Altar'), card(21, 'Miner', 1), card(22)]), rules)).toEqual([12]);
  });

  it('player targets store a null controller and match labels', () => {
    const d = decision('target', [{ ...option(7, 'Player 1'), obj: undefined, kind: 'player', player: 1 }]);
    const rules = stored(d, view());
    expect([...rules.values()][0].answer?.controller).toBeNull();
    expect(stickyAnswer(d, view(), rules)).toEqual([7]);
    expect(stickyAnswer({ ...d, options: [option(8, 'Player 2', undefined)] }, view(), rules)).toBeNull();
  });

  it('resolves source names and target controllers in all visible zones and the stack', () => {
    for (const zone of ['hand', 'battlefield', 'graveyard', 'exile', 'command', 'commanders', 'planar_deck', 'library_top']) {
      const v = { players: [{ [zone]: zone === 'library_top' ? card(20) : [card(20)] }], stack: [] } as unknown as View;
      const d = decision('target', [option(7)], { source: 20 });
      expect(stickyKey(d, v)).toBe('target\u0000Miner\u0000Choose one');
      expect(ruleFromAnswer(d, v, [7])?.answer?.controller).toBe(0);
    }
    const v = { players: [], stack: [{ id: 20, name: 'Spell', controller: 2 } as StackView] } as unknown as View;
    expect(ruleFromAnswer(decision('target', [option(7)], { source: 20 }), v, [7])?.answer?.controller).toBe(2);
    expect(stickyKey(decision('modes', [], { source: 20 }), v)).toBe('modes\u0000Spell\u0000Choose one');
  });

  it('does not stick other kinds, multi-select choose, or X/amount values (including omitted zero)', () => {
    for (const kind of ['priority', 'attackers', 'blockers', 'arrange', 'mulligan', 'replacement', 'commander_zone']) {
      expect(stickyKey({ ...decision(), kind }, view())).toBeNull();
    }
    const cases = [
      decision('choose', [option(7)], { max: 2 }),
      decision('choose', [{ ...option(7), kind: 'x' }]),
      decision('choose', [{ ...option(7), kind: 'amount' }]),
      decision('choose', [{ ...option(7), amount: 0 }]),
      decision('choose', [{ ...option(7), amount: 3 }]),
    ];
    for (const d of cases) {
      expect(stickyKey(d, view())).toBeNull();
      expect(ruleFromAnswer(d, view(), [7])).toBeNull();
      expect(stickyAnswer(d, view(), new Map())).toBeNull();
    }
  });

  it('refuses answers the one-label model cannot represent', () => {
    for (const chosen of [[], [99], [7, 7]]) expect(ruleFromAnswer(decision(), view(), chosen)).toBeNull();
    const d = decision('modes', [option(7)], { min: 2, max: 2 });
    expect(ruleFromAnswer(d, view(), [7])).toBeNull();
    expect(stickyAnswer(d, view(), stored(decision('modes'), view()))).toBeNull();
  });

  it('builds a human label from the source and prompt tail', () => {
    expect(ruleFromAnswer(decision('trigger_optional', [option(7)], { prompt: 'Apply effect? — Return it' }), view(), [7])?.label)
      .toBe('Altar: Return it');
  });
});

describe('sticky trigger order', () => {
  const v = view([card(10, 'Artist'), card(20, 'Warden')]);
  const a = option(7, 'Artist: Drain', 10);
  const b = option(8, 'Warden: Gain', 20);
  const d = decision('trigger_order', [a, b, { ...a, index: 9 }], { min: 3, max: 3 });

  it('keys by the sorted multiset; answers a reordered list with the first unused duplicate', () => {
    const rules = stored(d, v, [8, 9, 7]);
    expect([...rules.values()][0].order).toEqual(['Warden\u0000Gain', 'Artist\u0000Drain', 'Artist\u0000Drain']);
    expect(stickyKey(d, v)).toBe('trigger_order\u0000Artist\u0000Drain\u0000Artist\u0000Drain\u0000Warden\u0000Gain');
    const next = { ...d, prompt: 'Another order prompt', options: [{ ...a, index: 40 }, { ...a, index: 2 }, { ...b, index: 11 }] };
    expect(stickyKey(next, v)).toBe(stickyKey(d, v));
    expect(stickyAnswer(next, v, rules)).toEqual([11, 40, 2]);
  });

  it('misses on changed text, added/removed triggers or a mismatched stored multiset', () => {
    const rules = stored(d, v, [8, 7, 9]);
    for (const options of [[a, b], [a, b, a, a], [a, b, { ...a, label: 'Artist: Other' }]]) {
      expect(stickyAnswer({ ...d, options }, v, rules)).toBeNull();
    }
    const original = [...rules.values()][0];
    for (const order of [original.order!.slice(1), ['Artist\u0000Drain', 'Warden\u0000Gain', 'Warden\u0000Gain']]) {
      expect(stickyAnswer(d, v, new Map([[original.key, { ...original, order }]]))).toBeNull();
    }
    for (const chosen of [[7, 8], [7, 8, 8], [7, 8, 99]]) expect(ruleFromAnswer(d, v, chosen)).toBeNull();
  });

  it('unknown source names retain the full trigger label as text', () => {
    const absent = view([]);
    expect(stickyKey(d, absent)).toContain('\u0000\u0000Artist: Drain');
    expect(stickyAnswer(d, absent, stored(d, absent, [8, 7, 9]))).toEqual([8, 7, 9]);
  });
});

describe('per-game sticky store', () => {
  const rules = stored(decision(), view());

  it('isolates both tables and successive matches, and clear updates both layers', () => {
    const s = storage();
    saveSticky('scope', 1, rules, s);
    expect(loadSticky('scope', 1, s)).toEqual(rules);
    expect(loadSticky('scope', 2, s).size).toBe(0);
    expect(loadSticky('other', 1, s).size).toBe(0);
    saveSticky('scope', 2, rules, s);
    clear('scope', 1, s);
    expect(loadSticky('scope', 1, s).size).toBe(0);
    expect(s.getItem(stickyStorageKey('scope', 1))).toBe('[]');
    expect(loadSticky('scope', 2, s)).toEqual(rules);
  });

  it('restores the mirror on a cold load and keeps memory authoritative', () => {
    const s = storage();
    const mutable = new Map(rules);
    saveSticky('warm', 1, mutable, s);
    mutable.clear();
    s.setItem(stickyStorageKey('cold', 1), s.getItem(stickyStorageKey('warm', 1))!);
    expect(loadSticky('cold', 1, s)).toEqual(rules);
    s.setItem(stickyStorageKey('warm', 1), '[]');
    expect(loadSticky('warm', 1, s)).toEqual(rules);
    expect(stickyStorageKey('warm', 1)).toBe('gorge.sticky.warm:1');
  });

  it('round-trips ordered trigger rules too', () => {
    const d = decision('trigger_order', [option(7, 'Miner: Return')]);
    const ordered = stored(d, view());
    const s = storage();
    saveSticky('order-warm', 1, ordered, s);
    s.setItem(stickyStorageKey('order-cold', 1), s.getItem(stickyStorageKey('order-warm', 1))!);
    expect(loadSticky('order-cold', 1, s)).toEqual(ordered);
  });

  it('corrupt storage empties the whole store', () => {
    const s = storage();
    const r: StickyRule = [...rules.values()][0];
    const bad = ['{bad', '{}', '[7]', JSON.stringify([r, r]),
      ...[{ ...r, kind: 'priority' }, { ...r, key: 'wrong' }, { ...r, answer: {} },
        { ...r, answer: { label: 'Miner', controller: -1 } }, { ...r, answer: { label: 2, controller: null } },
        { key: 'trigger_order\u0000x', kind: 'trigger_order', label: 'x', order: [3] },
      ].map((b) => JSON.stringify([r, b]))];
    bad.forEach((raw, i) => {
      s.setItem(stickyStorageKey(`bad-${i}`, 1), raw);
      expect(loadSticky(`bad-${i}`, 1, s).size).toBe(0);
    });
  });

  it('swallows reads/writes that throw, and supports null storage', () => {
    const throwing = { getItem() { throw Error('denied'); }, setItem() { throw Error('quota'); } } as unknown as Storage;
    expect(loadSticky('throws', 1, throwing).size).toBe(0);
    saveSticky('throws', 1, rules, throwing);
    expect(loadSticky('throws', 1, throwing)).toEqual(rules);
    clear('throws', 1, throwing);
    expect(loadSticky('throws', 1, throwing).size).toBe(0);
    saveSticky('null', 1, rules, null);
    expect(loadSticky('null', 1, null)).toEqual(rules);
  });
});
