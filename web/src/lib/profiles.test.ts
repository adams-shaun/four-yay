import { describe, expect, it } from 'vitest';
import {
  PROFILES_KEY,
  emptyStore,
  listProfiles,
  loadProfiles,
  normaliseName,
  saveProfiles,
  storeApply,
  storeDelete,
  storeRename,
  storeSave,
  storeSetActive,
  validateStore,
  type ProfileStore,
} from './profiles';
import { PRESETS, defaultSettings, validate, type PlaySettings } from './playsettings';

/**
 * memStorage is the same in-memory Storage stub playsettings.test.ts uses,
 * including the throwing mode (a private-mode browser). The profile store
 * must swallow a throw exactly like the settings store.
 */
function memStorage(throwing = false): Storage {
  const m = new Map<string, string>();
  return {
    get length() {
      return m.size;
    },
    clear() {
      m.clear();
    },
    getItem(k: string) {
      if (throwing) throw new Error('denied');
      return m.has(k) ? m.get(k)! : null;
    },
    key(i: number) {
      return [...m.keys()][i] ?? null;
    },
    removeItem(k: string) {
      if (throwing) throw new Error('denied');
      m.delete(k);
    },
    setItem(k: string, v: string) {
      if (throwing) throw new Error('denied');
      m.set(k, v);
    },
  };
}

/** edited returns a settings object that differs from casual in one field. */
function edited(autoPass = false): PlaySettings {
  return { ...defaultSettings(), autoPass, preset: 'custom' };
}

describe('profiles — name validation', () => {
  it('accepts a trimmed non-empty name and rejects empty/whitespace/too long', () => {
    expect(normaliseName('  Aggro  ')).toBe('Aggro');
    expect(normaliseName('')).toBeNull();
    expect(normaliseName('   ')).toBeNull();
    expect(normaliseName('x'.repeat(25))).toBeNull();
    expect(normaliseName('x'.repeat(24))).toBe('x'.repeat(24));
  });
});

describe('profiles — save / round-trip / load', () => {
  it('saves a profile and round-trips it through storage byte-for-byte', () => {
    const storage = memStorage();
    const s = edited();
    const store = storeSave(emptyStore(), 'Aggro', s);
    saveProfiles(storage, store);

    // PRECONDITION: the stored settings actually differ from the default, so
    // a round-trip that silently returned casual would be caught.
    expect(s.autoPass).not.toBe(defaultSettings().autoPass);
    expect(store.profiles.Aggro.autoPass).toBe(false);

    const loaded = loadProfiles(storage);
    expect(listProfiles(loaded)).toEqual(['Aggro']);
    expect(loaded.profiles.Aggro).toEqual(s);
    expect(loaded.lastActive).toBe('Aggro');
  });

  it('stores an independent clone: mutating the caller settings later does not change the saved copy', () => {
    const s = edited();
    const store = storeSave(emptyStore(), 'Clone', s);
    s.autoPass = true; // mutate the caller's object after saving
    expect(store.profiles.Clone.autoPass).toBe(false);
  });

  it('applying a profile returns an independent clone with the earned preset label', () => {
    const store = storeSave(emptyStore(), 'Aggro', edited());
    const a = storeApply(store, 'Aggro')!;
    const b = storeApply(store, 'Aggro')!;
    expect(a).toEqual(edited());
    expect(a).not.toBe(b);
    a.autoPass = true;
    expect(b.autoPass).toBe(false);
    // A settings object saved from casual earns 'casual' back through validate
    // round-trip (the label is the configuration's, not a stored choice).
    const casualStore = storeSave(emptyStore(), 'Base', defaultSettings());
    const loaded = loadProfiles(memStorage());
    expect(loaded).toEqual(emptyStore());
    expect(storeApply(casualStore, 'Base')!.preset).toBe('casual');
    expect(storeApply(store, 'missing')).toBeNull();
  });

  it('order pins list order independent of object-key iteration order', () => {
    let store = emptyStore();
    store = storeSave(store, 'Zeta', edited());
    store = storeSave(store, 'Alpha', edited());
    store = storeSave(store, 'Mid', edited());
    expect(listProfiles(store)).toEqual(['Zeta', 'Alpha', 'Mid']);
  });
});

