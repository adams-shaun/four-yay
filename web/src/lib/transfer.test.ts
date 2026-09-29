import { describe, expect, it } from 'vitest';
import { exportFlow, exportKeymap, importFlow, importKeymap, uniqueName } from './transfer';
import { emptyStore, loadProfiles, saveProfiles, storeSave } from './profiles';
import { defaultSettings, PRESETS } from './playsettings';
import { defaultKeymap, withBinding } from './keymap';

describe('transfer', () => {
  it('flow round-trips in order', () => {
    let s = storeSave(emptyStore(), 'A', defaultSettings());
    s = storeSave(s, 'B', PRESETS['full-control']);
    const got = importFlow(exportFlow(s), emptyStore());
    expect('store' in got && got.store.order).toEqual(['A', 'B']);
  });

  it('a name clash gets " (2)", then " (3)"; the originals are untouched', () => {
    const mine = storeSave(emptyStore(), 'A', PRESETS['no-tells']);
    const file = exportFlow(storeSave(storeSave(emptyStore(), 'A', defaultSettings()), 'A (2)', defaultSettings()));
    const got = importFlow(file, mine);
    if (!('store' in got)) throw new Error(got.error);
    expect(got.store.order).toEqual(['A', 'A (2)', 'A (2) (2)']);
    expect(got.store.profiles.A).toEqual(mine.profiles.A);
  });

  it('an import never switches the active profile', () => {
    const mine = storeSave(emptyStore(), 'Mine', defaultSettings());
    const got = importFlow(exportFlow(storeSave(emptyStore(), 'Theirs', defaultSettings())), mine);
    if (!('store' in got)) throw new Error(got.error);
    expect(got.store.lastActive).toBe('Mine');
  });

  it('uniqueName respects the 24-char name limit', () => {
    const long = 'x'.repeat(24);
    expect(uniqueName(long, [long])).toBe(`${'x'.repeat(20)} (2)`);
  });

  it('wrong kind is an error; garbage is an error', () => {
    expect(importFlow(exportKeymap(defaultKeymap()), emptyStore())).toEqual({ error: 'This file holds keyboard shortcuts, not flow profiles.' });
    expect(importFlow('not json', emptyStore())).toEqual({ error: 'This file is not a gorge settings export.' });
    expect(importKeymap(exportFlow(emptyStore()))).toEqual({ error: 'This file holds flow profiles, not keyboard shortcuts.' });
  });

  it('bad entries are skipped and counted, good ones import', () => {
    const text = JSON.stringify({ kind: 'gorge-flow', version: 1, items: [{ name: 'Good', settings: defaultSettings() }, { name: 'Bad', settings: { version: 9 } }, { name: '', settings: defaultSettings() }] });
    const got = importFlow(text, emptyStore());
    if (!('store' in got)) throw new Error(got.error);
    expect(got.added).toEqual(['Good']);
    expect(got.skipped).toBe(2);
  });

  it('a "__proto__" profile in a file is skipped, and a save/reload stays clean', () => {
    const text = `{"kind":"gorge-flow","version":1,"items":[{"name":"__proto__","settings":${JSON.stringify(defaultSettings())}},{"name":"Good","settings":${JSON.stringify(defaultSettings())}}]}`;
    const got = importFlow(text, emptyStore());
    if (!('store' in got)) throw new Error(got.error);
    expect(got.added).toEqual(['Good']);
    expect(got.skipped).toBe(1);
    const mem = new Map<string, string>();
    const storage = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => void mem.set(k, v) } as unknown as Storage;
    saveProfiles(storage, got.store);
    const back = loadProfiles(storage);
    expect(back.order).toEqual(['Good']);
    expect([null, Object.prototype]).toContain(Object.getPrototypeOf(back.profiles));
    expect('steps' in back.profiles).toBe(false);
    expect(Object.hasOwn(back.profiles, '__proto__')).toBe(false);
  });

  it('keymap round-trips its overrides', () => {
    const k = withBinding(defaultKeymap(), 'resolve-all', { code: 'KeyR', ctrl: false, shift: false, alt: false });
    const got = importKeymap(exportKeymap(k));
    expect('keymap' in got && got.keymap).toEqual(k);
  });

  it('a keymap file whose items is not a plain object is an error, not a silent reset', () => {
    for (const items of [[], [{ code: 'KeyR' }], null, 'x', 3]) {
      expect(importKeymap(JSON.stringify({ kind: 'gorge-keymap', version: 1, items }))).toEqual({ error: 'This file is not a gorge settings export.' });
    }
  });

  it('an empty keymap file resets to the defaults and says so', () => {
    const got = importKeymap(JSON.stringify({ kind: 'gorge-keymap', version: 1, items: {} }));
    expect(got).toEqual({ keymap: defaultKeymap(), note: 'Keyboard shortcuts reset to defaults.' });
  });

  it('entries a keymap file could not carry are counted in the note', () => {
    const r = { code: 'KeyR', ctrl: false, shift: false, alt: false };
    const items = { 'resolve-all': [r], 'no-such-action': [r], 'undo': [{ code: 'CapsLock', ctrl: false, shift: false, alt: false }], 'pass': 'Space' };
    const got = importKeymap(JSON.stringify({ kind: 'gorge-keymap', version: 1, items }));
    expect('keymap' in got && got.keymap['resolve-all']).toEqual([r]);
    expect('keymap' in got && got.keymap['undo']).toEqual(defaultKeymap()['undo']);
    expect('note' in got && got.note).toBe('Keyboard shortcuts imported. 3 shortcuts could not be read.');
    const one = importKeymap(JSON.stringify({ kind: 'gorge-keymap', version: 1, items: { 'resolve-all': [r], 'nope': [r] } }));
    expect('note' in one && one.note).toBe('Keyboard shortcuts imported. 1 shortcut could not be read.');
    const clean = importKeymap(JSON.stringify({ kind: 'gorge-keymap', version: 1, items: { 'resolve-all': [r] } }));
    expect('note' in clean && clean.note).toBe('Keyboard shortcuts imported.');
  });
});

