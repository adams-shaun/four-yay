import { describe, expect, it } from 'vitest';
import { MotionStore } from './settings.svelte';
import { MOTION_KEY } from './settings';

function media(matches: boolean) {
  let listener: ((e: { matches: boolean }) => void) | null = null;
  const mq = { matches, addEventListener: (_: string, fn: (e: { matches: boolean }) => void) => { listener = fn; } };
  return { q: () => mq as unknown as MediaQueryList, flip: (m: boolean) => listener?.({ matches: m }) };
}

describe('MotionStore', () => {
  it('reduced motion forces off and follows the OS setting live', () => {
    const m = media(true);
    const s = new MotionStore(null, m.q);
    expect(s.speed).toBe('normal');
    expect(s.effective).toBe('off');
    m.flip(false);
    expect(s.effective).toBe('normal');
  });

  it('setSpeed persists', () => {
    const saved = new Map<string, string>();
    const storage = { getItem: (k: string) => saved.get(k) ?? null, setItem: (k: string, v: string) => saved.set(k, v) } as unknown as Storage;
    const s = new MotionStore(storage, null);
    s.setSpeed('fast');
    expect(s.effective).toBe('fast');
    expect(new MotionStore(storage, null).speed).toBe('fast');
    expect(saved.has(MOTION_KEY)).toBe(true);
  });
});
