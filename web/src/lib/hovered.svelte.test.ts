import { describe, expect, it } from 'vitest';
import { Hovered } from './hovered.svelte';

/** A minimal element: closest() walks a fixed ancestor chain of attribute bags. */
function el(chain: Record<string, string>[]): Element {
  const node = (i: number): Element => ({
    getAttribute: (a: string) => chain[i][a] ?? null,
    closest: (sel: string) => {
      const names = sel.split(',').map((x) => x.trim().replace(/^\[|\]$/g, ''));
      for (let j = i; j < chain.length; j++) if (names.some((n) => n in chain[j])) return node(j);
      return null;
    },
  }) as unknown as Element;
  return node(0);
}

describe('Hovered — the object and seat under the pointer', () => {
  it('reads the nearest data-obj and the seat that owns it', () => {
    const h = new Hovered();
    h.track(el([{ class: 'img' }, { 'data-obj': '42' }, { 'data-seat': '1' }]));
    expect(h.obj).toBe(42);
    expect(h.seat).toBe(1);
  });

  it('a seat header anchor names the seat with no object; nothing clears both', () => {
    const h = new Hovered();
    h.track(el([{ 'data-seat-anchor': '3' }]));
    expect([h.obj, h.seat]).toEqual([null, 3]);
    h.track(null);
    expect([h.obj, h.seat]).toEqual([null, null]);
  });
});
