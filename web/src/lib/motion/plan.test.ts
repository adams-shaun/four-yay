import { describe, expect, it } from 'vitest';
import type { Transition } from '../clientmodel/types';
import { counterText, planBatch } from './plan';
import { TIMINGS } from './settings';

const N = TIMINGS.normal;
const mv = (obj: number | null, from: string, to: string, seat = 0): Transition => ({
  kind: 'move', obj, from: { seat, zone: from as never }, to: { seat, zone: to as never },
});

describe('planBatch', () => {
  it('plays nothing when motion is off or the batch is empty', () => {
    expect(planBatch([mv(1, 'hand', 'stack')], 'off')).toEqual({ steps: [], duration: 0 });
    expect(planBatch([], 'normal')).toEqual({ steps: [], duration: 0 });
  });

  it('staggers flights in order and bumps a pile on landing', () => {
    const p = planBatch([mv(1, 'battlefield', 'graveyard'), mv(2, 'battlefield', 'graveyard', 1)], 'normal');
    const flights = p.steps.filter((s) => s.kind === 'flight');
    expect(flights.map((s) => s.delay)).toEqual([0, N.stagger]);
    const bumps = p.steps.filter((s) => s.kind === 'bump');
    expect(bumps).toEqual([
      { kind: 'bump', zone: { seat: 0, zone: 'graveyard' }, delay: N.flight, duration: N.bump },
      { kind: 'bump', zone: { seat: 1, zone: 'graveyard' }, delay: N.stagger + N.flight, duration: N.bump },
    ]);
    expect(p.duration).toBe(N.stagger + N.flight + N.bump);
  });

  it('coalesces one object hand → stack → graveyard into one flight', () => {
    const p = planBatch([mv(7, 'hand', 'stack'), mv(7, 'stack', 'graveyard')], 'fast');
    expect(p.steps.filter((s) => s.kind === 'flight')).toEqual([
      { kind: 'flight', objs: [7], card: null, from: { obj: 7, zone: { seat: 0, zone: 'hand' } }, to: { obj: 7, zone: { seat: 0, zone: 'graveyard' } }, count: 1, delay: 0, duration: TIMINGS.fast.flight },
    ]);
  });

  it('drops moves into nowhere and a round trip back to the same zone', () => {
    const p = planBatch([mv(1, 'battlefield', 'other'), mv(2, 'battlefield', 'exile'), mv(2, 'exile', 'battlefield')], 'normal');
    expect(p.steps).toEqual([]);
  });

  it('past the cap, collapses to one group flight per destination', () => {
    const ts: Transition[] = [];
    for (let i = 1; i <= 6; i++) ts.push(mv(i, 'battlefield', 'graveyard', 0));
    for (let i = 11; i <= 14; i++) ts.push(mv(i, 'battlefield', 'graveyard', 1));
    const p = planBatch(ts, 'normal', { cap: 8 });
    const flights = p.steps.filter((s) => s.kind === 'flight');
    expect(flights.map((f) => f.kind === 'flight' && [f.count, f.to.zone.seat, f.to.obj, f.delay])).toEqual([
      [6, 0, null, 0],
      [4, 1, null, N.stagger],
    ]);
  });

  it('damage floats and shakes; a damaged seat gets no second life float', () => {
    const p = planBatch([
      { kind: 'damage', to: { obj: 4 }, amount: 3 },
      { kind: 'damage', to: { seat: 1 }, amount: 2 },
      { kind: 'life', seat: 1, from: 20, to: 18 },
      { kind: 'life', seat: 0, from: 20, to: 23 },
    ], 'normal');
    expect(p.steps.map((s) => [s.kind, s.delay, s.kind === 'float' ? s.text : ''])).toEqual([
      ['float', 0, '−3'], ['shake', 0, ''],
      ['float', N.stagger, '−2'], ['shake', N.stagger, ''],
      ['float', 2 * N.stagger, '+3'],
    ]);
    const gain = p.steps[4];
    expect(gain.kind === 'float' && [gain.life, gain.tone]).toEqual([true, 'gain']);
  });

  it('counters float their change; taps and pops play nothing', () => {
    const p = planBatch([
      { kind: 'counter', on: { obj: 2 }, counter: 'P1P1', delta: 2 },
      { kind: 'tap', obj: 2, tapped: true },
      { kind: 'stack', op: 'pop', obj: 9 },
    ], 'normal');
    expect(p.steps.map((s) => s.kind === 'float' && s.text)).toEqual(['+1/+1 ×2']);
  });

  it('spells counter names for people', () => {
    expect(counterText('P1P1', 1)).toBe('+1/+1');
    expect(counterText('M1M1', -2)).toBe('−2 × −1/−1');
    expect(counterText('LOYALTY', -3)).toBe('−3 loyalty');
    expect(counterText('poison', 1)).toBe('+1 poison');
  });

  it('an anonymous draw lands in the hand pile; an ability push flies from its source', () => {
    const p = planBatch([mv(null, 'library', 'hand', 1), { kind: 'stack', op: 'push', obj: 50, source: 3 }], 'normal');
    expect(p.steps.map((s) => s.kind)).toEqual(['flight', 'flight', 'bump']);
    const push = p.steps[1];
    expect(push.kind === 'flight' && [push.from.obj, push.to.obj]).toEqual([3, 50]);
  });
});
