import { describe, expect, it } from 'vitest';
import { KeymapStore } from './keymap.svelte';
import { defaultKeymap, KEYMAP_KEY } from './keymap';

const mem = (): Storage => {
  const m = new Map<string, string>();
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: () => {}, clear: () => {}, key: () => null, length: 0 } as unknown as Storage;
};

describe('KeymapStore', () => {
  it('loads defaults, persists an added binding, resets', () => {
    const st = mem();
    const s = new KeymapStore(st);
    expect(s.current).toEqual(defaultKeymap());
    s.add('resolve-all', { code: 'KeyR', ctrl: false, shift: false, alt: false });
    expect(new KeymapStore(st).current['resolve-all']).toEqual([{ code: 'KeyR', ctrl: false, shift: false, alt: false }]);
    s.reset();
    expect(JSON.parse(st.getItem(KEYMAP_KEY)!).overrides).toEqual({});
  });
});
