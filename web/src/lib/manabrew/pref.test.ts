import { describe, expect, it } from 'vitest';
import { loadProtocol, PROTOCOL_KEY, saveProtocol } from './pref';

class Mem implements Storage {
  m = new Map<string, string>();
  get length() { return this.m.size; }
  clear() { this.m.clear(); }
  getItem(k: string) { return this.m.get(k) ?? null; }
  key(i: number) { return [...this.m.keys()][i] ?? null; }
  removeItem(k: string) { this.m.delete(k); }
  setItem(k: string, v: string) { this.m.set(k, v); }
}

describe('protocol setting', () => {
  it('defaults to native, round-trips manabrew, and ignores junk', () => {
    const s = new Mem();
    expect(loadProtocol(s)).toBe('native');
    saveProtocol('manabrew', s);
    expect(s.getItem(PROTOCOL_KEY)).toBe('manabrew');
    expect(loadProtocol(s)).toBe('manabrew');
    saveProtocol('native', s);
    expect(s.getItem(PROTOCOL_KEY)).toBeNull();
    s.setItem(PROTOCOL_KEY, 'bogus');
    expect(loadProtocol(s)).toBe('native');
    expect(loadProtocol(null)).toBe('native');
  });
});
