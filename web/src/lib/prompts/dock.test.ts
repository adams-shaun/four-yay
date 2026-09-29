import { describe, expect, it } from 'vitest';
import { DOCK_KEY, clampPosition, defaultDockLayout, loadDockLayout, readDockLayout, saveDockLayout } from './dock';

function memory(): Storage {
  const m = new Map<string, string>();
  return {
    get length() { return m.size; },
    clear: () => m.clear(),
    getItem: (k) => m.get(k) ?? null,
    key: (i) => [...m.keys()][i] ?? null,
    removeItem: (k) => void m.delete(k),
    setItem: (k, v) => void m.set(k, v),
  };
}

describe('dock layout storage', () => {
  it('defaults to the rail dock with no saved position', () => {
    expect(loadDockLayout(null)).toEqual(defaultDockLayout());
    expect(defaultDockLayout()).toEqual({ placement: 'rail', position: null });
  });

  it('round-trips placement and position under its own key', () => {
    const s = memory();
    saveDockLayout(s, { placement: 'floating', position: { x: 120, y: 340 } });
    expect(JSON.parse(s.getItem(DOCK_KEY) ?? '{}')).toEqual({ version: 1, placement: 'floating', position: { x: 120, y: 340 } });
    expect(loadDockLayout(s)).toEqual({ placement: 'floating', position: { x: 120, y: 340 } });
  });

  it('refuses malformed blobs', () => {
    expect(readDockLayout({ version: 2, placement: 'rail' })).toBeNull();
    expect(readDockLayout({ version: 1, placement: 'sideways' })).toBeNull();
    expect(readDockLayout({ version: 1, placement: 'floating', position: { x: 'a', y: 1 } })).toBeNull();
    const s = memory();
    s.setItem(DOCK_KEY, '{not json');
    expect(loadDockLayout(s)).toEqual(defaultDockLayout());
  });
});

describe('clampPosition', () => {
  it('keeps the grip row reachable after the window shrinks', () => {
    expect(clampPosition({ x: 1800, y: 1200 }, { w: 400, h: 300 }, { w: 1280, h: 800 })).toEqual({ x: 1136, y: 752 });
    expect(clampPosition({ x: -50, y: -20 }, { w: 400, h: 300 }, { w: 1280, h: 800 })).toEqual({ x: 0, y: 0 });
  });
});
