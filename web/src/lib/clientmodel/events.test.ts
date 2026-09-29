import { describe, expect, it } from 'vitest';
import type { Event } from '../../protocol';
import { moveChip, transitionsFromEvents } from './events';
import { card, player, spell, view } from './testkit';

const ev = (seq: number, kind: string, extra: Partial<Event> = {}): Event => ({ seq, kind, player: 0, ...extra });

describe('transitionsFromEvents', () => {
  it('a cast places both seats from the views', () => {
    const c = card(9, { controller: 1, owner: 1 });
    const prev = view([player(0), player(1)]);
    const next = view([player(0), player(1)], [spell(9, 1, c)]);
    expect(transitionsFromEvents([ev(10, 'stack_push', { player: 1, obj: 9, from: 'hand', to: 'stack' })], prev, next)).toEqual([
      { kind: 'move', obj: 9, name: 'c9', card: c, from: { seat: 1, zone: 'hand' }, to: { seat: 1, zone: 'stack' }, seq: 10 },
    ]);
  });

  it('a redacted draw is an anonymous library → hand move', () => {
    expect(transitionsFromEvents([ev(3, 'draw', { player: 1, from: 'library', to: 'hand' })], null, null)).toEqual([
      { kind: 'move', obj: null, from: { seat: 1, zone: 'library' }, to: { seat: 1, zone: 'hand' }, seq: 3 },
    ]);
  });

  it("a creature dying to its owner's graveyard reads the controller from prev", () => {
    const c = card(4, { controller: 0, owner: 1 });
    const prev = view([player(0, { battlefield: [c] }), player(1)]);
    const next = view([player(0), player(1, { graveyard: [c] })]);
    const [t] = transitionsFromEvents([ev(5, 'move_zone', { player: 1, obj: 4, from: 'battlefield', to: 'graveyard' })], prev, next);
    expect(t).toMatchObject({ kind: 'move', from: { seat: 0, zone: 'battlefield' }, to: { seat: 1, zone: 'graveyard' } });
  });

  it('damage to an object and to a player; cleanup damage is dropped', () => {
    expect(transitionsFromEvents([
      ev(1, 'damage', { obj: 4, amount: 3 }),
      ev(2, 'damage', { player: 1, amount: 2 }),
      ev(3, 'damage', { obj: 4, amount: -3 }),
    ], null, null)).toEqual([
      { kind: 'damage', to: { obj: 4 }, amount: 3, seq: 1 },
      { kind: 'damage', to: { seat: 1 }, amount: 2, seq: 2 },
    ]);
  });

  it('tap, untap, counters, pushes and tokens', () => {
    expect(transitionsFromEvents([
      ev(1, 'tap', { obj: 2 }),
      ev(2, 'untap', { obj: 2 }),
      ev(3, 'counter', { obj: 2, counter: '+1/+1', amount: 2 }),
      ev(4, 'player_counter', { player: 1, counter: 'poison', amount: 1 }),
      ev(5, 'trigger_push', { obj: 60 }),
      ev(6, 'token_create', { player: 1, text: 'w_1_1_soldier' }),
      ev(7, 'priority'),
    ], null, null)).toEqual([
      { kind: 'tap', obj: 2, tapped: true, seq: 1 },
      { kind: 'tap', obj: 2, tapped: false, seq: 2 },
      { kind: 'counter', on: { obj: 2 }, counter: '+1/+1', delta: 2, seq: 3 },
      { kind: 'counter', on: { seat: 1 }, counter: 'poison', delta: 1, seq: 4 },
      { kind: 'stack', op: 'push', obj: 60, seq: 5 },
      { kind: 'appear', obj: null, to: { seat: 1, zone: 'battlefield' }, seq: 6 },
    ]);
  });
});

describe('moveChip', () => {
  it('names the zones of a visible move and nothing else', () => {
    expect(moveChip(ev(1, 'stack_push', { obj: 3, from: 'hand', to: 'stack' }))).toBe('hand → stack');
    expect(moveChip(ev(1, 'move_zone', { obj: 3, from: 'battlefield', to: 'graveyard' }))).toBe('battlefield → grave');
    expect(moveChip(ev(1, 'move_zone', { obj: 3, from: 'battlefield', to: 'ceased' }))).toBeNull();
    expect(moveChip(ev(1, 'tap', { obj: 3 }))).toBeNull();
  });
});
