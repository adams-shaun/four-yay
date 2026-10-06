import { describe, expect, it } from 'vitest';
import { LayoutStore } from './layouts.svelte';
import { LAYOUTS_KEY } from './layoutlibrary';

function memStorage(): Storage {
  const m = new Map<string, string>();
  return {
    get length() {
      return m.size;
    },
    clear: () => m.clear(),
    getItem: (k: string) => (m.has(k) ? m.get(k)! : null),
    key: (i: number) => [...m.keys()][i] ?? null,
    removeItem: (k: string) => void m.delete(k),
    setItem: (k: string, v: string) => void m.set(k, v),
  };
}

describe('LayoutStore', () => {
  it('every edit persists the working copy, and a fresh store reads it back', () => {
    const s = memStorage();
    const a = new LayoutStore(s);
    a.setSplit(0.61234);
    a.toggleStacking();
    a.setRegion('lands', { row: 2 });
    a.setRegion('others', { row: 2 });
    expect(a.profile.table.split).toBe(0.61);
    expect(a.profile.regions.lands.row).toBe(1); // the gap is compacted
    const b = new LayoutStore(s);
    expect(b.profile.cards.stacking).toBe(false);
    expect(b.profile.table.split).toBe(0.61);
    expect(s.getItem(LAYOUTS_KEY)).not.toBeNull();
  });

  it('the hand card size writes through edit and survives a reload', () => {
    const s = memStorage();
    const a = new LayoutStore(s);
    expect(a.profile.hand.scale).toBe(1);
    a.edit((q) => (q.hand.scale = 1.35));
    expect(a.profile.hand.scale).toBe(1.35);
    const b = new LayoutStore(s);
    expect(b.profile.hand.scale).toBe(1.35);
  });

  it('the splitter clamps and resets by seat count', () => {
    const st = new LayoutStore(memStorage());
    st.setSplit(5);
    expect(st.profile.table.split).toBe(0.72);
    st.resetSplit(6);
    expect(st.profile.table.split).toBe(0.52);
  });

  it('applyAt is 1-based over presets then saves, and says when there is none', () => {
    const st = new LayoutStore(memStorage());
    expect(st.applyAt(5)).toBe(true);
    expect(st.label).toBe('8 players, focus');
    expect(st.applyAt(6)).toBe(false);
    st.saveAs('Mine');
    expect(st.applyAt(6)).toBe(true);
    expect(st.label).toBe('Mine');
    st.cycle(1);
    expect(st.label).toBe('Duel');
  });

  it('the floating prompt position is saved as a clamped viewport fraction', () => {
    const st = new LayoutStore(memStorage());
    st.setPromptPosition(1.4, -0.2);
    expect(st.prompt).toEqual({ placement: 'dock-bottom', x: 1, y: 0 });
  });
});
