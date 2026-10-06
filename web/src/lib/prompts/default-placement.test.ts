import { describe, expect, it } from 'vitest';
import { LayoutStore } from '../layouts.svelte';
import { LAYOUTS_KEY, loadLibrary, saveLibrary, withCurrent } from '../layoutlibrary';
import { cloneProfile, defaultProfile, PROMPT_PLACEMENTS, type PromptPlacement } from '../layoutprofile';
import { dockFromProfile, effectivePlacement } from './dock';

function memStorage(): Storage {
  const m = new Map<string, string>();
  return {
    get length() { return m.size; },
    clear: () => m.clear(),
    getItem: (k: string) => (m.has(k) ? m.get(k)! : null),
    key: (i: number) => [...m.keys()][i] ?? null,
    removeItem: (k: string) => void m.delete(k),
    setItem: (k: string, v: string) => void m.set(k, v),
  };
}

describe('the shipped prompt placement is the lower rail slot', () => {
  it('a fresh profile, an empty library and a fresh store all choose dock-bottom, in the right rail', () => {
    expect(defaultProfile().panels.prompt.placement).toBe('dock-bottom');
    expect(defaultProfile().panels.rail).toBe('right');
    expect(loadLibrary(memStorage()).current.panels.prompt.placement).toBe('dock-bottom');
    expect(loadLibrary(null).current.panels.prompt.placement).toBe('dock-bottom');
    const store = new LayoutStore(memStorage());
    expect(store.prompt.placement).toBe('dock-bottom');
    // It resolves to the component's lower slot for an ordinary question.
    expect(dockFromProfile(store.prompt, { w: 1000, h: 700 }).placement).toBe('rail-bottom');
    expect(effectivePlacement(store.prompt, null)).toBe('rail-bottom');
  });

  it('explicit saved placements are kept, not migrated to the new default', () => {
    for (const placement of PROMPT_PLACEMENTS) {
      const storage = memStorage();
      const p = cloneProfile(defaultProfile());
      p.panels.prompt.placement = placement as PromptPlacement;
      saveLibrary(storage, withCurrent(loadLibrary(storage), p));
      expect(storage.getItem(LAYOUTS_KEY)).not.toBeNull();
      expect(loadLibrary(storage).current.panels.prompt.placement).toBe(placement);
      expect(new LayoutStore(storage).prompt.placement).toBe(placement);
    }
    // Precondition: the loop covered a value other than the new default.
    expect(PROMPT_PLACEMENTS.filter((x) => x !== defaultProfile().panels.prompt.placement).length).toBeGreaterThan(0);
  });
});
