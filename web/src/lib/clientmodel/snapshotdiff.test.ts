import { describe, expect, it } from 'vitest';
import { diffViews, SnapshotTransitionSource } from './snapshotdiff';
import { ability, card, player, spell, view } from './testkit';
import type { ModelEvent } from './types';

describe('diffViews', () => {
  it('identical views give no transitions', () => {
    const v = view([player(0, { battlefield: [card(1)] }), player(1)]);
    expect(diffViews(v, v)).toEqual([]);
  });

  it('battlefield to graveyard by id', () => {
    const c = card(5, { controller: 1, owner: 1 });
    const a = view([player(0), player(1, { battlefield: [c] })]);
    const b = view([player(0), player(1, { graveyard: [c] })]);
    expect(diffViews(a, b)).toEqual([
      { kind: 'move', obj: 5, name: 'c5', card: c, from: { seat: 1, zone: 'battlefield' }, to: { seat: 1, zone: 'graveyard' } },
    ]);
  });

  it("the viewer's draw: the id appears in hand and the library shrinks", () => {
    const c = card(7);
    const a = view([player(0, { library_size: 40 }), player(1)]);
    const b = view([player(0, { library_size: 39, hand: [c], hand_size: 1 }), player(1)]);
    expect(diffViews(a, b)).toEqual([
      { kind: 'move', obj: 7, name: 'c7', card: c, from: { seat: 0, zone: 'library' }, to: { seat: 0, zone: 'hand' } },
    ]);
  });

  it("an opponent's draw is an anonymous move by counts", () => {
    const a = view([player(0), player(1, { library_size: 40, hand_size: 7, hand: [] })]);
    const b = view([player(0), player(1, { library_size: 38, hand_size: 9, hand: [] })]);
    expect(diffViews(a, b)).toEqual([
      { kind: 'move', obj: null, from: { seat: 1, zone: 'library' }, to: { seat: 1, zone: 'hand' } },
      { kind: 'move', obj: null, from: { seat: 1, zone: 'library' }, to: { seat: 1, zone: 'hand' } },
    ]);
  });

  it("an opponent's cast from a hidden hand is hand to stack", () => {
    const c = card(9, { controller: 1, owner: 1 });
    const a = view([player(0), player(1, { hand_size: 3 })]);
    const b = view([player(0), player(1, { hand_size: 2 })], [spell(9, 1, c)]);
    expect(diffViews(a, b)).toEqual([
      { kind: 'move', obj: 9, name: 'c9', card: c, from: { seat: 1, zone: 'hand' }, to: { seat: 1, zone: 'stack' } },
    ]);
  });

  it('a bounce into a hidden hand, and a token appearing', () => {
    const c = card(3, { controller: 1, owner: 1 });
    const t = card(4);
    const a = view([player(0), player(1, { battlefield: [c], hand_size: 1 })]);
    const b = view([player(0, { battlefield: [t] }), player(1, { hand_size: 2 })]);
    expect(diffViews(a, b)).toEqual([
      { kind: 'move', obj: 3, name: 'c3', card: c, from: { seat: 1, zone: 'battlefield' }, to: { seat: 1, zone: 'hand' } },
      { kind: 'appear', obj: 4, name: 'c4', to: { seat: 0, zone: 'battlefield' } },
    ]);
  });

  it('tap, damage increase only, counters and life', () => {
    const a = view([player(0, { life: 20, battlefield: [card(1, { damage: 2, counters: { '+1/+1': 1 } }), card(2)] }), player(1, { life: 20 })]);
    const b = view([player(0, { life: 17, battlefield: [card(1, { damage: 0, counters: { '+1/+1': 3 } }), card(2, { tapped: true, damage: 2 })] }), player(1, { life: 22 })]);
    expect(diffViews(a, b)).toEqual([
      { kind: 'tap', obj: 2, tapped: true },
      { kind: 'counter', on: { obj: 1 }, counter: '+1/+1', delta: 2 },
      { kind: 'damage', to: { obj: 2 }, amount: 2 },
      { kind: 'life', seat: 0, from: 20, to: 17 },
      { kind: 'life', seat: 1, from: 20, to: 22 },
    ]);
  });

  it('ability push and pop', () => {
    const a = view([player(0, { battlefield: [card(1)] })], [ability(50, 0, 1)]);
    const b = view([player(0, { battlefield: [card(1)] })], [ability(51, 0, 1)]);
    expect(diffViews(a, b)).toEqual([
      { kind: 'stack', op: 'pop', obj: 50, source: 1, name: 't50' },
      { kind: 'stack', op: 'push', obj: 51, source: 1, name: 't51' },
    ]);
  });

  it('a spell resolving from the stack to the graveyard', () => {
    const c = card(9);
    const a = view([player(0, { hand_size: 0 })], [spell(9, 0, c)]);
    const b = view([player(0, { graveyard: [c] })]);
    expect(diffViews(a, b)).toEqual([
      { kind: 'move', obj: 9, name: 'c9', card: c, from: { seat: 0, zone: 'stack' }, to: { seat: 0, zone: 'graveyard' } },
    ]);
  });
});

describe('SnapshotTransitionSource', () => {
  it('resets on the first view and a new game, steps otherwise', () => {
    const src = new SnapshotTransitionSource((a, b) => a.turn <= b.turn);
    const seen: ModelEvent['type'][] = [];
    const off = src.onModel((e) => seen.push(e.type));
    const v1 = view([player(0)]);
    src.push(v1);
    src.push({ ...v1, turn: 2 });
    src.push({ ...v1, turn: 1 });
    off();
    src.push(v1);
    expect(seen).toEqual(['reset', 'step', 'reset']);
  });
});