describe('profiles — corrupt blob isolation', () => {
  it('a corrupt profile blob yields an empty list AND leaves gorge.playsettings.v1 untouched', () => {
    const storage = memStorage();
    // Seed the primary settings key with a known raw value.
    const primaryRaw = JSON.stringify(PRESETS.casual);
    storage.setItem('gorge.playsettings.v1', primaryRaw);
    // A corrupt profile blob (not JSON) plus a bogus version.
    storage.setItem(PROFILES_KEY, '{not json');
    const loaded = loadProfiles(storage);
    expect(listProfiles(loaded)).toEqual([]);
    expect(loaded.profiles).toEqual({});
    expect(loaded.lastActive).toBeNull();
    // The primary blob is byte-identical before and after the load.
    expect(storage.getItem('gorge.playsettings.v1')).toBe(primaryRaw);

    // A parseable-but-wrong-version blob is also empty, and still no touch.
    storage.setItem(PROFILES_KEY, JSON.stringify({ version: 2, profiles: {} }));
    expect(loadProfiles(storage)).toEqual(emptyStore());
    expect(storage.getItem('gorge.playsettings.v1')).toBe(primaryRaw);
  });

  it('a bad entry drops only that entry; good entries and the order survive', () => {
    const good = edited();
    const blob = {
      version: 1,
      profiles: { Good: good, Bad: { version: 2, preset: 'nonsense' } },
      order: ['Good', 'Bad', 'Ghost'],
      lastActive: 'Bad',
    };
    const store = validateStore(blob);
    expect(listProfiles(store)).toEqual(['Good']);
    expect(store.profiles.Good).toEqual(good);
    // lastActive pointing at a dropped entry resolves to null.
    expect(store.lastActive).toBeNull();
  });

  it('validateStore accepts a v1 settings entry through the shared validate (no re-implementation)', () => {
    // A v1 blob (no autoOrderAllTriggers) is exactly what playsettings'
    // validate migrates; the profile store must accept it too.
    const v1 = { ...edited(), version: 1 } as unknown as Record<string, unknown>;
    delete v1.autoOrderAllTriggers;
    const store = validateStore({ version: 1, profiles: { Old: v1 }, order: ['Old'], lastActive: null });
    expect(listProfiles(store)).toEqual(['Old']);
    expect(store.profiles.Old.version).toBe(2);
    expect(validate(v1)).toEqual(store.profiles.Old);
  });
});

describe('profiles — absent storage / throwing storage', () => {
  it('a null storage yields the empty store', () => {
    expect(loadProfiles(null)).toEqual(emptyStore());
  });

  it('a throwing storage never escapes (private mode) and keeps the in-memory copy', () => {
    const storage = memStorage(true);
    expect(loadProfiles(storage)).toEqual(emptyStore());
    expect(() => saveProfiles(storage, storeSave(emptyStore(), 'X', edited()))).not.toThrow();
  });
});

describe('profiles — delete / rename / active', () => {
  it('delete removes a profile and its order entry', () => {
    let store = emptyStore();
    store = storeSave(store, 'A', edited());
    store = storeSave(store, 'B', edited());
    store = storeDelete(store, 'A');
    expect(listProfiles(store)).toEqual(['B']);
    expect(store.profiles.A).toBeUndefined();
  });

  it('deleting the active profile clears lastActive', () => {
    const store = storeSetActive(storeSave(emptyStore(), 'A', edited()), 'A');
    expect(store.lastActive).toBe('A');
    expect(storeDelete(store, 'A').lastActive).toBeNull();
  });

  it('rename preserves order position and follows the active identity', () => {
    let store = emptyStore();
    store = storeSave(store, 'A', edited());
    store = storeSave(store, 'B', edited());
    store = storeSetActive(store, 'A');
    const re = storeRename(store, 'A', 'A2');
    expect(listProfiles(re)).toEqual(['A2', 'B']);
    expect(re.profiles.A2).toEqual(edited());
    expect(re.lastActive).toBe('A2');
  });

  it('rename is a no-op on a rejected name, an absent old name, or a collision', () => {
    let store = emptyStore();
    store = storeSave(store, 'A', edited());
    store = storeSave(store, 'B', edited());
    expect(storeRename(store, 'A', '')).toBe(store);
    expect(storeRename(store, 'A', 'B')).toBe(store);
    expect(storeRename(store, 'Missing', 'C')).toBe(store);
  });

  it('setActive ignores a name that is not a profile', () => {
    const store = storeSave(emptyStore(), 'A', edited());
    expect(storeSetActive(store, 'Ghost').lastActive).toBeNull();
    expect(storeSetActive(store, 'A').lastActive).toBe('A');
  });
});

describe('profiles — an active-profile settings edit is not auto-saved', () => {
  it('a store round-trip preserves the STORED value, not the edited one', () => {
    const storage = memStorage();
    const saved = edited();
    saveProfiles(storage, storeSave(emptyStore(), 'A', saved));
    const loaded = loadProfiles(storage);
    // Simulate an edit made to the live settings after applying the profile.
    const live = storeApply(loaded, 'A')!;
    live.autoPass = true;
    // The stored profile still holds the original value: only an explicit
    // Save rewrites it.
    expect(loadProfiles(storage).profiles.A.autoPass).toBe(false);
    expect(live.autoPass).toBe(true);
  });
});
