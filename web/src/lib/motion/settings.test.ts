import { describe, expect, it } from 'vitest';
import { DEFAULT_MOTION_SPEED, effectiveSpeed, loadMotionSpeed, MOTION_KEY, saveMotionSpeed } from './settings';

function memStorage(): Storage {
  const m = new Map<string, string>();
  return {
    get length() { return m.size; },
    clear: () => m.clear(),
    getItem: (k) => m.get(k) ?? null,
    key: (i) => [...m.keys()][i] ?? null,
    removeItem: (k) => { m.delete(k); },
    setItem: (k, v) => { m.set(k, v); },
  };
}

describe('motion settings', () => {
  it('defaults to normal with nothing saved or no storage', () => {
    expect(DEFAULT_MOTION_SPEED).toBe('normal');
    expect(loadMotionSpeed(memStorage())).toBe('normal');
    expect(loadMotionSpeed(null)).toBe('normal');
  });

  it('round-trips a saved speed', () => {
    const s = memStorage();
    saveMotionSpeed(s, 'fast');
    expect(JSON.parse(s.getItem(MOTION_KEY)!)).toEqual({ version: 1, speed: 'fast' });
    expect(loadMotionSpeed(s)).toBe('fast');
  });

  it('a garbage or foreign blob loads as the default', () => {
    const s = memStorage();
    for (const raw of ['{', '{"version":2,"speed":"off"}', '{"version":1,"speed":"warp"}', 'null']) {
      s.setItem(MOTION_KEY, raw);
      expect(loadMotionSpeed(s)).toBe('normal');
    }
  });

  it('reduced motion forces off', () => {
    expect(effectiveSpeed('normal', true)).toBe('off');
    expect(effectiveSpeed('fast', false)).toBe('fast');
  });
});
