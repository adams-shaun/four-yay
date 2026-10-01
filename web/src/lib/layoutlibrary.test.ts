import { describe, expect, it } from 'vitest';
import {
  addProfile,
  applyAt,
  applyNamed,
  applyPreset,
  currentIndex,
  cycled,
  cycleList,
  labelOf,
  LAYOUTS_KEY,
  loadLibrary,
  removeNamed,
  renameNamed,
  saveAs,
  saveLibrary,
  withCurrent,
} from './layoutlibrary';
import { defaultProfile, HAND_SCALE_DEFAULT, HAND_SCALE_MAX, HAND_SCALE_MIN, LEGACY_LAYOUT_KEY, PRESET_IDS, validate } from './layoutprofile';

function memStorage(init: Record<string, string> = {}): Storage {
  const m = new Map(Object.entries(init));
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

const LEGACY =
  '{"version":1,"scale":{"creatures":1,"others":1,"lands":1,"command":1,"hand":1.4},"align":{"creatures":"right","others":"left","lands":"center","command":"left","hand":"center"},"handPeek":"hover","steppersOnBoard":false}';

describe('layout library storage', () => {
  it('validates optional hand scale compatibly and rejects values outside the declared bounds', () => {
    const profile = defaultProfile();
    expect(HAND_SCALE_MIN).toBe(0.6);
    expect(HAND_SCALE_MAX).toBe(1.6);
    expect(HAND_SCALE_DEFAULT).toBe(1);
    const old = JSON.parse(JSON.stringify(profile));
    delete old.hand.scale;
    expect(validate(old)?.hand.scale).toBe(HAND_SCALE_DEFAULT);
    expect(validate({ ...profile, hand: { ...profile.hand, scale: HAND_SCALE_MIN } })?.hand.scale).toBe(HAND_SCALE_MIN);
    expect(validate({ ...profile, hand: { ...profile.hand, scale: HAND_SCALE_MAX } })?.hand.scale).toBe(HAND_SCALE_MAX);
    expect(validate({ ...profile, hand: { ...profile.hand, scale: HAND_SCALE_MIN - 0.01 } })).toBeNull();
    expect(validate({ ...profile, hand: { ...profile.hand, scale: HAND_SCALE_MAX + 0.01 } })).toBeNull();
  });
  it('an empty browser gets the default working copy and no saves', () => {
    const lib = loadLibrary(memStorage());
    expect(lib.current).toEqual(defaultProfile());
    expect(lib.order).toEqual([]);
    expect(labelOf(lib)).toBe('Duel');
  });

  it('migrates the legacy layout blob into the working copy once, leaving the legacy key untouched', () => {
    const s = memStorage({ [LEGACY_LAYOUT_KEY]: LEGACY });
    const lib = loadLibrary(s);
    expect(lib.current.regions.creatures.anchor).toBe('end');
    expect(lib.current.regions.lands.anchor).toBe('center');
    expect(lib.current.hand).toEqual({ visible: 0.5, scale: 1.4, raise: true });
    saveLibrary(s, lib);
    expect(s.getItem(LEGACY_LAYOUT_KEY)).toBe(LEGACY);
    // Once the library key exists the legacy blob is never read again.
    s.setItem(LEGACY_LAYOUT_KEY, LEGACY.replace('"right"', '"left"'));
    expect(loadLibrary(s).current.regions.creatures.anchor).toBe('end');
  });

  it('a corrupt library key or a throwing storage yields defaults', () => {
    expect(loadLibrary(memStorage({ [LAYOUTS_KEY]: '{nope' })).current).toEqual(defaultProfile());
    const throwing = { getItem: () => { throw new Error('private'); } } as unknown as Storage;
    expect(loadLibrary(throwing).current).toEqual(defaultProfile());
    expect(() => saveLibrary({ setItem: () => { throw new Error('quota'); } } as unknown as Storage, loadLibrary(null))).not.toThrow();
  });

  it('round-trips saves, order and the active name; a bad saved profile drops alone', () => {
    const s = memStorage();
    let lib = loadLibrary(s);
    lib = withCurrent(lib, { ...lib.current, cards: { ...lib.current.cards, overflow: 'scroll' }, hand: { ...lib.current.hand, scale: 1.35 } });
    lib = saveAs(lib, 'Mine');
    lib = saveAs(applyPreset(lib, 'grid6'), 'Pod');
    saveLibrary(s, lib);
    const raw = JSON.parse(s.getItem(LAYOUTS_KEY)!);
    raw.profiles.Broken = { version: 1 };
    raw.order.push('Broken');
    s.setItem(LAYOUTS_KEY, JSON.stringify(raw));
    const back = loadLibrary(s);
    expect(back.order).toEqual(['Mine', 'Pod']);
    expect(back.active).toBe('Pod');
    expect(back.profiles.Mine.cards.overflow).toBe('scroll');
    expect(back.profiles.Mine.hand.scale).toBe(1.35);
  });
});

describe('layout library operations', () => {
  it('refuses reserved and empty names', () => {
    const lib = loadLibrary(null);
    expect(saveAs(lib, '__proto__')).toBe(lib);
    expect(saveAs(lib, '   ')).toBe(lib);
    expect(saveAs(lib, 'x'.repeat(40))).toBe(lib);
  });

  it('apply, rename and delete keep order and the active name coherent', () => {
    let lib = saveAs(loadLibrary(null), 'A');
    lib = saveAs(applyPreset(lib, 'focus8'), 'B');
    lib = applyNamed(lib, 'A');
    expect(lib.current.table.arrangement).toBe('columns');
    expect(lib.active).toBe('A');
    lib = renameNamed(lib, 'A', 'Alpha');
    expect(lib.order).toEqual(['Alpha', 'B']);
    expect(lib.active).toBe('Alpha');
    expect(renameNamed(lib, 'Alpha', 'B')).toBe(lib);
    lib = removeNamed(lib, 'Alpha');
    expect(lib.order).toEqual(['B']);
    expect(lib.active).toBeNull();
  });

  it('addProfile (the import path) never switches the working copy or the active name', () => {
    const lib = saveAs(loadLibrary(null), 'Mine');
    const next = addProfile(lib, 'Imported', applyPreset(lib, 'grid6').current);
    expect(next.active).toBe('Mine');
    expect(next.current).toEqual(lib.current);
    expect(next.order).toEqual(['Mine', 'Imported']);
  });

  it('the pill names the save, the preset, an edited save, or Custom', () => {
    let lib = loadLibrary(null);
    expect(labelOf(lib)).toBe('Duel');
    lib = applyPreset(lib, 'cmd4');
    expect(labelOf(lib)).toBe('Commander 4');
    lib = withCurrent(lib, { ...lib.current, table: { ...lib.current.table, split: 0.3 } });
    expect(labelOf(lib)).toBe('Custom');
    lib = saveAs(lib, 'Tall');
    expect(labelOf(lib)).toBe('Tall');
    lib = withCurrent(lib, { ...lib.current, table: { ...lib.current.table, split: 0.33 } });
    expect(labelOf(lib)).toBe('Tall (edited)');
  });

  it('the 1-9 list is the presets then the saves, and next/prev wraps', () => {
    let lib = saveAs(applyPreset(loadLibrary(null), 'duel'), 'Mine');
    lib = withCurrent(lib, { ...lib.current, cards: { ...lib.current.cards, artBelow: 70 } });
    lib = saveAs(lib, 'Mine');
    const list = cycleList(lib);
    expect(list.map((e) => e.label)).toEqual(['Duel', 'Duel, 3 rows', 'Commander 4', '6 players, grid', '8 players, focus', 'Mine']);
    expect(currentIndex(lib)).toBe(PRESET_IDS.length);
    expect(currentIndex(cycled(lib, 1))).toBe(0);
    expect(currentIndex(cycled(lib, -1))).toBe(PRESET_IDS.length - 1);
    expect(applyAt(lib, 2)!.current.table.split).toBe(0.46);
    expect(applyAt(lib, 9)).toBeNull();
  });
});
