import { describe, expect, it } from 'vitest';
import type { EventBody } from '../../protocol';
import { ModelBus, stepTransitions } from './gorge';
import { card, player, view } from './testkit';

const body = (seq: number, kind: string, extra: Record<string, unknown> = {}): EventBody => ({ event: { seq, kind, player: 0, ...extra }, line: '' });

describe('stepTransitions', () => {
  const c = card(4);
  const prev = view([player(0, { life: 20, battlefield: [c] })]);
  const next = view([player(0, { life: 18, graveyard: [c] })]);

  it('maps the events when the DVR holds every seq of the step', () => {
    const events = [body(10, 'damage', { player: 0, amount: 2 }), body(11, 'move_zone', { obj: 4, from: 'battlefield', to: 'graveyard' })];
    expect(stepTransitions(prev, next, 9, 11, events)).toEqual([
      { kind: 'damage', to: { seat: 0 }, amount: 2, seq: 10 },
      { kind: 'move', obj: 4, name: 'c4', card: c, from: { seat: 0, zone: 'battlefield' }, to: { seat: 0, zone: 'graveyard' }, seq: 11 },
      { kind: 'life', seat: 0, from: 20, to: 18 },
    ]);
  });

  it('diffs the views when an event of the step is missing', () => {
    const events = [body(11, 'move_zone', { obj: 4, from: 'battlefield', to: 'graveyard' })];
    expect(stepTransitions(prev, next, 9, 11, events)).toEqual([
      { kind: 'move', obj: 4, name: 'c4', card: c, from: { seat: 0, zone: 'battlefield' }, to: { seat: 0, zone: 'graveyard' } },
      { kind: 'life', seat: 0, from: 20, to: 18 },
    ]);
  });
});

describe('ModelBus', () => {
  it('delivers synchronously, isolates a throwing listener, unsubscribes', () => {
    const bus = new ModelBus();
    const got: string[] = [];
    bus.on(() => { throw new Error('boom'); });
    const off = bus.on((e) => got.push(e.type));
    bus.emit({ type: 'reset' });
    off();
    bus.emit({ type: 'reset' });
    expect(got).toEqual(['reset']);
    expect(bus.size).toBe(1);
  });
});
